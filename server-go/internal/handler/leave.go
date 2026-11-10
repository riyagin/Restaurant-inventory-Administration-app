package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
	if b, ok := scopedBranch(r); ok {
		params.BranchID = b
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
	// ConfirmBackdated acknowledges that a start date before today makes the
	// request a backdated one, which only a manager/owner can approve.
	ConfirmBackdated bool `json:"confirm_backdated"`
}

// codeBackdatedNeedsConfirm is returned (409) for a backdated request submitted
// without confirm_backdated, so the client can show its warning and resubmit.
const codeBackdatedNeedsConfirm = "backdated_requires_confirmation"

// CreateLeaveRequest — POST /api/hr/leave-requests
// Computes day_count server-side from the branch schedule + holidays and rejects
// overlapping pending/approved requests for the same employee.
//
// Leave planning is not meant to be rewritten after the fact, so a request that
// starts before today is refused until the caller confirms it, and is then
// frozen as is_backdated: it waits for a manager/owner to approve it on its own,
// never through bulk approval. The flag is set from the server's date, not the
// client's, so it cannot be dodged by omitting it.
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
	if !requireEmployeeInScope(w, r, h.queries, pgtype.UUID{Bytes: empID, Valid: true}) {
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
	isBackdated := startDate.Before(today())
	if isBackdated && !body.ConfirmBackdated {
		respondJSON(w, http.StatusConflict, map[string]string{
			"error": "tanggal mulai sudah lewat — pengajuan mundur tanggal memerlukan persetujuan manajer/pemilik",
			"code":  codeBackdatedNeedsConfirm,
		})
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
		IsBackdated: isBackdated,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pengajuan cuti")
		return
	}

	desc := fmt.Sprintf("Mengajukan cuti %s (%s s/d %s, %d hari)", emp.FullName, body.StartDate, body.EndDate, dayCount)
	if isBackdated {
		desc += " — MUNDUR TANGGAL, menunggu persetujuan manajer/pemilik"
	}
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      createdBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "leave_request",
		EntityID:    req.ID.Bytes,
		Description: desc,
	})

	respondJSON(w, http.StatusCreated, req)
}

type decisionBody struct {
	Note string `json:"note"`
}

// approveOne approves a single pending leave request inside its own transaction.
// Shared by ApproveLeaveRequest (single) and BulkApproveLeaveRequests; bulk
// refuses backdated requests, which must each be looked at individually.
func (h *LeaveHandler) approveOne(ctx context.Context, id [16]byte, note pgtype.Text, bulk bool) (*service.ApproveResult, error) {
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	req, err := h.queries.GetLeaveRequestByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errLeaveRequestNotFound
		}
		return nil, err
	}
	if req.Status != "pending" {
		return nil, errLeaveRequestNotPending
	}
	if bulk && req.IsBackdated {
		return nil, errLeaveRequestBackdatedBulk
	}

	leaveType, err := h.queries.GetLeaveTypeByID(ctx, req.LeaveTypeID)
	if err != nil {
		return nil, err
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	deciderID := middleware.UserIDFromCtx(ctx)
	res, err := service.ApproveLeave(ctx, qtx, req, leaveType, pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}}, note)
	if err != nil {
		return nil, err
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_request",
		EntityID:    id,
		Description: approveDescription(req, res),
	})

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return res, nil
}

