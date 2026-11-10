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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// BenefitFundHandler serves Dana Kesejahteraan Karyawan (health benefit + pension
// fund). Business rules live in service/benefit_fund.go.
type BenefitFundHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewBenefitFundHandler(pool *pgxpool.Pool, queries *db.Queries) *BenefitFundHandler {
	return &BenefitFundHandler{pool: pool, queries: queries}
}

// benefitErrStatus maps the service's rule violations to 409/400; anything else 500.
func benefitErrStatus(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrBenefitMonthClosed),
		errors.Is(err, service.ErrBenefitKasbonLocked),
		errors.Is(err, service.ErrBenefitStillEmployed),
		errors.Is(err, service.ErrBenefitNothingToPay),
		errors.Is(err, service.ErrBenefitUnsettled):
		return http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrBenefitNoAllowance),
		errors.Is(err, service.ErrBenefitFutureDate):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, service.ErrBenefitAccountsGone):
		return http.StatusInternalServerError, err.Error()
	}
	return http.StatusInternalServerError, "gagal memproses dana kesejahteraan: " + err.Error()
}

func parseISODate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", strings.TrimSpace(s))
}

// asOfForMonth picks the date whose wage structure describes a month: today for
// the current month, the month's last day for a past one, its first for a future one.
func asOfForMonth(month time.Time) time.Time {
	start, end := service.PeriodBounds(month)
	today := service.TodayWIB()
	switch {
	case today.Before(start):
		return start
	case today.After(end):
		return end
	}
	return today
}

func monthParam(r *http.Request) (time.Time, error) {
	if v := strings.TrimSpace(r.URL.Query().Get("month")); v != "" {
		return parseMonth(v)
	}
	return service.FirstOfMonth(service.TodayWIB()), nil
}

// Summary — GET /api/hr/benefit-fund/summary?month=YYYY-MM
func (h *BenefitFundHandler) Summary(w http.ResponseWriter, r *http.Request) {
	month, err := monthParam(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "bulan tidak valid")
		return
	}
	ctx := r.Context()
	rows, err := h.queries.ListBenefitFundSummary(ctx, &db.ListBenefitFundSummaryParams{
		AsOf:        pgtype.Date{Time: asOfForMonth(month), Valid: true},
		PeriodMonth: pgtype.Date{Time: month, Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil ringkasan dana kesejahteraan")
		return
	}
	if rows == nil {
		rows = []*db.ListBenefitFundSummaryRow{}
	}
	var monthOpen = true
	if p, err := h.queries.GetPayrollPeriodByMonth(ctx, pgtype.Date{Time: month, Valid: true}); err == nil {
		monthOpen = p.Status == "open"
	}
	var totHealth, totPension, totMonthly, totCovered int64
	for _, x := range rows {
		totHealth += x.HealthBalance
		totPension += x.PensionBalance
		totMonthly += x.MonthlyAmount
		totCovered += x.CoveredThisMonth
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"month":      month.Format("2006-01"),
		"month_open": monthOpen,
		"rows":       rows,
		"totals": map[string]int64{
			"monthly_amount":     totMonthly,
			"covered_this_month": totCovered,
			"health_balance":     totHealth,
			"pension_balance":    totPension,
		},
	})
}

// EmployeeDetail — GET /api/hr/benefit-fund/employees/{id}?month=YYYY-MM
func (h *BenefitFundHandler) EmployeeDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	month, err := monthParam(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "bulan tidak valid")
		return
	}
	ctx := r.Context()
	empID := pgtype.UUID{Bytes: id, Valid: true}
	emp, err := h.queries.GetEmployeeByID(ctx, empID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil karyawan")
		return
	}
	bal, err := h.queries.GetBenefitBalances(ctx, empID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil saldo")
		return
	}
	allowance, err := service.GetMonthAllowance(ctx, h.queries, empID, asOfForMonth(month))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil jatah bulan ini")
		return
	}
	movements, err := h.queries.ListBenefitMovementsByEmployee(ctx, empID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil mutasi")
		return
	}
	claims, err := h.queries.ListBenefitClaims(ctx, &db.ListBenefitClaimsParams{EmployeeID: empID})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil klaim")
		return
	}
	unsettled, _ := h.queries.CountUnsettledPayrollForEmployee(ctx, empID)
	if movements == nil {
		movements = []*db.ListBenefitMovementsByEmployeeRow{}
	}
	if claims == nil {
		claims = []*db.ListBenefitClaimsRow{}
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"employee": map[string]any{
			"id": emp.ID, "employee_code": emp.EmployeeCode, "full_name": emp.FullName,
			"status": emp.Status, "join_date": emp.JoinDate, "resign_date": emp.ResignDate,
			"branch_name": emp.BranchName,
		},
		"health_balance":     bal.HealthBalance,
		"pension_balance":    bal.PensionBalance,
		"allowance":          allowance,
		"unsettled_payrolls": unsettled,
		"movements":          movements,
		"claims":             claims,
	})
}

