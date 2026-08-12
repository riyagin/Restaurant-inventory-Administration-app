package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Profit & loss sliced into periods — one column per month or per year.
//
// This is the same money as ProfitLossByBranch and the org-wide financial
// report, bucketed. It reads the journal through the shared plAmountSQL /
// plActivityFromSQL fragments for the reason spelled out in reports_branch_pl.go:
// a second spelling of "what a P&L account did in a period" cannot be caught by
// either report's own totals, because each one still adds up internally while
// disagreeing with the other.
//
// Two things are genuinely new here.
//
// **Division ownership.** The branch report only needs account → branch. Here a
// division is a filter in its own right, so accountDivisionOwnerSQL resolves
// account → division by the same recursive descent: a division's revenue,
// expense and discount accounts, plus everything hung underneath them (expense
// categories). A branch's *own* accounts belong to no division and so drop out
// of a division-filtered view — correct, since "Dapur" means the division's
// accounts, not the branch overhead sitting beside it.
//
// **Divisions are matched by name across branches.** Each branch has its own
// division rows, so "Dapur" is several ids that happen to share a name. The
// filter takes the name, and a branch with no division of that name produces no
// group at all rather than an empty column of zeros — it is reported separately
// as excluded so the omission is visible instead of looking like a bad month.

const maxPeriodColumns = 12
const minCompareYears = 2
const maxCompareYears = 5

// accountDivisionOwnerSQL maps every revenue/expense account to the division
// that owns it. Same shape as accountBranchOwnerSQL — DISTINCT ON keeps the
// shallowest match so a re-parented account is attributed once, not twice.
const accountDivisionOwnerSQL = `
WITH RECURSIVE direct AS (
  SELECT d.id AS division_id, d.revenue_account_id  AS account_id FROM divisions d WHERE d.revenue_account_id  IS NOT NULL
  UNION SELECT d.id, d.expense_account_id  FROM divisions d WHERE d.expense_account_id  IS NOT NULL
  UNION SELECT d.id, d.discount_account_id FROM divisions d WHERE d.discount_account_id IS NOT NULL
),
owned AS (
  SELECT account_id, division_id, 0 AS depth FROM direct
  UNION ALL
  SELECT a.id, o.division_id, o.depth + 1
  FROM accounts a JOIN owned o ON a.parent_id = o.account_id
)
SELECT DISTINCT ON (account_id) account_id, division_id
FROM owned ORDER BY account_id, depth`

type periodColumn struct {
	Key   string `json:"key"`   // "2026-03" / "2026"
	Label string `json:"label"` // "Mar 2026" / "2026"
	Start string `json:"start"` // inclusive, ISO
	End   string `json:"end"`   // inclusive, ISO
}

var monthAbbrevID = [...]string{
	"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des",
}

// monthColumns returns the n calendar months ending with the month `now` falls
// in, oldest first.
func monthColumns(now time.Time, n int) []periodColumn {
	if n < 1 {
		n = 1
	}
	if n > maxPeriodColumns {
		n = maxPeriodColumns
	}
	out := make([]periodColumn, 0, n)
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -(n - 1), 0)
	for i := 0; i < n; i++ {
		start := first.AddDate(0, i, 0)
		end := start.AddDate(0, 1, -1)
		out = append(out, periodColumn{
			Key:   start.Format("2006-01"),
			Label: fmt.Sprintf("%s %d", monthAbbrevID[int(start.Month())-1], start.Year()),
			Start: start.Format("2006-01-02"),
			End:   end.Format("2006-01-02"),
		})
	}
	return out
}

// yearColumns returns the n calendar years ending with `now`'s year, oldest first.
func yearColumns(now time.Time, n int) []periodColumn {
	if n < minCompareYears {
		n = minCompareYears
	}
	if n > maxCompareYears {
		n = maxCompareYears
	}
	out := make([]periodColumn, 0, n)
	for i := n - 1; i >= 0; i-- {
		y := now.Year() - i
		out = append(out, periodColumn{
			Key:   strconv.Itoa(y),
			Label: strconv.Itoa(y),
			Start: fmt.Sprintf("%d-01-01", y),
			End:   fmt.Sprintf("%d-12-31", y),
		})
	}
	return out
}