// rejectOne rejects a single pending leave request.
// Shared by RejectLeaveRequest (single) and BulkRejectLeaveRequests.
func (h *LeaveHandler) rejectOne(ctx context.Context, id [16]byte, note pgtype.Text) (*db.LeaveRequest, error) {
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	req, err := h.queries.GetLeaveRequestByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errLeaveRequestNotFound
		}
		return nil, err
	}
	if req.Status != "pending" {
		return nil, errLeaveRequestNotPending
	}

	deciderID := middleware.UserIDFromCtx(ctx)
	updated, err := h.queries.SetLeaveRequestStatus(ctx, &db.SetLeaveRequestStatusParams{
		Status:       "rejected",
		DecidedBy:    pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}},
		DecisionNote: note,
		ID:           pgID,
	})
	if err != nil {
		return nil, err
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_request",
		EntityID:    id,
		Description: "Menolak pengajuan cuti",
	})

	return updated, nil
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

	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	res, err := h.approveOne(r.Context(), id, note, false)
	if err != nil {
		switch {
		case errors.Is(err, errLeaveRequestNotFound):
			respondError(w, http.StatusNotFound, "pengajuan cuti tidak ditemukan")
		case errors.Is(err, errLeaveRequestNotPending):
			respondError(w, http.StatusBadRequest, "hanya pengajuan berstatus menunggu yang dapat disetujui")
		case errors.Is(err, service.ErrQuotaExceeded):
			respondError(w, http.StatusBadRequest, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "gagal menyetujui pengajuan cuti")
		}
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

	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	updated, err := h.rejectOne(r.Context(), id, note)
	if err != nil {
		switch {
		case errors.Is(err, errLeaveRequestNotFound):
			respondError(w, http.StatusNotFound, "pengajuan cuti tidak ditemukan")
		case errors.Is(err, errLeaveRequestNotPending):
			respondError(w, http.StatusBadRequest, "hanya pengajuan berstatus menunggu yang dapat ditolak")
		default:
			respondError(w, http.StatusInternalServerError, "gagal menolak pengajuan cuti")
		}
		return
	}

	respondJSON(w, http.StatusOK, updated)
}

type bulkDecisionBody struct {
	IDs  []string `json:"ids"`
	Note string   `json:"note"`
}

type bulkDecisionResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// BulkApproveLeaveRequests — POST /api/hr/leave-requests/bulk-approve (manager only)
// Approves each pending request independently (each in its own transaction) so one
// failure (e.g. insufficient quota) doesn't block approval of the rest of the batch.
func (h *LeaveHandler) BulkApproveLeaveRequests(w http.ResponseWriter, r *http.Request) {
	var body bulkDecisionBody
	if err := parseBody(r, &body); err != nil || len(body.IDs) == 0 {
		respondError(w, http.StatusBadRequest, "daftar ID pengajuan cuti wajib diisi")
		return
	}

	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	ctx := r.Context()
	results := make([]bulkDecisionResult, 0, len(body.IDs))
	for _, idStr := range body.IDs {
		id, err := parseUUID(idStr)
		if err != nil {
			results = append(results, bulkDecisionResult{ID: idStr, Success: false, Error: "ID tidak valid"})
			continue
		}
		if _, err := h.approveOne(ctx, id, note, true); err != nil {
			results = append(results, bulkDecisionResult{ID: idStr, Success: false, Error: bulkErrorMessage(err)})
			continue
		}
		results = append(results, bulkDecisionResult{ID: idStr, Success: true})
	}

	respondJSON(w, http.StatusOK, map[string]any{"results": results})
}

// BulkRejectLeaveRequests — POST /api/hr/leave-requests/bulk-reject (manager only)
func (h *LeaveHandler) BulkRejectLeaveRequests(w http.ResponseWriter, r *http.Request) {
	var body bulkDecisionBody
	if err := parseBody(r, &body); err != nil || len(body.IDs) == 0 {
		respondError(w, http.StatusBadRequest, "daftar ID pengajuan cuti wajib diisi")
		return
	}

	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	ctx := r.Context()
	results := make([]bulkDecisionResult, 0, len(body.IDs))
	for _, idStr := range body.IDs {
		id, err := parseUUID(idStr)
		if err != nil {
			results = append(results, bulkDecisionResult{ID: idStr, Success: false, Error: "ID tidak valid"})
			continue
		}
		if _, err := h.rejectOne(ctx, id, note); err != nil {
			results = append(results, bulkDecisionResult{ID: idStr, Success: false, Error: bulkErrorMessage(err)})
			continue
		}
		results = append(results, bulkDecisionResult{ID: idStr, Success: true})
	}

	respondJSON(w, http.StatusOK, map[string]any{"results": results})
}

var (
	errLeaveRequestNotFound      = errors.New("pengajuan cuti tidak ditemukan")
	errLeaveRequestNotPending    = errors.New("pengajuan cuti sudah tidak menunggu persetujuan")
	errLeaveRequestBackdatedBulk = errors.New("pengajuan mundur tanggal harus disetujui satu per satu")
)

func bulkErrorMessage(err error) string {
	if errors.Is(err, service.ErrQuotaExceeded) ||
		errors.Is(err, errLeaveRequestNotFound) ||
		errors.Is(err, errLeaveRequestNotPending) ||
		errors.Is(err, errLeaveRequestBackdatedBulk) {
		return err.Error()
	}
	return "gagal memproses pengajuan cuti"
}

