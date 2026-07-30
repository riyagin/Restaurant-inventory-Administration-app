package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/testutil"
)

// The per-branch P&L is only as good as its account→branch mapping, and a
// misattribution there is silent: the org-wide total still reconciles while one
// branch quietly carries another's costs. These tests pin the four cases that
// matter — a branch's own account, a division's account, an account hung
// underneath a division (an expense category), and an account no branch owns.
//
// The fixtures post journal lines directly rather than going through service.Post
// so that accounts.balance is left alone: this report reads the journal, and not
// touching the cached balances keeps the run out of the trial-balance drift
// report.

// plBranchFixtures is one branch with one division, plus the two extra accounts
// the interesting cases need.
type plBranchFixtures struct {
	branchID   uuid.UUID
	branchName string

	branchRevID uuid.UUID // owned directly by the branch
	branchExpID uuid.UUID
	divRevID    uuid.UUID // owned by the division → same branch column
	divExpID    uuid.UUID
	catExpID    uuid.UUID // child of divExpID → inherits the branch
	orphanExpID uuid.UUID // owned by nobody → Umum

	entryDate string
}

// plFixtureDate is far enough outside any real data that the period filter can
// isolate the fixture's entries from the live books.
const plFixtureDate = "2001-03-04"

func setupPLBranchFixtures(t *testing.T, pool *pgxpool.Pool) plBranchFixtures {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.New().String()[:8]

	// Named "<Prefix> <8 hex>" so TestMain's sweep can find them if the explicit
	// cleanup below is ever prevented from running.
	newAccount := func(prefix, accType string, parent *uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		var parentArg any
		if parent != nil {
			parentArg = *parent
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO accounts (id, name, account_type, parent_id) VALUES ($1, $2, $3, $4)`,
			id, prefix+" "+suffix, accType, parentArg); err != nil {
			t.Fatalf("insert %s account: %v", prefix, err)
		}
		return id
	}

	f := plBranchFixtures{branchName: "PLBranch " + suffix, entryDate: plFixtureDate}
	f.branchRevID = newAccount("PLBranchRev", "revenue", nil)
	f.branchExpID = newAccount("PLBranchExp", "expense", nil)
	f.divRevID = newAccount("PLDivRev", "revenue", nil)
	f.divExpID = newAccount("PLDivExp", "expense", nil)
	f.catExpID = newAccount("PLCatExp", "expense", &f.divExpID)
	f.orphanExpID = newAccount("PLOrphanExp", "expense", nil)

	f.branchID = uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO branches (id, name, revenue_account_id, expense_account_id)
		 VALUES ($1, $2, $3, $4)`,
		f.branchID, f.branchName, f.branchRevID, f.branchExpID); err != nil {
		t.Fatalf("insert branch: %v", err)
	}

	divisionID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO divisions (id, branch_id, name, revenue_account_id, expense_account_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		divisionID, f.branchID, "PLDiv "+suffix, f.divRevID, f.divExpID); err != nil {
		t.Fatalf("insert division: %v", err)
	}

	// Two balanced entries. Debits are positive, so a revenue leg is negative:
	//   entry 1: Dr divExp 300 000 + Dr catExp 200 000, Cr branchRev 500 000
	//   entry 2: Dr orphanExp 70 000 + Dr branchExp 30 000, Cr divRev 100 000
	postEntry := func(legs map[uuid.UUID]int64) uuid.UUID {
		t.Helper()
		entryID := uuid.New()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin journal tx: %v", err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx,
			`INSERT INTO journal_entries (id, entry_date, source_type, description)
			 VALUES ($1, $2, 'test', 'per-branch P&L fixture')`,
			entryID, f.entryDate); err != nil {
			t.Fatalf("insert journal entry: %v", err)
		}
		for acct, amt := range legs {
			if _, err := tx.Exec(ctx,
				`INSERT INTO journal_lines (entry_id, account_id, amount) VALUES ($1, $2, $3)`,
				entryID, acct, amt); err != nil {
				t.Fatalf("insert journal line: %v", err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit journal entry: %v", err)
		}
		return entryID
	}

	entry1 := postEntry(map[uuid.UUID]int64{
		f.divExpID:    300_000,
		f.catExpID:    200_000,
		f.branchRevID: -500_000,
	})
	entry2 := postEntry(map[uuid.UUID]int64{
		f.orphanExpID: 70_000,
		f.branchExpID: 30_000,
		f.divRevID:    -100_000,
	})

	t.Cleanup(func() {
		cctx := context.Background()
		// Entries first: the lines cascade, freeing the accounts' FK references.
		pool.Exec(cctx, `DELETE FROM journal_entries WHERE id = ANY($1)`,
			[]uuid.UUID{entry1, entry2})
		pool.Exec(cctx, `DELETE FROM divisions WHERE id = $1`, divisionID)
		pool.Exec(cctx, `DELETE FROM branches WHERE id = $1`, f.branchID)
		// The category child references divExpID, so it goes before its parent.
		pool.Exec(cctx, `DELETE FROM accounts WHERE id = $1`, f.catExpID)
		pool.Exec(cctx, `DELETE FROM accounts WHERE id = ANY($1)`, []uuid.UUID{
			f.branchRevID, f.branchExpID, f.divRevID, f.divExpID, f.orphanExpID,
		})
	})

	return f
}

// plBranchResponse mirrors the handler's JSON shape.
type plBranchResponse struct {
	IsPeriod bool `json:"is_period"`
	Columns  []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"columns"`
	Accounts []struct {
		ID          string           `json:"id"`
		Name        string           `json:"name"`
		AccountType string           `json:"account_type"`
		ParentID    *string          `json:"parent_id"`
		Amounts     map[string]int64 `json:"amounts"`
		Total       int64            `json:"total"`
	} `json:"accounts"`
}

func fetchPLByBranch(t *testing.T, pool *pgxpool.Pool, start, end string) plBranchResponse {
	t.Helper()
	h := handler.NewReportsHandler(pool, db.New(pool))
	url := "/api/reports/profit-loss-by-branch"
	if start != "" {
		url += "?start_date=" + start + "&end_date=" + end
	}
	rec := httptest.NewRecorder()
	h.ProfitLossByBranch(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var out plBranchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

// TestProfitLossByBranchAttributesAccountsToOwningBranch is the core guarantee:
// each fixture account's activity shows up in exactly one column, and it is the
// right one — including the expense category, which is linked to no branch itself
// and can only be placed by walking up to its division's parent.
func TestProfitLossByBranchAttributesAccountsToOwningBranch(t *testing.T) {
	pool := testutil.OpenDB(t)
	f := setupPLBranchFixtures(t, pool)

	got := fetchPLByBranch(t, pool, plFixtureDate, plFixtureDate)

	if !got.IsPeriod {
		t.Error("is_period = false, want true when both dates are given")
	}

	branchKey := f.branchID.String()
	byID := map[string]struct {
		amounts map[string]int64
		total   int64
	}{}
	for _, a := range got.Accounts {
		byID[a.ID] = struct {
			amounts map[string]int64
			total   int64
		}{a.Amounts, a.Total}
	}

	cases := []struct {
		label  string
		id     uuid.UUID
		column string
		want   int64
	}{
		{"branch revenue account", f.branchRevID, branchKey, 500_000},
		{"branch expense account", f.branchExpID, branchKey, 30_000},
		{"division revenue account", f.divRevID, branchKey, 100_000},
		{"division expense account", f.divExpID, branchKey, 300_000},
		{"expense category under the division", f.catExpID, branchKey, 200_000},
		{"account owned by no branch", f.orphanExpID, "unallocated", 70_000},
	}

	for _, tc := range cases {
		row, ok := byID[tc.id.String()]
		if !ok {
			t.Errorf("%s: missing from response", tc.label)
			continue
		}
		if row.total != tc.want {
			t.Errorf("%s: total = %d, want %d", tc.label, row.total, tc.want)
		}
		if got := row.amounts[tc.column]; got != tc.want {
			t.Errorf("%s: amounts[%s] = %d, want %d", tc.label, tc.column, got, tc.want)
		}
		// One account belongs to one column; anything else would double-count it
		// once the frontend sums the columns into the Total.
		if len(row.amounts) != 1 {
			t.Errorf("%s: spread over %d columns, want 1 (%v)", tc.label, len(row.amounts), row.amounts)
		}
	}

	// Revenue is reported natural-sign (positive), not debit-sign, so it can be
	// compared against expense without the caller flipping anything.
	for _, tc := range cases {
		if row, ok := byID[tc.id.String()]; ok && row.total < 0 {
			t.Errorf("%s: total = %d, want a positive natural-sign figure", tc.label, row.total)
		}
	}

	var umum string
	for _, c := range got.Columns {
		if c.ID == "unallocated" {
			umum = c.Name
		}
	}
	if umum != "Umum" {
		t.Errorf("unallocated column name = %q, want %q", umum, "Umum")
	}
	if !hasColumn(got, branchKey, f.branchName) {
		t.Errorf("branch %q missing from columns", f.branchName)
	}
}

func hasColumn(r plBranchResponse, id, name string) bool {
	for _, c := range r.Columns {
		if c.ID == id && c.Name == name {
			return true
		}
	}
	return false
}

// TestProfitLossByBranchRespectsPeriod guards the date filter. Without it a
// "this month per branch" view would silently report all time.
func TestProfitLossByBranchRespectsPeriod(t *testing.T) {
	pool := testutil.OpenDB(t)
	f := setupPLBranchFixtures(t, pool)

	outside := fetchPLByBranch(t, pool, "2001-03-05", "2001-03-06")
	for _, a := range outside.Accounts {
		switch a.ID {
		case f.branchRevID.String(), f.branchExpID.String(), f.divRevID.String(),
			f.divExpID.String(), f.catExpID.String(), f.orphanExpID.String():
			if a.Total != 0 {
				t.Errorf("account %s: total = %d outside the period, want 0", a.Name, a.Total)
			}
		}
	}
}

// TestProfitLossByBranchColumnsReconcile checks the property the report is read
// for: the columns partition the P&L, so summing them reproduces the org-wide
// figure rather than dropping or duplicating anything.
func TestProfitLossByBranchColumnsReconcile(t *testing.T) {
	pool := testutil.OpenDB(t)
	setupPLBranchFixtures(t, pool)

	got := fetchPLByBranch(t, pool, plFixtureDate, plFixtureDate)

	perColumn := map[string]int64{}
	var net int64
	for _, a := range got.Accounts {
		sign := int64(1)
		if a.AccountType == "expense" {
			sign = -1
		}
		net += sign * a.Total
		for col, amt := range a.Amounts {
			perColumn[col] += sign * amt
		}
	}

	var summed int64
	for _, v := range perColumn {
		summed += v
	}
	if summed != net {
		t.Errorf("columns sum to %d but org-wide net is %d — the split is not a partition", summed, net)
	}
}
