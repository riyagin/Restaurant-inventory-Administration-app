package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// LeaveHandler serves the HR leave management endpoints (leave types, requests,
// balances). Approval/rejection routes are wired behind RequireManager; everything
// else behind RequireAdminOrManager.
type LeaveHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewLeaveHandler(pool *pgxpool.Pool, queries *db.Queries) *LeaveHandler {
	return &LeaveHandler{pool: pool, queries: queries}
}

// ── Leave Types ──────────────────────────────────────────────────────────────

// ListLeaveTypes — GET /api/hr/leave-types?active=1
func (h *LeaveHandler) ListLeaveTypes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var (
		types []*db.LeaveType
		err   error
	)
	if r.URL.Query().Get("active") == "1" {
		types, err = h.queries.ListActiveLeaveTypes(ctx)
	} else {
		types, err = h.queries.ListLeaveTypes(ctx)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil jenis cuti")
		return
	}
	if types == nil {
		types = []*db.LeaveType{}
	}
	respondJSON(w, http.StatusOK, types)
}

type leaveTypeBody struct {
	Name      string `json:"name"`
	IsPaid    *bool  `json:"is_paid"`
	UsesQuota *bool  `json:"uses_quota"`
	IsActive  *bool  `json:"is_active"`
}

// CreateLeaveType — POST /api/hr/leave-types
func (h *LeaveHandler) CreateLeaveType(w http.ResponseWriter, r *http.Request) {
	var body leaveTypeBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama jenis cuti wajib diisi")
		return
	}
	isPaid := true
	if body.IsPaid != nil {
		isPaid = *body.IsPaid
	}
	usesQuota := false
	if body.UsesQuota != nil {
		usesQuota = *body.UsesQuota
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	ctx := r.Context()
	lt, err := h.queries.CreateLeaveType(ctx, &db.CreateLeaveTypeParams{
		Name:      body.Name,
		IsPaid:    isPaid,
		UsesQuota: usesQuota,
		IsActive:  isActive,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama jenis cuti sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat jenis cuti")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "leave_type",
		EntityID:    lt.ID.Bytes,
		Description: fmt.Sprintf("Membuat jenis cuti %s", lt.Name),
	})

	respondJSON(w, http.StatusCreated, lt)
}

// UpdateLeaveType — PUT /api/hr/leave-types/:id
func (h *LeaveHandler) UpdateLeaveType(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body leaveTypeBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama jenis cuti wajib diisi")
		return
	}
	isPaid := true
	if body.IsPaid != nil {
		isPaid = *body.IsPaid
	}
	usesQuota := false
	if body.UsesQuota != nil {
		usesQuota = *body.UsesQuota
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	ctx := r.Context()
	lt, err := h.queries.UpdateLeaveType(ctx, &db.UpdateLeaveTypeParams{
		Name:      body.Name,
		IsPaid:    isPaid,
		UsesQuota: usesQuota,
		IsActive:  isActive,
		ID:        pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jenis cuti tidak ditemukan")
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama jenis cuti sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui jenis cuti")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_type",
		EntityID:    id,
		Description: fmt.Sprintf("Memperbarui jenis cuti %s", lt.Name),
	})

	respondJSON(w, http.StatusOK, lt)
}

// DeleteLeaveType — DELETE /api/hr/leave-types/:id
// Deletes only when unreferenced; otherwise deactivates (is_active = false).
func (h *LeaveHandler) DeleteLeaveType(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	lt, err := h.queries.GetLeaveTypeByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jenis cuti tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil jenis cuti")
		return
	}

	refs, err := h.queries.CountLeaveTypeReferences(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa penggunaan jenis cuti")
		return
	}

	if refs > 0 {
		updated, err := h.queries.SetLeaveTypeActive(ctx, &db.SetLeaveTypeActiveParams{
			IsActive: false,
			ID:       pgID,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menonaktifkan jenis cuti")
			return
		}
		_ = service.LogActivity(ctx, h.queries, service.LogParams{
			UserID:      middleware.UserIDFromCtx(ctx),
			Username:    middleware.UsernameFromCtx(ctx),
			Action:      "UPDATE",
			EntityType:  "leave_type",
			EntityID:    id,
			Description: fmt.Sprintf("Menonaktifkan jenis cuti %s (masih dipakai pengajuan)", lt.Name),
		})
		respondJSON(w, http.StatusOK, map[string]any{
			"message":     "jenis cuti masih dipakai pengajuan, dinonaktifkan (bukan dihapus)",
			"deactivated": true,
			"leave_type":  updated,
		})
		return
	}

	if err := h.queries.DeleteLeaveType(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus jenis cuti")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "leave_type",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus jenis cuti %s", lt.Name),
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"message":     "jenis cuti berhasil dihapus",
		"deactivated": false,
	})
}

