package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
	"inventory-app/server-go/internal/testutil"
)

type annualRentFixture struct {
	branchID, sewaCategoryID, listrikCategoryID uuid.UUID
	sewaAccountID, cashAccountID                uuid.UUID
}

// setupAnnualRentFixture builds a branch with an Operasional division, its Sewa
// and Listrik categories, and a cash account to pay from.
func setupAnnualRentFixture(t *testing.T, pool *pgxpool.Pool) annualRentFixture {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	suffix := uuid.New().String()[:8]

	mk := func(name, typ string) uuid.UUID {
		a, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: name + " " + suffix, AccountType: typ})
		if err != nil {
			t.Fatalf("create account %s: %v", name, err)
		}
		return a.ID.Bytes
	}
	opsExp := mk("SewaOpsExp", "expense")
	sewaAcct := mk("SewaBeban", "expense")
	listrikAcct := mk("SewaListrik", "expense")
	cash := mk("SewaKas", "asset")

	f := annualRentFixture{
		branchID: uuid.New(), sewaCategoryID: uuid.New(), listrikCategoryID: uuid.New(),
		sewaAccountID: sewaAcct, cashAccountID: cash,
	}
	divisionID := uuid.New()
	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v\n%s", err, sql)
		}
	}
	exec(`INSERT INTO branches (id, name) VALUES ($1, 'BrSewa '||$2)`, f.branchID, suffix)
	exec(`INSERT INTO divisions (id, branch_id, name, expense_account_id, is_system) VALUES ($1, $2, 'Operasional', $3, true)`,
		divisionID, f.branchID, opsExp)
	exec(`UPDATE accounts SET parent_id = $1 WHERE id IN ($2, $3)`, opsExp, sewaAcct, listrikAcct)
	exec(`INSERT INTO operational_expense_categories (id, division_id, name, account_id) VALUES ($1, $2, 'Sewa', $3)`,
		f.sewaCategoryID, divisionID, sewaAcct)
	exec(`INSERT INTO operational_expense_categories (id, division_id, name, account_id) VALUES ($1, $2, 'Listrik', $3)`,
		f.listrikCategoryID, divisionID, listrikAcct)

	t.Cleanup(func() {
		c := context.Background()
		var prepaid pgtype.UUID
		pool.QueryRow(c, `SELECT prepaid_rent_account_id FROM branches WHERE id = $1`, f.branchID).Scan(&prepaid)
		pool.Exec(c, `DELETE FROM journal_entries WHERE source_id IN (SELECT id FROM operational_expenses WHERE branch_id = $1)`, f.branchID)
		pool.Exec(c, `DELETE FROM operational_expenses WHERE branch_id = $1`, f.branchID)
		pool.Exec(c, `DELETE FROM operational_expense_categories WHERE division_id = $1`, divisionID)
		pool.Exec(c, `DELETE FROM divisions WHERE id = $1`, divisionID)
		pool.Exec(c, `DELETE FROM branches WHERE id = $1`, f.branchID)
		for _, id := range []any{prepaid, sewaAcct, listrikAcct, opsExp, cash} {
			pool.Exec(c, `DELETE FROM accounts WHERE id = $1`, id)
		}
	})
	return f
}

func TestAmortizationSchedule_Remainder(t *testing.T) {
	s := service.AmortizationSchedule(12_000_005, time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), 12)
	if len(s) != 12 {
		t.Fatalf("len = %d, want 12", len(s))
	}
	var sum int64
	for _, x := range s {
		sum += x.Amount
	}
	if sum != 12_000_005 || s[0].Amount != 1_000_000 || s[11].Amount != 1_000_005 {
		t.Errorf("split = first %d last %d sum %d", s[0].Amount, s[11].Amount, sum)
	}
	if got := s[0].PeriodMonth.Format("2006-01-02"); got != "2026-11-01" {
		t.Errorf("first month = %s, want 2026-11-01", got)
	}
	if got := s[11].PeriodMonth.Format("2006-01"); got != "2027-10" {
		t.Errorf("last month = %s, want 2027-10 (crosses the year)", got)
	}
}

