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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

func (h *LeaveHandler) requestDoctorNotes(ctx context.Context, ids []pgtype.UUID) (map[[16]byte]bool, error) {
	m := map[[16]byte]bool{}
	if len(ids) == 0 {
		return m, nil
	}
	rows, err := h.queries.ListLeaveRequestDoctorNotes(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		m[r.ID.Bytes] = r.HasDoctorNote
	}
	return m, nil
}

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
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan libur")
		return
	}
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	notes, err := h.requestDoctorNotes(ctx, ids)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan libur")
		return
	}
	needsNote, err := h.doctorNoteFlags(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan libur")
		return
	}
	out := make([]leaveRequestView, 0, len(rows))
	for _, row := range rows {
		out = append(out, leaveRequestView{ListLeaveRequestsRow: row, HasDoctorNote: notes[row.ID.Bytes],
			RequiresDoctorNote: needsNote[row.LeaveTypeID.Bytes]})
	}
	respondJSON(w, http.StatusOK, out)
}

// CreateLeaveRequest — POST /api/hr/leave-requests
// Quota types (Cuti) are approved on save or refused when short; others stay pending.
// A start before today (server date) needs confirm_backdated.
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
		respondError(w, http.StatusBadRequest, "jenis libur tidak valid")
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
			"error": "tanggal mulai sudah lewat — konfirmasi pengajuan mundur tanggal terlebih dahulu",
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

	leaveType, err := h.queries.GetLeaveTypeByID(ctx, pgTypeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "jenis libur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil jenis libur")
		return
	}

	pgStart := pgtype.Date{Time: startDate, Valid: true}
	pgEnd := pgtype.Date{Time: endDate, Valid: true}

	overlaps, err := h.queries.ListOverlappingRequests(ctx, &db.ListOverlappingRequestsParams{
		EmployeeID: pgEmpID,
		StartDate:  pgStart,
		EndDate:    pgEnd,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa tumpang tindih libur")
		return
	}
	if len(overlaps) > 0 {
		respondError(w, http.StatusConflict, "rentang tanggal bertabrakan dengan pengajuan libur lain yang masih aktif")
		return
	}

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
	pgCreatedBy := pgtype.UUID{Bytes: createdBy, Valid: createdBy != [16]byte{}}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pengajuan libur")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	req, err := qtx.CreateLeaveRequest(ctx, &db.CreateLeaveRequestParams{
		EmployeeID:  pgEmpID,
		LeaveTypeID: pgTypeID,
		StartDate:   pgStart,
		EndDate:     pgEnd,
		DayCount:    int32(dayCount),
		Reason:      reason,
		CreatedBy:   pgCreatedBy,
		IsBackdated: isBackdated,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pengajuan libur")
		return
	}
	if body.HasDoctorNote {
		if err := qtx.SetLeaveRequestDoctorNote(ctx, &db.SetLeaveRequestDoctorNoteParams{
			ID: req.ID, HasDoctorNote: true,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan pengajuan libur")
			return
		}
		req.HasDoctorNote = true
	}

	desc := fmt.Sprintf("Mengajukan libur %s (%s s/d %s, %d hari)", emp.FullName, body.StartDate, body.EndDate, dayCount)
	if leaveType.UsesQuota {
		res, err := service.ApproveLeave(ctx, qtx, req, leaveType, pgCreatedBy,
			pgtype.Text{String: "Disetujui otomatis (jatah libur bulanan)", Valid: true})
		if err != nil {
			if errors.Is(err, service.ErrQuotaExceeded) {
				respondError(w, http.StatusConflict, err.Error())
				return
			}
			respondError(w, http.StatusInternalServerError, "gagal menyimpan pengajuan libur")
			return
		}
		req = res.Request
		desc = fmt.Sprintf("Mencatat libur %s (%s s/d %s, %d hari) — disetujui otomatis, %d hari kehadiran ditandai libur",
			emp.FullName, body.StartDate, body.EndDate, dayCount, res.CoveredDays)
		if isBackdated {
			desc += " — MUNDUR TANGGAL"
		}
	} else if isBackdated {
		desc += " — MUNDUR TANGGAL, menunggu persetujuan manajer/pemilik"
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      createdBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "leave_request",
		EntityID:    req.ID.Bytes,
		Description: desc,
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pengajuan libur")
		return
	}
	respondJSON(w, http.StatusCreated, req)
}

// leaveApproveMode says who is approving: one request by hand, the leave
// page's bulk action (never backdated), or the queue-wide bulk approval, which
// may include backdated requests when the manager explicitly asks for it.
type leaveApproveMode int

const (
	leaveApproveSingle leaveApproveMode = iota
	leaveApproveBulk
	leaveApproveBulkWithBackdated
)

// approveOne runs in its own transaction.
func (h *LeaveHandler) approveOne(ctx context.Context, id [16]byte, note pgtype.Text, mode leaveApproveMode) (*service.ApproveResult, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	res, err := h.approveInTx(ctx, h.queries.WithTx(tx), id, note, mode)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return res, nil
}

