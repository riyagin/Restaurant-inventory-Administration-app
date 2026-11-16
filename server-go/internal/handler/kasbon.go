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

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type KasbonHandler struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	uploadsDir string
}

func NewKasbonHandler(pool *pgxpool.Pool, queries *db.Queries) *KasbonHandler {
	return &KasbonHandler{pool: pool, queries: queries}
}

// SetUploadsDir injects the uploads directory into the handler.
func (h *KasbonHandler) SetUploadsDir(dir string) {
	h.uploadsDir = dir
}

func (h *KasbonHandler) resolveUploadsDir() string {
	if h.uploadsDir != "" {
		return h.uploadsDir
	}
	return filepath.Join("..", "server", "uploads")
}

// parseMonth parses a "YYYY-MM" or "YYYY-MM-DD" string into a first-of-month UTC date.
func parseMonth(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if len(s) == 7 {
		s += "-01"
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	return service.FirstOfMonth(t), nil
}

// kasbonInstallmentInput is the request payload for one installment row.
type kasbonInstallmentInput struct {
	DueMonth string `json:"due_month"`
	Amount   int64  `json:"amount"`
}

type kasbonBody struct {
	EmployeeID          string                   `json:"employee_id"`
	Amount              int64                    `json:"amount"`
	Details             string                   `json:"details"`
	SendingMethod       string                   `json:"sending_method"`
	FundSourceAccountID string                   `json:"fund_source_account_id"`
	ResolutionMonth     string                   `json:"resolution_month"`
	Installments        []kasbonInstallmentInput `json:"installments"`
}

// kasbonView is a kasbon plus its installments and (optionally) the employee's last
// resolved kasbon info.
type kasbonView struct {
	*db.Kasbon
	Installments []*db.KasbonInstallment      `json:"installments"`
	LastResolved *db.GetLastResolvedKasbonRow `json:"last_resolved,omitempty"`
	// Kebijakan Kasbon (migration 088): the score the request was judged on
	// and its track — priority (≥ 90) or evaluation (70–89).
	ScoreAtRequest pgtype.Int4 `json:"score_at_request"`
	ApprovalTrack  pgtype.Text `json:"approval_track"`
}

// kasbonListRow adds the policy check to a listed kasbon.
type kasbonListRow struct {
	*db.ListKasbonsRow
	ScoreAtRequest pgtype.Int4 `json:"score_at_request"`
	ApprovalTrack  pgtype.Text `json:"approval_track"`
}

// policyRefusal maps a Kebijakan Kasbon refusal to 422; ok=false when err is
// some other error.
func policyRefusal(w http.ResponseWriter, err error) bool {
	if errors.Is(err, service.ErrKasbonPolicy) {
		respondError(w, http.StatusUnprocessableEntity, err.Error())
		return true
	}
	return false
}

// checkKasbonPolicy runs Kebijakan Kasbon for a request. excludeID is the
// kasbon being edited (zero UUID on create).
func (h *KasbonHandler) checkKasbonPolicy(r *http.Request, emp *db.GetEmployeeByIDRow, excludeID pgtype.UUID,
	requestDate time.Time, amount int64, installments []service.InstallmentInput) (string, int, error) {
	ctx := r.Context()
	settings, err := service.LoadPolicySettings(ctx, h.queries)
	if err != nil {
		return "", 0, err
	}
	if !excludeID.Valid {
		excludeID = pgtype.UUID{Valid: true}
	}
	open, err := h.queries.CountOpenKasbonsForEmployee(ctx, &db.CountOpenKasbonsForEmployeeParams{
		EmployeeID: emp.ID, ExcludeID: excludeID,
	})
	if err != nil {
		return "", 0, err
	}
	score, err := service.CurrentMonthScore(ctx, h.queries, emp.ID, requestDate)
	if err != nil {
		return "", 0, err
	}
	track, err := service.CheckKasbonEligibility(settings, service.KasbonEligibilityInput{
		RequestDate:    requestDate,
		Amount:         amount,
		EmploymentType: emp.EmploymentType,
		OpenKasbons:    int(open),
		Score:          score,
		Installments:   installments,
	})
	return track, score, err
}

// Eligibility — GET /api/hr/kasbons/eligibility?employee_id=
// Previews Kebijakan Kasbon for the form before the request is filed: the
// settings, the employee's score this month, and the first rule that would
// refuse a request today (amount and installments are judged on submit).
func (h *KasbonHandler) Eligibility(w http.ResponseWriter, r *http.Request) {
	empID, err := parseUUID(strings.TrimSpace(r.URL.Query().Get("employee_id")))
	if err != nil {
		respondError(w, http.StatusBadRequest, "karyawan tidak valid")
		return
	}
	pgEmpID := pgtype.UUID{Bytes: empID, Valid: true}
	if !requireEmployeeInScope(w, r, h.queries, pgEmpID) {
		return
	}
	ctx := r.Context()
	emp, err := h.queries.GetEmployeeByID(ctx, pgEmpID)
	if err != nil {
		respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
		return
	}
	settings, err := service.LoadPolicySettings(ctx, h.queries)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memuat kebijakan kasbon")
		return
	}
	today := service.TodayWIB()
	track, score, err := h.checkKasbonPolicy(r, emp, pgtype.UUID{}, today, 0, nil)
	reason := ""
	if err != nil {
		if !errors.Is(err, service.ErrKasbonPolicy) {
			respondError(w, http.StatusInternalServerError, "gagal memeriksa kebijakan kasbon")
			return
		}
		reason = err.Error()
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"eligible":        reason == "",
		"reason":          reason,
		"score":           score,
		"track":           track,
		"employment_type": emp.EmploymentType,
		"request_date":    today.Format("2006-01-02"),
		"settings":        settings,
	})
}

