package handler

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Overpayment still reads as paid; the excess stays as a debit on the vendor's payable.
func invoicePaymentStatus(amountPaid, total int64) string {
	switch {
	case amountPaid <= 0:
		return "unpaid"
	case amountPaid >= total:
		return "paid"
	default:
		return "partial"
	}
}

// Pay — POST /api/invoices/:id/pay
func (h *InvoicesHandler) Pay(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		CashAccountID string  `json:"cash_account_id"`
		Amount        float64 `json:"amount"`
		PaymentDate   string  `json:"payment_date"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.CashAccountID == "" {
		respondError(w, http.StatusBadRequest, "cash_account_id diperlukan")
		return
	}
	cashAcctID, err := parseUUID(body.CashAccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "cash_account_id tidak valid")
		return
	}

	payDate, err := parsePaymentDate(body.PaymentDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	invoiceUUID := pgtype.UUID{Bytes: id, Valid: true}

	inv, err := h.queries.GetInvoiceWithTotal(ctx, invoiceUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "faktur tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data faktur")
		return
	}
	if inv.PaymentStatus == "paid" {
		respondError(w, http.StatusBadRequest, "faktur sudah lunas")
		return
	}

	remaining := inv.TotalAmount - inv.AmountPaid
	payAmount := int64(body.Amount)
	if payAmount <= 0 || payAmount > remaining {
		payAmount = remaining
	}
	if payAmount <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah pembayaran tidak valid")
		return
	}

	cashAcct, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: cashAcctID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "akun kas tidak ditemukan")
		return
	}
	if cashAcct.Balance < payAmount {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("saldo akun \"%s\" tidak cukup", cashAcct.Name))
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	apAcctID, err := service.VendorPayableAccountID(ctx, qtx, inv.VendorID.Bytes)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil akun hutang vendor")
		return
	}

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        payDate,
		SourceType:  service.SourceInvoicePay,
		SourceID:    id,
		Description: fmt.Sprintf("Pembayaran faktur %s", inv.InvoiceNumber),
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Dr(apAcctID, payAmount),
			service.Cr(cashAcctID, payAmount),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembayaran")
		return
	}

	newAmountPaid := inv.AmountPaid + payAmount
	newStatus := "partial"
	if newAmountPaid >= inv.TotalAmount {
		newStatus = "paid"
	}

	updated, err := qtx.UpdateInvoicePayment(ctx, &db.UpdateInvoicePaymentParams{
		AmountPaid:    newAmountPaid,
		PaymentStatus: newStatus,
		AccountID:     pgtype.UUID{Bytes: cashAcctID, Valid: true},
		PaymentDate:   pgtype.Date{Time: payDate, Valid: true},
		ID:            invoiceUUID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui status pembayaran")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pembayaran")
		return
	}

	userID := middleware.UserIDFromCtx(ctx)
	username := middleware.UsernameFromCtx(ctx)
	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      userID,
		Username:    username,
		Action:      "UPDATE",
		EntityType:  "Invoice",
		EntityID:    id,
		Description: fmt.Sprintf("Bayar faktur %s sebesar %d via \"%s\" tanggal %s", inv.InvoiceNumber, payAmount, cashAcct.Name, payDate.Format("2006-01-02")),
	})

	respondJSON(w, http.StatusOK, updated)
}

// A future payment date is refused.
func parsePaymentDate(s string) (time.Time, error) {
	if s == "" {
		return time.Now(), nil
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("format tanggal pembayaran tidak valid")
	}
	now := time.Now()
	if d.After(time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, d.Location())) {
		return time.Time{}, fmt.Errorf("tanggal pembayaran tidak boleh di masa depan")
	}
	return d, nil
}
