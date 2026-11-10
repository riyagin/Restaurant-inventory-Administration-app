package handler

import (
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

// Attendance permits ("izin") — part-day permissions that change how a day is
// judged once approved:
//
//	late_arrival     Izin Masuk Siang   — a late arrival becomes a half day
//	early_departure  Izin Pulang Awal   — an early departure becomes a half day
//	off_site         Dinas Luar         — no late/early/missing flags; present without punches
//
// Created by whoever runs HR for the branch; approved / rejected by a manager. A
// permit for a day that has already passed is marked backdated (allowed unless
// HR settings forbid it). Approving or cancelling an approved permit rebuilds
// that day and re-evaluates its violations immediately.

type AttendancePermissionHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewAttendancePermissionHandler(pool *pgxpool.Pool, queries *db.Queries) *AttendancePermissionHandler {
	return &AttendancePermissionHandler{pool: pool, queries: queries}
}

var permissionTypes = map[string]string{
	"late_arrival":    "Izin Masuk Siang",
	"early_departure": "Izin Pulang Awal",
	"off_site":        "Dinas Luar",
}

// List — GET /api/hr/attendance-permissions?status=&branch_id=&month=YYYY-MM&employee_id=
func (h *AttendancePermissionHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	branchIDStr := strings.TrimSpace(q.Get("branch_id"))
	if b, ok := scopedBranchString(r); ok {
		branchIDStr = b
	}
	sql := `
SELECT p.id, p.employee_id, e.full_name, e.employee_code, e.branch_id, b.name,
       p.date, p.type, p.reason, p.status, p.is_backdated, p.decision_note,
       p.decided_at, du.username, cu.username, p.created_at
FROM attendance_permissions p
JOIN employees e ON e.id = p.employee_id
LEFT JOIN branches b ON b.id = e.branch_id
LEFT JOIN users cu ON cu.id = p.created_by
LEFT JOIN users du ON du.id = p.decided_by
WHERE true`
	args := []any{}
	if s := strings.TrimSpace(q.Get("status")); s != "" {
		args = append(args, s)
		sql += fmt.Sprintf(" AND p.status = $%d", len(args))
	}
	if m := strings.TrimSpace(q.Get("month")); m != "" {
		t, err := parseMonth(m)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
			return
		}
		args = append(args, t, t.AddDate(0, 1, -1))
		sql += fmt.Sprintf(" AND p.date >= $%d AND p.date <= $%d", len(args)-1, len(args))
	}
	if e := strings.TrimSpace(q.Get("employee_id")); e != "" {
		id, err := parseUUID(e)
		if err != nil {
			respondError(w, http.StatusBadRequest, "employee_id tidak valid")
			return
		}
		args = append(args, pgtype.UUID{Bytes: id, Valid: true})
		sql += fmt.Sprintf(" AND p.employee_id = $%d", len(args))
	}
	if branchIDStr != "" {
		id, err := parseUUID(branchIDStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		args = append(args, pgtype.UUID{Bytes: id, Valid: true})
		sql += fmt.Sprintf(" AND e.branch_id = $%d", len(args))
	}
	sql += " ORDER BY p.date DESC, e.full_name"

	rows, err := h.pool.Query(r.Context(), sql, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data izin")
		return
	}
	defer rows.Close()

	type row struct {
		ID                pgtype.UUID        `json:"id"`
		EmployeeID        pgtype.UUID        `json:"employee_id"`
		EmployeeName      string             `json:"employee_name"`
		EmployeeCode      string             `json:"employee_code"`
		BranchID          pgtype.UUID        `json:"branch_id"`
		BranchName        pgtype.Text        `json:"branch_name"`
		Date              string             `json:"date"`
		Type              string             `json:"type"`
		TypeLabel         string             `json:"type_label"`
		Reason            string             `json:"reason"`
		Status            string             `json:"status"`
		IsBackdated       bool               `json:"is_backdated"`
		DecisionNote      pgtype.Text        `json:"decision_note"`
		DecidedAt         pgtype.Timestamptz `json:"decided_at"`
		DecidedByUsername pgtype.Text        `json:"decided_by_username"`
		CreatedByUsername pgtype.Text        `json:"created_by_username"`
		CreatedAt         pgtype.Timestamptz `json:"created_at"`
	}
	out := []row{}
	for rows.Next() {
		var x row
		var d pgtype.Date
		if err := rows.Scan(&x.ID, &x.EmployeeID, &x.EmployeeName, &x.EmployeeCode, &x.BranchID, &x.BranchName,
			&d, &x.Type, &x.Reason, &x.Status, &x.IsBackdated, &x.DecisionNote,
			&x.DecidedAt, &x.DecidedByUsername, &x.CreatedByUsername, &x.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data izin")
			return
		}
		x.Date = d.Time.Format("2006-01-02")
		x.TypeLabel = permissionTypes[x.Type]
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data izin")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

type permissionCreateBody struct {
	EmployeeID string `json:"employee_id"`
	Date       string `json:"date"`
	Type       string `json:"type"`
	Reason     string `json:"reason"`
	// ApproveNow: a manager granting the permit directly (e.g. after the fact
	// from the review list). Requires the manager role.
	ApproveNow bool   `json:"approve_now"`
	Note       string `json:"note"`
}

// Create — POST /api/hr/attendance-permissions
func (h *AttendancePermissionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body permissionCreateBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	empID, err := parseUUID(body.EmployeeID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "employee_id tidak valid")
		return
	}
	pgEmp := pgtype.UUID{Bytes: empID, Valid: true}
	if !requireEmployeeInScope(w, r, h.queries, pgEmp) {
		return
	}
	if _, ok := permissionTypes[body.Type]; !ok {
		respondError(w, http.StatusBadRequest, "jenis izin tidak valid")
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		respondError(w, http.StatusBadRequest, "alasan izin wajib diisi")
		return
	}
	date, err := time.Parse("2006-01-02", strings.TrimSpace(body.Date))
	if err != nil {
		respondError(w, http.StatusBadRequest, "format tanggal tidak valid (YYYY-MM-DD)")
		return
	}
	ctx := r.Context()
	if body.ApproveNow && !middleware.HasRole(ctx, middleware.RoleManager) {
		respondError(w, http.StatusForbidden, "hanya manajer yang dapat langsung menyetujui izin")
		return
	}
	backdated := date.Before(service.TodayWIB())
	if backdated {
		if s, err := h.queries.GetAttendanceReviewSettings(ctx); err == nil && !s.AllowBackdatedPermission {
			respondError(w, http.StatusBadRequest, "izin untuk tanggal yang sudah lewat tidak diizinkan (Pengaturan HR)")
			return
		}
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)
	userID := middleware.UserIDFromCtx(ctx)
	user := pgtype.UUID{Bytes: userID, Valid: userID != [16]byte{}}

	p, err := qtx.CreateAttendancePermission(ctx, &db.CreateAttendancePermissionParams{
		EmployeeID: pgEmp, Date: pgtype.Date{Time: date, Valid: true}, Type: body.Type,
		Reason: reason, IsBackdated: backdated, CreatedBy: user,
	})
	if err != nil {
		if strings.Contains(err.Error(), "uq_attendance_permissions_live") {
			respondError(w, http.StatusConflict, "karyawan ini sudah memiliki izin yang sama di tanggal tersebut")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menyimpan izin")
		return
	}
	if body.ApproveNow {
		note := strings.TrimSpace(body.Note)
		if p, err = qtx.ApproveAttendancePermission(ctx, &db.ApproveAttendancePermissionParams{
			ID: p.ID, DecidedBy: user, DecisionNote: pgtype.Text{String: note, Valid: note != ""},
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyetujui izin")
			return
		}
		if err := service.RecomputeDay(ctx, qtx, pgEmp, date); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghitung ulang kehadiran")
			return
		}
	}
	desc := fmt.Sprintf("Mengajukan %s tanggal %s", permissionTypes[body.Type], date.Format("2006-01-02"))
	if body.ApproveNow {
		desc = fmt.Sprintf("Memberikan %s tanggal %s", permissionTypes[body.Type], date.Format("2006-01-02"))
	}
	if backdated {
		desc += " (mundur tanggal)"
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID: userID, Username: middleware.UsernameFromCtx(ctx), Action: "CREATE",
		EntityType: "attendance_permission", EntityID: p.ID.Bytes, Description: desc,
	})
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan izin")
		return
	}
	respondJSON(w, http.StatusCreated, p)
}

type permissionDecisionBody struct {
	Note string `json:"note"`
}

// decide runs approve / reject / cancel in one transaction and rebuilds the day
// when the permit's effect changes (approved now, or approved before and
// cancelled now).
func (h *AttendancePermissionHandler) decide(w http.ResponseWriter, r *http.Request, action string) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body permissionDecisionBody
	_ = parseBody(r, &body)
	note := strings.TrimSpace(body.Note)
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	before, err := h.queries.GetAttendancePermission(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "izin tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil izin")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, before.EmployeeID) {
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)
	userID := middleware.UserIDFromCtx(ctx)
	user := pgtype.UUID{Bytes: userID, Valid: userID != [16]byte{}}
	pgNote := pgtype.Text{String: note, Valid: note != ""}

	var p *db.AttendancePermission
	var verb, badState string
	switch action {
	case "approve":
		p, err = qtx.ApproveAttendancePermission(ctx, &db.ApproveAttendancePermissionParams{ID: pgID, DecidedBy: user, DecisionNote: pgNote})
		verb, badState = "Menyetujui", "hanya izin berstatus menunggu yang dapat disetujui"
	case "reject":
		p, err = qtx.RejectAttendancePermission(ctx, &db.RejectAttendancePermissionParams{ID: pgID, DecidedBy: user, DecisionNote: pgNote})
		verb, badState = "Menolak", "hanya izin berstatus menunggu yang dapat ditolak"
	default:
		p, err = qtx.CancelAttendancePermission(ctx, &db.CancelAttendancePermissionParams{ID: pgID, DecidedBy: user, DecisionNote: pgNote})
		verb, badState = "Membatalkan", "hanya izin menunggu atau disetujui yang dapat dibatalkan"
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusBadRequest, badState)
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memproses izin")
		return
	}
	if action == "approve" || (action == "cancel" && before.Status == "approved") {
		if err := service.RecomputeDay(ctx, qtx, p.EmployeeID, p.Date.Time); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghitung ulang kehadiran")
			return
		}
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID: userID, Username: middleware.UsernameFromCtx(ctx), Action: "UPDATE",
		EntityType: "attendance_permission", EntityID: p.ID.Bytes,
		Description: fmt.Sprintf("%s %s tanggal %s", verb, permissionTypes[p.Type], p.Date.Time.Format("2006-01-02")),
	})
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan keputusan izin")
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// Approve — POST /api/hr/attendance-permissions/{id}/approve (manager)
func (h *AttendancePermissionHandler) Approve(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, "approve")
}

