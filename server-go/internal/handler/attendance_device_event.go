package handler

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

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
//	employee_code    string  (required)
//	event_type       string  check_in | check_out | auto   (required)
//	age_ms           int     ms since the event was captured, on the device's
//	                         monotonic clock. Present → the punch is timed by the
//	                         server (received − age). See service/device_punch.go.
//	timestamp        string  RFC3339 device wall clock. Only trusted (and flagged
//	                         unverified) when age_ms is absent; otherwise kept
//	                         for audit.
//	client_event_id  uuid    optional; makes retries idempotent
//	photo            file    optional (face check-in evidence)
//
// Response 200:
//
//	{
//	  "greeting":    "Selamat pagi, Budi Santoso",
//	  "status":      "present",
//	  "event_type":  "check_in",
//	  "timestamp":   "2026-06-09T07:58:00+07:00",  // the punch as the server recorded it
//	  "time_source": "server" | "queued" | "device",
//	  "time_verified": true,
//	  "server_time": "...",
//	  "check_in":    "2026-06-09T07:58:00+07:00" | null,
//	  "check_out":   "..." | null,
//	  "is_late":     false,
//	  "late_minutes": 0,
//	  "full_name":   "Budi Santoso"
//	}
func (h *AttendanceDeviceHandler) Event(w http.ResponseWriter, r *http.Request) {
	// Taken before the body is read: age_ms was measured when the app sent the
	// request, so the upload time must not be added to it.
	received := time.Now()

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
	var deviceTs *time.Time
	if rawTimestamp != "" {
		t, err := time.Parse(time.RFC3339, rawTimestamp)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format timestamp tidak valid (gunakan RFC3339)")
			return
		}
		deviceTs = &t
	}
	var ageMs *int64
	if raw := strings.TrimSpace(r.FormValue("age_ms")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "age_ms harus bilangan bulat")
			return
		}
		ageMs = &v
	}
	punchTime, err := service.ResolveDevicePunchTime(received, ageMs, deviceTs)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A malformed id is ignored rather than refused: it only costs idempotency.
	var clientEventID pgtype.UUID
	if raw := strings.TrimSpace(r.FormValue("client_event_id")); raw != "" {
		if id, perr := uuid.Parse(raw); perr == nil {
			clientEventID = pgtype.UUID{Bytes: id, Valid: true}
		}
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

	// The branch where the employee checks in governs the schedule: a visitor
	// called to another branch is judged by that branch's hours/work-days. For a
	// normal same-branch check-in the device branch equals the home branch, so
	// behaviour is unchanged.
	scheduleBranch := emp.BranchID
	if dev != nil && dev.BranchID.Valid {
		scheduleBranch = dev.BranchID
	}
	base := h.scheduleForBranch(r, scheduleBranch)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		log.Printf("attendance device event: begin tx failed: %v", err)
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	// Optional photo. It is NOT kept as attendance evidence: attendance records
	// only hold the times and the method. The photo is held in memory and, after
	// the record is saved, used to refresh the employee's face photo when this is
	// their first face check-in of the day. Otherwise it is discarded.
	var photo []byte
	if file, header, ferr := r.FormFile("photo"); ferr == nil {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" {
			b, rerr := io.ReadAll(io.LimitReader(file, maxFacePhotoBytes+1))
			if rerr != nil {
				log.Printf("attendance device event: read photo failed: %v", rerr)
			} else if len(b) > 0 && len(b) <= maxFacePhotoBytes {
				photo = b
			}
		}
		file.Close()
	}

	source := "face" // device events are face check-ins by default
	if isManual {
		source = "manual"
	}
	hint := "" // "auto": the day's punches decide (see service.ResolveDay)
	if eventType != "auto" {
		hint = eventType
	}
	deviceID := pgtype.UUID{}
	if dev != nil {
		deviceID = dev.DeviceID
	}

	// One employee's punches can arrive from several kiosks at once; serialise
	// them so each rebuild sees the others' punches (service/attendance_sync.go).
	// Taken before the client_event_id lookup, so two copies of one retried
	// event cannot both miss each other either.
	if err := service.LockEmployeeAttendance(ctx, tx, emp.ID); err != nil {
		log.Printf("attendance device event: lock (emp=%s) failed: %v", employeeCode, err)
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
		return
	}

	// Log the punch on the work day its shift window places it in (a check-out
	// at 00:20 after a late shift belongs to the previous day), then rebuild that
	// day: first punch = check-in, last = check-out, by the rostered shift.
	logged, err := service.LogDevicePunch(ctx, qtx, tx, emp.ID,
		service.Punch{At: punchTime.At, Source: source, Hint: hint}, punchTime, clientEventID, base, deviceID)
	if errors.Is(err, service.ErrClientEventConflict) {
		respondError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		log.Printf("attendance device event: LogDevicePunch(emp=%s at=%s) failed: %v", employeeCode, punchTime.At, err)
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
		return
	}
	ts, workDate, sched := logged.At, logged.WorkDate, logged.Schedule

	// Audit note for manager-assisted manual entries.
	manualNote := ""
	if isManual {
		manualNote = fmt.Sprintf("Absen manual (visitor) oleh %s", recordedBy)
	}
	state, rec, err := service.RebuildDay(ctx, qtx, emp.ID, workDate, sched, service.RebuildOptions{DeviceID: deviceID, Note: manualNote})
	if err != nil || state == nil {
		log.Printf("attendance device event: RebuildDay(emp=%s date=%s) failed: %v", employeeCode, workDate.Format("2006-01-02"), err)
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
		return
	}
	// A punch for a day the nightly job already judged (an offline kiosk
	// flushing late) must lift the Alpha penalty that day was given.
	if err := service.ReevaluateFinishedDay(ctx, qtx, rec); err != nil {
		log.Printf("attendance device event: re-evaluate (emp=%s date=%s) failed: %v", employeeCode, workDate.Format("2006-01-02"), err)
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
		return
	}

	// Which slot this punch ended up in, for the app's message.
	direction := "check_in"
	switch {
	case state.CheckOut != nil && state.CheckOut.Equal(ts):
		direction = "check_out"
	case state.CheckIn != nil && state.CheckIn.Equal(ts):
		direction = "check_in"
	case state.CheckIn != nil:
		direction = "check_out"
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("attendance device event: commit failed (emp=%s date=%s): %v", employeeCode, rawTimestamp, err)
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kehadiran")
		return
	}

	// The first face check-in of the day refreshes the employee's face photo,
	// which HR sees on the profile. Best effort: the attendance is already saved.
	// The photo's day is taken from the time's location, so pass it on WIB.
	if !isManual && !logged.Duplicate && len(photo) > 0 {
		if _, perr := service.RecordDailyCheckinPhoto(ctx, h.pool, h.resolveUploadsDir(), emp.ID, photo, service.InWIB(ts)); perr != nil {
			log.Printf("attendance device event: daily face photo (emp=%s) failed: %v", employeeCode, perr)
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		// success/message mirror what the Android app parses (AttendanceEventResponse).
		// direction ("check_in"/"check_out") is echoed as message so the app can show
		// the resolved direction; the richer fields below are used by the web UI.
		"success":    true,
		"message":    direction,
		"greeting":   greeting(ts, emp.FullName),
		"full_name":  emp.FullName,
		"status":     state.Status,
		"event_type": direction,
		// The punch as the server recorded it; the app shows this, not its own clock.
		"timestamp":     service.InWIB(ts).Format(time.RFC3339),
		"time_source":   logged.Source,
		"time_verified": logged.Source != service.PunchTimeDevice,
		"server_time":   service.InWIB(received).Format(time.RFC3339),
		"duplicate":     logged.Duplicate,
		"check_in":      tsString(state.CheckIn),
		"check_out":     tsString(state.CheckOut),
		"is_late":       state.IsLate,
		"late_minutes":  state.LateMinutes,
	})
}

// greeting returns an Indonesian time-of-day greeting with the employee name.
func greeting(ts time.Time, name string) string {
	hour := service.InWIB(ts).Hour()
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
