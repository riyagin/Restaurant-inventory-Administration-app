package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// AttendanceDeviceHandler serves the device-key-authenticated endpoints the
// Android face app talks to. These routes live OUTSIDE the JWT group.
type AttendanceDeviceHandler struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	uploadsDir string
}

func NewAttendanceDeviceHandler(pool *pgxpool.Pool, queries *db.Queries) *AttendanceDeviceHandler {
	return &AttendanceDeviceHandler{pool: pool, queries: queries}
}

func (h *AttendanceDeviceHandler) SetUploadsDir(dir string) { h.uploadsDir = dir }

func (h *AttendanceDeviceHandler) resolveUploadsDir() string {
	if h.uploadsDir != "" {
		return h.uploadsDir
	}
	return filepath.Join("..", "server", "uploads")
}

// scheduleForBranch loads the branch schedule or falls back to the default.
func (h *AttendanceDeviceHandler) scheduleForBranch(r *http.Request, branchID pgtype.UUID) service.Schedule {
	if !branchID.Valid {
		return service.DefaultSchedule()
	}
	ws, err := h.queries.GetWorkScheduleByBranch(r.Context(), branchID)
	if err != nil || ws == nil {
		return service.DefaultSchedule()
	}
	return service.ScheduleFromRow(ws)
}

// Event — POST /api/hr/attendance/device/event
//
// Request (multipart/form-data OR application/x-www-form-urlencoded):
//
//	employee_code  string  (required)
//	event_type     string  check_in | check_out | auto   (required)
//	timestamp      string  RFC3339, e.g. 2026-06-09T07:58:00+07:00 (required)
//	photo          file    optional (face check-in evidence)
//
// Response 200:
//
//	{
//	  "greeting":   "Selamat pagi, Budi Santoso",
//	  "status":     "present",
//	  "event_type": "check_in",
//	  "check_in":   "2026-06-09T07:58:00+07:00" | null,
//	  "check_out":  "..." | null,
//	  "is_late":    false,
//	  "late_minutes": 0,
//	  "full_name":  "Budi Santoso"
//	}
func (h *AttendanceDeviceHandler) Event(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		// Fall back to plain form parsing (no file part).
		if err := r.ParseForm(); err != nil {
			respondError(w, http.StatusBadRequest, "gagal membaca permintaan")
			return
		}
	}

	employeeCode := strings.TrimSpace(r.FormValue("employee_code"))
	eventType := strings.TrimSpace(r.FormValue("event_type"))
	rawTimestamp := strings.TrimSpace(r.FormValue("timestamp"))

	if employeeCode == "" {
		respondError(w, http.StatusBadRequest, "employee_code wajib diisi")
		return
	}
	switch eventType {
	case "check_in", "check_out", "auto":
	default:
		respondError(w, http.StatusBadRequest, "event_type harus check_in, check_out, atau auto")
		return
	}
	if rawTimestamp == "" {
		respondError(w, http.StatusBadRequest, "timestamp wajib diisi (format RFC3339)")
		return
	}
	ts, err := time.Parse(time.RFC3339, rawTimestamp)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format timestamp tidak valid (gunakan RFC3339)")
		return
	}

	ctx := r.Context()
	dev, _ := middleware.DeviceFromCtx(ctx)

	emp, err := h.queries.GetEmployeeByCode(ctx, employeeCode)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan dengan kode tersebut tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	// Record date is the calendar date of the event (in its own offset).
	dateOnly := time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, time.UTC)
	pgDate := pgtype.Date{Time: dateOnly, Valid: true}

	sched := h.scheduleForBranch(r, emp.BranchID)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	existing, err := qtx.GetAttendanceRecordByEmployeeDate(ctx, &db.GetAttendanceRecordByEmployeeDateParams{
		EmployeeID: emp.ID,
		Date:       pgDate,
	})
	hasExisting := err == nil

	var state *service.AttendanceState
	if hasExisting {
		state = service.StateFromRecord(existing)
	} else {
		state = service.EmptyState()
	}

	// Resolve auto direction: if no check_in yet => check_in, else check_out.
	direction := eventType
	if direction == "auto" {
		if state.CheckIn == nil {
			direction = "check_in"
		} else {
			direction = "check_out"
		}
	}

	// Optional photo (only meaningful for a check-in / face evidence).
	photoPath := ""
	if file, header, ferr := r.FormFile("photo"); ferr == nil {
		defer file.Close()
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" {
			fname := fmt.Sprintf("attendance-%s-%d%s", emp.ID.Bytes, time.Now().UnixNano(), ext)
			uploadsDir := h.resolveUploadsDir()
			if mkErr := os.MkdirAll(uploadsDir, 0755); mkErr == nil {
				if dst, cerr := os.Create(filepath.Join(uploadsDir, fname)); cerr == nil {
					if _, werr := io.Copy(dst, file); werr == nil {
						photoPath = fname
					}
					dst.Close()
				}
			}
		}
	}

	// Merge the event (face primary, fingerprint fills, 5-min dedup).
	service.MergeAttendanceEvent(state, service.AttendanceEvent{
		Timestamp: ts,
		Source:    "face", // device events are face check-ins
		Direction: direction,
	})

	dayOver := service.DayIsOver(dateOnly, sched, time.Now())
	service.ComputeAnomalies(state, sched, dayOver)

	deviceID := pgtype.UUID{}
	if dev != nil {
		deviceID = dev.DeviceID
	}

	if hasExisting {
		params := db.UpdateAttendanceRecordParams{
			DeviceID: deviceID,
			Note:     existing.Note,
			ID:       existing.ID,
		}
		// Preserve any prior photo unless we just captured one.
		if photoPath != "" {
			params.CheckInPhotoPath = pgtype.Text{String: photoPath, Valid: true}
		} else {
			params.CheckInPhotoPath = existing.CheckInPhotoPath
		}
		service.FillUpdateParams(&params, state)
		if _, err := qtx.UpdateAttendanceRecord(ctx, &params); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
			return
		}
	} else {
		params := db.InsertAttendanceRecordParams{
			EmployeeID: emp.ID,
			Date:       pgDate,
			DeviceID:   deviceID,
		}
		if photoPath != "" {
			params.CheckInPhotoPath = pgtype.Text{String: photoPath, Valid: true}
		}
		service.FillInsertParams(&params, state)
		if _, err := qtx.InsertAttendanceRecord(ctx, &params); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"greeting":     greeting(ts, emp.FullName),
		"full_name":    emp.FullName,
		"status":       state.Status,
		"event_type":   direction,
		"check_in":     tsString(state.CheckIn),
		"check_out":    tsString(state.CheckOut),
		"is_late":      state.IsLate,
		"late_minutes": state.LateMinutes,
	})
}