// ListClaims — GET /api/hr/benefit-fund/claims?month=YYYY-MM&employee_id=
func (h *BenefitFundHandler) ListClaims(w http.ResponseWriter, r *http.Request) {
	params := &db.ListBenefitClaimsParams{}
	q := r.URL.Query()
	if v := strings.TrimSpace(q.Get("month")); v != "" {
		m, err := parseMonth(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "bulan tidak valid")
			return
		}
		params.PeriodMonth = pgtype.Date{Time: m, Valid: true}
	}
	if v := strings.TrimSpace(q.Get("employee_id")); v != "" {
		id, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "employee_id tidak valid")
			return
		}
		params.EmployeeID = pgtype.UUID{Bytes: id, Valid: true}
	}
	rows, err := h.queries.ListBenefitClaims(r.Context(), params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil klaim")
		return
	}
	if rows == nil {
		rows = []*db.ListBenefitClaimsRow{}
	}
	respondJSON(w, http.StatusOK, rows)
}

// PreviewClaim — GET /api/hr/benefit-fund/claims/preview?employee_id=&claim_date=&amount=
func (h *BenefitFundHandler) PreviewClaim(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, err := parseUUID(q.Get("employee_id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "karyawan tidak valid")
		return
	}
	date, err := parseISODate(q.Get("claim_date"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal klaim tidak valid")
		return
	}
	amount, _ := strconv.ParseInt(q.Get("amount"), 10, 64)
	if amount < 0 {
		amount = 0
	}
	split, allowance, err := service.PreviewClaim(r.Context(), h.queries, pgtype.UUID{Bytes: id, Valid: true}, date, amount)
	if err != nil {
		st, msg := benefitErrStatus(err)
		respondError(w, st, msg)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"split": split, "allowance": allowance})
}

type benefitClaimBody struct {
	EmployeeID          string `json:"employee_id"`
	ClaimDate           string `json:"claim_date"`
	Amount              int64  `json:"amount"`
	Description         string `json:"description"`
	FundSourceAccountID string `json:"fund_source_account_id"`
	LeaveRequestID      string `json:"leave_request_id"`
	// Only used when the claim exceeds the month's remaining allowance.
	KasbonSendingMethod   string                   `json:"kasbon_sending_method"`
	KasbonResolutionMonth string                   `json:"kasbon_resolution_month"`
	KasbonInstallments    []kasbonInstallmentInput `json:"kasbon_installments"`
}

// CreateClaim — POST /api/hr/benefit-fund/claims
func (h *BenefitFundHandler) CreateClaim(w http.ResponseWriter, r *http.Request) {
	var body benefitClaimBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	empUUID, err := parseUUID(body.EmployeeID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "karyawan tidak valid")
		return
	}
	empID := pgtype.UUID{Bytes: empUUID, Valid: true}
	if !requireEmployeeInScope(w, r, h.queries, empID) {
		return
	}
	claimDate, err := parseISODate(body.ClaimDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal klaim tidak valid")
		return
	}
	if body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "nominal klaim harus lebih dari 0")
		return
	}
	body.Description = strings.TrimSpace(body.Description)
	if body.Description == "" {
		respondError(w, http.StatusBadRequest, "keterangan klaim wajib diisi")
		return
	}
	fundID, err := parseUUID(body.FundSourceAccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "sumber dana tidak valid")
		return
	}
	var leaveID pgtype.UUID
	if s := strings.TrimSpace(body.LeaveRequestID); s != "" {
		lid, err := parseUUID(s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "pengajuan cuti tidak valid")
			return
		}
		leaveID = pgtype.UUID{Bytes: lid, Valid: true}
	}

	ctx := r.Context()
	emp, err := h.queries.GetEmployeeByID(ctx, empID)
	if err != nil {
		respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
		return
	}
	if _, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: fundID, Valid: true}); err != nil {
		respondError(w, http.StatusBadRequest, "akun sumber dana tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	// Serialise claims per employee: two claims entered at once must not both see
	// the full month's allowance.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('benefit_claim:' || $1::text))`, empUUID.String()); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengunci data karyawan")
		return
	}
	qtx := h.queries.WithTx(tx)

	split, _, err := service.PreviewClaim(ctx, qtx, empID, claimDate, body.Amount)
	if err != nil {
		st, msg := benefitErrStatus(err)
		respondError(w, st, msg)
		return
	}

	in := service.ClaimInput{
		EmployeeID:          empID,
		EmployeeName:        emp.FullName,
		ClaimDate:           claimDate,
		TotalAmount:         body.Amount,
		FundSourceAccountID: fundID,
		Description:         body.Description,
		LeaveRequestID:      leaveID,
		Actor:               middleware.UserIDFromCtx(ctx),
	}
	if split.Excess > 0 {
		in.KasbonSendingMethod = strings.TrimSpace(body.KasbonSendingMethod)
		if in.KasbonSendingMethod == "" {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("klaim melebihi sisa jatah bulan ini sebesar Rp %d — metode pengiriman kasbon wajib diisi", split.Excess))
			return
		}
		resMonth := service.FirstOfMonth(service.TodayWIB()).AddDate(0, 1, 0)
		if s := strings.TrimSpace(body.KasbonResolutionMonth); s != "" {
			if resMonth, err = parseMonth(s); err != nil {
				respondError(w, http.StatusBadRequest, "bulan penyelesaian kasbon tidak valid")
				return
			}
		}
		reqDate := service.TodayWIB()
		if err := service.ValidateResolutionWindow(reqDate, resMonth); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		ins, err := buildInstallments(split.Excess, reqDate, resMonth, body.KasbonInstallments)
		if err != nil {
			respondError(w, http.StatusBadRequest, "cicilan kasbon: "+err.Error())
			return
		}
		in.KasbonResolutionMonth = resMonth
		in.KasbonInstallments = ins
	}

	res, err := service.CreateClaim(ctx, qtx, in)
	if err != nil {
		st, msg := benefitErrStatus(err)
		respondError(w, st, msg)
		return
	}

	desc := fmt.Sprintf("Klaim kesehatan %s untuk %s: Rp %d (ditanggung Rp %d", res.Claim.ClaimNumber, emp.FullName, body.Amount, split.Covered)
	if res.Kasbon != nil {
		desc += fmt.Sprintf(", kasbon %s Rp %d", res.Kasbon.KasbonNumber, split.Excess)
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      in.Actor,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "benefit_claim",
		EntityID:    res.Claim.ID.Bytes,
		Description: desc + ")",
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan klaim")
		return
	}
	respondJSON(w, http.StatusCreated, res)
}

