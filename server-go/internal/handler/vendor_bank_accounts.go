package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Vendor transfer accounts — the bank details a vendor is paid to.
//
// These are payment instructions, not chart-of-accounts entries: nothing here
// posts to the ledger, and a vendor's only accounting object remains its payable
// sub-account. A vendor may hold any number of them, exactly one of which is the
// default (`is_primary`) that payment screens reach for first.

// bankAccountBody is the shared shape of the create and update requests.
type bankAccountBody struct {
	BankName      string `json:"bank_name"`
	AccountNumber string `json:"account_number"`
	AccountHolder string `json:"account_holder"`
	BankBranch    string `json:"bank_branch"`
	IsPrimary     bool   `json:"is_primary"`
	Note          string `json:"note"`
}

func (b *bankAccountBody) normalise() error {
	b.BankName = strings.TrimSpace(b.BankName)
	b.AccountNumber = strings.TrimSpace(b.AccountNumber)
	b.AccountHolder = strings.TrimSpace(b.AccountHolder)
	b.BankBranch = strings.TrimSpace(b.BankBranch)
	b.Note = strings.TrimSpace(b.Note)

	if b.BankName == "" {
		return errors.New("nama bank wajib diisi")
	}
	if b.AccountNumber == "" {
		return errors.New("nomor rekening wajib diisi")
	}
	// A number the office cannot transfer to is worse than no number at all, so
	// reject anything without digits rather than storing a note in the field.
	if !strings.ContainsAny(b.AccountNumber, "0123456789") {
		return errors.New("nomor rekening harus mengandung angka")
	}
	return nil
}

// bankAccountConflict turns a unique-index violation into the message that
// explains which rule was broken. The primary-flag index cannot realistically
// fire (the handler clears the old default first) but is mapped anyway so a
// future caller gets a sentence instead of a 500.
func bankAccountConflict(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return "", false
	}
	switch pgErr.ConstraintName {
	case "idx_vendor_bank_accounts_unique":
		return "rekening dengan bank dan nomor yang sama sudah terdaftar untuk vendor ini", true
	case "idx_vendor_bank_accounts_primary":
		return "vendor ini sudah memiliki rekening utama", true
	}
	return "rekening tersebut sudah terdaftar", true
}

// ListBankAccounts — GET /api/vendors/{id}/bank-accounts
func (h *VendorsHandler) ListBankAccounts(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	accounts, err := h.queries.ListVendorBankAccounts(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil rekening vendor")
		return
	}
	if accounts == nil {
		accounts = []*db.VendorBankAccount{}
	}
	respondJSON(w, http.StatusOK, accounts)
}