// buildInstallments validates the split (or builds a single default installment in
// resolution_month) against the total and the resolution window.
func buildInstallments(total int64, requestDate, resolutionMonth time.Time, raw []kasbonInstallmentInput) ([]service.InstallmentInput, error) {
	if len(raw) == 0 {
		return []service.InstallmentInput{{DueMonth: resolutionMonth, Amount: total}}, nil
	}
	out := make([]service.InstallmentInput, 0, len(raw))
	for _, r := range raw {
		dm, err := parseMonth(r.DueMonth)
		if err != nil {
			return nil, errors.New("format bulan cicilan tidak valid")
		}
		out = append(out, service.InstallmentInput{DueMonth: dm, Amount: r.Amount})
	}
	if err := service.ValidateInstallmentSplit(total, requestDate, out); err != nil {
		return nil, err
	}
	return out, nil
}

// List — GET /api/hr/kasbons?status=&employee_id=&q=
func (h *KasbonHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	params := &db.ListKasbonsParams{
		Status: strings.TrimSpace(q.Get("status")),
		Q:      strings.TrimSpace(q.Get("q")),
	}
	if v := strings.TrimSpace(q.Get("employee_id")); v != "" {
		eid, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "employee_id tidak valid")
			return
		}
		params.EmployeeID = pgtype.UUID{Bytes: eid, Valid: true}
	}
	if b, ok := scopedBranch(r); ok {
		params.BranchID = b
	}

	rows, err := h.queries.ListKasbons(ctx, params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kasbon")
		return
	}
	checks, err := h.queries.ListKasbonPolicyChecks(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kasbon")
		return
	}
	byID := make(map[[16]byte]*db.ListKasbonPolicyChecksRow, len(checks))
	for _, c := range checks {
		byID[c.ID.Bytes] = c
	}
	out := make([]kasbonListRow, 0, len(rows))
	for _, row := range rows {
		v := kasbonListRow{ListKasbonsRow: row}
		if c := byID[row.ID.Bytes]; c != nil {
			v.ScoreAtRequest, v.ApprovalTrack = c.ScoreAtRequest, c.ApprovalTrack
		}
		out = append(out, v)
	}
	respondJSON(w, http.StatusOK, out)
}

// Get — GET /api/hr/kasbons/:id (incl. installments + last resolved info)
func (h *KasbonHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	k, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, k.EmployeeID) {
		return
	}

	installments, err := h.queries.ListKasbonInstallments(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil cicilan kasbon")
		return
	}
	if installments == nil {
		installments = []*db.KasbonInstallment{}
	}

	lastResolved, _ := service.LastResolvedKasbon(ctx, h.queries, k.EmployeeID)

	view := kasbonView{Kasbon: k, Installments: installments, LastResolved: lastResolved}
	if c, err := h.queries.GetKasbonPolicyCheck(ctx, pgID); err == nil && c != nil {
		view.ScoreAtRequest, view.ApprovalTrack = c.ScoreAtRequest, c.ApprovalTrack
	}
	respondJSON(w, http.StatusOK, view)
}