// approveInTx approves one request on qtx; the caller owns the transaction.
func (h *LeaveHandler) approveInTx(ctx context.Context, qtx *db.Queries, id [16]byte, note pgtype.Text, mode leaveApproveMode) (*service.ApproveResult, error) {
	req, err := qtx.GetLeaveRequestByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errLeaveRequestNotFound
		}
		return nil, err
	}
	if req.Status != "pending" {
		return nil, errLeaveRequestNotPending
	}
	if mode == leaveApproveBulk && req.IsBackdated {
		return nil, errLeaveRequestBackdatedBulk
	}

	leaveType, err := qtx.GetLeaveTypeByID(ctx, req.LeaveTypeID)
	if err != nil {
		return nil, err
	}

	deciderID := middleware.UserIDFromCtx(ctx)
	res, err := service.ApproveLeave(ctx, qtx, req, leaveType, pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}}, note)
	if err != nil {
		return nil, err
	}

	desc := approveDescription(req, res)
	if mode == leaveApproveBulkWithBackdated {
		desc += " — persetujuan massal"
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_request",
		EntityID:    id,
		Description: desc,
	})
	return res, nil
}

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
		Description: "Menolak pengajuan libur",
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

	res, err := h.approveOne(r.Context(), id, note, leaveApproveSingle)
	if err != nil {
		switch {
		case errors.Is(err, errLeaveRequestNotFound):
			respondError(w, http.StatusNotFound, "pengajuan libur tidak ditemukan")
		case errors.Is(err, errLeaveRequestNotPending):
			respondError(w, http.StatusBadRequest, "hanya pengajuan berstatus menunggu yang dapat disetujui")
		case errors.Is(err, service.ErrQuotaExceeded):
			respondError(w, http.StatusBadRequest, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "gagal menyetujui pengajuan libur")
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
			respondError(w, http.StatusNotFound, "pengajuan libur tidak ditemukan")
		case errors.Is(err, errLeaveRequestNotPending):
			respondError(w, http.StatusBadRequest, "hanya pengajuan berstatus menunggu yang dapat ditolak")
		default:
			respondError(w, http.StatusInternalServerError, "gagal menolak pengajuan libur")
		}
		return
	}

	respondJSON(w, http.StatusOK, updated)
}

// BulkApproveLeaveRequests — POST /api/hr/leave-requests/bulk-approve (manager only)
// One transaction per request, so one failure does not block the rest.
func (h *LeaveHandler) BulkApproveLeaveRequests(w http.ResponseWriter, r *http.Request) {
	var body bulkDecisionBody
	if err := parseBody(r, &body); err != nil || len(body.IDs) == 0 {
		respondError(w, http.StatusBadRequest, "daftar ID pengajuan libur wajib diisi")
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
		if _, err := h.approveOne(ctx, id, note, leaveApproveBulk); err != nil {
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
		respondError(w, http.StatusBadRequest, "daftar ID pengajuan libur wajib diisi")
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

func bulkErrorMessage(err error) string {
	if errors.Is(err, service.ErrQuotaExceeded) ||
		errors.Is(err, errLeaveRequestNotFound) ||
		errors.Is(err, errLeaveRequestNotPending) ||
		errors.Is(err, errLeaveRequestBackdatedBulk) {
		return err.Error()
	}
	return "gagal memproses pengajuan libur"
}

func approveDescription(req *db.LeaveRequest, res *service.ApproveResult) string {
	desc := fmt.Sprintf("Menyetujui pengajuan libur (%d hari kehadiran ditandai libur)", res.CoveredDays)
	if req.IsBackdated {
		desc = "Menyetujui pengajuan libur MUNDUR TANGGAL " +
			req.StartDate.Time.Format("2006-01-02") + " s/d " + req.EndDate.Time.Format("2006-01-02") +
			fmt.Sprintf(" (%d hari kehadiran ditandai libur)", res.CoveredDays)
	}
	return desc
}

// CancelLeaveRequest — POST /api/hr/leave-requests/:id/cancel (admin/manager)
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
			respondError(w, http.StatusNotFound, "pengajuan libur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan libur")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, req.EmployeeID) {
		return
	}
	if req.Status == "cancelled" || req.Status == "rejected" {
		respondError(w, http.StatusBadRequest, "pengajuan libur sudah tidak aktif")
		return
	}

	leaveType, err := h.queries.GetLeaveTypeByID(ctx, req.LeaveTypeID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil jenis libur")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if req.Status == "approved" {
		if err := service.CancelApprovedLeave(ctx, qtx, req, leaveType, service.TodayWIB()); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membatalkan efek persetujuan libur")
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
		respondError(w, http.StatusInternalServerError, "gagal membatalkan pengajuan libur")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_request",
		EntityID:    id,
		Description: "Membatalkan pengajuan libur",
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, updated)
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
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat libur")
		return
	}
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	notes, err := h.requestDoctorNotes(ctx, ids)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil riwayat libur")
		return
	}
	type view struct {
		*db.ListLeaveRequestsByEmployeeRow
		HasDoctorNote bool `json:"has_doctor_note"`
	}
	out := make([]view, 0, len(rows))
	for _, row := range rows {
		out = append(out, view{ListLeaveRequestsByEmployeeRow: row, HasDoctorNote: notes[row.ID.Bytes]})
	}
	respondJSON(w, http.StatusOK, out)
}

// SetDoctorNote — PUT /api/hr/leave-requests/:id/doctor-note
// Re-judges past days at once; generated payroll lines need a regenerate.
func (h *LeaveHandler) SetDoctorNote(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body struct {
		HasDoctorNote bool `json:"has_doctor_note"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	req, err := qtx.GetLeaveRequestByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "pengajuan libur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil pengajuan libur")
		return
	}
	if err := qtx.SetLeaveRequestDoctorNote(ctx, &db.SetLeaveRequestDoctorNoteParams{
		ID: pgID, HasDoctorNote: body.HasDoctorNote,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan surat dokter")
		return
	}
	if req.Status == "approved" {
		if err := service.ReevaluateLeaveRequest(ctx, qtx, req); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengevaluasi ulang poin")
			return
		}
	}
	state := "tidak ada"
	if body.HasDoctorNote {
		state = "ada"
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "leave_request",
		EntityID:    id,
		Description: fmt.Sprintf("Menandai surat keterangan dokter: %s", state),
	})
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"id": req.ID, "has_doctor_note": body.HasDoctorNote})
}
