package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/testutil"
)

// The column builders decide what "6 bulan terakhir" and "tahun berjalan"
// actually mean, and both are easy to get wrong by one at a year boundary — the
// six months before January 2026 run back into 2025, and a year-count picker
// must include the current year, not stop before it.

func TestMonthColumnsSpansYearBoundary(t *testing.T) {
	now := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	cols := monthColumns(now, 6)

	if len(cols) != 6 {
		t.Fatalf("len = %d, want 6", len(cols))
	}
	if cols[0].Key != "2025-08" {
		t.Errorf("first column = %s, want 2025-08", cols[0].Key)
	}
	if cols[5].Key != "2026-01" {
		t.Errorf("last column = %s, want 2026-01 (the current month is included)", cols[5].Key)
	}
	// Each column is a whole calendar month, inclusive at both ends — the query
	// filters BETWEEN start AND end, so an end of the 30th would silently drop
	// the 31st's postings.
	if cols[0].Start != "2025-08-01" || cols[0].End != "2025-08-31" {
		t.Errorf("Aug 2025 = %s..%s, want 2025-08-01..2025-08-31", cols[0].Start, cols[0].End)
	}
	if cols[5].Start != "2026-01-01" || cols[5].End != "2026-01-31" {
		t.Errorf("Jan 2026 = %s..%s, want 2026-01-01..2026-01-31", cols[5].Start, cols[5].End)
	}
}

func TestMonthColumnsFebruaryEndsOnTheLastDay(t *testing.T) {
	// 2024 is a leap year: the 29th exists and must be inside the column.
	cols := monthColumns(time.Date(2024, 2, 10, 0, 0, 0, 0, time.UTC), 1)
	if cols[0].End != "2024-02-29" {
		t.Errorf("Feb 2024 ends %s, want 2024-02-29", cols[0].End)
	}
	cols = monthColumns(time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), 1)
	if cols[0].End != "2025-02-28" {
		t.Errorf("Feb 2025 ends %s, want 2025-02-28", cols[0].End)
	}
}

func TestMonthColumnsCappedAtTwelve(t *testing.T) {
	if got := len(monthColumns(time.Now(), 48)); got != maxPeriodColumns {
		t.Errorf("len = %d, want the cap of %d", got, maxPeriodColumns)
	}
}

func TestYearColumnsIncludeCurrentYearAndClamp(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	cols := yearColumns(now, 3)
	if len(cols) != 3 {
		t.Fatalf("len = %d, want 3", len(cols))
	}
	if cols[0].Key != "2024" || cols[2].Key != "2026" {
		t.Errorf("years = %s..%s, want 2024..2026", cols[0].Key, cols[2].Key)
	}
	if cols[2].Start != "2026-01-01" || cols[2].End != "2026-12-31" {
		t.Errorf("2026 = %s..%s, want the whole calendar year", cols[2].Start, cols[2].End)
	}

	// The picker offers 2–5; anything outside is clamped rather than rejected,
	// so a stale bookmark still renders a table.
	if got := len(yearColumns(now, 1)); got != minCompareYears {
		t.Errorf("years=1 gave %d columns, want the floor of %d", got, minCompareYears)
	}
	if got := len(yearColumns(now, 99)); got != maxCompareYears {
		t.Errorf("years=99 gave %d columns, want the cap of %d", got, maxCompareYears)
	}
}

// TestPeriodicBucketsSumToPeriodTotal is the periodic report's half of the rule
// the branch split already carries: there is one definition of what a P&L
// account did in a period, and slicing it by month or year must re-cut the same
// money rather than compute a second version of it. Both queries here are built
// from plAmountSQL / plActivityFromSQL, so this fails the moment either grows
// its own arithmetic or filter.
//
// It runs against the live books over a window wide enough to contain them, so
// it is checking real data rather than a fixture's two rows.
func TestPeriodicBucketsSumToPeriodTotal(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx := context.Background()

	const start, end = "1990-01-01", "2999-12-31"

	flat, err := plActivityByAccount(ctx, pool, start, end)
	if err != nil {
		t.Fatalf("plActivityByAccount: %v", err)
	}
	bucketed, err := plActivityByAccountBucketed(ctx, pool, start, end, "year", "YYYY")
	if err != nil {
		t.Fatalf("plActivityByAccountBucketed: %v", err)
	}

	// Per account, not merely in total: an error that happens to cancel out
	// between revenue and expense would pass a net check.
	for id, want := range flat {
		var got int64
		for _, v := range bucketed[id] {
			got += v
		}
		if got != want {
			t.Errorf("account %s: buckets sum to %d, period total is %d", id, got, want)
		}
	}
	for id, buckets := range bucketed {
		if _, ok := flat[id]; ok {
			continue
		}
		var got int64
		for _, v := range buckets {
			got += v
		}
		if got != 0 {
			t.Errorf("account %s: bucketed %d but absent from the period total", id, got)
		}
	}
}

// The division filter's contract, checked against the live chart of accounts:
// a branch with no division of the chosen name produces no group at all, rather
// than a column of zeros that reads like a bad month.
func TestPeriodicDivisionFilterExcludesBranchesWithoutThatDivision(t *testing.T) {
	pool := testutil.OpenDB(t)
	h := NewReportsHandler(pool, db.New(pool))

	fetch := func(query string) periodicResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ProfitLossPeriodic(rec, httptest.NewRequest(http.MethodGet, "/api/reports/profit-loss-periodic"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
		}
		var out periodicResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	base := fetch("")
	if len(base.Columns) != 6 {
		t.Errorf("default gave %d columns, want 6 (6 bulan terakhir)", len(base.Columns))
	}
	if len(base.DivisionNames) == 0 {
		t.Skip("no divisions in this database — nothing to filter by")
	}

	name := base.DivisionNames[0]
	got := fetch("?division=" + url.QueryEscape(name))

	if len(got.IncludedBranches)+len(got.ExcludedBranches) != len(base.Branches) {
		t.Errorf("included(%d) + excluded(%d) != branches(%d): every branch must land on one side",
			len(got.IncludedBranches), len(got.ExcludedBranches), len(base.Branches))
	}
	included := map[string]bool{}
	for _, b := range got.IncludedBranches {
		included[b.ID] = true
	}
	for _, g := range got.Groups {
		if g.ID == "unallocated" {
			t.Errorf("division view produced an Umum group: %q owns no division", g.Name)
			continue
		}
		if !included[g.ID] {
			t.Errorf("group %q is not among the branches that have division %q", g.Name, name)
		}
	}
}

type periodicResponse struct {
	Columns          []periodColumn `json:"columns"`
	Branches         []namedRef     `json:"branches"`
	DivisionNames    []string       `json:"division_names"`
	IncludedBranches []namedRef     `json:"included_branches"`
	ExcludedBranches []namedRef     `json:"excluded_branches"`
	Groups           []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Accounts []struct {
			ID      string           `json:"id"`
			Name    string           `json:"name"`
			Amounts map[string]int64 `json:"amounts"`
		} `json:"accounts"`
	} `json:"groups"`
}
