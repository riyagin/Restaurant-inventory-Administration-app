package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Reading the chart of accounts one branch at a time.
//
// Two different questions hide inside "show me this branch's accounts", and they
// have different answers:
//
//   1. Which accounts does this branch *own*? Answered by the chart of accounts
//      alone — `accountBranchOwnerSQL`. A branch owns its revenue and expense
//      accounts, its divisions' revenue/expense/discount accounts, its petty cash
//      box, and everything hung underneath any of those. For these accounts the
//      whole balance is the branch's, by construction, and no attribution is
//      involved.
//
//   2. How much of a *shared* account belongs to this branch? Kas, Utang Usaha -
//      <vendor>, Persediaan - <gudang> and equity belong to nobody in particular,
//      and `journal_lines` carries no branch dimension. The only honest answer is
//      to walk each entry back to what caused it, which works for most sources
//      and for none of the rest — so the untraceable remainder is reported as its
//      own figure rather than being silently spread or silently dropped.
//
// The second is a derivation, not a fact, which is why it is kept separate from
// the first everywhere it surfaces.

// entryBranchSQL resolves each journal entry to the branch that caused it, where
// that is knowable. It emits (entry_id, branch_id) and omits entries it cannot
// place — a LEFT JOIN against it is the intended usage, so "no branch" stays
// distinguishable from "branch X".
//
// Two independent legs, in priority order:
//
//   * the source document. `journal_entries.source_id` points at the row that
//     caused the entry, and most of those rows carry a branch_id outright. This
//     is authoritative: an invoice knows its branch even when its journal legs
//     (Dr Persediaan / Cr Utang Usaha) touch no branch-owned account at all.
//
//   * the entry's own lines. If every branch-owned account an entry touches
//     belongs to one branch, that is the entry's branch. This covers the sources
//     with no branch column — a POS import is placed by the division revenue
//     accounts it credits, exactly as `pos_settlement_by_branch` places its
//     payment lines.
//
// An entry whose lines span two branches resolves to neither. That is deliberate
// and it is the right answer for a closed payroll period, which debits several
// branches' wage accounts in one balanced entry: picking one of them would be
// fiction, and splitting the entry here would be a second definition of payroll's
// branch split. Those entries land in the untraceable remainder, where they are
// visible.
const entryBranchSQL = `WITH RECURSIVE ` + accountBranchOwnerCTE + `, owner AS (
  SELECT DISTINCT ON (account_id) account_id, branch_id FROM owned ORDER BY account_id, depth
), doc_branch AS (
  SELECT je.id AS entry_id,
         COALESCE(inv.branch_id, disp.branch_id, dp.branch_id, oe.branch_id, cd.branch_id, s.branch_id, emp.branch_id) AS branch_id
  FROM journal_entries je
  -- 'invoice_payment' carries the invoice id as its source, so both settle here.
  LEFT JOIN invoices             inv  ON je.source_type IN ('invoice', 'invoice_payment') AND inv.id  = je.source_id
  LEFT JOIN dispatches           disp ON je.source_type = 'dispatch'            AND disp.id = je.source_id
  LEFT JOIN daily_purchases      dp   ON je.source_type = 'daily_purchase'      AND dp.id   = je.source_id
  LEFT JOIN operational_expenses oe   ON je.source_type = 'operational_expense' AND oe.id   = je.source_id
  LEFT JOIN cash_deposits        cd   ON je.source_type = 'cash_deposit'        AND cd.id   = je.source_id
  LEFT JOIN sales                s    ON je.source_type = 'sale'                AND s.id    = je.source_id
  -- A kasbon belongs to the branch of the employee who took it.
  LEFT JOIN kasbons              k    ON je.source_type = 'kasbon'              AND k.id    = je.source_id
  LEFT JOIN employees            emp  ON emp.id = k.employee_id
), line_branch AS (
  SELECT jl.entry_id,
         (array_agg(DISTINCT ow.branch_id))[1] AS branch_id,
         COUNT(DISTINCT ow.branch_id)          AS branch_count
  FROM journal_lines jl
  JOIN owner ow ON ow.account_id = jl.account_id
  GROUP BY jl.entry_id
)
SELECT je.id AS entry_id,
       COALESCE(db.branch_id, CASE WHEN lb.branch_count = 1 THEN lb.branch_id END) AS branch_id
FROM journal_entries je
LEFT JOIN doc_branch  db ON db.entry_id = je.id
LEFT JOIN line_branch lb ON lb.entry_id = je.id
WHERE COALESCE(db.branch_id, CASE WHEN lb.branch_count = 1 THEN lb.branch_id END) IS NOT NULL`

