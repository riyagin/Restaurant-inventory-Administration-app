package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// Beban Operasional — the standing bills of keeping a branch open.
//
// Listrik, air, sewa, iuran keamanan: costs that are not goods, arrive as a bill
// rather than a delivery, and are settled the moment they are recorded. They
// were previously booked straight onto a branch's "Operasional" division expense
// account, which made the total visible and the composition invisible.
//
// Each posted row is one balanced entry — Dr the category's sub-account, Cr
// whatever paid it — and nothing here touches stock, units or payables. Which
// account was debited and which credited are frozen onto the row: repointing a
// branch at a new cash box must not rewrite where last month's electricity came
// from, the same rule daily_purchases.petty_cash_account_id follows.
type OperationalExpensesHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewOperationalExpensesHandler(pool *pgxpool.Pool, queries *db.Queries) *OperationalExpensesHandler {
	return &OperationalExpensesHandler{pool: pool, queries: queries}
}

// ── Categories ───────────────────────────────────────────────────────────────

// ListCategories — GET /api/operational-expense-categories?branch_id=
//
// A category belongs to a branch's Operasional division, so the branch is what
// callers filter by; the division is an implementation detail of where the
// account hangs and is returned rather than asked for.
func (h *OperationalExpensesHandler) ListCategories(w http.ResponseWriter, r *http.Request) {
	sql := `
		SELECT oec.id::text AS id, oec.name, oec.is_system, oec.sort_order,
		       oec.division_id::text AS division_id, d.name AS division_name,
		       d.branch_id::text AS branch_id, b.name AS branch_name,
		       oec.account_id::text AS account_id, a.account_number, a.name AS account_name, a.balance
		FROM operational_expense_categories oec
		JOIN divisions d ON d.id = oec.division_id
		JOIN branches b ON b.id = d.branch_id
		JOIN accounts a ON a.id = oec.account_id`
	var params []any
	if raw := r.URL.Query().Get("branch_id"); raw != "" {
		id, err := parseUUID(raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		sql += ` WHERE d.branch_id = $1`
		params = append(params, pgUUID(id))
	}
	sql += ` ORDER BY b.name, oec.sort_order, oec.name`

	rows, err := h.pool.Query(r.Context(), sql, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil kategori beban operasional")
		return
	}
	categories, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses kategori beban operasional")
		return
	}
	respondJSON(w, http.StatusOK, nilToEmpty(categories))
}

// CreateCategory — POST /api/operational-expense-categories
//
// Creating a category creates the account it posts to, in the same transaction:
// a category with no account could be selected on the form and would then have
// nowhere to debit.
func (h *OperationalExpensesHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
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
	branchID, err := parseUUID(body.BranchID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "branch_id tidak valid")
		return
	}

	ctx := r.Context()

	divisionID, expenseAccountID, parentName, branchName, err := h.operasionalDivision(ctx, branchID)
	if err != nil {
		respondError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	// Same fully-qualified naming as the seeded set and as expense categories:
	// the tree view makes the parent obvious, but the CoA export is a flat list
	// sorted by number, where "Listrik" alone is ambiguous across branches.
	accountName := parentName + " - " + body.Name

	var accountID pgtype.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO accounts (name, account_number, account_type, parent_id, is_system)
		VALUES ($1,
		        (SELECT COALESCE(MAX(account_number), 49999) + 1 FROM accounts
		         WHERE account_number BETWEEN 50000 AND 59999),
		        'expense', $2, false)
		RETURNING id`, accountName, expenseAccountID).Scan(&accountID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, fmt.Sprintf("akun %q sudah ada", accountName))
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat akun kategori")
		return
	}

	var categoryID pgtype.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO operational_expense_categories (division_id, name, account_id, is_system, sort_order)
		VALUES ($1, $2, $3, false,
		        (SELECT COALESCE(MAX(sort_order), 0) + 10 FROM operational_expense_categories WHERE division_id = $1))
		RETURNING id`, divisionID, body.Name, accountID).Scan(&categoryID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondError(w, http.StatusConflict, "kategori ini sudah ada di cabang tersebut")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal membuat kategori beban operasional")
		return
	}

	qtx := h.queries.WithTx(tx)
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "operational_expense_category",
		EntityID:    categoryID.Bytes,
		Description: fmt.Sprintf("Menambahkan kategori beban operasional %q di cabang %q beserta akunnya", body.Name, branchName),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan kategori")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":           uuidBytesToString(categoryID.Bytes),
		"name":         body.Name,
		"division_id":  uuidBytesToString(divisionID.Bytes),
		"branch_id":    body.BranchID,
		"account_id":   uuidBytesToString(accountID.Bytes),
		"account_name": accountName,
		"is_system":    false,
	})
}

