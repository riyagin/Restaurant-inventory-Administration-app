package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Per-branch profit & loss.
//
// Branch ownership is not a dimension on the journal — `journal_lines` carries
// only an account. It does not need to be: revenue and expense accounts are
// branch-scoped by construction. A branch owns a revenue and an expense account;
// each of its divisions owns a revenue, expense and discount account; and the
// accounts hung underneath those (expense categories, `Beban Gaji - <cabang>`)
// inherit the owner of their ancestor. So the split is a question about the chart
// of accounts, resolved once here, and the amounts come from the journal.
//
// The amounts deliberately come from `journal_lines` rather than from the source
// tables the org-wide period report sums (sales, pos_import_lines, invoices).
// Those three are only part of what moves a P&L account: dispatch usage debits a
// division's expense parent directly, payroll debits `Beban Gaji`, opname writes
// off to `Selisih Persediaan`, enumerations move value between items, and manual
// adjustments post wherever they are told to. The journal is the one place that
// has all of it, and `GET /api/accounts/trial-balance` is what proves it agrees
// with the cached balances.

// unallocatedBranchKey labels the column for revenue and expense accounts that
// belong to no branch — general administration, suspense, anything posted above
// the branch level. They are reported rather than hidden, and rather than spread
// across branches by some revenue-share guess: a branch's profit should carry the
// costs it actually owns.
const unallocatedBranchKey = "unallocated"

// accountBranchOwnerSQL maps every revenue/expense account to the branch that
// owns it. The recursive leg walks *down* from each directly-linked account, so a
// division's expense categories and a branch's wage account land on the right
// branch without needing their own link.
//
// DISTINCT ON keeps the shallowest match if an account is somehow reachable from
// two branches (a hand-edited parent_id), which is better than double-counting it.
const accountBranchOwnerSQL = `
WITH RECURSIVE direct AS (
  SELECT b.id AS branch_id, b.revenue_account_id AS account_id FROM branches b WHERE b.revenue_account_id IS NOT NULL
  UNION SELECT b.id, b.expense_account_id  FROM branches b  WHERE b.expense_account_id  IS NOT NULL
  UNION SELECT d.branch_id, d.revenue_account_id  FROM divisions d WHERE d.revenue_account_id  IS NOT NULL AND d.branch_id IS NOT NULL
  UNION SELECT d.branch_id, d.expense_account_id  FROM divisions d WHERE d.expense_account_id  IS NOT NULL AND d.branch_id IS NOT NULL
  UNION SELECT d.branch_id, d.discount_account_id FROM divisions d WHERE d.discount_account_id IS NOT NULL AND d.branch_id IS NOT NULL
),
owned AS (
  SELECT account_id, branch_id, 0 AS depth FROM direct
  UNION ALL
  SELECT a.id, o.branch_id, o.depth + 1
  FROM accounts a JOIN owned o ON a.parent_id = o.account_id
)
SELECT DISTINCT ON (account_id) account_id, branch_id
FROM owned ORDER BY account_id, depth`

