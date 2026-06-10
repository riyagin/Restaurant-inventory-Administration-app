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
