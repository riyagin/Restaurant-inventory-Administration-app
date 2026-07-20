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
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// AttendanceHandler serves the JWT (admin/manager) attendance endpoints.
type AttendanceHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	parser  service.FingerprintParser
}

func NewAttendanceHandler(pool *pgxpool.Pool, queries *db.Queries) *AttendanceHandler {
	return &AttendanceHandler{
		pool:    pool,
		queries: queries,
		parser: service.NewDATFingerprintParser(),
	}
}

// ── List with filters — GET /api/hr/attendance ───────────────────────────────
//
// Query params: date_from, date_to, branch_id, employee_id, status, source,
// anomaly_only (true/false). Returns records joined with employee name/code.
func (h *AttendanceHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	var args []any
	var conds []string

	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	if v := strings.TrimSpace(q.Get("date_from")); v != "" {
		add("ar.date >= $%d", v)
	}
	if v := strings.TrimSpace(q.Get("date_to")); v != "" {
		add("ar.date <= $%d", v)
	} else if strings.TrimSpace(q.Get("date")) != "" {
		add("ar.date = $%d", strings.TrimSpace(q.Get("date")))
	}
	if v := strings.TrimSpace(q.Get("branch_id")); v != "" {
		add("e.branch_id = $%d::uuid", v)
	}
	if v := strings.TrimSpace(q.Get("employee_id")); v != "" {
		add("ar.employee_id = $%d::uuid", v)
	}
	if v := strings.TrimSpace(q.Get("status")); v != "" && v != "all" {
		add("ar.status = $%d", v)
	}
	if v := strings.TrimSpace(q.Get("source")); v != "" && v != "all" {
		add("(ar.check_in_source = $%d OR ar.check_out_source = $%d)", v)
		// add() only handled one placeholder; rewrite the last cond for two refs.
		conds[len(conds)-1] = fmt.Sprintf("(ar.check_in_source = $%d OR ar.check_out_source = $%d)", len(args), len(args))
	}
	if v := strings.TrimSpace(q.Get("search")); v != "" {
		add("(lower(e.full_name) LIKE $%d OR lower(e.employee_code) LIKE $%d)", "%"+strings.ToLower(v)+"%")
		conds[len(conds)-1] = fmt.Sprintf("(lower(e.full_name) LIKE $%d OR lower(e.employee_code) LIKE $%d)", len(args), len(args))
	}
	if strings.EqualFold(q.Get("anomaly_only"), "true") {
		conds = append(conds, "(ar.is_late OR ar.is_early_leave OR ar.is_missing_checkout)")
	}
	if strings.EqualFold(q.Get("half_day_only"), "true") {
		conds = append(conds, "ar.is_half_day")
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	sql := fmt.Sprintf(`
		SELECT
		    ar.id, ar.employee_id, ar.date, ar.check_in, ar.check_out,
		    ar.check_in_source, ar.check_out_source, ar.check_in_photo_path,
		    ar.device_id, ar.status, ar.is_late, ar.late_minutes,
		    ar.is_early_leave, ar.early_leave_minutes, ar.is_missing_checkout, ar.note,
		    ar.is_half_day, ar.half_day_lost_minutes,
		    e.full_name, e.employee_code, e.branch_id, b.name AS branch_name,
		    COALESCE(d.branch_id <> e.branch_id, false) AS is_remote,
		    db.name AS clock_in_branch_name
		FROM attendance_records ar
		JOIN employees e ON e.id = ar.employee_id
		JOIN branches  b ON b.id = e.branch_id
		LEFT JOIN attendance_devices d  ON d.id = ar.device_id
		LEFT JOIN branches          db ON db.id = d.branch_id
		%s
		ORDER BY ar.date DESC, e.full_name
		LIMIT 1000`, where)

	rows, err := h.pool.Query(ctx, sql, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kehadiran")
		return
	}
	defer rows.Close()

	type recordRow struct {
		ID                pgtype.UUID        `json:"id"`
		EmployeeID        pgtype.UUID        `json:"employee_id"`
		Date              pgtype.Date        `json:"date"`
		CheckIn           pgtype.Timestamptz `json:"check_in"`
		CheckOut          pgtype.Timestamptz `json:"check_out"`
		CheckInSource     pgtype.Text        `json:"check_in_source"`
		CheckOutSource    pgtype.Text        `json:"check_out_source"`
		CheckInPhotoPath  pgtype.Text        `json:"check_in_photo_path"`
		DeviceID          pgtype.UUID        `json:"device_id"`
		Status            string             `json:"status"`
		IsLate            bool               `json:"is_late"`
		LateMinutes       int32              `json:"late_minutes"`
		IsEarlyLeave      bool               `json:"is_early_leave"`
		EarlyLeaveMinutes int32              `json:"early_leave_minutes"`
		IsMissingCheckout  bool               `json:"is_missing_checkout"`
		Note               pgtype.Text        `json:"note"`
		IsHalfDay          bool               `json:"is_half_day"`
		HalfDayLostMinutes int32              `json:"half_day_lost_minutes"`
		FullName           string             `json:"full_name"`
		EmployeeCode      string             `json:"employee_code"`
		BranchID          pgtype.UUID        `json:"branch_id"`
		BranchName        string             `json:"branch_name"`
		// is_remote: the recording device's branch differs from the employee's
		// home branch (a cross-branch / visitor check-in). clock_in_branch_name
		// is where they actually clocked in (null for non-device records).
		IsRemote          bool               `json:"is_remote"`
		ClockInBranchName pgtype.Text        `json:"clock_in_branch_name"`
	}

	items := []recordRow{}
	for rows.Next() {
		var x recordRow
		if err := rows.Scan(
			&x.ID, &x.EmployeeID, &x.Date, &x.CheckIn, &x.CheckOut,
			&x.CheckInSource, &x.CheckOutSource, &x.CheckInPhotoPath,
			&x.DeviceID, &x.Status, &x.IsLate, &x.LateMinutes,
			&x.IsEarlyLeave, &x.EarlyLeaveMinutes, &x.IsMissingCheckout, &x.Note,
			&x.IsHalfDay, &x.HalfDayLostMinutes,
			&x.FullName, &x.EmployeeCode, &x.BranchID, &x.BranchName,
			&x.IsRemote, &x.ClockInBranchName,
		); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data kehadiran")
			return
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data kehadiran")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

// ── Manual correction — PUT /api/hr/attendance/:id ───────────────────────────
//
// Body: { check_in?, check_out?, status?, note (REQUIRED) }. Source becomes
// 'manual', anomalies are recomputed, and the change is logged.
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
	sched := h.scheduleForEmployee(ctx, existing.EmployeeID)
	service.ComputeAnomalies(state, sched, service.DayIsOver(existing.Date.Time, sched, time.Now()))

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

	// Re-run performance evaluation for this record: drop its stale auto
	// violations, re-evaluate against current policies, and recompute the
	// employee's monthly score. Runs in the same transaction as the correction.
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

// ── Half-day correction — POST /api/hr/attendance/:id/half-day ───────────────
//
// Reclassifies a day where the employee arrived past the maximum late threshold
// as a "half day": the person started work from a certain hour. The wage is later
// reduced by the lost working hours (scheduled start → entry) and the performance
// score is deducted by the 'half_day' policy instead of the normal late rule.
// Manual manager action, no approval. Body: { start_time (RFC3339, optional —
// defaults to the current check-in), note (REQUIRED) }.
func (h *AttendanceHandler) SetHalfDay(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	var body struct {
		StartTime *string `json:"start_time"` // RFC3339; the corrected entry time
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

	state := service.StateFromRecord(existing)

	// Resolve the corrected entry time: an explicit start_time overrides the
	// current check-in; otherwise the existing check-in is used as-is.
	if body.StartTime != nil {
		v := strings.TrimSpace(*body.StartTime)
		if v == "" {
			respondError(w, http.StatusBadRequest, "waktu mulai tidak boleh kosong")
			return
		}
		t, perr := time.Parse(time.RFC3339, v)
		if perr != nil {
			respondError(w, http.StatusBadRequest, "format waktu mulai tidak valid (RFC3339)")
			return
		}
		state.CheckIn = &t
		state.CheckInSource = "manual"
	}
	if state.CheckIn == nil {
		respondError(w, http.StatusBadRequest, "waktu mulai wajib diisi karena kehadiran belum memiliki jam masuk")
		return
	}

	// A half day is a present day with a partial start.
	state.Status = "present"
	state.IsHalfDay = true

	sched := h.scheduleForEmployee(ctx, existing.EmployeeID)
	state.HalfDayLostMinutes = service.ComputeLostMinutes(state.CheckIn, sched)
	service.ComputeAnomalies(state, sched, service.DayIsOver(existing.Date.Time, sched, time.Now()))

	updated, err := h.persistCorrection(ctx, existing, state, body.Note,
		fmt.Sprintf("Koreksi setengah hari (%d menit hilang): %s", state.HalfDayLostMinutes, body.Note))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan koreksi setengah hari")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// ── Clear half-day — DELETE /api/hr/attendance/:id/half-day ───────────────────
// Reverts a half-day correction: clears the flag and lost minutes and re-evaluates
// performance (the normal late/early rules apply again). The check-in is left as-is.
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

	state := service.StateFromRecord(existing)
	state.IsHalfDay = false
	state.HalfDayLostMinutes = 0

	sched := h.scheduleForEmployee(ctx, existing.EmployeeID)
	service.ComputeAnomalies(state, sched, service.DayIsOver(existing.Date.Time, sched, time.Now()))

	note := textOrEmpty(existing.Note)
	updated, err := h.persistCorrection(ctx, existing, state, note, "Membatalkan koreksi setengah hari")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan koreksi setengah hari")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// persistCorrection writes a corrected attendance state, re-runs performance
// evaluation for the record and logs the change — the shared tail of the manual
// half-day set/clear handlers. `note` is stored on the record; `logDesc` is the
// activity-log description.
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

// scheduleForEmployee resolves the branch schedule for an employee, falling back
// to the default schedule.
func (h *AttendanceHandler) scheduleForEmployee(ctx context.Context, employeeID pgtype.UUID) service.Schedule {
	var branchID pgtype.UUID
	row := h.pool.QueryRow(ctx, `SELECT branch_id FROM employees WHERE id = $1`, employeeID)
	if err := row.Scan(&branchID); err != nil || !branchID.Valid {
		return service.DefaultSchedule()
	}
	ws, err := h.queries.GetWorkScheduleByBranch(ctx, branchID)
	if err != nil || ws == nil {
		return service.DefaultSchedule()
	}
	return service.ScheduleFromRow(ws)
}

// ── Reconcile — POST /api/hr/attendance/reconcile?date=YYYY-MM-DD ─────────────
func (h *AttendanceHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dateStr := strings.TrimSpace(r.URL.Query().Get("date"))
	var date time.Time
	if dateStr == "" {
		date = time.Now().AddDate(0, 0, -1) // default: previous day
	} else {
		d, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid (YYYY-MM-DD)")
			return
		}
		date = d
	}

	res, err := service.ReconcileAbsent(ctx, h.queries, date)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menjalankan rekonsiliasi")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "hr_attendance",
		Description: fmt.Sprintf("Rekonsiliasi absen %s: %d ditandai absen", res.Date, res.AbsentCreated),
	})

	respondJSON(w, http.StatusOK, res)
}
