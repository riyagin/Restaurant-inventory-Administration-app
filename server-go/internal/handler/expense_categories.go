package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// ExpenseCategoriesHandler manages the subaccount level under a division's
// operational expense account.
//
// A category is not a label — it is a real COA account. Creating one creates the
// account; the invoice that names the category debits that account instead of
// the division parent. Nothing else about the posting path changes, and because
// the COA rolls parents up from their children, the division's total is the same
// number it always was.
type ExpenseCategoriesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewExpenseCategoriesHandler(pool *pgxpool.Pool, queries *db.Queries) *ExpenseCategoriesHandler {
	return &ExpenseCategoriesHandler{pool: pool, queries: queries}
}

func (h *ExpenseCategoriesHandler) List(w http.ResponseWriter, r *http.Request) {
	var divisionID pgtype.UUID
	if raw := r.URL.Query().Get("division_id"); raw != "" {
		id, err := parseUUID(raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "division_id tidak valid")
			return
		}
		divisionID = pgtype.UUID{Bytes: id, Valid: true}
	}

	categories, err := h.queries.ListExpenseCategories(r.Context(), divisionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil kategori beban")
		return
	}
	if categories == nil {
		categories = []*db.ExpenseCategoryRow{}
	}
	respondJSON(w, http.StatusOK, categories)
}

func (h *ExpenseCategoriesHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()

	parent, err := h.queries.GetDivisionExpenseParent(ctx, pgtype.UUID{Bytes: divisionUUID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "divisi tidak ditemukan")
		return
	}
	// Without a parent expense account there is nothing to nest under, and a
	// rootless expense account would silently become its own top-level line in
	// the P&L. Refuse rather than create one.
	if !parent.ExpenseAccountID.Valid {
		respondError(w, http.StatusUnprocessableEntity, "divisi ini belum punya akun beban induk")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	num, err := qtx.GetNextExpenseAccountNumber(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mendapatkan nomor akun")
		return
	}

	// Fully qualified name, matching "Beban - <cabang> - <divisi>". The tree view
	// makes the parent obvious, but the COA export is a flat list sorted by
	// number — there the short name alone would be ambiguous across divisions.
	accountName := fmt.Sprintf("Beban - %s - %s - %s", parent.BranchName, parent.DivisionName, body.Name)

	accountID, err := qtx.CreateExpenseCategoryAccount(ctx, &db.CreateExpenseCategoryAccountParams{
		Name:          accountName,
		AccountNumber: pgtype.Int4{Int32: num, Valid: true},
		ParentID:      parent.ExpenseAccountID,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat akun kategori")
		return
	}

	category, err := qtx.CreateExpenseCategory(ctx, &db.CreateExpenseCategoryParams{
		DivisionID: pgtype.UUID{Bytes: divisionUUID, Valid: true},
		Name:       body.Name,
		AccountID:  accountID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "kategori beban sudah ada di divisi ini")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat kategori beban")
		return
	}

	// Inside the transaction — the category and its account come into existence
	// together or not at all.
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "expense_category",
		EntityID:    category.ID.Bytes,
		Description: fmt.Sprintf("Menambahkan kategori beban %q beserta akun %d %q", body.Name, num, accountName),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kategori beban")
		return
	}

	full, err := h.queries.GetExpenseCategoryByID(ctx, category.ID)
	if err != nil {
		respondJSON(w, http.StatusCreated, category)
		return
	}
	respondJSON(w, http.StatusCreated, full)
}

// Delete removes a category and, with it, the account it created — but only
// while that account is still untouched.
//
// Once anything has posted to the account it carries journal lines, and deleting
// an account a balanced entry references would tear the books apart. In that
// case the category is refused rather than half-removed: the account is real
// history now, and history is not deletable in this system.
func (h *ExpenseCategoriesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}

	ctx := r.Context()

	existing, err := h.queries.GetExpenseCategoryByID(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusNotFound, "kategori beban tidak ditemukan")
		return
	}

	lines, err := h.queries.CountJournalLinesForAccount(ctx, existing.AccountID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memeriksa mutasi akun kategori")
		return
	}
	if lines > 0 || existing.AccountBalance != 0 {
		respondError(w, http.StatusConflict,
			fmt.Sprintf("kategori %q sudah dipakai dan tidak dapat dihapus — akunnya menyimpan riwayat transaksi", existing.Name))
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	// Category first: the account is still referenced by the FK until it is gone.
	if err := qtx.DeleteExpenseCategory(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus kategori beban")
		return
	}
	if err := qtx.DeleteAccount(ctx, existing.AccountID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus akun kategori")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "expense_category",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus kategori beban %q beserta akunnya", existing.Name),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "kategori beban berhasil dihapus"})
}
