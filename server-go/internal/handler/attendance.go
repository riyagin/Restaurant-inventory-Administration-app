package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
		parser:  service.NewDATFingerprintParser(),
	}
}

// List — GET /api/hr/attendance?date_from=&date_to=&branch_id=&employee_id=&status=&source=&anomaly_only=
const noShiftSQL = `(ar.status = 'present' AND NOT EXISTS (
		SELECT 1 FROM shift_assignments sa WHERE sa.employee_id = ar.employee_id AND sa.date = ar.date))`

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
	branchFilter := strings.TrimSpace(q.Get("branch_id"))
	if b, ok := scopedBranchString(r); ok {
		// A branch operator sees their own branch whatever the filter says.
		branchFilter = b
	}
	if branchFilter != "" {
		add("e.branch_id = $%d::uuid", branchFilter)
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
		conds = append(conds, "(ar.is_late OR ar.is_early_leave OR ar.is_missing_checkout OR ar.is_missing_checkin OR ar.is_leave_punch OR "+noShiftSQL+")")
	}
	if strings.EqualFold(q.Get("half_day_only"), "true") {
		conds = append(conds, "ar.is_half_day")
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	// Paginated only when `page` is sent; otherwise LIMIT 1000.
	paginate := strings.TrimSpace(q.Get("page")) != ""
	var total int64
	limit, offset := 1000, 0
	if paginate {
		page := 1
		if v, err := strconv.Atoi(strings.TrimSpace(q.Get("page"))); err == nil && v > 0 {
			page = v
		}
		pageSize := 50
		if v, err := strconv.Atoi(strings.TrimSpace(q.Get("page_size"))); err == nil && v > 0 {
			pageSize = v
		}
		if pageSize > 500 {
			pageSize = 500
		}
		limit, offset = pageSize, (page-1)*pageSize

		countSQL := fmt.Sprintf(`
			SELECT count(*)
			FROM attendance_records ar
			JOIN employees e ON e.id = ar.employee_id
			JOIN branches  b ON b.id = e.branch_id
			%s`, where)
		if err := h.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil data kehadiran")
			return
		}
	}

	sql := fmt.Sprintf(`
		SELECT
		    ar.id, ar.employee_id, ar.date, ar.check_in, ar.check_out,
		    ar.check_in_source, ar.check_out_source, ar.check_in_photo_path,
		    ar.device_id, ar.status, ar.is_late, ar.late_minutes,
		    ar.is_early_leave, ar.early_leave_minutes, ar.is_missing_checkout, ar.note,
		    ar.is_half_day, ar.half_day_lost_minutes, ar.half_day_type, ar.is_missing_checkin, ar.is_no_punch,
		    ar.is_leave_punch, ar.excused_by,
		    e.full_name, e.employee_code, e.branch_id, b.name AS branch_name,
		    COALESCE(d.branch_id <> e.branch_id, false) AS is_remote,
		    db.name AS clock_in_branch_name,
		    `+noShiftSQL+` AS no_shift,
		    (SELECT fp.photo_path FROM employee_face_photos fp WHERE fp.employee_id = e.id ORDER BY fp.captured_at DESC, fp.created_at DESC LIMIT 1) AS face_photo_path
		FROM attendance_records ar
		JOIN employees e ON e.id = ar.employee_id
		JOIN branches  b ON b.id = e.branch_id
		LEFT JOIN attendance_devices d  ON d.id = ar.device_id
		LEFT JOIN branches          db ON db.id = d.branch_id
		%s
		ORDER BY ar.date DESC, e.full_name
		LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)

	rows, err := h.pool.Query(ctx, sql, append(args, limit, offset)...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kehadiran")
		return
	}
	defer rows.Close()

	type recordRow struct {
		ID                 pgtype.UUID        `json:"id"`
		EmployeeID         pgtype.UUID        `json:"employee_id"`
		Date               pgtype.Date        `json:"date"`
		CheckIn            pgtype.Timestamptz `json:"check_in"`
		CheckOut           pgtype.Timestamptz `json:"check_out"`
		CheckInSource      pgtype.Text        `json:"check_in_source"`
		CheckOutSource     pgtype.Text        `json:"check_out_source"`
		CheckInPhotoPath   pgtype.Text        `json:"check_in_photo_path"`
		DeviceID           pgtype.UUID        `json:"device_id"`
		Status             string             `json:"status"`
		IsLate             bool               `json:"is_late"`
		LateMinutes        int32              `json:"late_minutes"`
		IsEarlyLeave       bool               `json:"is_early_leave"`
		EarlyLeaveMinutes  int32              `json:"early_leave_minutes"`
		IsMissingCheckout  bool               `json:"is_missing_checkout"`
		Note               pgtype.Text        `json:"note"`
		IsHalfDay          bool               `json:"is_half_day"`
		HalfDayLostMinutes int32              `json:"half_day_lost_minutes"`
		HalfDayType        pgtype.Text        `json:"half_day_type"`
		IsMissingCheckin   bool               `json:"is_missing_checkin"`
		IsNoPunch          bool               `json:"is_no_punch"`
		// is_leave_punch: punched on an approved leave day (status stays 'leave').
		IsLeavePunch bool `json:"is_leave_punch"`
		// excused_by: the approved permit that shaped the day (late_arrival |
		// early_departure | off_site), null when none.
		ExcusedBy    pgtype.Text `json:"excused_by"`
		FullName     string      `json:"full_name"`
		EmployeeCode string      `json:"employee_code"`
		BranchID     pgtype.UUID `json:"branch_id"`
		BranchName   string      `json:"branch_name"`
		// is_remote: clocked in at another branch's device.
		IsRemote          bool        `json:"is_remote"`
		ClockInBranchName pgtype.Text `json:"clock_in_branch_name"`
		// no_shift: present without a rostered shift, so never judged late or early.
		NoShift bool `json:"no_shift"`
		// Latest face photo of the employee, for a small avatar on the card.
		FacePhotoPath pgtype.Text `json:"face_photo_path"`
	}

	items := []recordRow{}
	for rows.Next() {
		var x recordRow
		if err := rows.Scan(
			&x.ID, &x.EmployeeID, &x.Date, &x.CheckIn, &x.CheckOut,
			&x.CheckInSource, &x.CheckOutSource, &x.CheckInPhotoPath,
			&x.DeviceID, &x.Status, &x.IsLate, &x.LateMinutes,
			&x.IsEarlyLeave, &x.EarlyLeaveMinutes, &x.IsMissingCheckout, &x.Note,
			&x.IsHalfDay, &x.HalfDayLostMinutes, &x.HalfDayType, &x.IsMissingCheckin, &x.IsNoPunch,
			&x.IsLeavePunch, &x.ExcusedBy,
			&x.FullName, &x.EmployeeCode, &x.BranchID, &x.BranchName,
			&x.IsRemote, &x.ClockInBranchName, &x.NoShift, &x.FacePhotoPath,
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

	resp := map[string]any{"data": items}
	if paginate {
		resp["total"] = total
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *AttendanceHandler) scheduleForEmployee(ctx context.Context, employeeID pgtype.UUID, date time.Time) service.Schedule {
	var branchID pgtype.UUID
	row := h.pool.QueryRow(ctx, `SELECT branch_id FROM employees WHERE id = $1`, employeeID)
	if err := row.Scan(&branchID); err != nil || !branchID.Valid {
		return service.ScheduleWithShift(ctx, h.pool, service.DefaultSchedule(), employeeID, date)
	}
	sched := service.DefaultSchedule()
	if ws, err := h.queries.GetWorkScheduleByBranch(ctx, branchID); err == nil && ws != nil {
		sched = service.ScheduleFromRow(ws)
	}
	return service.ScheduleWithShift(ctx, h.pool, sched, employeeID, date)
}

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
