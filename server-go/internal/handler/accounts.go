package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

type AccountsHandler struct {
	queries *db.Queries
}

func NewAccountsHandler(queries *db.Queries) *AccountsHandler {
	return &AccountsHandler{queries: queries}
}

func (h *AccountsHandler) List(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.queries.ListAccounts(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data akun")
		return
	}
	if accounts == nil {
		accounts = []*db.Account{}
	}
	respondJSON(w, http.StatusOK, accounts)
}

func (h *AccountsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string  `json:"name"`
		AccountNumber *int32  `json:"account_number"`
		AccountType   string  `json:"account_type"`
		ParentID      *string `json:"parent_id"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama akun wajib diisi")
		return
	}

	params := &db.CreateAccountParams{
		Name:        body.Name,
		AccountType: body.AccountType,
	}
	if body.AccountNumber != nil {
		params.AccountNumber = pgtype.Int4{Int32: *body.AccountNumber, Valid: true}
	}
	if body.ParentID != nil && *body.ParentID != "" {
		parentID, err := parseUUID(*body.ParentID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "parent_id tidak valid")
			return
		}
		params.ParentID = pgtype.UUID{Bytes: parentID, Valid: true}
	}

	account, err := h.queries.CreateAccount(r.Context(), params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat akun")
		return
	}
	respondJSON(w, http.StatusCreated, account)
}

func (h *AccountsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	existing, err := h.queries.GetAccountByID(r.Context(), pgID)
	if err != nil {
		respondError(w, http.StatusNotFound, "akun tidak ditemukan")
		return
	}
	if existing.IsSystem {
		respondError(w, http.StatusForbidden, "akun sistem tidak dapat diubah")
		return
	}

	var body struct {
		Name          string  `json:"name"`
		AccountNumber *int32  `json:"account_number"`
		AccountType   string  `json:"account_type"`
		ParentID      *string `json:"parent_id"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama akun wajib diisi")
		return
	}

	params := &db.UpdateAccountParams{
		Name:        body.Name,
		AccountType: body.AccountType,
		ID:          pgID,
	}
	if body.AccountNumber != nil {
		params.AccountNumber = pgtype.Int4{Int32: *body.AccountNumber, Valid: true}
	}
	if body.ParentID != nil && *body.ParentID != "" {
		parentID, err := parseUUID(*body.ParentID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "parent_id tidak valid")
			return
		}
		params.ParentID = pgtype.UUID{Bytes: parentID, Valid: true}
	}

	account, err := h.queries.UpdateAccount(r.Context(), params)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memperbarui akun")
		return
	}
	respondJSON(w, http.StatusOK, account)
}

func (h *AccountsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	existing, err := h.queries.GetAccountByID(r.Context(), pgID)
	if err != nil {
		respondError(w, http.StatusNotFound, "akun tidak ditemukan")
		return
	}
	if existing.IsSystem {
		respondError(w, http.StatusForbidden, "akun sistem tidak dapat dihapus")
		return
	}

	if err := h.queries.DeleteAccount(r.Context(), pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus akun")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "akun berhasil dihapus"})
}

// TrialBalance reports whether the books balance, and where they do not.
//
// Two independent checks:
//
//   - `equation`: assets - (liabilities + equity + revenue - expense) over the
//     cached balances. Zero is the only correct value. This is the number that
//     was 982,420,908 before the journal existed.
//   - `drift`: per account, cached balance minus the balance implied by its
//     journal lines. Non-zero means something wrote accounts.balance outside
//     service.Post, or that the account carries history from before the journal.
//
// Only accounts with non-zero drift are returned, so a healthy system responds
// with an empty list.
func (h *AccountsHandler) TrialBalance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	eq, err := h.queries.AccountingEquation(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung persamaan akuntansi")
		return
	}

	rows, err := h.queries.TrialBalance(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung neraca saldo")
		return
	}

	drifted := []*db.TrialBalanceRow{}
	var totalDrift int64
	for _, row := range rows {
		if row.Drift != 0 {
			drifted = append(drifted, row)
			totalDrift += row.Drift
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"equation":    eq,
		"balanced":    eq.Difference == 0,
		"total_drift": totalDrift,
		"drifted":     drifted,
	})
}
