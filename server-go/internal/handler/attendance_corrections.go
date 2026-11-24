package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Update — PUT /api/hr/attendance/:id (note required; source becomes manual)
func (h *AttendanceHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	var body struct {
		CheckIn  *string `json:"check_in"`  // RFC3339 or null to clear
		CheckOut *string `json:"check_out"` // RFC3339 or null to clear
		Status   string  `json:"status"`
		Note     string  `json:"note"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Note = strings.TrimSpace(body.Note)
	if body.Note == "" {
		respondError(w, http.StatusBadRequest, "catatan koreksi wajib diisi")
		return
	}

	ctx := r.Context()

	existing, err := h.queries.GetAttendanceRecordByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "data kehadiran tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kehadiran")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, existing.EmployeeID) {
		return
	}

	state := service.StateFromRecord(existing)

	// Apply manual edits. Presence of the key (non-nil pointer) means "set".
	if body.CheckIn != nil {
		if v := strings.TrimSpace(*body.CheckIn); v == "" {
			state.CheckIn = nil
			state.CheckInSource = ""
		} else {
			t, perr := time.Parse(time.RFC3339, v)
			if perr != nil {
				respondError(w, http.StatusBadRequest, "format check_in tidak valid (RFC3339)")
				return
			}
			state.CheckIn = &t
			state.CheckInSource = "manual"
		}
	}
	if body.CheckOut != nil {
		if v := strings.TrimSpace(*body.CheckOut); v == "" {
			state.CheckOut = nil
			state.CheckOutSource = ""
		} else {
			t, perr := time.Parse(time.RFC3339, v)
			if perr != nil {
				respondError(w, http.StatusBadRequest, "format check_out tidak valid (RFC3339)")
				return
			}
			state.CheckOut = &t
			state.CheckOutSource = "manual"
		}
	}
	if s := strings.TrimSpace(body.Status); s != "" {
		switch s {
		case "present", "absent", "leave", "holiday":
			state.Status = s
		default:
			respondError(w, http.StatusBadRequest, "status tidak valid")
			return
		}
	}

	// Recompute anomalies against the branch schedule.
	sched := h.scheduleForEmployee(ctx, existing.EmployeeID, existing.Date.Time)
	// Anomalies plus any approved permit for the day (Izin / Dinas Luar).
	if err := service.ComputeDay(ctx, h.queries, existing.EmployeeID, state, sched); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung ulang kehadiran")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	params := db.UpdateAttendanceRecordParams{
		CheckInPhotoPath: existing.CheckInPhotoPath,
		DeviceID:         existing.DeviceID,
		Note:             pgtype.Text{String: body.Note, Valid: true},
		ID:               pgID,
	}
	service.FillUpdateParams(&params, state)
	updated, err := qtx.UpdateAttendanceRecord(ctx, &params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi")
		return
	}

	if err := service.DeleteAutoViolationsForRecord(ctx, qtx, updated); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung ulang skor kinerja")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "hr_attendance",
		EntityID:    id,
		Description: fmt.Sprintf("Koreksi manual kehadiran: %s", body.Note),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi")
		return
	}

	respondJSON(w, http.StatusOK, updated)
}

// SetHalfDay — POST /api/hr/attendance/:id/half-day {type: late|early, start_time?, end_time?, note}
func (h *AttendanceHandler) SetHalfDay(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	var body struct {
		Type      string  `json:"type"`       // "late" | "early"
		StartTime *string `json:"start_time"` // RFC3339; corrected entry time (late)
		EndTime   *string `json:"end_time"`   // RFC3339; corrected exit time (early)
		Note      string  `json:"note"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Note = strings.TrimSpace(body.Note)
	if body.Note == "" {
		respondError(w, http.StatusBadRequest, "catatan koreksi wajib diisi")
		return
	}
	hdType := strings.TrimSpace(body.Type)
	if hdType == "" {
		hdType = "late"
	}
	if hdType != "late" && hdType != "early" {
		respondError(w, http.StatusBadRequest, "tipe setengah hari tidak valid (late/early)")
		return
	}

	ctx := r.Context()
	existing, err := h.queries.GetAttendanceRecordByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "data kehadiran tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kehadiran")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, existing.EmployeeID) {
		return
	}

	state := service.StateFromRecord(existing)
	sched := h.scheduleForEmployee(ctx, existing.EmployeeID, existing.Date.Time)

	if hdType == "late" {
		// Resolve the corrected entry time: an explicit start_time overrides the
		// current check-in; otherwise the existing check-in is used as-is.
		if body.StartTime != nil {
			t, ok := parseHalfDayTime(w, *body.StartTime, "waktu mulai")
			if !ok {
				return
			}
			state.CheckIn = &t
			state.CheckInSource = "manual"
		}
		if state.CheckIn == nil {
			respondError(w, http.StatusBadRequest, "waktu mulai wajib diisi karena kehadiran belum memiliki jam masuk")
			return
		}
		state.HalfDayLostMinutes = service.ComputeLostMinutes(state.CheckIn, sched)
	} else { // early
		if body.EndTime != nil {
			t, ok := parseHalfDayTime(w, *body.EndTime, "waktu pulang")
			if !ok {
				return
			}
			state.CheckOut = &t
			state.CheckOutSource = "manual"
		}
		if state.CheckOut == nil {
			respondError(w, http.StatusBadRequest, "waktu pulang wajib diisi karena kehadiran belum memiliki jam pulang")
			return
		}
		state.HalfDayLostMinutes = service.ComputeEarlyLeaveLostMinutes(state.CheckOut, sched)
	}

	// A half day is a present day with a partial start or end.
	state.Status = "present"
	state.IsHalfDay = true
	state.HalfDayType = hdType
	// Set by hand: no longer the effect of a permit (a rebuild must keep it).
	state.ExcusedBy = ""

	// Anomalies plus any approved permit for the day (Izin / Dinas Luar).
	if err := service.ComputeDay(ctx, h.queries, existing.EmployeeID, state, sched); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung ulang kehadiran")
		return
	}

	label := "datang siang"
	if hdType == "early" {
		label = "pulang awal"
	}
	updated, err := h.persistCorrection(ctx, existing, state, body.Note,
		fmt.Sprintf("Koreksi setengah hari (%s, %d menit hilang): %s", label, state.HalfDayLostMinutes, body.Note))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi setengah hari")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// parseHalfDayTime parses an RFC3339 time from a half-day request, writing the