// ── Leave Requests ───────────────────────────────────────────────────────────

// ListLeaveRequests — GET /api/hr/leave-requests?status=&branch_id=&employee_id=&year=
func (h *LeaveHandler) ListLeaveRequests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	params := &db.ListLeaveRequestsParams{
		Status: strings.TrimSpace(q.Get("status")),
	}
	if v := strings.TrimSpace(q.Get("branch_id")); v != "" {
		bid, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		params.BranchID = pgtype.UUID{Bytes: bid, Valid: true}
	}
	if v := strings.TrimSpace(q.Get("employee_id")); v != "" {
		eid, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "employee_id tidak valid")
			return
		}
		params.EmployeeID = pgtype.UUID{Bytes: eid, Valid: true}
	}
	if v := strings.TrimSpace(q.Get("year")); v != "" {
		yr, err := strconv.Atoi(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tahun tidak valid")
			return
		}
		params.Year = int32(yr)
	}

	rows, err := h.queries.ListLeaveRequests(ctx, params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan cuti")
		return
	}
	if rows == nil {
		rows = []*db.ListLeaveRequestsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

type createLeaveRequestBody struct {
	EmployeeID  string `json:"employee_id"`
	LeaveTypeID string `json:"leave_type_id"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Reason      string `json:"reason"`
}

// CreateLeaveRequest — POST /api/hr/leave-requests
// Computes day_count server-side from the branch schedule + holidays and rejects
// overlapping pending/approved requests for the same employee.
func (h *LeaveHandler) CreateLeaveRequest(w http.ResponseWriter, r *http.Request) {
	var body createLeaveRequestBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	empID, err := parseUUID(strings.TrimSpace(body.EmployeeID))
	if err != nil {
		respondError(w, http.StatusBadRequest, "karyawan tidak valid")
		return
	}
	typeID, err := parseUUID(strings.TrimSpace(body.LeaveTypeID))
	if err != nil {
		respondError(w, http.StatusBadRequest, "jenis cuti tidak valid")
		return
	}
	startDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.StartDate))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal mulai tidak valid")
		return
	}
	endDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.EndDate))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal selesai tidak valid")
		return
	}
	if endDate.Before(startDate) {
		respondError(w, http.StatusBadRequest, "tanggal selesai harus setelah atau sama dengan tanggal mulai")
		return
	}

	ctx := r.Context()
	pgEmpID := pgtype.UUID{Bytes: empID, Valid: true}
	pgTypeID := pgtype.UUID{Bytes: typeID, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgEmpID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	if _, err := h.queries.GetLeaveTypeByID(ctx, pgTypeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jenis cuti tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil jenis cuti")
		return
	}

	pgStart := pgtype.Date{Time: startDate, Valid: true}
	pgEnd := pgtype.Date{Time: endDate, Valid: true}

	// Reject overlapping pending/approved requests for the same employee.
	overlaps, err := h.queries.ListOverlappingRequests(ctx, &db.ListOverlappingRequestsParams{
		EmployeeID: pgEmpID,
		StartDate:  pgStart,
		EndDate:    pgEnd,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa tumpang tindih cuti")
		return
	}
	if len(overlaps) > 0 {
		respondError(w, http.StatusConflict, "rentang tanggal bertabrakan dengan pengajuan cuti lain yang masih aktif")
		return
	}

	// Compute working-day count from the employee's branch schedule + holidays.
	dayCount, err := service.ComputeLeaveDayCount(ctx, h.queries, emp.BranchID, startDate, endDate)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung jumlah hari kerja")
		return
	}
	if dayCount <= 0 {
		respondError(w, http.StatusBadRequest, "rentang tanggal tidak memuat hari kerja")
		return
	}

	reason := pgtype.Text{}
	if s := strings.TrimSpace(body.Reason); s != "" {
		reason = pgtype.Text{String: s, Valid: true}
	}
	createdBy := middleware.UserIDFromCtx(ctx)

	req, err := h.queries.CreateLeaveRequest(ctx, &db.CreateLeaveRequestParams{
		EmployeeID:  pgEmpID,
		LeaveTypeID: pgTypeID,
		StartDate:   pgStart,
		EndDate:     pgEnd,
		DayCount:    int32(dayCount),
		Reason:      reason,
		CreatedBy:   pgtype.UUID{Bytes: createdBy, Valid: createdBy != [16]byte{}},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pengajuan cuti")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      createdBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "leave_request",
		EntityID:    req.ID.Bytes,
		Description: fmt.Sprintf("Mengajukan cuti %s (%s s/d %s, %d hari)", emp.FullName, body.StartDate, body.EndDate, dayCount),
	})

	respondJSON(w, http.StatusCreated, req)
}

type decisionBody struct {
	Note string `json:"note"`
}

// ApproveLeaveRequest — POST /api/hr/leave-requests/:id/approve (manager only)
func (h *LeaveHandler) ApproveLeaveRequest(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body decisionBody
	_ = parseBody(r, &body)

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	req, err := h.queries.GetLeaveRequestByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengajuan cuti tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan cuti")
		return
	}
	if req.Status != "pending" {
		respondError(w, http.StatusBadRequest, "hanya pengajuan berstatus menunggu yang dapat disetujui")
		return
	}

	leaveType, err := h.queries.GetLeaveTypeByID(ctx, req.LeaveTypeID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil jenis cuti")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	deciderID := middleware.UserIDFromCtx(ctx)
	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	res, err := service.ApproveLeave(ctx, qtx, req, leaveType, pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}}, note)
	if err != nil {
		if errors.Is(err, service.ErrQuotaExceeded) {
			respondError(w, http.StatusBadRequest, "kuota cuti tahunan tidak mencukupi untuk pengajuan ini")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menyetujui pengajuan cuti")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_request",
		EntityID:    id,
		Description: fmt.Sprintf("Menyetujui pengajuan cuti (%d hari kehadiran ditandai cuti)", res.CoveredDays),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, res)
}

// RejectLeaveRequest — POST /api/hr/leave-requests/:id/reject (manager only)
func (h *LeaveHandler) RejectLeaveRequest(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body decisionBody
	_ = parseBody(r, &body)

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	req, err := h.queries.GetLeaveRequestByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengajuan cuti tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan cuti")
		return
	}
	if req.Status != "pending" {
		respondError(w, http.StatusBadRequest, "hanya pengajuan berstatus menunggu yang dapat ditolak")
		return
	}

	deciderID := middleware.UserIDFromCtx(ctx)
	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	updated, err := h.queries.SetLeaveRequestStatus(ctx, &db.SetLeaveRequestStatusParams{
		Status:       "rejected",
		DecidedBy:    pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}},
		DecisionNote: note,
		ID:           pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menolak pengajuan cuti")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_request",
		EntityID:    id,
		Description: "Menolak pengajuan cuti",
	})

	respondJSON(w, http.StatusOK, updated)
}

// CancelLeaveRequest — POST /api/hr/leave-requests/:id/cancel (admin/manager)
// Cancelling an approved future quota request decrements used_days and removes the
// future leave attendance rows it created (those without a check-in).
func (h *LeaveHandler) CancelLeaveRequest(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body decisionBody
	_ = parseBody(r, &body)

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	req, err := h.queries.GetLeaveRequestByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengajuan cuti tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan cuti")
		return
	}
	if req.Status == "cancelled" || req.Status == "rejected" {
		respondError(w, http.StatusBadRequest, "pengajuan cuti sudah tidak aktif")
		return
	}

	leaveType, err := h.queries.GetLeaveTypeByID(ctx, req.LeaveTypeID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil jenis cuti")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	// Reverse quota + attendance side-effects only if the request was approved.
	if req.Status == "approved" {
		if err := service.CancelApprovedLeave(ctx, qtx, req, leaveType, time.Now().UTC()); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membatalkan efek persetujuan cuti")
			return
		}
	}

	deciderID := middleware.UserIDFromCtx(ctx)
	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	updated, err := qtx.SetLeaveRequestStatus(ctx, &db.SetLeaveRequestStatusParams{
		Status:       "cancelled",
		DecidedBy:    pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}},
		DecisionNote: note,
		ID:           pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan pengajuan cuti")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_request",
		EntityID:    id,
		Description: "Membatalkan pengajuan cuti",
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, updated)
}

// ── Leave Balances ───────────────────────────────────────────────────────────

func yearParam(r *http.Request) int32 {
	if v := strings.TrimSpace(r.URL.Query().Get("year")); v != "" {
		if yr, err := strconv.Atoi(v); err == nil {
			return int32(yr)
		}
	}
	return int32(time.Now().UTC().Year())
}

// GetLeaveBalance — GET /api/hr/employees/:id/leave-balance?year=
// Auto-creates a balance row (default quota 12) when missing for the year.
func (h *LeaveHandler) GetLeaveBalance(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgEmpID := pgtype.UUID{Bytes: id, Valid: true}
	year := yearParam(r)

	if _, err := h.queries.GetEmployeeByID(ctx, pgEmpID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	bal, err := h.queries.GetLeaveBalance(ctx, &db.GetLeaveBalanceParams{EmployeeID: pgEmpID, Year: year})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			bal, err = h.queries.CreateLeaveBalance(ctx, &db.CreateLeaveBalanceParams{
				EmployeeID: pgEmpID,
				Year:       year,
				QuotaDays:  12,
			})
			if err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membuat saldo cuti")
				return
			}
		} else {
			respondError(w, http.StatusInternalServerError, "gagal mengambil saldo cuti")
			return
		}
	}

	respondJSON(w, http.StatusOK, bal)
}

// ListEmployeeLeaveRequests — GET /api/hr/employees/:id/leave-requests
func (h *LeaveHandler) ListEmployeeLeaveRequests(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	rows, err := h.queries.ListLeaveRequestsByEmployee(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat cuti")
		return
	}
	if rows == nil {
		rows = []*db.ListLeaveRequestsByEmployeeRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// ── Manpower Planning ────────────────────────────────────────────────────────

type manpowerDay struct {
	Date      string `json:"date"`
	Status    string `json:"status"` // "hadir" | "cuti"
	LeaveType string `json:"leave_type,omitempty"`
}

type manpowerEmployee struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Position string        `json:"position"`
	Days     []manpowerDay `json:"days"`
}

type manpowerBranch struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Employees []manpowerEmployee `json:"employees"`
}

type manpowerPlanningResponse struct {
	StartDate string           `json:"start_date"`
	Dates     []string         `json:"dates"`
	Branches  []manpowerBranch `json:"branches"`
}

// GetManpowerPlanning — GET /api/hr/manpower-planning?date=YYYY-MM-DD
// Returns all active employees grouped by branch with their leave status for
// the 7 days starting from the given date (default: today UTC).
func (h *LeaveHandler) GetManpowerPlanning(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var startDate time.Time
	if v := strings.TrimSpace(r.URL.Query().Get("date")); v != "" {
		var err error
		startDate, err = time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid (YYYY-MM-DD)")
			return
		}
	} else {
		startDate = time.Now().UTC().Truncate(24 * time.Hour)
	}
	endDate := startDate.AddDate(0, 0, 6)

	dates := make([]string, 7)
	for i := range dates {
		dates[i] = startDate.AddDate(0, 0, i).Format("2006-01-02")
	}

	// 1. All active employees with branch + position info.
	empRows, err := h.pool.Query(ctx, `
		SELECT e.id::text, e.full_name,
		       COALESCE(e.branch_id::text, ''), COALESCE(b.name, 'Tanpa Cabang'),
		       COALESCE(p.name, '')
		FROM employees e
		LEFT JOIN branches b ON b.id = e.branch_id
		LEFT JOIN positions p ON p.id = e.position_id
		WHERE e.status = 'active'
		ORDER BY COALESCE(b.name, 'Tanpa Cabang'), e.full_name
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	defer empRows.Close()

	type empInfo struct{ id, name, branchID, branchName, position string }
	var employees []empInfo
	for empRows.Next() {
		var e empInfo
		if err := empRows.Scan(&e.id, &e.name, &e.branchID, &e.branchName, &e.position); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data karyawan")
			return
		}
		employees = append(employees, e)
	}
	if err := empRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data karyawan")
		return
	}

	// 2. Approved leave requests overlapping the date window.
	startPg := pgtype.Date{Time: startDate, Valid: true}
	endPg := pgtype.Date{Time: endDate, Valid: true}
	leaveRows, err := h.pool.Query(ctx, `
		SELECT lr.employee_id::text, lr.start_date, lr.end_date, lt.name
		FROM leave_requests lr
		JOIN leave_types lt ON lt.id = lr.leave_type_id
		WHERE lr.status = 'approved'
		  AND lr.start_date <= $2
		  AND lr.end_date   >= $1
	`, startPg, endPg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data cuti")
		return
	}
	defer leaveRows.Close()

	// Build lookup: employeeID → date string → leave type name.
	leaveLookup := map[string]map[string]string{}
	for leaveRows.Next() {
		var empID, leaveTypeName string
		var sd, ed pgtype.Date
		if err := leaveRows.Scan(&empID, &sd, &ed, &leaveTypeName); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data cuti")
			return
		}
		if leaveLookup[empID] == nil {
			leaveLookup[empID] = map[string]string{}
		}
		for cur := sd.Time; !cur.After(ed.Time); cur = cur.AddDate(0, 0, 1) {
			leaveLookup[empID][cur.Format("2006-01-02")] = leaveTypeName
		}
	}
	if err := leaveRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data cuti")
		return
	}

	// 3. Build response grouped by branch preserving order from the query.
	branchMap := map[string]*manpowerBranch{}
	var branchOrder []string
	for _, e := range employees {
		if _, ok := branchMap[e.branchID]; !ok {
			branchMap[e.branchID] = &manpowerBranch{ID: e.branchID, Name: e.branchName}
			branchOrder = append(branchOrder, e.branchID)
		}
		days := make([]manpowerDay, 7)
		for i, d := range dates {
			day := manpowerDay{Date: d, Status: "hadir"}
			if lt, onLeave := leaveLookup[e.id][d]; onLeave {
				day.Status = "cuti"
				day.LeaveType = lt
			}
			days[i] = day
		}
		branchMap[e.branchID].Employees = append(branchMap[e.branchID].Employees, manpowerEmployee{
			ID:       e.id,
			Name:     e.name,
			Position: e.position,
			Days:     days,
		})
	}

	branches := make([]manpowerBranch, len(branchOrder))
	for i, id := range branchOrder {
		branches[i] = *branchMap[id]
	}

	respondJSON(w, http.StatusOK, manpowerPlanningResponse{
		StartDate: startDate.Format("2006-01-02"),
		Dates:     dates,
		Branches:  branches,
	})
}

