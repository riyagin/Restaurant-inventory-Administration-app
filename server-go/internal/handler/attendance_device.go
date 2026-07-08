package handler

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
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
			log.Printf("attendance device event: parse form failed: %v", err)
			respondError(w, http.StatusBadRequest, "gagal membaca permintaan")
			return
		}
	}

	employeeCode := strings.TrimSpace(r.FormValue("employee_code"))
	eventType := strings.TrimSpace(r.FormValue("event_type"))
	rawTimestamp := strings.TrimSpace(r.FormValue("timestamp"))
	// recorded_by is set only for manager-assisted manual entries (visitor help):
	// it carries the operator's username for the audit trail. Its presence marks
	// the event as a manual entry (source "manual") rather than a face check-in.
	recordedBy := strings.TrimSpace(r.FormValue("recorded_by"))
	isManual := recordedBy != ""

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
		log.Printf("attendance device event: GetEmployeeByCode(%q) failed: %v", employeeCode, err)
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	// Record date is the calendar date of the event (in its own offset).
	dateOnly := time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, time.UTC)
	pgDate := pgtype.Date{Time: dateOnly, Valid: true}

	// The branch where the employee checks in governs the schedule: a visitor
	// called to another branch is judged by that branch's hours/work-days. For a
	// normal same-branch check-in the device branch equals the home branch, so
	// behaviour is unchanged.
	scheduleBranch := emp.BranchID
	if dev != nil && dev.BranchID.Valid {
		scheduleBranch = dev.BranchID
	}
	sched := h.scheduleForBranch(r, scheduleBranch)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		log.Printf("attendance device event: begin tx failed: %v", err)
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
			// emp.ID.Bytes is a raw [16]byte; format it as a proper UUID string so
			// the filename is valid (a bare %s on the byte array yields control
			// characters that fail os.Create, silently losing the photo).
			empID := uuid.UUID(emp.ID.Bytes).String()
			fname := fmt.Sprintf("attendance-%s-%d%s", empID, time.Now().UnixNano(), ext)
			uploadsDir := h.resolveUploadsDir()
			if mkErr := os.MkdirAll(uploadsDir, 0755); mkErr != nil {
				log.Printf("attendance device event: mkdir uploads %q failed: %v", uploadsDir, mkErr)
			} else if dst, cerr := os.Create(filepath.Join(uploadsDir, fname)); cerr != nil {
				log.Printf("attendance device event: create photo %q failed: %v", fname, cerr)
			} else {
				if _, werr := io.Copy(dst, file); werr == nil {
					photoPath = fname
				} else {
					log.Printf("attendance device event: write photo %q failed: %v", fname, werr)
				}
				dst.Close()
			}
		}
	}

	// Merge the event (face primary, fingerprint fills, 5-min dedup).
	source := "face" // device events are face check-ins by default
	if isManual {
		source = "manual"
	}
	service.MergeAttendanceEvent(state, service.AttendanceEvent{
		Timestamp: ts,
		Source:    source,
		Direction: direction,
	})

	// Audit note for manager-assisted manual entries.
	manualNote := ""
	if isManual {
		manualNote = fmt.Sprintf("Absen manual (visitor) oleh %s", recordedBy)
	}

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
		if isManual {
			params.Note = pgtype.Text{String: manualNote, Valid: true}
		}
		// Preserve any prior photo unless we just captured one.
		if photoPath != "" {
			params.CheckInPhotoPath = pgtype.Text{String: photoPath, Valid: true}
		} else {
			params.CheckInPhotoPath = existing.CheckInPhotoPath
		}
		service.FillUpdateParams(&params, state)
		if _, err := qtx.UpdateAttendanceRecord(ctx, &params); err != nil {
			log.Printf("attendance device event: UpdateAttendanceRecord(emp=%s date=%s) failed: %v", employeeCode, rawTimestamp, err)
			respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
			return
		}
	} else {
		params := db.InsertAttendanceRecordParams{
			EmployeeID: emp.ID,
			Date:       pgDate,
			DeviceID:   deviceID,
		}
		if isManual {
			params.Note = pgtype.Text{String: manualNote, Valid: true}
		}
		if photoPath != "" {
			params.CheckInPhotoPath = pgtype.Text{String: photoPath, Valid: true}
		}
		service.FillInsertParams(&params, state)
		if _, err := qtx.InsertAttendanceRecord(ctx, &params); err != nil {
			log.Printf("attendance device event: InsertAttendanceRecord(emp=%s date=%s) failed: %v", employeeCode, rawTimestamp, err)
			respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("attendance device event: commit failed (emp=%s date=%s): %v", employeeCode, rawTimestamp, err)
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		// success/message mirror what the Android app parses (AttendanceEventResponse).
		// direction ("check_in"/"check_out") is echoed as message so the app can show
		// the resolved direction; the richer fields below are used by the web UI.
		"success":      true,
		"message":      direction,
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