// DeleteCategory — DELETE /api/operational-expense-categories/{id}
//
// Refused on two counts, for two different reasons. A seeded category is refused
// because it exists in every branch by construction — that is what makes
// "listrik across all four branches" answerable, and a branch quietly missing
// one turns a comparison into a wrong answer rather than an obvious gap. Any
// category whose account carries journal lines is refused because deleting an
// account a balanced entry references would tear the books apart; the account is
// history now, and history is not deletable in this system.
func (h *OperationalExpensesHandler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	ctx := r.Context()

	var name string
	var isSystem bool
	var accountID pgtype.UUID
	var balance int64
	var lines, uses int
	err = h.pool.QueryRow(ctx, `
		SELECT oec.name, oec.is_system, oec.account_id, a.balance,
		       (SELECT COUNT(*) FROM journal_lines jl WHERE jl.account_id = oec.account_id),
		       (SELECT COUNT(*) FROM operational_expenses oe WHERE oe.category_id = oec.id)
		FROM operational_expense_categories oec
		JOIN accounts a ON a.id = oec.account_id
		WHERE oec.id = $1`, pgUUID(id)).Scan(&name, &isSystem, &accountID, &balance, &lines, &uses)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "kategori tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data kategori")
		return
	}
	if isSystem {
		respondError(w, http.StatusConflict,
			fmt.Sprintf("kategori %q adalah kategori bawaan dan tidak dapat dihapus", name))
		return
	}
	if lines > 0 || uses > 0 || balance != 0 {
		respondError(w, http.StatusConflict,
			fmt.Sprintf("kategori %q sudah dipakai dan tidak dapat dihapus — akunnya menyimpan riwayat transaksi", name))
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	// Category first: the account is still referenced by the FK until it is gone.
	if _, err := tx.Exec(ctx, `DELETE FROM operational_expense_categories WHERE id = $1`, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus kategori")
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghapus akun kategori")
		return
	}

	qtx := h.queries.WithTx(tx)
	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "DELETE",
		EntityType:  "operational_expense_category",
		EntityID:    id,
		Description: fmt.Sprintf("Menghapus kategori beban operasional %q beserta akunnya", name),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan perubahan")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "kategori berhasil dihapus"})
}

// operasionalDivision resolves a branch's system-owned Operasional division and
// the expense account its categories hang under.
func (h *OperationalExpensesHandler) operasionalDivision(ctx context.Context, branchID uuid.UUID) (pgtype.UUID, pgtype.UUID, string, string, error) {
	var divisionID, expenseAccountID pgtype.UUID
	var parentName, branchName string
	err := h.pool.QueryRow(ctx, `
		SELECT d.id, d.expense_account_id, a.name, b.name
		FROM divisions d
		JOIN branches b ON b.id = d.branch_id
		JOIN accounts a ON a.id = d.expense_account_id
		WHERE d.branch_id = $1 AND lower(d.name) = 'operasional'
		ORDER BY d.is_system DESC
		LIMIT 1`, pgUUID(branchID)).Scan(&divisionID, &expenseAccountID, &parentName, &branchName)
	if err != nil {
		return divisionID, expenseAccountID, "", "", errors.New("cabang ini belum punya divisi Operasional beserta akun bebannya")
	}
	return divisionID, expenseAccountID, parentName, branchName, nil
}

// seededOperationalCategories is the standard overhead breakdown every branch
// gets. It is fixed rather than per-branch on purpose: these rows exist in every
// branch by construction, which is what makes "listrik across all four branches"
// a question with an answer. A branch needing something else adds it; nothing
// stops a branch from simply never posting to one it does not pay.
//
// Keep in step with the seed list in migration 073 — that migration backfills
// existing branches, this creates them for new ones, and the two disagreeing
// would make a branch's breakdown depend on when it was opened.
var seededOperationalCategories = []string{
	"Listrik", "Air", "Sewa", "Internet", "Telepon",
	"Gas", "Kebersihan", "Keamanan", "Perbaikan", "Lain-lain",
}

