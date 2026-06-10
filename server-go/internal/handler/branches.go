package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
)

type BranchesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewBranchesHandler(pool *pgxpool.Pool, queries *db.Queries) *BranchesHandler {
	return &BranchesHandler{pool: pool, queries: queries}
}

func (h *BranchesHandler) List(w http.ResponseWriter, r *http.Request) {
	branches, err := h.queries.ListBranches(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data cabang")
		return
	}
	if branches == nil {
		branches = []*db.ListBranchesRow{}
	}
	respondJSON(w, http.StatusOK, branches)
}

func (h *BranchesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	branch, err := h.queries.GetBranchByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "cabang tidak ditemukan")
		return
	}
	respondJSON(w, http.StatusOK, branch)
}

func (h *BranchesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama cabang wajib diisi")
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	revNum, err := qtx.GetNextRevenueAccountNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mendapatkan nomor akun")
		return
	}
	revID, err := qtx.CreateAccountForBranch(ctx, &db.CreateAccountForBranchParams{
		Name:          "Pendapatan - " + body.Name,
		AccountNumber: pgtype.Int4{Int32: revNum, Valid: true},
		AccountType:   "revenue",
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat akun pendapatan")
		return
	}

	expNum, err := qtx.GetNextExpenseAccountNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mendapatkan nomor akun")
		return
	}
	expID, err := qtx.CreateAccountForBranch(ctx, &db.CreateAccountForBranchParams{
		Name:          "Beban - " + body.Name,
		AccountNumber: pgtype.Int4{Int32: expNum, Valid: true},
		AccountType:   "expense",
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat akun beban")
		return
	}

	newBranch, err := qtx.CreateBranch(ctx, &db.CreateBranchParams{
		Name:             body.Name,
		RevenueAccountID: revID,
		ExpenseAccountID: expID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama cabang sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat cabang")
		return
	}

	branch, err := qtx.GetBranchByID(ctx, newBranch.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data cabang")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, branch)
}

func (h *BranchesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "nama cabang wajib diisi")
		return
	}

	branch, err := h.queries.UpdateBranch(r.Context(), &db.UpdateBranchParams{
		Name: body.Name,
		ID:   pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama cabang sudah digunakan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui cabang")
		return
	}
	respondJSON(w, http.StatusOK, branch)
}

func (h *BranchesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	ctx := r.Context()
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	count, err := h.queries.CountDivisionsByBranch(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa divisi")
		return
	}
	if count > 0 {
		respondError(w, http.StatusConflict, "cabang masih memiliki divisi aktif")
		return
	}

	if err := h.queries.DeleteBranch(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus cabang")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "cabang berhasil dihapus"})
}