// plActivityByAccountBucketed is plActivityByAccount with a GROUP BY on the
// period. Composed from the same SQL fragments, so the bucketed figures sum back
// to the unbucketed one over the same range by construction rather than by
// coincidence — TestPeriodicBucketsSumToPeriodTotal holds that.
//
// Returns account id → period key → amount.
func plActivityByAccountBucketed(
	ctx context.Context, pool *pgxpool.Pool, startDate, endDate, trunc, format string,
) (map[string]map[string]int64, error) {
	sql := `SELECT jl.account_id, to_char(date_trunc($3, je.entry_date), $4) AS bucket, ` +
		plAmountSQL + ` AS total` + plActivityFromSQL +
		` AND je.entry_date BETWEEN $1 AND $2 GROUP BY jl.account_id, bucket`

	rows, err := pool.Query(ctx, sql, startDate, endDate, trunc, format)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]map[string]int64{}
	for rows.Next() {
		var accountID pgtype.UUID
		var bucket string
		var total int64
		if err := rows.Scan(&accountID, &bucket, &total); err != nil {
			return nil, err
		}
		if !accountID.Valid {
			continue
		}
		id := uuidBytesToString(accountID.Bytes)
		if out[id] == nil {
			out[id] = map[string]int64{}
		}
		out[id][bucket] = total
	}
	return out, rows.Err()
}

type plGroupAccount struct {
	ID            string           `json:"id"`
	AccountNumber pgtype.Int4      `json:"account_number"`
	Name          string           `json:"name"`
	AccountType   string           `json:"account_type"`
	ParentID      pgtype.UUID      `json:"parent_id"`
	Amounts       map[string]int64 `json:"amounts"`
}

type plGroup struct {
	ID       string            `json:"id"`   // branch id, or "unallocated"
	Name     string            `json:"name"` // branch name, or "Umum"
	Accounts []*plGroupAccount `json:"accounts"`
}

type namedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProfitLossPeriodic — GET /api/reports/profit-loss-periodic
//
// Params:
//
//	granularity  month (default) | year
//	range        month mode: 6m (default) | ytd
//	years        year mode: 2..5 (default 3)
//	branch_id    optional — restrict to one branch
//	division     optional — a division *name*, matched across branches
func (h *ReportsHandler) ProfitLossPeriodic(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ctx := r.Context()
	now := time.Now()

	granularity := q.Get("granularity")
	if granularity != "year" {
		granularity = "month"
	}

	var columns []periodColumn
	rangeKey := q.Get("range")
	switch granularity {
	case "year":
		years := minCompareYears + 1
		if v, err := strconv.Atoi(q.Get("years")); err == nil {
			years = v
		}
		columns = yearColumns(now, years)
	default:
		// "ytd" is January of the current year to now; anything else is the last
		// six months. Both are bounded — a P&L table wider than a screen stops
		// being a comparison and starts being a spreadsheet.
		if rangeKey == "ytd" {
			columns = monthColumns(now, int(now.Month()))
		} else {
			rangeKey = "6m"
			columns = monthColumns(now, 6)
		}
	}
	if len(columns) == 0 {
		respondError(w, http.StatusBadRequest, "rentang periode tidak valid")
		return
	}
	trunc, format := "month", "YYYY-MM"
	if granularity == "year" {
		trunc, format = "year", "YYYY"
	}

	branchFilter := strings.TrimSpace(q.Get("branch_id"))
	divisionFilter := strings.TrimSpace(q.Get("division"))

	// ── Branches, divisions, and the account→owner maps ──
	branches, err := h.plBranches(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data cabang")
		return
	}
	divisionNames, divisionBranch, divisionName, err := h.plDivisions(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal mengambil data divisi")
		return
	}
	branchOf, err := h.plOwnerMap(ctx, accountBranchOwnerSQL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memetakan akun ke cabang")
		return
	}
	divisionOf, err := h.plOwnerMap(ctx, accountDivisionOwnerSQL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memetakan akun ke divisi")
		return
	}

	// A division name is a set of division ids, one per branch that has it. The
	// branches that don't are reported rather than silently absent.
	var included, excluded []namedRef
	wantedDivisions := map[string]bool{}
	if divisionFilter != "" {
		branchHas := map[string]bool{}
		for divID, name := range divisionName {
			if !strings.EqualFold(name, divisionFilter) {
				continue
			}
			wantedDivisions[divID] = true
			if b, ok := divisionBranch[divID]; ok {
				branchHas[b] = true
			}
		}
		for _, b := range branches {
			if branchHas[b.ID] {
				included = append(included, b)
			} else {
				excluded = append(excluded, b)
			}
		}
	}

	amountOf, err := plActivityByAccountBucketed(ctx, h.pool,
		columns[0].Start, columns[len(columns)-1].End, trunc, format)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "gagal menghitung mutasi laba rugi")
		return
	}

	// ── Assign each P&L account to a group ──
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

	groups := map[string]*plGroup{}
	order := []string{}
	addTo := func(groupID, groupName string, acc *plGroupAccount) {
		g, ok := groups[groupID]
		if !ok {
			g = &plGroup{ID: groupID, Name: groupName}
			groups[groupID] = g
			order = append(order, groupID)
		}
		g.Accounts = append(g.Accounts, acc)
	}

	for accRows.Next() {
		var id pgtype.UUID
		acc := &plGroupAccount{}
		if err := accRows.Scan(&id, &acc.AccountNumber, &acc.Name, &acc.AccountType, &acc.ParentID); err != nil {
			respondError(w, http.StatusInternalServerError, "gagal memproses data akun")
			return
		}
		acc.ID = uuidBytesToString(id.Bytes)

		owner, owned := branchOf[acc.ID]
		if divisionFilter != "" {
			// Division view: only accounts belonging to a division of that name.
			// A branch-level account has no division and is excluded, which is
			// the point of the filter.
			divID, inDiv := divisionOf[acc.ID]
			if !inDiv || !wantedDivisions[divID] {
				continue
			}
			owner = divisionBranch[divID]
			owned = owner != ""
		}
		if !owned {
			owner = unallocatedBranchKey
		}
		if branchFilter != "" && owner != branchFilter {
			continue
		}

		acc.Amounts = map[string]int64{}
		for _, col := range columns {
			if v, ok := amountOf[acc.ID][col.Key]; ok && v != 0 {
				acc.Amounts[col.Key] = v
			}
		}
		addTo(owner, plBranchName(branches, owner), acc)
	}
	if err := accRows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "gagal memproses data akun")
		return
	}

	// Branch order for the response, with Umum last — a group that belongs to no
	// branch is context, not a peer.
	outGroups := make([]*plGroup, 0, len(order))
	for _, b := range branches {
		if g, ok := groups[b.ID]; ok {
			outGroups = append(outGroups, g)
		}
	}
	if g, ok := groups[unallocatedBranchKey]; ok {
		outGroups = append(outGroups, g)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"granularity":       granularity,
		"range":             rangeKey,
		"columns":           columns,
		"groups":            outGroups,
		"branches":          branches,
		"division_names":    divisionNames,
		"division":          divisionFilter,
		"branch_id":         branchFilter,
		"included_branches": included,
		"excluded_branches": excluded,
	})
}

