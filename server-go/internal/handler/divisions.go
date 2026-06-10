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

type DivisionsHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewDivisionsHandler(pool *pgxpool.Pool, queries *db.Queries) *DivisionsHandler {
	return &DivisionsHandler{pool: pool, queries: queries}
}

func (h *DivisionsHandler) List(w http.ResponseWriter, r *http.Request) {
	var branchID pgtype.UUID
	if raw := r.URL.Query().Get("branch_id"); raw != "" {
		id, err := parseUUID(raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		branchID = pgtype.UUID{Bytes: id, Valid: true}
	}

	divisions, err := h.queries.ListDivisions(r.Context(), branchID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data divisi")
		return
	}
	if divisions == nil {
		divisions = []*db.ListDivisionsRow{}
	}
	respondJSON(w, http.StatusOK, divisions)
}

func (h *DivisionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BranchID string `json:"branch_id"`
		Name     string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.BranchID == "" || body.Name == "" {
		respondError(w, http.StatusBadRequest, "branch_id dan name wajib diisi")
		return
	}

	branchUUID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "branch_id tidak valid")
		return
	}

	ctx := r.Context()

	branch, err := h.queries.GetBranchByID(ctx, pgtype.UUID{Bytes: branchUUID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "cabang tidak ditemukan")
		return
	}

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
		Name:          "Pendapatan - " + branch.Name + " - " + body.Name,
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
		Name:          "Beban - " + branch.Name + " - " + body.Name,
		AccountNumber: pgtype.Int4{Int32: expNum, Valid: true},
		AccountType:   "expense",
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat akun beban")
		return
	}

	discNum, err := qtx.GetNextExpenseAccountNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mendapatkan nomor akun")
		return
	}
	discID, err := qtx.CreateAccountForBranch(ctx, &db.CreateAccountForBranchParams{
		Name:          "Diskon - " + branch.Name + " - " + body.Name,
		AccountNumber: pgtype.Int4{Int32: discNum, Valid: true},
		AccountType:   "expense",
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat akun diskon")
		return
	}

	newDiv, err := qtx.CreateDivision(ctx, &db.CreateDivisionParams{
		BranchID:          pgtype.UUID{Bytes: branchUUID, Valid: true},
		Name:              body.Name,
		RevenueAccountID:  revID,
		ExpenseAccountID:  expID,
		DiscountAccountID: discID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama divisi sudah digunakan di cabang ini")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat divisi")
		return
	}

	division, err := qtx.GetDivisionByID(ctx, newDiv.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data divisi")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan data")
		return
	}

	respondJSON(w, http.StatusCreated, division)
}

func (h *DivisionsHandler) Update(w http.ResponseWriter, r *http.Request) {
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
		respondError(w, http.StatusBadRequest, "nama divisi wajib diisi")
		return
	}

	division, err := h.queries.UpdateDivision(r.Context(), &db.UpdateDivisionParams{
		Name: body.Name,
		ID:   pgtype.UUID{Bytes: id, Valid: true},
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "nama divisi sudah digunakan di cabang ini")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal memperbarui divisi")
		return
	}
	respondJSON(w, http.StatusOK, division)
}

func (h *DivisionsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	if err := h.queries.DeleteDivision(r.Context(), pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus divisi")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "divisi berhasil dihapus"})
}

func (h *DivisionsHandler) ListCategories(w http.ResponseWriter, r *http.Request) {
	var divisionID pgtype.UUID
	if raw := r.URL.Query().Get("division_id"); raw != "" {
		id, err := parseUUID(raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "division_id tidak valid")
			return
		}
		divisionID = pgtype.UUID{Bytes: id, Valid: true}
	}

	categories, err := h.queries.ListDivisionCategories(r.Context(), divisionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kategori")
		return
	}
	if categories == nil {
		categories = []*db.DivisionCategory{}
	}
	respondJSON(w, http.StatusOK, categories)
}

func (h *DivisionsHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DivisionID string `json:"division_id"`
		Name       string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.DivisionID == "" || body.Name == "" {
		respondError(w, http.StatusBadRequest, "division_id dan name wajib diisi")
		return
	}

	divisionUUID, err := parseUUID(body.DivisionID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "division_id tidak valid")
		return
	}

	category, err := h.queries.CreateDivisionCategory(r.Context(), &db.CreateDivisionCategoryParams{
		DivisionID: pgtype.UUID{Bytes: divisionUUID, Valid: true},
		Name:       body.Name,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "kategori sudah ada di divisi ini")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat kategori")
		return
	}
	respondJSON(w, http.StatusCreated, category)
}

func (h *DivisionsHandler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}

	if err := h.queries.DeleteDivisionCategory(r.Context(), pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus kategori")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "kategori berhasil dihapus"})
}
