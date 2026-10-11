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

// parseMonthParam accepts "YYYY-MM" — what a month input sends — or a full
// "YYYY-MM-DD", and normalises either to the first day of that month. Accepting
// both means the API is usable from a date picker or a month picker without the
// caller having to know which one the column wants.
func parseMonthParam(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("bulan tagihan wajib diisi")
	}
	if t, err := time.Parse("2006-01", raw); err == nil {
		return t, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), nil
}

// ── Expenses ─────────────────────────────────────────────────────────────────

const operationalExpenseSelect = `
	SELECT oe.id::text AS id, oe.number, oe.date, oe.period_month, oe.amount, oe.reference, oe.notes, oe.photo_path,
	       oe.billing, oe.due_date, oe.term_months,
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
		PeriodMonth     string `json:"period_month"`
		BranchID        string `json:"branch_id"`
		CategoryID      string `json:"category_id"`
		CreditAccountID string `json:"credit_account_id"`
		VendorID        string `json:"vendor_id"`
		Amount          int64  `json:"amount"`
		Reference       string `json:"reference"`
		Notes           string `json:"notes"`
		// "monthly" (default) or "annual". Annual is rent paid a year up front:
		// booked to the branch's prepaid rent asset and expensed a twelfth a month.
		Billing string `json:"billing"`
		// Tanggal jatuh tempo. Optional on a monthly bill; on an annual one it is
		// required and is the first day of the lease year.
		DueDate string `json:"due_date"`
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
	dueDate := pgtype.Date{}
	if strings.TrimSpace(body.DueDate) != "" {
		d, err := parseDayParam(body.DueDate)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal jatuh tempo tidak valid")
			return
		}
		dueDate = pgtype.Date{Time: d, Valid: true}
	}
	billing := strings.TrimSpace(body.Billing)
	if billing == "" {
		billing = billingMonthly
	}
	var period time.Time
	switch billing {
	case billingMonthly:
		// The month the bill covers, which is not the day it was paid — July's
		// electricity settled in August belongs to July everywhere except the ledger.
		period, err = parseMonthParam(body.PeriodMonth)
		if err != nil {
			respondError(w, http.StatusBadRequest, "bulan tagihan tidak valid")
			return
		}
	case billingAnnual:
		// The lease year starts on the due date, so that is the month the first
		// twelfth is used up in; any period_month sent alongside is ignored rather
		// than allowed to disagree with it.
		if !dueDate.Valid {
			respondError(w, http.StatusBadRequest, "tanggal jatuh tempo wajib diisi untuk sewa tahunan")
			return
		}
		period = time.Date(dueDate.Time.Year(), dueDate.Time.Month(), 1, 0, 0, 0, 0, time.UTC)
		if body.Amount < service.AnnualTermMonths {
			respondError(w, http.StatusBadRequest, "harga sewa tahunan terlalu kecil untuk dibagi 12 bulan")
			return
		}
	default:
		respondError(w, http.StatusBadRequest, "jenis pembayaran tidak valid (monthly | annual)")
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
	// Annual billing is offered for rent only: it is the one standing bill that
	// is routinely settled a year up front, and keeping it there keeps the
	// Statistik page's other rows strictly monthly.
	if billing == billingAnnual && !strings.EqualFold(strings.TrimSpace(categoryName), "sewa") {
		respondError(w, http.StatusBadRequest, "pembayaran tahunan hanya tersedia untuk Sewa")
		return
	}
	// The category's own account is where the cost ends up either way; on an
	// annual payment it is reached a month at a time through the schedule.
	expenseAccountID := debitID

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

	termMonths := 1
	if billing == billingAnnual {
		termMonths = service.AnnualTermMonths
		// Paid rent is an asset until it is used: the payment debits the branch's
		// prepaid rent account, not the expense.
		prepaidID, err := ensurePrepaidRentAccount(ctx, tx, branchID, branchName)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal menyiapkan akun sewa dibayar di muka")
			return
		}
		if prepaidID.Bytes == creditID {
			respondError(w, http.StatusBadRequest, "akun sewa dibayar di muka tidak dapat menjadi sumber dana")
			return
		}
		debitID = prepaidID
	}

	var number string
	if err := tx.QueryRow(ctx,
		`SELECT 'BO-' || LPAD(nextval('operational_expense_number_seq')::TEXT, 6, '0')`).Scan(&number); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal membuat nomor beban operasional")
		return
	}

	var id pgtype.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO operational_expenses
		  (number, date, period_month, branch_id, category_id, debit_account_id, credit_account_id,
		   amount, vendor_id, reference, notes, created_by, billing, due_date, term_months)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id`,
		number, pgtype.Date{Time: date, Valid: true}, pgtype.Date{Time: period, Valid: true},
		branchID, pgUUID(categoryID), debitID, pgUUID(creditID), body.Amount, vendorID,
		strings.TrimSpace(body.Reference), strings.TrimSpace(body.Notes), pgUserID(ctx),
		billing, dueDate, termMonths).Scan(&id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menyimpan beban operasional")
		return
	}

	qtx := h.queries.WithTx(tx)
	description := fmt.Sprintf("Beban operasional %s: %s - %s %s (%s)",
		number, branchName, categoryName, period.Format("Jan 2006"), credit.Name)
	if billing == billingAnnual {
		last := period.AddDate(0, service.AnnualTermMonths-1, 0)
		description = fmt.Sprintf("Sewa tahunan %s: %s - %s %s s/d %s (%s)",
			number, branchName, categoryName, period.Format("Jan 2006"), last.Format("Jan 2006"), credit.Name)
	}

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

	amortized := 0
	if billing == billingAnnual {
		for _, s := range service.AmortizationSchedule(body.Amount, period, service.AnnualTermMonths) {
			if _, err := tx.Exec(ctx, `
				INSERT INTO operational_expense_amortizations
				  (expense_id, seq, period_month, amount, expense_account_id, prepaid_account_id)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				id, s.Seq, pgtype.Date{Time: s.PeriodMonth, Valid: true}, s.Amount, expenseAccountID, debitID); err != nil {
				respondError(w, http.StatusInternalServerError, "gagal menyimpan jadwal amortisasi")
				return
			}
		}
		// A lease whose year has already begun gets its elapsed months expensed
		// now, in this transaction, rather than at the next background sweep.
		amortized, err = service.PostDueAmortizationsFor(ctx, tx, qtx, id.Bytes, time.Now(), middleware.UserIDFromCtx(ctx))
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mencatat amortisasi sewa")
			return
		}
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
		"id":               uuidBytesToString(id.Bytes),
		"number":           number,
		"period_month":     period.Format("2006-01-02"),
		"billing":          billing,
		"months_amortized": amortized,
	})
}

const (
	billingMonthly = "monthly"
	billingAnnual  = "annual"
)

// ensurePrepaidRentAccount returns the branch's "Sewa Dibayar di Muka" account,
// creating it on the branch's first annual rent. Numbered 10501-10599 under the
// system parent 10500 — outside 11000-19999, where GetNextInventoryAccountNumber
// takes MAX + 1 and any other account would shift the next warehouse's number.
//
// The branch row is locked so two annual payments recorded at once cannot each
// create an account for the same branch.
func ensurePrepaidRentAccount(ctx context.Context, tx pgx.Tx, branchID pgtype.UUID, branchName string) (pgtype.UUID, error) {
	var existing pgtype.UUID
	if err := tx.QueryRow(ctx,
		`SELECT prepaid_rent_account_id FROM branches WHERE id = $1 FOR UPDATE`, branchID).Scan(&existing); err != nil {
		return pgtype.UUID{}, err
	}
	if existing.Valid {
		return existing, nil
	}

	var id pgtype.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO accounts (name, account_number, account_type, parent_id)
		SELECT $1, n.next, 'asset', p.id
		FROM (SELECT COALESCE(MAX(account_number), 10500) + 1 AS next
		      FROM accounts WHERE account_number BETWEEN 10501 AND 10599) n,
		     (SELECT id FROM accounts WHERE account_number = 10500) p
		WHERE n.next <= 10599
		RETURNING id`, "Sewa Dibayar di Muka - "+branchName).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, errors.New("nomor akun sewa dibayar di muka (10501-10599) sudah habis")
		}
		return pgtype.UUID{}, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE branches SET prepaid_rent_account_id = $1 WHERE id = $2`, id, branchID); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
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

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memulai transaksi")
		return
	}
	defer tx.Rollback(ctx)

	// Locked: the amortization poster checks the payment's status before posting
	// a month, so the status read here must not go stale mid-cancellation.
	var number, status, billing string
	var amount int64
	var debitID, creditID pgtype.UUID
	err = tx.QueryRow(ctx, `
		SELECT number, status, billing, amount, debit_account_id, credit_account_id
		FROM operational_expenses WHERE id = $1 FOR UPDATE`, pgUUID(id)).
		Scan(&number, &status, &billing, &amount, &debitID, &creditID)
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

	// A monthly bill reverses as it was posted. An annual one has, by now, moved
	// part of the payment from prepaid rent into expense, so the reversal credits
	// each of those back where it currently sits: the months already expensed
	// come off the expense account, and the rest off prepaid rent. Every month
	// posted is still undone — reverse-and-keep, never un-post.
	lines := []service.Line{
		service.Dr(creditID.Bytes, amount),
		service.Cr(debitID.Bytes, amount),
	}
	if billing == billingAnnual {
		rows, err := tx.Query(ctx, `
			SELECT expense_account_id, status, amount
			FROM operational_expense_amortizations
			WHERE expense_id = $1 FOR UPDATE`, pgUUID(id))
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca jadwal amortisasi")
			return
		}
		var postedTotal int64
		postedByAccount := map[uuid.UUID]int64{}
		var order []uuid.UUID
		for rows.Next() {
			var acct uuid.UUID
			var rowStatus string
			var rowAmount int64
			if err := rows.Scan(&acct, &rowStatus, &rowAmount); err != nil {
				rows.Close()
				respondError(w, http.StatusInternalServerError, "gagal membaca jadwal amortisasi")
				return
			}
			if rowStatus != "posted" {
				continue
			}
			if _, seen := postedByAccount[acct]; !seen {
				order = append(order, acct)
			}
			postedByAccount[acct] += rowAmount
			postedTotal += rowAmount
		}
		rows.Close()
		if rows.Err() != nil {
			respondError(w, http.StatusInternalServerError, "gagal membaca jadwal amortisasi")
			return
		}

		lines = []service.Line{
			service.Dr(creditID.Bytes, amount),
			service.Cr(debitID.Bytes, amount-postedTotal),
		}
		for _, acct := range order {
			lines = append(lines, service.Cr(acct, postedByAccount[acct]))
		}

		if _, err := tx.Exec(ctx, `
			UPDATE operational_expense_amortizations SET status = 'cancelled'
			WHERE expense_id = $1 AND status = 'scheduled'`, pgUUID(id)); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal membatalkan jadwal amortisasi")
			return
		}
	}

	qtx := h.queries.WithTx(tx)
	if _, err := service.Post(ctx, qtx, service.Entry{
		Date:        time.Now(),
		SourceType:  service.SourceOperationalExpense,
		SourceID:    id,
		Description: fmt.Sprintf("Pembatalan beban operasional %s", number),
		CreatedBy:   middleware.UserIDFromCtx(ctx),
		Lines:       lines,
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

// ListAnnual — GET /api/operational-expenses/annual?branch_id=&include_cancelled=
//
// Every annual rent payment with its amortization schedule: what has been
// expensed so far, what is still sitting in prepaid rent, and when the next
// year falls due. This is what the Sewa page's "Tahunan" tab draws.
//
// "Renewed" means a later annual payment exists for the same branch and
// category. The tab uses it to stop flagging a lease's upcoming due date once
// the next year has already been paid.
func (h *OperationalExpensesHandler) ListAnnual(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := []string{"oe.billing = 'annual'"}
	var params []any
	if v := q.Get("branch_id"); v != "" {
		id, err := parseUUID(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		params = append(params, pgUUID(id))
		where = append(where, fmt.Sprintf("oe.branch_id = $%d", len(params)))
	}
	if q.Get("include_cancelled") != "true" {
		where = append(where, "oe.status = 'posted'")
	}

	ctx := r.Context()
	rows, err := h.pool.Query(ctx, operationalExpenseSelect+` WHERE `+strings.Join(where, " AND ")+
		` ORDER BY oe.due_date DESC, oe.created_at DESC`, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data sewa tahunan")
		return
	}
	leases, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data sewa tahunan")
		return
	}

	ids := make([]string, 0, len(leases))
	for _, l := range leases {
		ids = append(ids, l["id"].(string))
	}

	type slice struct {
		Seq         int        `json:"seq"`
		PeriodMonth string     `json:"period_month"`
		Amount      int64      `json:"amount"`
		Status      string     `json:"status"`
		PostedAt    *time.Time `json:"posted_at"`
		LastError   string     `json:"last_error"`
	}
	schedules := map[string][]slice{}
	if len(ids) > 0 {
		sRows, err := h.pool.Query(ctx, `
			SELECT expense_id::text, seq, to_char(period_month, 'YYYY-MM-DD'), amount, status, posted_at, last_error
			FROM operational_expense_amortizations
			WHERE expense_id = ANY($1::uuid[])
			ORDER BY expense_id, seq`, ids)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "gagal mengambil jadwal amortisasi")
			return
		}
		for sRows.Next() {
			var expenseID string
			var s slice
			if err := sRows.Scan(&expenseID, &s.Seq, &s.PeriodMonth, &s.Amount, &s.Status, &s.PostedAt, &s.LastError); err != nil {
				sRows.Close()
				respondError(w, http.StatusInternalServerError, "gagal memproses jadwal amortisasi")
				return
			}
			schedules[expenseID] = append(schedules[expenseID], s)
		}
		sRows.Close()
		if sRows.Err() != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses jadwal amortisasi")
			return
		}
	}

	type key struct{ branch, category string }
	latestDue := map[key]time.Time{}
	for _, l := range leases {
		if l["status"] != "posted" {
			continue
		}
		due, _ := l["due_date"].(time.Time)
		k := key{l["branch_id"].(string), l["category_id"].(string)}
		if due.After(latestDue[k]) {
			latestDue[k] = due
		}
	}

	out := make([]map[string]any, 0, len(leases))
	for _, l := range leases {
		id := l["id"].(string)
		sched := schedules[id]
		if sched == nil {
			sched = []slice{}
		}
		var amortized int64
		posted := 0
		for _, s := range sched {
			if s.Status == "posted" {
				amortized += s.Amount
				posted++
			}
		}
		due, _ := l["due_date"].(time.Time)
		start := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.UTC)
		nextDue := start.AddDate(0, service.AnnualTermMonths, 0)
		k := key{l["branch_id"].(string), l["category_id"].(string)}

		l["schedule"] = sched
		l["amortized"] = amortized
		l["months_posted"] = posted
		l["remaining"] = toInt64(l["amount"]) - amortized
		l["coverage_start"] = start.Format("2006-01-02")
		l["coverage_end"] = nextDue.AddDate(0, 0, -1).Format("2006-01-02")
		l["next_due_date"] = nextDue.Format("2006-01-02")
		l["renewed"] = latestDue[k].After(start)
		out = append(out, l)
	}

	respondJSON(w, http.StatusOK, out)
}