// createOperasionalDivision gives a freshly-created branch its system-owned
// Operasional division, that division's three accounts, and the standard
// overhead sub-accounts beneath its expense account.
//
// Called inside the branch-creation transaction: a branch that exists without
// somewhere to book its electricity is exactly the gap migration 073 closed, and
// it would reopen the moment the next branch was added.
func createOperasionalDivision(ctx context.Context, tx pgx.Tx, branchID pgtype.UUID, branchName string) error {
	const nextExpenseNumber = `(SELECT COALESCE(MAX(account_number), 49999) + 1 FROM accounts WHERE account_number BETWEEN 50000 AND 59999)`

	var revID, expID, discID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO accounts (name, account_number, account_type)
		VALUES ($1, (SELECT COALESCE(MAX(account_number), 39999) + 1 FROM accounts
		             WHERE account_number BETWEEN 40000 AND 49999), 'revenue')
		RETURNING id`, "Pendapatan - "+branchName+" - Operasional").Scan(&revID); err != nil {
		return err
	}
	expenseName := "Beban - " + branchName + " - Operasional"
	if err := tx.QueryRow(ctx, `
		INSERT INTO accounts (name, account_number, account_type, is_system)
		VALUES ($1, `+nextExpenseNumber+`, 'expense', true)
		RETURNING id`, expenseName).Scan(&expID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO accounts (name, account_number, account_type)
		VALUES ($1, `+nextExpenseNumber+`, 'expense')
		RETURNING id`, "Diskon - "+branchName+" - Operasional").Scan(&discID); err != nil {
		return err
	}

	var divID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO divisions (branch_id, name, revenue_account_id, expense_account_id, discount_account_id, is_system)
		VALUES ($1, 'Operasional', $2, $3, $4, true)
		RETURNING id`, branchID, revID, expID, discID).Scan(&divID); err != nil {
		return err
	}

	for i, name := range seededOperationalCategories {
		var acctID pgtype.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO accounts (name, account_number, account_type, parent_id, is_system)
			VALUES ($1, `+nextExpenseNumber+`, 'expense', $2, true)
			RETURNING id`, expenseName+" - "+name, expID).Scan(&acctID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO operational_expense_categories (division_id, name, account_id, is_system, sort_order)
			VALUES ($1, $2, $3, true, $4)`, divID, name, acctID, (i+1)*10); err != nil {
			return err
		}
	}
	return nil
}

// ── Expenses ─────────────────────────────────────────────────────────────────

const operationalExpenseSelect = `
	SELECT oe.id::text AS id, oe.number, oe.date, oe.amount, oe.reference, oe.notes, oe.photo_path,
	       oe.status, oe.created_at, oe.cancelled_at, oe.cancel_reason,
	       oe.branch_id::text AS branch_id, b.name AS branch_name,
	       oe.category_id::text AS category_id, oec.name AS category_name,
	       oe.debit_account_id::text AS debit_account_id, da.account_number AS debit_account_number, da.name AS debit_account_name,
	       oe.credit_account_id::text AS credit_account_id, ca.account_number AS credit_account_number, ca.name AS credit_account_name,
	       oe.vendor_id::text AS vendor_id, v.name AS vendor_name,
	       cu.username AS created_by_name, xu.username AS cancelled_by_name
	FROM operational_expenses oe
	JOIN branches b ON b.id = oe.branch_id
	JOIN operational_expense_categories oec ON oec.id = oe.category_id
	JOIN accounts da ON da.id = oe.debit_account_id
	JOIN accounts ca ON ca.id = oe.credit_account_id
	LEFT JOIN vendors v ON v.id = oe.vendor_id
	LEFT JOIN users cu ON cu.id = oe.created_by
	LEFT JOIN users xu ON xu.id = oe.cancelled_by`

// List — GET /api/operational-expenses?branch_id=&category_id=&from=&to=&status=
//
// The response carries a per-category total alongside the rows: the question the
// page exists to answer is "what did this branch spend on electricity this
// month", and computing it from a truncated list on the client would answer it
// wrongly the moment the list grows past a page.
func (h *OperationalExpensesHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := []string{}
	var params []any

	add := func(clause string, value any) {
		params = append(params, value)
		where = append(where, fmt.Sprintf(clause, len(params)))
	}

	if v := q.Get("branch_id"); v != "" {
		id, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		add("oe.branch_id = $%d", pgUUID(id))
	}
	if v := q.Get("category_id"); v != "" {
		id, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "category_id tidak valid")
			return
		}
		add("oe.category_id = $%d", pgUUID(id))
	}
	if v := q.Get("from"); v != "" {
		d, err := parseDayParam(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal awal tidak valid")
			return
		}
		add("oe.date >= $%d", pgtype.Date{Time: d, Valid: true})
	}
	if v := q.Get("to"); v != "" {
		d, err := parseDayParam(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal akhir tidak valid")
			return
		}
		add("oe.date <= $%d", pgtype.Date{Time: d, Valid: true})
	}
	if v := q.Get("status"); v != "" {
		add("oe.status = $%d", v)
	}

	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	ctx := r.Context()
	rows, err := h.pool.Query(ctx, operationalExpenseSelect+clause+` ORDER BY oe.date DESC, oe.created_at DESC`, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data beban operasional")
		return
	}
	expenses, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data beban operasional")
		return
	}

	// Cancelled rows are listed but excluded from the totals — they are reversed
	// in the ledger, so counting them would disagree with the P&L.
	totalClause := clause
	if totalClause == "" {
		totalClause = " WHERE oe.status = 'posted'"
	} else {
		totalClause += " AND oe.status = 'posted'"
	}
	sumRows, err := h.pool.Query(ctx, `
		SELECT oec.id::text AS category_id, oec.name AS category_name, b.name AS branch_name,
		       SUM(oe.amount)::BIGINT AS total, COUNT(*)::INT AS entries
		FROM operational_expenses oe
		JOIN operational_expense_categories oec ON oec.id = oe.category_id
		JOIN branches b ON b.id = oe.branch_id`+totalClause+`
		GROUP BY oec.id, oec.name, b.name
		ORDER BY total DESC`, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung total beban operasional")
		return
	}
	byCategory, err := pgx.CollectRows(sumRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses total beban operasional")
		return
	}

	var total int64
	for _, row := range byCategory {
		total += toInt64(row["total"])
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"expenses":    nilToEmpty(expenses),
		"by_category": nilToEmpty(byCategory),
		"total":       total,
	})
}

// Create — POST /api/operational-expenses
func (h *OperationalExpensesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date            string `json:"date"`
		BranchID        string `json:"branch_id"`
		CategoryID      string `json:"category_id"`
		CreditAccountID string `json:"credit_account_id"`
		VendorID        string `json:"vendor_id"`
		Amount          int64  `json:"amount"`
		Reference       string `json:"reference"`
		Notes           string `json:"notes"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "format permintaan tidak valid")
		return
	}
	if body.Amount <= 0 {
		respondError(w, http.StatusBadRequest, "jumlah harus lebih dari 0")
		return
	}
	date, err := parseDayParam(body.Date)
	if err != nil {
		respondError(w, http.StatusBadRequest, "tanggal tidak valid")
		return
	}
	categoryID, err := parseUUID(body.CategoryID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "kategori tidak valid")
		return
	}
	creditID, err := parseUUID(body.CreditAccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "akun sumber dana tidak valid")
		return
	}

	ctx := r.Context()

	// The branch is taken from the category rather than from the request: the
	// category already belongs to exactly one branch's Operasional division, and
	// trusting a separate branch_id would let a mismatched pair book a Cimanggu
	// bill against Warung Jambu's account.
	var branchID, divisionID, debitID pgtype.UUID
	var categoryName, branchName, debitName string
	err = h.pool.QueryRow(ctx, `
		SELECT d.branch_id, oec.division_id, oec.account_id, oec.name, b.name, a.name
		FROM operational_expense_categories oec
		JOIN divisions d ON d.id = oec.division_id
		JOIN branches b ON b.id = d.branch_id
		JOIN accounts a ON a.id = oec.account_id
		WHERE oec.id = $1`, pgUUID(categoryID)).
		Scan(&branchID, &divisionID, &debitID, &categoryName, &branchName, &debitName)
	if err != nil {
		respondError(w, http.StatusNotFound, "kategori beban operasional tidak ditemukan")
		return
	}
	if debitID.Bytes == creditID {
		respondError(w, http.StatusBadRequest, "akun beban dan akun sumber dana tidak boleh sama")
		return
	}

	credit, err := h.queries.GetAccountByID(ctx, pgUUID(creditID))
	if err != nil {
		respondError(w, http.StatusNotFound, "akun sumber dana tidak ditemukan")
		return
	}

	vendorID := pgtype.UUID{}
	if strings.TrimSpace(body.VendorID) != "" {
		id, err := parseUUID(body.VendorID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "vendor tidak valid")
			return
		}
		vendorID = pgUUID(id)
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	var number string
	if err := tx.QueryRow(ctx,
		`SELECT 'BO-' || LPAD(nextval('operational_expense_number_seq')::TEXT, 6, '0')`).Scan(&number); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor beban operasional")
		return
	}

	var id pgtype.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO operational_expenses
		  (number, date, branch_id, category_id, debit_account_id, credit_account_id,
		   amount, vendor_id, reference, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id`,
		number, pgtype.Date{Time: date, Valid: true}, branchID, pgUUID(categoryID),
		debitID, pgUUID(creditID), body.Amount, vendorID,
		strings.TrimSpace(body.Reference), strings.TrimSpace(body.Notes), pgUserID(ctx)).Scan(&id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan beban operasional")
		return
	}

	qtx := h.queries.WithTx(tx)
	description := fmt.Sprintf("Beban operasional %s: %s - %s (%s)", number, branchName, categoryName, credit.Name)

	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        date,
		SourceType:  service.SourceOperationalExpense,
		SourceID:    id.Bytes,
		Description: description,
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Dr(debitID.Bytes, body.Amount),
			service.Cr(creditID, body.Amount),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal beban operasional")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CREATE",
		EntityType:  "operational_expense",
		EntityID:    id.Bytes,
		Description: fmt.Sprintf("%s sebesar %d", description, body.Amount),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan beban operasional")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":     uuidBytesToString(id.Bytes),
		"number": number,
	})
}

// Cancel — POST /api/operational-expenses/{id}/cancel
//
// Reverse-and-keep, like every other posted document here: the row and its
// number stay, and a compensating entry dated today undoes the ledger. Dating
// the reversal today rather than on the original date is deliberate — a
// cancellation must not reach back into a month that has already been reported.
func (h *OperationalExpensesHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = parseBody(r, &body)
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		respondError(w, http.StatusBadRequest, "alasan pembatalan wajib diisi")
		return
	}

	ctx := r.Context()

	var number, status string
	var amount int64
	var debitID, creditID pgtype.UUID
	err = h.pool.QueryRow(ctx, `
		SELECT number, status, amount, debit_account_id, credit_account_id
		FROM operational_expenses WHERE id = $1`, pgUUID(id)).
		Scan(&number, &status, &amount, &debitID, &creditID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, http.StatusNotFound, "beban operasional tidak ditemukan")
			return
		}
		respondError(w, http.StatusInternalServerError, "gagal mengambil data beban operasional")
		return
	}
	if status == "cancelled" {
		respondError(w, http.StatusConflict, "beban operasional ini sudah dibatalkan")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)
	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        time.Now(),
		SourceType:  service.SourceOperationalExpense,
		SourceID:    id,
		Description: fmt.Sprintf("Pembatalan beban operasional %s", number),
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines: []service.Line{
			service.Dr(creditID.Bytes, amount),
			service.Cr(debitID.Bytes, amount),
		},
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mencatat jurnal pembatalan")
		return
	}

	if _, err := tx.Exec(ctx, `
		UPDATE operational_expenses
		SET status = 'cancelled', cancelled_by = $1, cancelled_at = now(), cancel_reason = $2
		WHERE id = $3`, pgUserID(ctx), body.Reason, pgUUID(id)); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membatalkan beban operasional")
		return
	}

	_ = service.LogActivity(ctx, qtx, service.LogParams{
		UserID:      middleware.UserIDFromCtx(ctx),
		Username:    middleware.UsernameFromCtx(ctx),
		Action:      "CANCEL",
		EntityType:  "operational_expense",
		EntityID:    id,
		Description: fmt.Sprintf("Membatalkan beban operasional %s: %s", number, body.Reason),
	})

	if err := tx.Commit(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan pembatalan")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "beban operasional berhasil dibatalkan"})
}
