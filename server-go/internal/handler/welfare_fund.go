package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// WelfareFundHandler serves Dana Kesejahteraan Karyawan, the retirement fund
// (migration 090). Business rules live in service/welfare_fund.go.
type WelfareFundHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewWelfareFundHandler(pool *pgxpool.Pool, queries *db.Queries) *WelfareFundHandler {
	return &WelfareFundHandler{pool: pool, queries: queries}
}

func welfareErrStatus(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrWelfareStillEmployed),
		errors.Is(err, service.ErrWelfareNothingToPay),
		errors.Is(err, service.ErrWelfareUnsettled):
		return http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrWelfareFutureDate):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, service.ErrWelfareAccountGone):
		return http.StatusInternalServerError, err.Error()
	}
	return http.StatusInternalServerError, "gagal memproses dana kesejahteraan: " + err.Error()
}

// EmployeeDetail — GET /api/hr/welfare-fund/employees/{id}
func (h *WelfareFundHandler) EmployeeDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
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
	balance, err := h.queries.GetWelfareBalance(ctx, empID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil saldo")
		return
	}
	perPeriod, err := h.queries.GetWelfareMonthlyAmount(ctx, &db.GetWelfareMonthlyAmountParams{
		EmployeeID: empID, AsOf: pgtype.Date{Time: service.TodayWIB(), Valid: true},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil iuran")
		return
	}
	movements, err := h.queries.ListWelfareMovementsByEmployee(ctx, empID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil mutasi")
		return
	}
	if movements == nil {
		movements = []*db.ListWelfareMovementsByEmployeeRow{}
	}
	var contributed, paidOut int64
	for _, m := range movements {
		switch m.MovementType {
		case service.WelfarePayout:
			paidOut -= m.Amount
		default:
			contributed += m.Amount
		}
	}
	unsettled, _ := h.queries.CountUnsettledPayrollForEmployee(ctx, empID)
	respondJSON(w, http.StatusOK, map[string]any{
		"employee": map[string]any{
			"id": emp.ID, "employee_code": emp.EmployeeCode, "full_name": emp.FullName,
			"status": emp.Status, "join_date": emp.JoinDate, "resign_date": emp.ResignDate,
		},
		"balance":            balance,
		"per_period_amount":  perPeriod,
		"total_contributed":  contributed,
		"total_paid_out":     paidOut,
		"unsettled_payrolls": unsettled,
		"movements":          movements,
	})
}

// Payout — POST /api/hr/welfare-fund/employees/{id}/payout
// {payout_date, fund_source_account_id, note}. Manager-only (routing).
func (h *WelfareFundHandler) Payout(w http.ResponseWriter, r *http.Request) {
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
	// Serialise payouts per employee so a double click cannot pay the balance twice.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('welfare_payout:' || $1::text))`, id.String()); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengunci data karyawan")
		return
	}
	qtx := h.queries.WithTx(tx)

	actor := middleware.UserIDFromCtx(ctx)
	res, err := service.PayoutWelfare(ctx, qtx, service.WelfarePayoutInput{
		EmployeeID:          empID,
		EmployeeName:        emp.FullName,
		EmployeeStatus:      emp.Status,
		PayoutDate:          date,
		FundSourceAccountID: fundID,
		Note:                strings.TrimSpace(body.Note),
		Actor:               actor,
	})
	if err != nil {
		st, msg := welfareErrStatus(err)
		respondError(w, st, msg)
		return
	}
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      actor,
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "welfare_payout",
		EntityID:    emp.ID.Bytes,
		Description: fmt.Sprintf("Mencairkan dana kesejahteraan %s: Rp %d", emp.FullName, res.Amount),
	})
	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pencairan")
		return
	}
	respondJSON(w, http.StatusOK, res)
}