// ProfitLossByBranch — GET /api/reports/profit-loss-by-branch
// Params: start_date (or from), end_date (or to) — both optional; omitting them
// reports every journal entry ever posted.
func (h *ReportsHandler) ProfitLossByBranch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startDate := firstNonEmpty(q.Get("start_date"), q.Get("from"))
	endDate := firstNonEmpty(q.Get("end_date"), q.Get("to"))
	usePeriod := startDate != "" && endDate != ""
	ctx := r.Context()

	// ── Branch columns ──
	type branchCol struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	bRows, err := h.pool.Query(ctx, `SELECT id, name FROM branches ORDER BY name`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data cabang")
		return
	}
	branchCols, err := pgx.CollectRows(bRows, func(row pgx.CollectableRow) (branchCol, error) {
		var id pgtype.UUID
		var name string
		if err := row.Scan(&id, &name); err != nil {
			return branchCol{}, err
		}
		return branchCol{ID: uuidBytesToString(id.Bytes), Name: name}, nil
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data cabang")
		return
	}

	// ── Account → owning branch ──
	ownerRows, err := h.pool.Query(ctx, accountBranchOwnerSQL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memetakan akun ke cabang")
		return
	}
	ownerOf := map[string]string{}
	for ownerRows.Next() {
		var accountID, branchID pgtype.UUID
		if err := ownerRows.Scan(&accountID, &branchID); err == nil && accountID.Valid && branchID.Valid {
			ownerOf[uuidBytesToString(accountID.Bytes)] = uuidBytesToString(branchID.Bytes)
		}
	}
	ownerRows.Close()
	if err := ownerRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memetakan akun ke cabang")
		return
	}

	// ── Period activity per P&L account, from the journal ──
	//
	// `journal_lines.amount` is debit-positive. Natural sign is +amount for an
	// expense and -amount for revenue, which is the same convention
	// `accounts.balance` is cached in, so the two are directly comparable.
	activitySQL := `
SELECT jl.account_id,
       SUM(CASE WHEN a.account_type = 'revenue' THEN -jl.amount ELSE jl.amount END)::BIGINT AS total
FROM journal_lines jl
JOIN journal_entries je ON je.id = jl.entry_id
JOIN accounts a ON a.id = jl.account_id
WHERE a.account_type IN ('revenue', 'expense')`
	var params []any
	if usePeriod {
		activitySQL += ` AND je.entry_date BETWEEN $1 AND $2`
		params = []any{startDate, endDate}
	}
	activitySQL += ` GROUP BY jl.account_id`

	actRows, err := h.pool.Query(ctx, activitySQL, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung mutasi laba rugi")
		return
	}
	amountOf := map[string]int64{}
	for actRows.Next() {
		var accountID pgtype.UUID
		var total int64
		if err := actRows.Scan(&accountID, &total); err == nil && accountID.Valid {
			amountOf[uuidBytesToString(accountID.Bytes)] = total
		}
	}
	actRows.Close()
	if err := actRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung mutasi laba rugi")
		return
	}

	// ── The P&L chart of accounts ──
	//
	// Every revenue/expense account is returned, including the ones with no
	// movement in the period: the frontend needs the parent rows to build the
	// tree, and hiding an empty leaf is a presentation decision it already makes.
	accRows, err := h.pool.Query(ctx,
		`SELECT id, account_number, name, account_type, parent_id
		 FROM accounts
		 WHERE account_type IN ('revenue', 'expense')
		 ORDER BY account_type, account_number NULLS LAST, name`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data akun")
		return
	}
	defer accRows.Close()

	type plAccount struct {
		ID            string           `json:"id"`
		AccountNumber pgtype.Int4      `json:"account_number"`
		Name          string           `json:"name"`
		AccountType   string           `json:"account_type"`
		ParentID      pgtype.UUID      `json:"parent_id"`
		Amounts       map[string]int64 `json:"amounts"`
		Total         int64            `json:"total"`
	}

	accounts := []plAccount{}
	hasUnallocated := false
	for accRows.Next() {
		var id pgtype.UUID
		var acc plAccount
		if err := accRows.Scan(&id, &acc.AccountNumber, &acc.Name, &acc.AccountType, &acc.ParentID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses data akun")
			return
		}
		acc.ID = uuidBytesToString(id.Bytes)

		// An account sits in exactly one column: its own activity is attributed
		// to its owning branch, or to Umum when it has none. Amounts are kept as
		// a per-column map so the frontend can roll a parent's children up into
		// the same columns without re-deriving ownership.
		column := unallocatedBranchKey
		if owner, ok := ownerOf[acc.ID]; ok {
			column = owner
		} else {
			hasUnallocated = true
		}
		acc.Total = amountOf[acc.ID]
		acc.Amounts = map[string]int64{column: acc.Total}
		accounts = append(accounts, acc)
	}
	if err := accRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data akun")
		return
	}

	columns := make([]branchCol, 0, len(branchCols)+1)
	columns = append(columns, branchCols...)
	if hasUnallocated {
		columns = append(columns, branchCol{ID: unallocatedBranchKey, Name: "Umum"})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"start_date": startDate,
		"end_date":   endDate,
		"is_period":  usePeriod,
		"columns":    columns,
		"accounts":   accounts,
	})
}