func plBranchName(branches []namedRef, id string) string {
	if id == unallocatedBranchKey {
		return "Umum"
	}
	for _, b := range branches {
		if b.ID == id {
			return b.Name
		}
	}
	return "—"
}

func (h *ReportsHandler) plBranches(ctx context.Context) ([]namedRef, error) {
	rows, err := h.pool.Query(ctx, `SELECT id, name FROM branches ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []namedRef{}
	for rows.Next() {
		var id pgtype.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out = append(out, namedRef{ID: uuidBytesToString(id.Bytes), Name: name})
	}
	return out, rows.Err()
}

// plDivisions returns the distinct division names (for the picker), plus
// division id → branch id and division id → name.
func (h *ReportsHandler) plDivisions(ctx context.Context) ([]string, map[string]string, map[string]string, error) {
	rows, err := h.pool.Query(ctx, `SELECT id, name, branch_id FROM divisions ORDER BY name`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	names := []string{}
	seen := map[string]bool{}
	branchOf := map[string]string{}
	nameOf := map[string]string{}
	for rows.Next() {
		var id, branchID pgtype.UUID
		var name string
		if err := rows.Scan(&id, &name, &branchID); err != nil {
			return nil, nil, nil, err
		}
		divID := uuidBytesToString(id.Bytes)
		nameOf[divID] = name
		if branchID.Valid {
			branchOf[divID] = uuidBytesToString(branchID.Bytes)
		}
		if key := strings.ToLower(name); !seen[key] {
			seen[key] = true
			names = append(names, name)
		}
	}
	return names, branchOf, nameOf, rows.Err()
}

// plOwnerMap runs one of the recursive ownership queries into account id → owner id.
func (h *ReportsHandler) plOwnerMap(ctx context.Context, query string) (map[string]string, error) {
	rows, err := h.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var accountID, ownerID pgtype.UUID
		if err := rows.Scan(&accountID, &ownerID); err != nil {
			return nil, err
		}
		if accountID.Valid && ownerID.Valid {
			out[uuidBytesToString(accountID.Bytes)] = uuidBytesToString(ownerID.Bytes)
		}
	}
	return out, rows.Err()
}
