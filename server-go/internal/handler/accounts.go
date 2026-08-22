package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

type AccountsHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewAccountsHandler(pool *pgxpool.Pool, queries *db.Queries) *AccountsHandler {
	return &AccountsHandler{pool: pool, queries: queries}
}

// List — GET /api/accounts?branch_id=
//
// Every account, always — the branch parameter annotates rather than filters, so
// the response can carry the whole tree while telling the caller which parts of
// it belong to the branch. Filtering server-side would break the tree: an expense
// category's parent may be branch-owned while a `Kas` several levels up is not,
// and a client that receives only some nodes cannot rebuild the hierarchy the
// page renders.
//
// `owner_branch_id` is always present (null for accounts no branch owns). With a
// branch selected each account additionally carries `branch_amount` — the part of
// its balance traceable to that branch through the documents behind its journal
// entries — and `unplaced_amount`, the part traceable to no branch at all. Those
// two are derivations and are named so they cannot be mistaken for `balance`.
func (h *AccountsHandler) List(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.queries.ListAccounts(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data akun")
		return
	}
	if accounts == nil {
		accounts = []*db.Account{}
	}

	branchID := r.URL.Query().Get("branch_id")
	if branchID != "" {
		if _, err := parseUUID(branchID); err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
	}

	owners, err := h.accountOwners(r)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memetakan akun ke cabang")
		return
	}

	var branchAmount, unplaced map[string]int64
	if branchID != "" {
		branchAmount, unplaced, err = h.branchAmountsForAccounts(r, branchID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menghitung porsi cabang")
			return
		}
	}

	out := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		id := uuidBytesToString(a.ID.Bytes)
		row := map[string]any{
			"id":              a.ID,
			"name":            a.Name,
			"balance":         a.Balance,
			"account_number":  a.AccountNumber,
			"account_type":    a.AccountType,
			"parent_id":       a.ParentID,
			"is_system":       a.IsSystem,
			"is_cash_drawer":  a.IsCashDrawer,
			"owner_branch_id": nil,
		}
		if owner, ok := owners[id]; ok {
			row["owner_branch_id"] = owner
		}
		if branchID != "" {
			row["branch_amount"] = branchAmount[id]
			row["unplaced_amount"] = unplaced[id]
		}
		out = append(out, row)
	}

	respondJSON(w, http.StatusOK, out)
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

	logMutation(r, h.queries, "CREATE", "account", account.ID.Bytes,
		fmt.Sprintf("Menambahkan akun %q (%s)", body.Name, body.AccountType))

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

	var changes []string
	if existing.Name != body.Name {
		changes = append(changes, fmt.Sprintf("nama: %q → %q", existing.Name, body.Name))
	}
	if existing.AccountType != body.AccountType {
		// Retyping an account moves it between sections of the financial report.
		changes = append(changes, fmt.Sprintf("tipe: %s → %s", existing.AccountType, body.AccountType))
	}
	logMutation(r, h.queries, "UPDATE", "account", id,
		fmt.Sprintf("Mengubah akun %q%s", existing.Name, changeClause(changes)))

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

	logMutation(r, h.queries, "DELETE", "account", id,
		fmt.Sprintf("Menghapus akun %q (%s, saldo %s)",
			existing.Name, existing.AccountType, formatRupiahShort(existing.Balance)))

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

// CapitalInjection records money the owner puts into the business — including
// the cash carried over when a previous system is replaced.
//
// This exists because its absence is what broke the books in the first place.
// There was no legitimate way to record an owner's deposit, so opening balances
// were typed straight into accounts.balance (the old Node backend let
// POST /api/accounts set a balance directly) with nothing on the credit side.
// That is not an error in the money — the cash was real — but the matching
// capital entry was missing, and the books were out by that amount from day one.
//
// Here the entry is two-sided by construction: Dr the cash account, Cr "Modal
// Pemilik". There is no way to record one without the other.
func (h *AccountsHandler) CapitalInjection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID   string `json:"account_id"`
		Amount      int64  `json:"amount"`
		Date        string `json:"date"`
		Description string `json:"description"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah setoran harus lebih dari nol")
		return
	}

	accountID, err := parseUUID(body.AccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "account_id tidak valid")
		return
	}

	entryDate := time.Now()
	if body.Date != "" {
		parsed, err := time.Parse("2006-01-02", body.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, "format tanggal tidak valid (gunakan YYYY-MM-DD)")
			return
		}
		entryDate = parsed
	}

	ctx := r.Context()

	target, err := h.queries.GetAccountByID(ctx, pgtype.UUID{Bytes: accountID, Valid: true})
	if err != nil {
		respondError(w, http.StatusNotFound, "akun tujuan tidak ditemukan")
		return
	}
	// Capital arrives as an asset — cash, a bank account, equipment. Crediting
	// equity against anything else is a different transaction entirely.
	if target.AccountType != "asset" {
		respondError(w, http.StatusBadRequest, "setoran modal harus masuk ke akun aset")
		return
	}

	capital, err := h.queries.GetSystemAccountByNumber(ctx, pgtype.Int4{Int32: service.OwnerCapitalAccountNumber, Valid: true})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "akun modal pemilik tidak ditemukan")
		return
	}

	description := strings.TrimSpace(body.Description)
	if description == "" {
		description = "Setoran modal pemilik"
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.queries.WithTx(tx)

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        entryDate,
		SourceType:  service.SourceCapital,
		Description: description,
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Dr(accountID, body.Amount),
			service.Cr(capital.ID.Bytes, body.Amount),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal setoran modal")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan setoran modal")
		return
	}

	_ = service.LogActivity(ctx, h.queries, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "CapitalInjection",
		Description: description,
	})

	respondJSON(w, http.StatusCreated, map[string]any{
		"account":     target.Name,
		"amount":      body.Amount,
		"credited_to": capital.Name,
	})
}

// CashReconciliation reports, per cash account, the balance against what
// recorded transactions actually explain. A positive `unexplained` is money that
// entered outside any transaction — the signature of an opening balance carried
// over from a previous system, or of a one-sided write.
//
// Run this before trusting the capital/error split that migration 042 made:
// check each figure against what the old system said the account held at
// cutover, and move anything misattributed between "Modal Pemilik" and
// "Selisih Migrasi" with an ordinary journal entry.
func (h *AccountsHandler) CashReconciliation(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, name, balance, explained, unexplained
		FROM cash_reconciliation ORDER BY unexplained DESC`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung rekonsiliasi kas")
		return
	}
	defer rows.Close()

	type row struct {
		ID          pgtype.UUID `json:"id"`
		Name        string      `json:"name"`
		Balance     int64       `json:"balance"`
		Explained   int64       `json:"explained"`
		Unexplained int64       `json:"unexplained"`
	}
	out := []row{}
	for rows.Next() {
		var v row
		if err := rows.Scan(&v.ID, &v.Name, &v.Balance, &v.Explained, &v.Unexplained); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca rekonsiliasi kas")
			return
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membaca rekonsiliasi kas")
		return
	}
	respondJSON(w, http.StatusOK, out)
}