// appropriate 400 on failure. `field` names the field for the error message.
func parseHalfDayTime(w http.ResponseWriter, raw, field string) (time.Time, bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		respondError(w, http.StatusBadRequest, field+" tidak boleh kosong")
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		respondError(w, http.StatusBadRequest, "format "+field+" tidak valid (RFC3339)")
		return time.Time{}, false
	}
	return t, true
}

// ClearHalfDay — DELETE /api/hr/attendance/:id/half-day
func (h *AttendanceHandler) ClearHalfDay(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	ctx := r.Context()
	existing, err := h.queries.GetAttendanceRecordByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "data kehadiran tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kehadiran")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, existing.EmployeeID) {
		return
	}

	state := service.StateFromRecord(existing)
	state.IsHalfDay = false
	state.HalfDayLostMinutes = 0
	state.HalfDayType = ""
	state.ExcusedBy = ""

	sched := h.scheduleForEmployee(ctx, existing.EmployeeID, existing.Date.Time)
	// Anomalies plus any approved permit for the day (Izin / Dinas Luar).
	if err := service.ComputeDay(ctx, h.queries, existing.EmployeeID, state, sched); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung ulang kehadiran")
		return
	}

	note := textOrEmpty(existing.Note)
	updated, err := h.persistCorrection(ctx, existing, state, note, "Membatalkan koreksi setengah hari")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan koreksi setengah hari")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// MarkPresentNoPunch — POST /api/hr/attendance/:id/present-no-punch {note}
func (h *AttendanceHandler) MarkPresentNoPunch(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	var body struct {
		Note string `json:"note"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Note = strings.TrimSpace(body.Note)
	if body.Note == "" {
		respondError(w, http.StatusBadRequest, "catatan koreksi wajib diisi")
		return
	}

	ctx := r.Context()
	existing, err := h.queries.GetAttendanceRecordByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "data kehadiran tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kehadiran")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, existing.EmployeeID) {
		return
	}

	// Present but with no punches. Clear any punches and any half-day classification
	// (a no-punch day is neither a late nor an early half day).
	state := service.StateFromRecord(existing)
	state.Status = "present"
	state.CheckIn = nil
	state.CheckInSource = ""
	state.CheckOut = nil
	state.CheckOutSource = ""
	state.IsHalfDay = false
	state.HalfDayType = ""
	state.HalfDayLostMinutes = 0

	sched := h.scheduleForEmployee(ctx, existing.EmployeeID, existing.Date.Time)
	// Anomalies plus any approved permit for the day (Izin / Dinas Luar).
	if err := service.ComputeDay(ctx, h.queries, existing.EmployeeID, state, sched); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung ulang kehadiran")
		return
	}

	updated, err := h.persistCorrection(ctx, existing, state, body.Note,
		fmt.Sprintf("Koreksi hadir tanpa absen: %s", body.Note))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi hadir tanpa absen")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// ── Clear present-no-punch — DELETE /api/hr/attendance/:id/present-no-punch ────
// Reverts a "present without scan" correction back to 'absent' and re-evaluates.
func (h *AttendanceHandler) ClearPresentNoPunch(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	ctx := r.Context()
	existing, err := h.queries.GetAttendanceRecordByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "data kehadiran tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kehadiran")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, existing.EmployeeID) {
		return
	}

	state := service.StateFromRecord(existing)
	state.Status = "absent"
	state.CheckIn = nil
	state.CheckInSource = ""
	state.CheckOut = nil
	state.CheckOutSource = ""

	sched := h.scheduleForEmployee(ctx, existing.EmployeeID, existing.Date.Time)
	// Anomalies plus any approved permit for the day (Izin / Dinas Luar).
	if err := service.ComputeDay(ctx, h.queries, existing.EmployeeID, state, sched); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung ulang kehadiran")
		return
	}

	note := textOrEmpty(existing.Note)
	updated, err := h.persistCorrection(ctx, existing, state, note, "Membatalkan koreksi hadir tanpa absen (ditandai absen)")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan koreksi")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

func (h *AttendanceHandler) persistCorrection(ctx context.Context, existing *db.AttendanceRecord, state *service.AttendanceState, note, logDesc string) (*db.AttendanceRecord, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	params := db.UpdateAttendanceRecordParams{
		CheckInPhotoPath: existing.CheckInPhotoPath,
		DeviceID:         existing.DeviceID,
		Note:             pgtype.Text{String: note, Valid: note != ""},
		ID:               existing.ID,
	}
	service.FillUpdateParams(&params, state)
	updated, err := qtx.UpdateAttendanceRecord(ctx, &params)
	if err != nil {
		return nil, err
	}

	if err := service.DeleteAutoViolationsForRecord(ctx, qtx, updated); err != nil {
		return nil, err
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "hr_attendance_correction",
		EntityID:    existing.ID.Bytes,
		Description: logDesc,
	})

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return updated, nil
}

// textOrEmpty returns the string value of a pgtype.Text or "" when null.
func textOrEmpty(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}