// Employees — GET /api/hr/attendance/device/employees
//
// Returns the active roster for the device's branch so the app can sync faces.
// Response: [{ "employee_code": "...", "full_name": "...", "photo_path": "..." }]
func (h *AttendanceDeviceHandler) Employees(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dev, ok := middleware.DeviceFromCtx(ctx)
	if !ok || dev == nil {
		respondError(w, http.StatusUnauthorized, "perangkat tidak dikenal")
		return
	}
	if !dev.BranchID.Valid {
		respondError(w, http.StatusBadRequest, "perangkat belum terhubung ke cabang")
		return
	}

	roster, err := h.queries.ListDeviceRosterByBranch(ctx, dev.BranchID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar karyawan")
		return
	}
	if roster == nil {
		roster = []*db.ListDeviceRosterByBranchRow{}
	}
	respondJSON(w, http.StatusOK, roster)
}

func tsString(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}

// greeting returns an Indonesian time-of-day greeting with the employee name.
func greeting(ts time.Time, name string) string {
	hour := ts.Hour()
	var g string
	switch {
	case hour < 11:
		g = "Selamat pagi"
	case hour < 15:
		g = "Selamat siang"
	case hour < 18:
		g = "Selamat sore"
	default:
		g = "Selamat malam"
	}
	return fmt.Sprintf("%s, %s", g, name)
}