func approveDescription(req *db.LeaveRequest, res *service.ApproveResult) string {
	desc := fmt.Sprintf("Menyetujui pengajuan cuti (%d hari kehadiran ditandai cuti)", res.CoveredDays)
	if req.IsBackdated {
		desc = "Menyetujui pengajuan cuti MUNDUR TANGGAL " +
			req.StartDate.Time.Format("2006-01-02") + " s/d " + req.EndDate.Time.Format("2006-01-02") +
			fmt.Sprintf(" (%d hari kehadiran ditandai cuti)", res.CoveredDays)
	}
	return desc
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
	if !requireEmployeeInScope(w, r, h.queries, req.EmployeeID) {
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
		if err := service.CancelApprovedLeave(ctx, qtx, req, leaveType, service.TodayWIB()); err != nil {
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
	return int32(service.TodayWIB().Year())
}

// GetLeaveBalance — GET /api/hr/employees/:id/leave-balance?year=
// The employee's paid-leave allowance month by month for the year: quota, used
// (weekend days counted double) and unused. Unused days in closed months are
// what evaluation converts to cash.
func (h *LeaveHandler) GetLeaveBalance(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgEmpID := pgtype.UUID{Bytes: id, Valid: true}
	year := int(yearParam(r))
	if !requireEmployeeInScope(w, r, h.queries, pgEmpID) {
		return
	}

	if _, err := h.queries.GetEmployeeByID(ctx, pgEmpID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	months, err := service.MonthlyLeaveBalances(ctx, h.queries, pgEmpID, year, today())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil saldo cuti")
		return
	}
	unusedClosed := 0
	for _, m := range months {
		if m.Closed {
			unusedClosed += m.UnusedDays
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"year":                year,
		"default_quota":       service.DefaultMonthlyLeaveQuota,
		"weekend_cost":        service.WeekendLeaveCost,
		"months":              months,
		"unused_closed_total": unusedClosed,
	})
}

// ListMonthlyLeaveBalances — GET /api/hr/leave-balances?month=YYYY-MM
// Every active employee's allowance for one month — the unused-leave indicator
// HR reads at evaluation time. Defaults to the current month.
func (h *LeaveHandler) ListMonthlyLeaveBalances(w http.ResponseWriter, r *http.Request) {
	month := service.MonthStart(today())
	if v := strings.TrimSpace(r.URL.Query().Get("month")); v != "" {
		m, err := time.Parse("2006-01", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
			return
		}
		month = m
	}

	ctx := r.Context()
	rows, err := h.queries.ListMonthlyLeaveBalances(ctx, &db.ListMonthlyLeaveBalancesParams{
		Month:        pgtype.Date{Time: month, Valid: true},
		DefaultQuota: service.DefaultMonthlyLeaveQuota,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil saldo cuti")
		return
	}

	type row struct {
		EmployeeID   pgtype.UUID `json:"employee_id"`
		EmployeeName string      `json:"employee_name"`
		EmployeeCode string      `json:"employee_code"`
		BranchName   string      `json:"branch_name"`
		service.MonthlyLeaveBalance
	}
	out := make([]row, 0, len(rows))
	for _, rr := range rows {
		out = append(out, row{
			EmployeeID:   rr.EmployeeID,
			EmployeeName: rr.EmployeeName,
			EmployeeCode: rr.EmployeeCode,
			BranchName:   rr.BranchName,
			MonthlyLeaveBalance: service.NewMonthlyLeaveBalance(month, int(rr.QuotaDays), int(rr.UsedDays),
				rr.Overridden, today()),
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"month":         month.Format("2006-01"),
		"closed":        month.Before(service.MonthStart(today())),
		"default_quota": service.DefaultMonthlyLeaveQuota,
		"weekend_cost":  service.WeekendLeaveCost,
		"employees":     out,
	})
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
	Status    string `json:"status"` // "hadir" | "cuti" | "pending"
	LeaveType string `json:"leave_type,omitempty"`
	// Rostered shift in force (approved). DivisionID is the day's override; the
	// employee's home division applies when it is empty.
	ShiftID    string `json:"shift_id,omitempty"`
	DivisionID string `json:"division_id,omitempty"`
	// An open change to today's roster awaiting a manager. PendingShiftID is
	// empty when the request removes the shift.
	PendingRequestID  string `json:"pending_request_id,omitempty"`
	PendingShiftID    string `json:"pending_shift_id,omitempty"`
	PendingDivisionID string `json:"pending_division_id,omitempty"`
}

type manpowerEmployee struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Position        string        `json:"position"`
	DivisionID      string        `json:"division_id"`
	DivisionName    string        `json:"division_name"`
	IsDeliveryStaff bool          `json:"is_delivery_staff"`
	Days            []manpowerDay `json:"days"`
	// LeaveBalance is the paid-leave allowance for each month the window touches,
	// so the planner sees how much day-off budget is left before adding more.
	LeaveBalance []service.MonthlyLeaveBalance `json:"leave_balance"`
}

type manpowerDivision struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type manpowerBranch struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Shifts    []*shiftDefinition `json:"shifts"`
	Divisions []manpowerDivision `json:"divisions"`
	Employees []manpowerEmployee `json:"employees"`
}

type manpowerPlanningResponse struct {
	StartDate string           `json:"start_date"`
	Today     string           `json:"today"`
	Dates     []string         `json:"dates"`
	Branches  []manpowerBranch `json:"branches"`
}

// GetManpowerPlanning — GET /api/hr/manpower-planning?date=YYYY-MM-DD&days=N&branch_id=
// Returns all active employees grouped by branch with their leave status and
// rostered shift for N days starting from the given date (default: today UTC).
// N is clamped to [7, 31] (default 7) — a flexible view range from one week up
// to one month. Each branch carries its shift definitions (inactive ones too, so
// a day rostered on a retired shift still renders) and its divisions.
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
		startDate = service.TodayWIB()
	}

	numDays := 7
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			numDays = n
		}
	}
	if numDays < 7 {
		numDays = 7
	}
	if numDays > 31 {
		numDays = 31
	}

	endDate := startDate.AddDate(0, 0, numDays-1)

	dates := make([]string, numDays)
	for i := range dates {
		dates[i] = startDate.AddDate(0, 0, i).Format("2006-01-02")
	}

	branchFilter := strings.TrimSpace(r.URL.Query().Get("branch_id"))

	// 1. All active employees with branch + position + division info.
	empRows, err := h.pool.Query(ctx, `
		SELECT e.id::text, e.full_name,
		       COALESCE(e.branch_id::text, ''), COALESCE(b.name, 'Tanpa Cabang'),
		       COALESCE(p.name, ''),
		       COALESCE(e.division_id::text, ''), COALESCE(d.name, ''), e.is_delivery_staff
		FROM employees e
		LEFT JOIN branches b ON b.id = e.branch_id
		LEFT JOIN positions p ON p.id = e.position_id
		LEFT JOIN divisions d ON d.id = e.division_id
		WHERE e.status = 'active'
		  AND ($1 = '' OR e.branch_id::text = $1)
		ORDER BY COALESCE(b.name, 'Tanpa Cabang'), e.full_name
	`, branchFilter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	defer empRows.Close()

	type empInfo struct {
		id, name, branchID, branchName, position, divisionID, divisionName string
		deliveryStaff                                                     bool
	}
	var employees []empInfo
	for empRows.Next() {
		var e empInfo
		if err := empRows.Scan(&e.id, &e.name, &e.branchID, &e.branchName, &e.position,
			&e.divisionID, &e.divisionName, &e.deliveryStaff); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data karyawan")
			return
		}
		employees = append(employees, e)
	}
	if err := empRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data karyawan")
		return
	}

	// 2. Approved + pending leave requests overlapping the date window.
	startPg := pgtype.Date{Time: startDate, Valid: true}
	endPg := pgtype.Date{Time: endDate, Valid: true}
	leaveRows, err := h.pool.Query(ctx, `
		SELECT lr.employee_id::text, lr.start_date, lr.end_date, lt.name, lr.status
		FROM leave_requests lr
		JOIN leave_types lt ON lt.id = lr.leave_type_id
		WHERE lr.status IN ('approved', 'pending')
		  AND lr.start_date <= $2
		  AND lr.end_date   >= $1
	`, startPg, endPg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data cuti")
		return
	}
	defer leaveRows.Close()

	// Build lookup: employeeID → date string → {leave type name, status}.
	// Approved requests take priority over pending ones on the same date.
	type leaveEntry struct {
		leaveType string
		status    string // "cuti" | "pending"
	}
	leaveLookup := map[string]map[string]leaveEntry{}
	for leaveRows.Next() {
		var empID, leaveTypeName, status string
		var sd, ed pgtype.Date
		if err := leaveRows.Scan(&empID, &sd, &ed, &leaveTypeName, &status); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca data cuti")
			return
		}
		entryStatus := "pending"
		if status == "approved" {
			entryStatus = "cuti"
		}
		if leaveLookup[empID] == nil {
			leaveLookup[empID] = map[string]leaveEntry{}
		}
		for cur := sd.Time; !cur.After(ed.Time); cur = cur.AddDate(0, 0, 1) {
			d := cur.Format("2006-01-02")
			if existing, ok := leaveLookup[empID][d]; ok && existing.status == "cuti" {
				continue // approved already recorded for this date — don't downgrade to pending
			}
			leaveLookup[empID][d] = leaveEntry{leaveType: leaveTypeName, status: entryStatus}
		}
	}
	if err := leaveRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca data cuti")
		return
	}

	// 3. Paid-leave allowance for every month the window touches (at most two).
	var windowMonths []time.Time
	for m := service.MonthStart(startDate); !m.After(endDate); m = m.AddDate(0, 1, 0) {
		windowMonths = append(windowMonths, m)
	}
	now := today()
	balanceBy := map[string][]service.MonthlyLeaveBalance{} // employee id → one per window month
	for _, m := range windowMonths {
		rows, err := h.queries.ListMonthlyLeaveBalances(ctx, &db.ListMonthlyLeaveBalancesParams{
			Month:        pgtype.Date{Time: m, Valid: true},
			DefaultQuota: service.DefaultMonthlyLeaveQuota,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil saldo cuti")
			return
		}
		for _, rr := range rows {
			empID := uuid.UUID(rr.EmployeeID.Bytes).String()
			balanceBy[empID] = append(balanceBy[empID], service.NewMonthlyLeaveBalance(
				m, int(rr.QuotaDays), int(rr.UsedDays), rr.Overridden, now))
		}
	}

	// 4. Roster: the approved shift per cell, plus open change requests. A
	// request for a day already gone is left out — it can only be rejected, and
	// that happens on the approvals page.
	type rosterCell struct{ shiftID, divisionID, reqID, reqShiftID, reqDivisionID string }
	roster := map[string]map[string]*rosterCell{} // employee id → date → cell
	cellFor := func(empID, d string) *rosterCell {
		if roster[empID] == nil {
			roster[empID] = map[string]*rosterCell{}
		}
		if roster[empID][d] == nil {
			roster[empID][d] = &rosterCell{}
		}
		return roster[empID][d]
	}
	rosterRows, err := h.pool.Query(ctx, `
		SELECT employee_id::text, to_char(date, 'YYYY-MM-DD'), shift_id::text, COALESCE(division_id::text, '')
		FROM shift_assignments WHERE date BETWEEN $1 AND $2`, startPg, endPg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil jadwal shift")
		return
	}
	for rosterRows.Next() {
		var empID, d, shiftID, divID string
		if err := rosterRows.Scan(&empID, &d, &shiftID, &divID); err != nil {
			rosterRows.Close()
			respondError(w, http.StatusInternalServerError, "gagal membaca jadwal shift")
			return
		}
		c := cellFor(empID, d)
		c.shiftID, c.divisionID = shiftID, divID
	}
	rosterRows.Close()
	reqRows, err := h.pool.Query(ctx, `
		SELECT id::text, employee_id::text, to_char(date, 'YYYY-MM-DD'),
		       COALESCE(shift_id::text, ''), COALESCE(division_id::text, '')
		FROM shift_change_requests
		WHERE status = 'pending' AND date BETWEEN GREATEST($1::date, $3::date) AND $2`,
		startPg, endPg, pgtype.Date{Time: now, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan shift")
		return
	}
	for reqRows.Next() {
		var id, empID, d, shiftID, divID string
		if err := reqRows.Scan(&id, &empID, &d, &shiftID, &divID); err != nil {
			reqRows.Close()
			respondError(w, http.StatusInternalServerError, "gagal membaca pengajuan shift")
			return
		}
		c := cellFor(empID, d)
		c.reqID, c.reqShiftID, c.reqDivisionID = id, shiftID, divID
	}
	reqRows.Close()

	// 5. Build response grouped by branch preserving order from the query.
	branchMap := map[string]*manpowerBranch{}
	var branchOrder []string
	for _, e := range employees {
		if _, ok := branchMap[e.branchID]; !ok {
			branchMap[e.branchID] = &manpowerBranch{ID: e.branchID, Name: e.branchName,
				Shifts: []*shiftDefinition{}, Divisions: []manpowerDivision{}}
			branchOrder = append(branchOrder, e.branchID)
		}
		days := make([]manpowerDay, numDays)
		for i, d := range dates {
			day := manpowerDay{Date: d, Status: "hadir"}
			if entry, onLeave := leaveLookup[e.id][d]; onLeave {
				day.Status = entry.status
				day.LeaveType = entry.leaveType
			}
			if c := roster[e.id][d]; c != nil {
				day.ShiftID, day.DivisionID = c.shiftID, c.divisionID
				day.PendingRequestID, day.PendingShiftID, day.PendingDivisionID = c.reqID, c.reqShiftID, c.reqDivisionID
			}
			days[i] = day
		}
		branchMap[e.branchID].Employees = append(branchMap[e.branchID].Employees, manpowerEmployee{
			ID:              e.id,
			Name:            e.name,
			Position:        e.position,
			DivisionID:      e.divisionID,
			DivisionName:    e.divisionName,
			IsDeliveryStaff: e.deliveryStaff,
			Days:            days,
			LeaveBalance:    balanceBy[e.id],
		})
	}

	// 6. Each branch's shifts and divisions.
	shifts, err := loadShiftDefinitions(ctx, h.pool, branchFilter, true)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil daftar shift")
		return
	}
	for _, s := range shifts {
		if b, ok := branchMap[s.BranchID]; ok {
			b.Shifts = append(b.Shifts, s)
		}
	}
	divRows, err := h.pool.Query(ctx, `
		SELECT id::text, branch_id::text, name FROM divisions
		WHERE ($1 = '' OR branch_id::text = $1) ORDER BY name`, branchFilter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil divisi")
		return
	}
	for divRows.Next() {
		var d manpowerDivision
		var branchID string
		if err := divRows.Scan(&d.ID, &branchID, &d.Name); err != nil {
			divRows.Close()
			respondError(w, http.StatusInternalServerError, "gagal membaca divisi")
			return
		}
		if b, ok := branchMap[branchID]; ok {
			b.Divisions = append(b.Divisions, d)
		}
	}
	divRows.Close()

	branches := make([]manpowerBranch, len(branchOrder))
	for i, id := range branchOrder {
		branches[i] = *branchMap[id]
	}

	respondJSON(w, http.StatusOK, manpowerPlanningResponse{
		StartDate: startDate.Format("2006-01-02"),
		Today:     now.Format("2006-01-02"),
		Dates:     dates,
		Branches:  branches,
	})
}