// Create — POST /api/hr/kasbons
func (h *KasbonHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body kasbonBody
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
	fundID, err := parseUUID(strings.TrimSpace(body.FundSourceAccountID))
	if err != nil {
		respondError(w, http.StatusBadRequest, "sumber dana tidak valid")
		return
	}
	if body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah kasbon harus lebih dari 0")
		return
	}
	body.Details = strings.TrimSpace(body.Details)
	if body.Details == "" {
		respondError(w, http.StatusBadRequest, "keterangan kasbon wajib diisi")
		return
	}
	body.SendingMethod = strings.TrimSpace(body.SendingMethod)
	if body.SendingMethod == "" {
		respondError(w, http.StatusBadRequest, "metode pengiriman wajib diisi")
		return
	}
	resolutionMonth, err := parseMonth(body.ResolutionMonth)
	if err != nil {
		respondError(w, http.StatusBadRequest, "bulan penyelesaian tidak valid")
		return
	}

	requestDateRaw := service.TodayWIB() // the request is dated on the WIB business day
	if err := service.ValidateResolutionWindow(requestDateRaw, resolutionMonth); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	installments, err := buildInstallments(body.Amount, requestDateRaw, resolutionMonth, body.Installments)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	pgEmpID := pgtype.UUID{Bytes: empID, Valid: true}

	emp, err := h.queries.GetEmployeeByID(ctx, pgEmpID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	if _, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: fundID, Valid: true}); err != nil {
		respondError(w, http.StatusBadRequest, "akun sumber dana tidak ditemukan")
		return
	}

	// Kebijakan Kasbon: window, PKWTT, no running kasbon, cap, installments and
	// the score gate. The track is frozen on the kasbon for the approver.
	track, score, err := h.checkKasbonPolicy(r, emp, pgtype.UUID{}, requestDateRaw, body.Amount, installments)
	if err != nil {
		if !policyRefusal(w, err) {
			respondError(w, http.StatusInternalServerError, "gagal memeriksa kebijakan kasbon")
		}
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	year := requestDateRaw.Year()
	maxSeq, err := qtx.GetMaxKasbonSeqForYear(ctx, fmt.Sprintf("%04d", year))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor kasbon")
		return
	}
	number := service.GenerateKasbonNumber(year, maxSeq)

	createdBy := middleware.UserIDFromCtx(ctx)
	k, err := qtx.CreateKasbon(ctx, &db.CreateKasbonParams{
		KasbonNumber:        number,
		EmployeeID:          pgEmpID,
		Amount:              body.Amount,
		Details:             body.Details,
		SendingMethod:       body.SendingMethod,
		FundSourceAccountID: pgtype.UUID{Bytes: fundID, Valid: true},
		RequestDate:         pgtype.Date{Time: requestDateRaw, Valid: true},
		ResolutionMonth:     pgtype.Date{Time: resolutionMonth, Valid: true},
		CreatedBy:           pgtype.UUID{Bytes: createdBy, Valid: createdBy != [16]byte{}},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kasbon")
		return
	}

	for _, ins := range installments {
		if _, err := qtx.CreateKasbonInstallment(ctx, &db.CreateKasbonInstallmentParams{
			KasbonID: k.ID,
			DueMonth: pgtype.Date{Time: service.FirstOfMonth(ins.DueMonth), Valid: true},
			Amount:   ins.Amount,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan cicilan kasbon")
			return
		}
	}

	if err := qtx.SetKasbonPolicyCheck(ctx, &db.SetKasbonPolicyCheckParams{
		ID:             k.ID,
		ScoreAtRequest: pgtype.Int4{Int32: int32(score), Valid: true},
		ApprovalTrack:  pgtype.Text{String: track, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kasbon")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      createdBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "kasbon",
		EntityID:    k.ID.Bytes,
		Description: fmt.Sprintf("Mengajukan kasbon %s untuk %s (Rp %d)", number, emp.FullName, body.Amount),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, k)
}

// Update — PUT /api/hr/kasbons/:id (pending only)
func (h *KasbonHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body kasbonBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	existing, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !service.CanEdit(existing.Status) {
		respondError(w, http.StatusBadRequest, "hanya kasbon berstatus menunggu yang dapat diubah")
		return
	}

	fundID, err := parseUUID(strings.TrimSpace(body.FundSourceAccountID))
	if err != nil {
		respondError(w, http.StatusBadRequest, "sumber dana tidak valid")
		return
	}
	if body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah kasbon harus lebih dari 0")
		return
	}
	body.Details = strings.TrimSpace(body.Details)
	if body.Details == "" {
		respondError(w, http.StatusBadRequest, "keterangan kasbon wajib diisi")
		return
	}
	body.SendingMethod = strings.TrimSpace(body.SendingMethod)
	if body.SendingMethod == "" {
		respondError(w, http.StatusBadRequest, "metode pengiriman wajib diisi")
		return
	}
	resolutionMonth, err := parseMonth(body.ResolutionMonth)
	if err != nil {
		respondError(w, http.StatusBadRequest, "bulan penyelesaian tidak valid")
		return
	}

	// Validate window/split against the original request date.
	requestDate := existing.RequestDate.Time
	if err := service.ValidateResolutionWindow(requestDate, resolutionMonth); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	installments, err := buildInstallments(body.Amount, requestDate, resolutionMonth, body.Installments)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if _, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: fundID, Valid: true}); err != nil {
		respondError(w, http.StatusBadRequest, "akun sumber dana tidak ditemukan")
		return
	}

	// Re-judge an edited request against Kebijakan Kasbon, as of its original
	// request date (so the window still holds).
	emp, err := h.queries.GetEmployeeByID(ctx, existing.EmployeeID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data karyawan")
		return
	}
	track, score, err := h.checkKasbonPolicy(r, emp, existing.ID, requestDate, body.Amount, installments)
	if err != nil {
		if !policyRefusal(w, err) {
			respondError(w, http.StatusInternalServerError, "gagal memeriksa kebijakan kasbon")
		}
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if err := qtx.SetKasbonPolicyCheck(ctx, &db.SetKasbonPolicyCheckParams{
		ID:             existing.ID,
		ScoreAtRequest: pgtype.Int4{Int32: int32(score), Valid: true},
		ApprovalTrack:  pgtype.Text{String: track, Valid: true},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui kasbon")
		return
	}

	updated, err := qtx.UpdateKasbon(ctx, &db.UpdateKasbonParams{
		Amount:              body.Amount,
		Details:             body.Details,
		SendingMethod:       body.SendingMethod,
		FundSourceAccountID: pgtype.UUID{Bytes: fundID, Valid: true},
		ResolutionMonth:     pgtype.Date{Time: resolutionMonth, Valid: true},
		ID:                  pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui kasbon")
		return
	}

	// Rebuild installments.
	if err := qtx.DeleteKasbonInstallments(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui cicilan kasbon")
		return
	}
	for _, ins := range installments {
		if _, err := qtx.CreateKasbonInstallment(ctx, &db.CreateKasbonInstallmentParams{
			KasbonID: pgID,
			DueMonth: pgtype.Date{Time: service.FirstOfMonth(ins.DueMonth), Valid: true},
			Amount:   ins.Amount,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyimpan cicilan kasbon")
			return
		}
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "kasbon",
		EntityID:    id,
		Description: fmt.Sprintf("Memperbarui kasbon %s", updated.KasbonNumber),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, updated)
}

// Approve — POST /api/hr/kasbons/:id/approve (manager only)
// The approve body MAY adjust the installment split; it is re-validated.
func (h *KasbonHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body struct {
		Note         string                   `json:"note"`
		Installments []kasbonInstallmentInput `json:"installments"`
	}
	_ = parseBody(r, &body)

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	k, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !service.CanApprove(k.Status) {
		respondError(w, http.StatusBadRequest, "hanya kasbon berstatus menunggu yang dapat disetujui")
		return
	}

	// Optionally re-validate an adjusted split.
	var newInstallments []service.InstallmentInput
	if len(body.Installments) > 0 {
		newInstallments, err = buildInstallments(k.Amount, k.RequestDate.Time, k.ResolutionMonth.Time, body.Installments)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		settings, err := service.LoadPolicySettings(ctx, h.queries)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memuat kebijakan kasbon")
			return
		}
		if err := service.CheckKasbonInstallmentPolicy(settings, k.RequestDate.Time, newInstallments); err != nil {
			respondError(w, http.StatusUnprocessableEntity, err.Error())
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

	deciderID := middleware.UserIDFromCtx(ctx)
	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	if newInstallments != nil {
		if err := qtx.DeleteKasbonInstallments(ctx, pgID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui cicilan kasbon")
			return
		}
		for _, ins := range newInstallments {
			if _, err := qtx.CreateKasbonInstallment(ctx, &db.CreateKasbonInstallmentParams{
				KasbonID: pgID,
				DueMonth: pgtype.Date{Time: service.FirstOfMonth(ins.DueMonth), Valid: true},
				Amount:   ins.Amount,
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan cicilan kasbon")
				return
			}
		}
	}

	updated, err := qtx.ApproveKasbon(ctx, &db.ApproveKasbonParams{
		ApprovedBy:   pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}},
		ApprovalNote: note,
		ID:           pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyetujui kasbon")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "kasbon",
		EntityID:    id,
		Description: fmt.Sprintf("Menyetujui kasbon %s", updated.KasbonNumber),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, updated)
}

// Reject — POST /api/hr/kasbons/:id/reject (manager only)
func (h *KasbonHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = parseBody(r, &body)

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	k, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !service.CanApprove(k.Status) {
		respondError(w, http.StatusBadRequest, "hanya kasbon berstatus menunggu yang dapat ditolak")
		return
	}

	deciderID := middleware.UserIDFromCtx(ctx)
	note := pgtype.Text{}
	if s := strings.TrimSpace(body.Note); s != "" {
		note = pgtype.Text{String: s, Valid: true}
	}

	updated, err := h.queries.RejectKasbon(ctx, &db.RejectKasbonParams{
		ApprovedBy:   pgtype.UUID{Bytes: deciderID, Valid: deciderID != [16]byte{}},
		ApprovalNote: note,
		ID:           pgID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menolak kasbon")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      deciderID,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "kasbon",
		EntityID:    id,
		Description: fmt.Sprintf("Menolak kasbon %s", updated.KasbonNumber),
	})

	respondJSON(w, http.StatusOK, updated)
}

// Cancel — POST /api/hr/kasbons/:id/cancel (pending/approved only)
func (h *KasbonHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	k, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, k.EmployeeID) {
		return
	}
	if !service.CanCancel(k.Status) {
		respondError(w, http.StatusBadRequest, "kasbon hanya dapat dibatalkan saat berstatus menunggu atau disetujui")
		return
	}

	updated, err := h.queries.SetKasbonCancelled(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan kasbon")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "kasbon",
		EntityID:    id,
		Description: fmt.Sprintf("Membatalkan kasbon %s", updated.KasbonNumber),
	})

	respondJSON(w, http.StatusOK, updated)
}

// Process — POST /api/hr/kasbons/:id/process (multipart, optional photo)
// In one transaction: status → processed, debit fund source, credit Piutang Karyawan.
func (h *KasbonHandler) Process(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	k, err := h.queries.GetKasbonByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kasbon tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil kasbon")
		return
	}
	if !service.CanProcess(k.Status) {
		respondError(w, http.StatusBadRequest, "hanya kasbon yang sudah disetujui yang dapat diproses")
		return
	}

	// Optional multipart photo evidence.
	evidence := pgtype.Text{}
	if err := r.ParseMultipartForm(20 << 20); err == nil {
		if file, header, ferr := r.FormFile("photo"); ferr == nil {
			defer file.Close()
			ext := strings.ToLower(filepath.Ext(header.Filename))
			switch ext {
			case ".jpg", ".jpeg", ".png":
			default:
				respondError(w, http.StatusBadRequest, "format file tidak didukung (jpg, jpeg, png)")
				return
			}
			filename := fmt.Sprintf("kasbon-%s-%d%s", id.String(), time.Now().Unix(), ext)
			uploadsDir := h.resolveUploadsDir()
			if err := os.MkdirAll(uploadsDir, 0755); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal membuat direktori upload")
				return
			}
			dst, cerr := os.Create(filepath.Join(uploadsDir, filename))
			if cerr != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan file")
				return
			}
			if _, werr := io.Copy(dst, file); werr != nil {
				dst.Close()
				respondError(w, http.StatusInternalServerError, "gagal menulis file")
				return
			}
			dst.Close()
			evidence = pgtype.Text{String: filename, Valid: true}
		}
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	processedBy := middleware.UserIDFromCtx(ctx)
	updated, err := service.ProcessKasbon(ctx, qtx, k,
		pgtype.UUID{Bytes: processedBy, Valid: processedBy != [16]byte{}}, evidence)
	if err != nil {
		if errors.Is(err, service.ErrPiutangAccountMissing) {
			respondError(w, http.StatusInternalServerError, "akun sistem 'Piutang Karyawan' tidak ditemukan; jalankan migrasi 011")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memproses kasbon")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      processedBy,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "kasbon",
		EntityID:    id,
		Description: fmt.Sprintf("Memproses kasbon %s (mendebit sumber dana Rp %d)", updated.KasbonNumber, updated.Amount),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusOK, updated)
}