// TestAnnualRent_Lifecycle: pay a year of rent whose term began two months
// ago, check the elapsed months are expensed at once and the rest sits in
// prepaid rent, let the poster catch up, read the lease back, check the monthly
// report spreads it, then cancel and check every account returns to zero.
func TestAnnualRent_Lifecycle(t *testing.T) {
	pool := testutil.OpenDB(t)
	f := setupAnnualRentFixture(t, pool)
	userID := createTestUser(t, pool)
	ctx := context.Background()
	q := db.New(pool)
	h := handler.NewOperationalExpensesHandler(pool, q)
	authCtx := middleware.ContextWithClaims(ctx, testClaims(userID))

	now := time.Now()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	due := thisMonth.AddDate(0, -2, 9) // the 10th, two months ago
	const amount = int64(12_000_005)

	balance := func(id uuid.UUID) int64 { return getBalance(t, pool, id) }

	// ── validation ──
	for name, body := range map[string]map[string]any{
		"annual without due date": {"billing": "annual", "category_id": f.sewaCategoryID.String()},
		"annual on listrik":       {"billing": "annual", "category_id": f.listrikCategoryID.String(), "due_date": due.Format("2006-01-02")},
	} {
		body["date"] = now.Format("2006-01-02")
		body["amount"] = amount
		body["credit_account_id"] = f.cashAccountID.String()
		if rr := postJSON(t, h.Create, authCtx, body); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", name, rr.Code, rr.Body.String())
		}
	}

	// ── create ──
	rr := postJSON(t, h.Create, authCtx, map[string]any{
		"billing":           "annual",
		"due_date":          due.Format("2006-01-02"),
		"date":              now.Format("2006-01-02"),
		"category_id":       f.sewaCategoryID.String(),
		"credit_account_id": f.cashAccountID.String(),
		"amount":            amount,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID              string `json:"id"`
		PeriodMonth     string `json:"period_month"`
		MonthsAmortized int    `json:"months_amortized"`
	}
	decodeJSON(t, rr, &created)
	if created.MonthsAmortized != 3 {
		t.Errorf("months amortized on create = %d, want 3 (two past months + this one)", created.MonthsAmortized)
	}
	if created.PeriodMonth != due.Format("2006-01")+"-01" {
		t.Errorf("period_month = %s, want the due date's month", created.PeriodMonth)
	}

	var prepaid uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT prepaid_rent_account_id FROM branches WHERE id = $1`, f.branchID).Scan(&prepaid); err != nil {
		t.Fatalf("prepaid account not linked to branch: %v", err)
	}
	var prepaidNumber int
	pool.QueryRow(ctx, `SELECT account_number FROM accounts WHERE id = $1`, prepaid).Scan(&prepaidNumber)
	if prepaidNumber < 10501 || prepaidNumber > 10599 {
		t.Errorf("prepaid account number = %d, want 10501-10599", prepaidNumber)
	}

	if got := balance(f.cashAccountID); got != -amount {
		t.Errorf("cash = %d, want %d", got, -amount)
	}
	if got := balance(f.sewaAccountID); got != 3_000_000 {
		t.Errorf("sewa expense after create = %d, want 3 000 000", got)
	}
	if got := balance(prepaid); got != amount-3_000_000 {
		t.Errorf("prepaid after create = %d, want %d", got, amount-3_000_000)
	}

	// ── background poster: two more months, and a second sweep is a no-op ──
	poster := service.NewAmortizationPoster(pool, q)
	// Other annual leases in the database may be due too, so only this
	// lease's balances are asserted, not the sweep's count.
	poster.Sweep(ctx, thisMonth.AddDate(0, 2, 0))
	poster.Sweep(ctx, thisMonth.AddDate(0, 2, 0))
	if got := balance(f.sewaAccountID); got != 5_000_000 {
		t.Errorf("sewa expense after sweep = %d, want 5 000 000", got)
	}
	var entries int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM journal_entries WHERE source_id = $1`, created.ID).Scan(&entries)
	if entries != 6 { // payment + five months
		t.Errorf("journal entries = %d, want 6 (repeated sweep must not re-post)", entries)
	}

	// ── the Tahunan tab's data ──
	req := httptest.NewRequest(http.MethodGet, "/api/operational-expenses/annual?branch_id="+f.branchID.String(), nil)
	lrr := httptest.NewRecorder()
	h.ListAnnual(lrr, req)
	if lrr.Code != http.StatusOK {
		t.Fatalf("list annual: %d %s", lrr.Code, lrr.Body.String())
	}
	var leases []struct {
		Amortized    int64  `json:"amortized"`
		Remaining    int64  `json:"remaining"`
		MonthsPosted int    `json:"months_posted"`
		NextDueDate  string `json:"next_due_date"`
		CoverageEnd  string `json:"coverage_end"`
		Renewed      bool   `json:"renewed"`
		Schedule     []struct {
			Amount int64  `json:"amount"`
			Status string `json:"status"`
		} `json:"schedule"`
	}
	decodeJSON(t, lrr, &leases)
	if len(leases) != 1 {
		t.Fatalf("leases = %d, want 1", len(leases))
	}
	l := leases[0]
	if l.Amortized != 5_000_000 || l.Remaining != amount-5_000_000 || l.MonthsPosted != 5 || len(l.Schedule) != 12 {
		t.Errorf("lease = amortized %d remaining %d posted %d schedule %d", l.Amortized, l.Remaining, l.MonthsPosted, len(l.Schedule))
	}
	if want := due.AddDate(1, 0, 0).Format("2006-01-02"); l.NextDueDate != want {
		t.Errorf("next due = %s, want %s", l.NextDueDate, want)
	}
	if want := due.AddDate(1, 0, -1).Format("2006-01-02"); l.CoverageEnd != want {
		t.Errorf("coverage end = %s, want %s", l.CoverageEnd, want)
	}
	if l.Renewed {
		t.Errorf("a lease with no successor reported as renewed")
	}

	// ── the Statistik report sees one twelfth per month, not one spike ──
	rh := handler.NewReportsHandler(pool, q)
	rrr := httptest.NewRecorder()
	rh.OperationalExpenses(rrr, httptest.NewRequest(http.MethodGet,
		"/api/reports/operational-expenses?months=12&end="+due.AddDate(0, 11, 0).Format("2006-01")+"&branch_id="+f.branchID.String(), nil))
	if rrr.Code != http.StatusOK {
		t.Fatalf("report: %d %s", rrr.Code, rrr.Body.String())
	}
	var report struct {
		Branches []struct {
			ID         string `json:"id"`
			Categories []struct {
				Category string           `json:"category"`
				Amounts  map[string]int64 `json:"amounts"`
				Total    int64            `json:"total"`
			} `json:"categories"`
		} `json:"branches"`
	}
	decodeJSON(t, rrr, &report)
	found := false
	for _, b := range report.Branches {
		if b.ID != f.branchID.String() {
			continue
		}
		for _, c := range b.Categories {
			if c.Category != "Sewa" {
				continue
			}
			found = true
			if c.Total != amount || len(c.Amounts) != 12 || c.Amounts[due.Format("2006-01")] != 1_000_000 {
				t.Errorf("report Sewa = total %d over %d months, first %d", c.Total, len(c.Amounts), c.Amounts[due.Format("2006-01")])
			}
		}
	}
	if !found {
		t.Errorf("report has no Sewa row for the branch")
	}

	// ── cancel: everything back to zero, remaining months never post ──
	crr := callWithID(t, h.Cancel, authCtx, http.MethodPost, created.ID, map[string]any{"reason": "salah input"})
	if crr.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", crr.Code, crr.Body.String())
	}
	for name, id := range map[string]uuid.UUID{"cash": f.cashAccountID, "sewa": f.sewaAccountID, "prepaid": prepaid} {
		if got := balance(id); got != 0 {
			t.Errorf("%s after cancel = %d, want 0", name, got)
		}
	}
	poster.Sweep(ctx, thisMonth.AddDate(2, 0, 0))
	if got := balance(f.sewaAccountID); got != 0 {
		t.Errorf("sweep after cancel posted a month: sewa = %d", got)
	}
	var scheduled int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM operational_expense_amortizations WHERE expense_id = $1 AND status = 'scheduled'`,
		created.ID).Scan(&scheduled)
	if scheduled != 0 {
		t.Errorf("%d months still scheduled after cancel", scheduled)
	}

	// Cached balances must still agree with the journal.
	var drift int
	pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM accounts a
		WHERE a.id = ANY($1::uuid[])
		  AND a.balance <> COALESCE((SELECT SUM(amount) FROM journal_lines WHERE account_id = a.id), 0)`,
		[]uuid.UUID{f.cashAccountID, f.sewaAccountID, prepaid}).Scan(&drift)
	if drift != 0 {
		t.Errorf("%d accounts drifted from the journal", drift)
	}
}