type setQuotaBody struct {
	Month     string `json:"month"` // YYYY-MM
	QuotaDays int32  `json:"quota_days"`
}

// SetLeaveBalanceQuota — PUT /api/hr/employees/:id/leave-balance
// Overrides one month's allowance for the employee (e.g. a part month after
// joining). Lowering it below what is already used is allowed — the month then
// simply shows no unused days.
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
	month, err := time.Parse("2006-01", strings.TrimSpace(body.Month))
	if err != nil {
		respondError(w, http.StatusBadRequest, "format bulan tidak valid (YYYY-MM)")
		return
	}

	ctx := r.Context()
	pgEmpID := pgtype.UUID{Bytes: id, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgEmpID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}

	if err := h.queries.UpsertLeaveMonthlyQuota(ctx, &db.UpsertLeaveMonthlyQuotaParams{
		EmployeeID: pgEmpID,
		Month:      pgtype.Date{Time: month, Valid: true},
		QuotaDays:  body.QuotaDays,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui kuota cuti")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_balance",
		EntityID:    id,
		Description: fmt.Sprintf("Mengatur jatah cuti %s bulan %s menjadi %d hari", emp.FullName, month.Format("01/2006"), body.QuotaDays),
	})

	months, err := service.MonthlyLeaveBalances(ctx, h.queries, pgEmpID, month.Year(), today())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil saldo cuti")
		return
	}
	respondJSON(w, http.StatusOK, months[month.Month()-1])
}