// CancelClaim — POST /api/hr/benefit-fund/claims/{id}/cancel  {note}
func (h *BenefitFundHandler) CancelClaim(w http.ResponseWriter, r *http.Request) {
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
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	claim, err := qtx.GetBenefitClaimByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "klaim tidak ditemukan")
		return
	}
	if !requireEmployeeInScope(w, r, h.queries, claim.EmployeeID) {
		return
	}
	actor := middleware.UserIDFromCtx(ctx)
	updated, err := service.CancelClaim(ctx, qtx, claim, actor, strings.TrimSpace(body.Note))
	if err != nil {
		st, msg := benefitErrStatus(err)
		respondError(w, st, msg)
		return
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      actor,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "UPDATE",
		EntityType:  "benefit_claim",
		EntityID:    claim.ID.Bytes,
		Description: fmt.Sprintf("Membatalkan klaim kesehatan %s", claim.ClaimNumber),
	})
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pembatalan")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// Payout — POST /api/hr/benefit-fund/employees/{id}/payout
// {payout_date, fund_source_account_id, note}. Manager-only (routing).
func (h *BenefitFundHandler) Payout(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body struct {
		PayoutDate          string `json:"payout_date"`
		FundSourceAccountID string `json:"fund_source_account_id"`
		Note                string `json:"note"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	date := service.TodayWIB()
	if s := strings.TrimSpace(body.PayoutDate); s != "" {
		if date, err = parseISODate(s); err != nil {
			respondError(w, http.StatusBadRequest, "tanggal pencairan tidak valid")
			return
		}
	}
	fundID, err := parseUUID(body.FundSourceAccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "sumber dana tidak valid")
		return
	}

	ctx := r.Context()
	empID := pgtype.UUID{Bytes: id, Valid: true}
	emp, err := h.queries.GetEmployeeByID(ctx, empID)
	if err != nil {
		respondError(w, http.StatusNotFound, "karyawan tidak ditemukan")
		return
	}
	if _, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: fundID, Valid: true}); err != nil {
		respondError(w, http.StatusBadRequest, "akun sumber dana tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('benefit_claim:' || $1::text))`, id.String()); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengunci data karyawan")
		return
	}
	qtx := h.queries.WithTx(tx)

	actor := middleware.UserIDFromCtx(ctx)
	res, err := service.PayoutPension(ctx, qtx, service.PayoutInput{
		EmployeeID:          empID,
		EmployeeName:        emp.FullName,
		EmployeeStatus:      emp.Status,
		BranchID:            emp.BranchID,
		BranchName:          emp.BranchName,
		ResignDate:          emp.ResignDate,
		PayoutDate:          date,
		FundSourceAccountID: fundID,
		Note:                strings.TrimSpace(body.Note),
		Actor:               actor,
	})
	if err != nil {
		st, msg := benefitErrStatus(err)
		respondError(w, st, msg)
		return
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      actor,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "benefit_payout",
		EntityID:    emp.ID.Bytes,
		Description: fmt.Sprintf("Mencairkan dana pensiun %s: Rp %d", emp.FullName, res.Amount),
	})
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pencairan")
		return
	}
	respondJSON(w, http.StatusOK, res)
}