// branchAmountsForAccounts returns, per account, how much of its balance is
// traceable to the given branch and how much of it is traceable to no branch at
// all. Both figures use the same natural-sign convention as `accounts.balance`,
// so they are directly comparable with it.
//
// The untraceable figure is per account rather than a single page total on
// purpose: "Rp 43jt of Kas cannot be placed" is actionable, "Rp 43jt somewhere
// cannot be placed" is not.
func (h *AccountsHandler) branchAmountsForAccounts(r *http.Request, branchID string) (map[string]int64, map[string]int64, error) {
	sql := `
		WITH entry_branch AS (` + entryBranchSQL + `)
		SELECT jl.account_id::text,
		       COALESCE(SUM(` + naturalDeltaSQL + `) FILTER (WHERE eb.branch_id = $1::uuid), 0)::BIGINT AS branch_amount,
		       COALESCE(SUM(` + naturalDeltaSQL + `) FILTER (WHERE eb.branch_id IS NULL), 0)::BIGINT   AS unplaced_amount
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id
		JOIN accounts a ON a.id = jl.account_id
		LEFT JOIN entry_branch eb ON eb.entry_id = je.id
		GROUP BY jl.account_id`

	rows, err := h.pool.Query(r.Context(), sql, branchID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	branchAmount := map[string]int64{}
	unplaced := map[string]int64{}
	for rows.Next() {
		var id string
		var amount, un int64
		if err := rows.Scan(&id, &amount, &un); err != nil {
			return nil, nil, err
		}
		branchAmount[id] = amount
		unplaced[id] = un
	}
	return branchAmount, unplaced, rows.Err()
}

// accountOwners returns account id → owning branch id for every branch-owned
// account.
func (h *AccountsHandler) accountOwners(r *http.Request) (map[string]string, error) {
	rows, err := h.pool.Query(r.Context(), accountBranchOwnerSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var accountID, branchID pgtype.UUID
		if err := rows.Scan(&accountID, &branchID); err != nil {
			return nil, err
		}
		if accountID.Valid && branchID.Valid {
			out[uuidBytesToString(accountID.Bytes)] = uuidBytesToString(branchID.Bytes)
		}
	}
	return out, rows.Err()
}

// Ledger — GET /api/accounts/{id}/ledger?branch_id=&from=&to=&limit=
//
// Every posting that ever touched one account, newest first, each tagged with the
// branch it is traceable to. This is the drill-down the chart of accounts never
// had: until now a balance could be checked against the trial balance but never
// explained, and "why is Kas Kecil - Cimanggu at this number" had no answer short
// of querying the database by hand.
//
// The running balance is computed oldest-first over the *unfiltered* account and
// then the rows are reversed for display, so it always reads as the account's
// real balance after each posting. Recomputing it over a filtered subset would
// produce a column of numbers that look like balances and are not — the branch
// filter narrows which rows you see, and cannot narrow what the account holds.
func (h *AccountsHandler) Ledger(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	q := r.URL.Query()
	ctx := r.Context()

	var account struct {
		Name          string
		AccountNumber pgtype.Int4
		AccountType   string
		Balance       int64
	}
	if err := h.pool.QueryRow(ctx,
		`SELECT name, account_number, account_type, balance FROM accounts WHERE id = $1`, pgUUID(id)).
		Scan(&account.Name, &account.AccountNumber, &account.AccountType, &account.Balance); err != nil {
		respondError(w, http.StatusNotFound, "akun tidak ditemukan")
		return
	}

	limit := 500
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}

	// The natural-sign delta per line, matching accounts.balance's convention so
	// the running total lands on the figure the CoA shows.
	credit := account.AccountType == "liability" || account.AccountType == "equity" || account.AccountType == "revenue"
	sign := "jl.amount"
	if credit {
		sign = "-jl.amount"
	}

	where := []string{}
	params := []any{pgUUID(id)}
	add := func(clause string, v any) {
		params = append(params, v)
		where = append(where, clause+"$"+strconv.Itoa(len(params)))
	}
	if v := q.Get("from"); v != "" {
		d, err := parseDayParam(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal awal tidak valid")
			return
		}
		add("al.entry_date >= ", pgtype.Date{Time: d, Valid: true})
	}
	if v := q.Get("to"); v != "" {
		d, err := parseDayParam(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "tanggal akhir tidak valid")
			return
		}
		add("al.entry_date <= ", pgtype.Date{Time: d, Valid: true})
	}
	branchFilter := q.Get("branch_id")
	if branchFilter != "" {
		if _, err := parseUUID(branchFilter); err != nil {
			respondError(w, http.StatusBadRequest, "branch_id tidak valid")
			return
		}
		// "unplaced" is a first-class choice, not the absence of one: an entry
		// nobody can attribute is exactly what someone auditing a shared account
		// wants to look at.
		add("eb.branch_id = ", branchFilter)
	} else if q.Get("unplaced") == "true" {
		where = append(where, "eb.branch_id IS NULL")
	}

	clause := ""
	for i, c := range where {
		if i == 0 {
			clause = " WHERE " + c
		} else {
			clause += " AND " + c
		}
	}

	// The running balance is computed inside `all_lines`, over every posting the
	// account ever received, and only then are the filters applied. A window
	// function runs after WHERE, so computing it alongside the filters would
	// produce a column that looks like the account's balance and is really the
	// balance of whatever subset happens to be on screen — off by the whole of
	// every excluded line. The filter narrows what you see; it cannot narrow what
	// the account holds.
	sql := `
		WITH entry_branch AS (` + entryBranchSQL + `),
		all_lines AS (
			SELECT jl.id, je.id AS entry_id, je.entry_date, je.created_at, je.created_by,
			       je.source_type, je.source_id, je.description, jl.memo, jl.amount,
			       (` + sign + `)::BIGINT AS delta,
			       SUM(` + sign + `) OVER (ORDER BY je.entry_date, je.created_at, jl.id)::BIGINT AS running_balance
			FROM journal_lines jl
			JOIN journal_entries je ON je.id = jl.entry_id
			WHERE jl.account_id = $1
		)
		SELECT al.entry_id::text AS entry_id, al.entry_date, al.source_type,
		       al.source_id::text AS source_id, al.description, al.memo, al.amount,
		       al.delta, al.running_balance,
		       eb.branch_id::text AS branch_id, b.name AS branch_name,
		       u.username AS created_by_name
		FROM all_lines al
		LEFT JOIN entry_branch eb ON eb.entry_id = al.entry_id
		LEFT JOIN branches b ON b.id = eb.branch_id
		LEFT JOIN users u ON u.id = al.created_by` + clause + `
		ORDER BY al.entry_date DESC, al.created_at DESC, al.id DESC
		LIMIT ` + strconv.Itoa(limit)

	rows, err := h.pool.Query(ctx, sql, params...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil buku besar akun")
		return
	}
	lines, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses buku besar akun")
		return
	}

	// Per-branch split of this one account, so the ledger page can say how the
	// balance divides without a second round trip.
	splitRows, err := h.pool.Query(ctx, `
		WITH entry_branch AS (`+entryBranchSQL+`)
		SELECT COALESCE(b.name, 'Tidak dapat ditelusuri') AS branch_name,
		       eb.branch_id::text AS branch_id,
		       (`+naturalAmountSQL+`) AS amount,
		       COUNT(*)::INT AS lines
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id
		JOIN accounts a ON a.id = jl.account_id
		LEFT JOIN entry_branch eb ON eb.entry_id = je.id
		LEFT JOIN branches b ON b.id = eb.branch_id
		WHERE jl.account_id = $1
		GROUP BY b.name, eb.branch_id
		ORDER BY amount DESC`, pgUUID(id))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung rincian cabang")
		return
	}
	split, err := pgx.CollectRows(splitRows, pgx.RowToMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses rincian cabang")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"account": map[string]any{
			"id":             uuidBytesToString(pgUUID(id).Bytes),
			"name":           account.Name,
			"account_number": account.AccountNumber,
			"account_type":   account.AccountType,
			"balance":        account.Balance,
		},
		"lines":        nilToEmpty(lines),
		"branch_split": nilToEmpty(split),
		"truncated":    len(lines) == limit,
	})
}