// Reject — POST /api/hr/attendance-permissions/{id}/reject (manager)
func (h *AttendancePermissionHandler) Reject(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, "reject")
}

// Cancel — POST /api/hr/attendance-permissions/{id}/cancel (HR access)
func (h *AttendancePermissionHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, "cancel")
}

// ── Review settings & review resolution ──────────────────────────────────────

// GetReviewSettings — GET /api/hr/attendance/review-settings
func (h *AttendancePermissionHandler) GetReviewSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.queries.GetAttendanceReviewSettings(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengaturan peninjauan")
		return
	}
	respondJSON(w, http.StatusOK, s)
}

// UpdateReviewSettings — PUT /api/hr/attendance/review-settings
func (h *AttendancePermissionHandler) UpdateReviewSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ReviewLateMinutes        int  `json:"review_late_minutes"`
		ReviewEarlyMinutes       int  `json:"review_early_minutes"`
		AllowBackdatedPermission bool `json:"allow_backdated_permission"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.ReviewLateMinutes < 0 || body.ReviewEarlyMinutes < 0 || body.ReviewLateMinutes > 24*60 || body.ReviewEarlyMinutes > 24*60 {
		respondError(w, http.StatusBadRequest, "ambang menit harus antara 0 dan 1440")
		return
	}
	ctx := r.Context()
	s, err := h.queries.UpdateAttendanceReviewSettings(ctx, &db.UpdateAttendanceReviewSettingsParams{
		ReviewLateMinutes:        int32(body.ReviewLateMinutes),
		ReviewEarlyMinutes:       int32(body.ReviewEarlyMinutes),
		AllowBackdatedPermission: body.AllowBackdatedPermission,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pengaturan peninjauan")
		return
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID: middleware.UserIDFromCtx(ctx), Username: middleware.UsernameFromCtx(ctx), Action: "UPDATE",
		EntityType: "hr_settings",
		Description: fmt.Sprintf("Pengaturan peninjauan absensi: telat ≥ %d mnt, pulang awal ≥ %d mnt, izin mundur tanggal %v",
			body.ReviewLateMinutes, body.ReviewEarlyMinutes, body.AllowBackdatedPermission),
	})
	respondJSON(w, http.StatusOK, s)
}

// SetReviewed — POST /api/hr/attendance/{id}/review  {"resolved": true}
// A manager reviewed a flagged day (very late / very early without a permit) and
// keeps it as it is: it leaves the review list; scoring is unchanged.
func (h *AttendancePermissionHandler) SetReviewed(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body struct {
		Resolved bool `json:"resolved"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	rec, err := h.queries.GetAttendanceRecordByID(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusNotFound, "data kehadiran tidak ditemukan")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, rec.EmployeeID) {
		return
	}
	row, err := h.queries.SetAttendanceReviewResolved(ctx, &db.SetAttendanceReviewResolvedParams{ID: pgID, ReviewResolved: body.Resolved})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan peninjauan")
		return
	}
	verb := "Menandai sudah ditinjau"
	if !body.Resolved {
		verb = "Membuka kembali peninjauan"
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID: middleware.UserIDFromCtx(ctx), Username: middleware.UsernameFromCtx(ctx), Action: "UPDATE",
		EntityType: "attendance_record", EntityID: id,
		Description: fmt.Sprintf("%s: kehadiran %s", verb, row.Date.Time.Format("2006-01-02")),
	})
	respondJSON(w, http.StatusOK, row)
}
