package handler

import (
	"context"
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

type BranchesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewBranchesHandler(pool *pgxpool.Pool, queries *db.Queries) *BranchesHandler {
	return &BranchesHandler{pool: pool, queries: queries}
}

// createBranchPettyCashAccount allocates a branch's cash box account: an asset
// numbered in 11100-11199, hung under the system parent "Kas dan Setara Kas"
// (11000) so the balance sheet's cash total picks it up for free.
//
// A missing parent does not fail the branch — the account is created without one
// and shows up unparented on the accounts page, which is visible and fixable,
// unlike refusing to open a branch over a chart-of-accounts quirk.
func createBranchPettyCashAccount(ctx context.Context, qtx *db.Queries, branchName string) (pgtype.UUID, error) {
	num, err := qtx.GetNextPettyCashAccountNumber(ctx)
	if err != nil {
		return pgtype.UUID{}, err
	}

	parent := pgtype.UUID{}
	if p, err := qtx.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: service.CashAccountNumber, Valid: true}); err == nil {
		parent = p.ID
	}

	return qtx.CreatePettyCashAccountForBranch(ctx, &db.CreatePettyCashAccountForBranchParams{
		Name:          "Kas Kecil - " + branchName,
		AccountNumber: pgtype.Int4{Int32: num, Valid: true},
		ParentID:      parent,
	})
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

	// Every branch keeps a physical cash box, so it gets its own asset account
	// under "Kas dan Setara Kas" the moment it exists — Pembelanjaan Harian has
	// somewhere to post from on day one, and the box is on the balance sheet
	// without anyone remembering to add it. Missing parent is not fatal: the
	// account is still created, just unparented, which the accounts page shows.
	pettyID, err := createBranchPettyCashAccount(ctx, qtx, body.Name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat akun kas kecil")
		return
	}

	newBranch, err := qtx.CreateBranch(ctx, &db.CreateBranchParams{
		Name:               body.Name,
		RevenueAccountID:   revID,
		ExpenseAccountID:   expID,
		PettyCashAccountID: pettyID,
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

	// Inside the transaction: creating a branch also creates its revenue and
	// expense accounts, and the log entry should share their fate.
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "branch",
		EntityID:    newBranch.ID.Bytes,
		Description: fmt.Sprintf("Menambahkan cabang %q beserta akun pendapatan, beban dan kas kecilnya", body.Name),
	})

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

	before, err := h.queries.GetBranchByID(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "cabang tidak ditemukan")
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

	// Keep the cash box's label in step with the branch, the same way a vendor
	// rename relabels its payable sub-account. The revenue and expense accounts
	// are deliberately left alone here — that is pre-existing behaviour and
	// changing it is a separate decision.
	if branch.PettyCashAccountID.Valid {
		if err := h.queries.RenameAccount(r.Context(), &db.RenameAccountParams{
			Name: "Kas Kecil - " + branch.Name,
			ID:   branch.PettyCashAccountID,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memperbarui nama akun kas kecil")
			return
		}
	}

	logMutation(r, h.queries, "UPDATE", "branch", id,
		fmt.Sprintf("Mengubah nama cabang %q → %q", before.Name, branch.Name))

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

	existing, err := h.queries.GetBranchByID(ctx, pgID)
	if err != nil {
		respondError(w, http.StatusNotFound, "cabang tidak ditemukan")
		return
	}

	// Money still in the box is money the business owns. Deleting the branch
	// would set petty_cash_account_id to NULL and strand it in an account nobody
	// looks at, so the balance has to be dealt with first.
	if existing.PettyCashBalance != 0 {
		respondError(w, http.StatusConflict,
			fmt.Sprintf("kas kecil cabang masih bersaldo %d — setorkan atau nolkan dulu", existing.PettyCashBalance))
		return
	}

	if err := h.queries.DeleteBranch(ctx, pgID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus cabang")
		return
	}

	logMutation(r, h.queries, "DELETE", "branch", id,
		fmt.Sprintf("Menghapus cabang %q", existing.Name))

	respondJSON(w, http.StatusOK, map[string]string{"message": "cabang berhasil dihapus"})
}