type setQuotaBody struct {
	Year      *int32 `json:"year"`
	QuotaDays int32  `json:"quota_days"`
}

// SetLeaveBalanceQuota — PUT /api/hr/employees/:id/leave-balance
func (h *LeaveHandler) SetLeaveBalanceQuota(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body setQuotaBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.QuotaDays < 0 {
		respondError(w, http.StatusBadRequest, "kuota cuti tidak boleh negatif")
		return
	}
	year := int32(time.Now().UTC().Year())
	if body.Year != nil {
		year = *body.Year
	}

	ctx := r.Context()
	pgEmpID := pgtype.UUID{Bytes: id, Valid: true}

	if _, err := h.queries.GetEmployeeByID(ctx, pgEmpID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	// Ensure the row exists, then set the quota.
	if _, err := h.queries.GetLeaveBalance(ctx, &db.GetLeaveBalanceParams{EmployeeID: pgEmpID, Year: year}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := h.queries.CreateLeaveBalance(ctx, &db.CreateLeaveBalanceParams{
				EmployeeID: pgEmpID,
				Year:       year,
				QuotaDays:  body.QuotaDays,
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membuat saldo cuti")
				return
			}
		} else {
			respondError(w, http.StatusInternalServerError, "gagal mengambil saldo cuti")
			return
		}
	}

	bal, err := h.queries.SetLeaveBalanceQuota(ctx, &db.SetLeaveBalanceQuotaParams{
		QuotaDays:  body.QuotaDays,
		EmployeeID: pgEmpID,
		Year:       year,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui kuota cuti")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_balance",
		EntityID:    bal.ID.Bytes,
		Description: fmt.Sprintf("Mengatur kuota cuti %d menjadi %d hari", year, body.QuotaDays),
	})

	respondJSON(w, http.StatusOK, bal)
}