// CreateBankAccount — POST /api/vendors/{id}/bank-accounts
func (h *VendorsHandler) CreateBankAccount(w http.ResponseWriter, r *http.Request) {
	vendorID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body bankAccountBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if err := body.normalise(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	pgVendorID := pgtype.UUID{Bytes: vendorID, Valid: true}

	vendor, err := h.queries.GetVendorByID(ctx, pgVendorID)
	if err != nil {
		respondError(w, http.StatusNotFound, "vendor tidak ditemukan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	existing, err := qtx.ListVendorBankAccounts(ctx, pgVendorID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil rekening vendor")
		return
	}
	// The first account a vendor gets is its default whether or not the form
	// said so — a vendor with exactly one number and no default is a trap for
	// whoever pays it next.
	if len(existing) == 0 {
		body.IsPrimary = true
	}
	if body.IsPrimary {
		if err := qtx.ClearVendorPrimaryBankAccount(ctx, pgVendorID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui rekening utama")
			return
		}
	}

	account, err := qtx.CreateVendorBankAccount(ctx, &db.CreateVendorBankAccountParams{
		VendorID:      pgVendorID,
		BankName:      body.BankName,
		AccountNumber: body.AccountNumber,
		AccountHolder: body.AccountHolder,
		BankBranch:    body.BankBranch,
		IsPrimary:     body.IsPrimary,
		Note:          body.Note,
	})
	if err != nil {
		if msg, ok := bankAccountConflict(err); ok {
			respondError(w, http.StatusConflict, msg)
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal menyimpan rekening vendor")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "vendor_bank_account",
		EntityID:    account.ID.Bytes,
		Description: fmt.Sprintf("Menambahkan rekening %s %s untuk vendor %q", account.BankName, account.AccountNumber, vendor.Name),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan rekening vendor")
		return
	}
	respondJSON(w, http.StatusCreated, account)
}

// UpdateBankAccount — PUT /api/vendors/{id}/bank-accounts/{bankId}
func (h *VendorsHandler) UpdateBankAccount(w http.ResponseWriter, r *http.Request) {
	vendorID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	bankID, err := parseUUID(chi.URLParam(r, "bankId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID rekening tidak valid")
		return
	}

	var body bankAccountBody
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if err := body.normalise(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	pgVendorID := pgtype.UUID{Bytes: vendorID, Valid: true}
	pgBankID := pgtype.UUID{Bytes: bankID, Valid: true}

	before, err := h.queries.GetVendorBankAccountByID(ctx, pgBankID)
	if err != nil {
		respondError(w, http.StatusNotFound, "rekening tidak ditemukan")
		return
	}
	// The vendor in the path owns the account or the request is malformed —
	// without this check a stale link could rewrite another vendor's details.
	if before.VendorID.Bytes != vendorID {
		respondError(w, http.StatusNotFound, "rekening tidak ditemukan pada vendor ini")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	// Clearing the flag on the only default would leave the vendor without one,
	// so the flag can be moved elsewhere but never simply switched off here.
	if before.IsPrimary {
		body.IsPrimary = true
	}
	if body.IsPrimary && !before.IsPrimary {
		if err := qtx.ClearVendorPrimaryBankAccount(ctx, pgVendorID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui rekening utama")
			return
		}
	}

	account, err := qtx.UpdateVendorBankAccount(ctx, &db.UpdateVendorBankAccountParams{
		BankName:      body.BankName,
		AccountNumber: body.AccountNumber,
		AccountHolder: body.AccountHolder,
		BankBranch:    body.BankBranch,
		IsPrimary:     body.IsPrimary,
		Note:          body.Note,
		ID:            pgBankID,
	})
	if err != nil {
		if msg, ok := bankAccountConflict(err); ok {
			respondError(w, http.StatusConflict, msg)
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui rekening vendor")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:     middleware.UserIDFromCtx(ctx),
		Username:   middleware.UsernameFromCtx(ctx),
		Action:     "UPDATE",
		EntityType: "vendor_bank_account",
		EntityID:   bankID,
		Description: fmt.Sprintf("Mengubah rekening vendor %s %s → %s %s",
			before.BankName, before.AccountNumber, account.BankName, account.AccountNumber),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
		return
	}
	respondJSON(w, http.StatusOK, account)
}

// DeleteBankAccount — DELETE /api/vendors/{id}/bank-accounts/{bankId}
func (h *VendorsHandler) DeleteBankAccount(w http.ResponseWriter, r *http.Request) {
	vendorID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	bankID, err := parseUUID(chi.URLParam(r, "bankId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID rekening tidak valid")
		return
	}

	ctx := r.Context()
	pgVendorID := pgtype.UUID{Bytes: vendorID, Valid: true}
	pgBankID := pgtype.UUID{Bytes: bankID, Valid: true}

	account, err := h.queries.GetVendorBankAccountByID(ctx, pgBankID)
	if err != nil {
		respondError(w, http.StatusNotFound, "rekening tidak ditemukan")
		return
	}
	if account.VendorID.Bytes != vendorID {
		respondError(w, http.StatusNotFound, "rekening tidak ditemukan pada vendor ini")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if err := qtx.DeleteVendorBankAccount(ctx, pgBankID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus rekening vendor")
		return
	}
	// Deleting the default leaves the vendor with numbers but no answer to
	// "which one?"; hand the flag to the oldest survivor. A no-op when another
	// account already holds it, or when none are left.
	if err := qtx.PromoteOldestVendorBankAccount(ctx, pgVendorID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menetapkan rekening utama")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "vendor_bank_account",
		EntityID:    bankID,
		Description: fmt.Sprintf("Menghapus rekening %s %s", account.BankName, account.AccountNumber),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "rekening berhasil dihapus"})
}

// vendorBankAccountsFor is used by the vendor activity page, which shows the
// transfer details alongside the payable summary.
func (h *VendorsHandler) vendorBankAccountsFor(r *http.Request, vendorID uuid.UUID) []*db.VendorBankAccount {
	accounts, err := h.queries.ListVendorBankAccounts(r.Context(), pgtype.UUID{Bytes: vendorID, Valid: true})
	if err != nil || accounts == nil {
		return []*db.VendorBankAccount{}
	}
	return accounts
}
