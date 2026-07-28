package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/testutil"
)

// posAccounts holds account IDs for POS import tests.
type posAccounts struct {
	revenueID  uuid.UUID
	cashID     uuid.UUID
	expenseID  uuid.UUID
	discountID uuid.UUID
}

func setupPOSAccounts(t *testing.T, pool *pgxpool.Pool) posAccounts {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	suffix := uuid.New().String()[:8]

	mk := func(name, typ string) uuid.UUID {
		a, err := q.CreateAccount(ctx, &db.CreateAccountParams{
			Name: name + "-" + suffix, AccountType: typ,
		})
		if err != nil {
			t.Fatalf("create account %s: %v", name, err)
		}
		return a.ID.Bytes
	}

	accts := posAccounts{
		revenueID: mk("POSRevenue", "revenue"),
		cashID:    mk("POSCash", "asset"),
		// The "expense_mappings" field carries the POS "Biaya Tambahan" column,
		// which is the delivery fee — revenue, not a cost. The field name is kept
		// for the client's payload shape; the account behind it is revenue.
		expenseID:  mk("POSDeliveryFee", "revenue"),
		discountID: mk("POSDiscount", "expense"),
	}

	t.Cleanup(func() {
		bCtx := context.Background()
		for _, id := range []uuid.UUID{accts.revenueID, accts.cashID, accts.expenseID, accts.discountID} {
			pool.Exec(bCtx, `DELETE FROM accounts WHERE id = $1`, id)
		}
	})
	return accts
}

func cleanupPOSImport(t *testing.T, pool *pgxpool.Pool, filename string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx,
			`DELETE FROM pos_import_lines WHERE import_id IN (SELECT id FROM pos_imports WHERE source_file = $1)`,
			filename)
		pool.Exec(ctx, `DELETE FROM pos_imports WHERE source_file = $1`, filename)
	})
}

// TestPOSImportConfirm_RevenueAndCashUpdated is the primary account-binding
// test. After a confirm, revenue and cash accounts increase and discount stays 0.
func TestPOSImportConfirm_RevenueAndCashUpdated(t *testing.T) {
	pool := testutil.OpenDB(t)
	accts := setupPOSAccounts(t, pool)
	userID := createTestUser(t, pool)
	filename := "test-rev-" + uuid.New().String()[:8] + ".xlsx"
	cleanupPOSImport(t, pool, filename)

	h := handler.NewPOSImportHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Confirm, authCtx, map[string]any{
		"filename": filename,
		"imports": []map[string]any{{
			"date": "2026-06-01", "description": "Test",
			"revenue_mappings":  []map[string]any{{"label": "F&B", "account_id": accts.revenueID.String(), "amount": 1000000}},
			"cash_mappings":     []map[string]any{{"label": "Cash", "account_id": accts.cashID.String(), "amount": 950000}},
			"discount_mappings": []map[string]any{{"label": "Disc", "account_id": accts.discountID.String(), "amount": 50000}},
			"expense_mappings":  []map[string]any{},
		}},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	if bal := getBalance(t, pool, accts.revenueID); bal != 1000000 {
		t.Errorf("revenue balance = %d, want 1000000", bal)
	}
	if bal := getBalance(t, pool, accts.cashID); bal != 950000 {
		t.Errorf("cash balance = %d, want 950000", bal)
	}
	if bal := getBalance(t, pool, accts.discountID); bal != 0 {
		t.Errorf("discount balance = %d, want 0 (discount must NOT touch balance)", bal)
	}

	var importCount int
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pos_imports WHERE source_file = $1`, filename).Scan(&importCount)
	if importCount != 1 {
		t.Errorf("expected 1 pos_import row, got %d", importCount)
	}
}

// TestPOSImportConfirm_DeliveryFeeIsRevenue covers the POS "Biaya Tambahan"
// column (carried in expense_mappings): it is the delivery fee, which is revenue
// earned at the sale but excluded from the POS payment breakdown. So it credits
// the mapped revenue account and debits the delivery-fee receivable, rather than
// being debited as a cost with no counter-leg — which is how it used to leave
// the books short by its own value on every import.
func TestPOSImportConfirm_DeliveryFeeIsRevenue(t *testing.T) {
	pool := testutil.OpenDB(t)
	accts := setupPOSAccounts(t, pool)
	userID := createTestUser(t, pool)
	filename := "test-exp-" + uuid.New().String()[:8] + ".xlsx"
	cleanupPOSImport(t, pool, filename)

	h := handler.NewPOSImportHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Confirm, authCtx, map[string]any{
		"filename": filename,
		"imports": []map[string]any{{
			"date": "2026-06-02", "description": "With expense",
			"revenue_mappings":  []map[string]any{{"label": "Rev", "account_id": accts.revenueID.String(), "amount": 500000}},
			"cash_mappings":     []map[string]any{{"label": "Cash", "account_id": accts.cashID.String(), "amount": 480000}},
			"discount_mappings": []map[string]any{},
			"expense_mappings":  []map[string]any{{"label": "Biaya Tambahan", "account_id": accts.expenseID.String(), "amount": 200000}},
		}},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	if bal := getBalance(t, pool, accts.revenueID); bal != 500000 {
		t.Errorf("revenue balance = %d, want 500000", bal)
	}
	if bal := getBalance(t, pool, accts.cashID); bal != 480000 {
		t.Errorf("cash balance = %d, want 480000", bal)
	}
	// Credited as revenue, not debited as a cost.
	if bal := getBalance(t, pool, accts.expenseID); bal != 200000 {
		t.Errorf("delivery fee revenue balance = %d, want 200000", bal)
	}

	// And the matching debit lands in the receivable, so the entry balances on
	// its own without falling through to the suspense account.
	// Scoped through pos_imports.source_file, which is unique per run — this
	// database keeps what the handler commits, so a description filter would
	// also pick up earlier runs of this same test.
	var receivable int64
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(jl.amount), 0) FROM journal_lines jl
		 JOIN journal_entries je ON je.id = jl.entry_id
		 JOIN accounts a ON a.id = jl.account_id
		 JOIN pos_imports p ON p.id = je.source_id
		 WHERE a.account_number = 10400 AND p.source_file = $1`, filename).Scan(&receivable); err != nil {
		t.Fatalf("read receivable lines: %v", err)
	}
	if receivable != 200000 {
		t.Errorf("delivery fee receivable = %d, want 200000", receivable)
	}
}

// TestPOSImportConfirm_MultiDateIndependent verifies two date entries in one
// confirm request each update balances independently (totals are additive).
func TestPOSImportConfirm_MultiDateIndependent(t *testing.T) {
	pool := testutil.OpenDB(t)
	accts := setupPOSAccounts(t, pool)
	userID := createTestUser(t, pool)
	filename := "test-multi-" + uuid.New().String()[:8] + ".xlsx"
	cleanupPOSImport(t, pool, filename)

	h := handler.NewPOSImportHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Confirm, authCtx, map[string]any{
		"filename": filename,
		"imports": []map[string]any{
			{
				"date": "2026-06-01", "description": "Day 1",
				"revenue_mappings":  []map[string]any{{"label": "Rev", "account_id": accts.revenueID.String(), "amount": 300000}},
				"cash_mappings":     []map[string]any{{"label": "Cash", "account_id": accts.cashID.String(), "amount": 300000}},
				"discount_mappings": []map[string]any{},
				"expense_mappings":  []map[string]any{},
			},
			{
				"date": "2026-06-02", "description": "Day 2",
				"revenue_mappings":  []map[string]any{{"label": "Rev", "account_id": accts.revenueID.String(), "amount": 700000}},
				"cash_mappings":     []map[string]any{{"label": "Cash", "account_id": accts.cashID.String(), "amount": 700000}},
				"discount_mappings": []map[string]any{},
				"expense_mappings":  []map[string]any{},
			},
		},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// 300 000 + 700 000 = 1 000 000 for both revenue and cash.
	if bal := getBalance(t, pool, accts.revenueID); bal != 1000000 {
		t.Errorf("revenue balance = %d, want 1000000", bal)
	}
	if bal := getBalance(t, pool, accts.cashID); bal != 1000000 {
		t.Errorf("cash balance = %d, want 1000000", bal)
	}

	// Two pos_import rows — one per date.
	var importCount int
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pos_imports WHERE source_file = $1`, filename).Scan(&importCount)
	if importCount != 2 {
		t.Errorf("expected 2 pos_import rows, got %d", importCount)
	}
}

// TestPOSImportConfirm_MissingAccountIDRejected confirms 400 when a revenue
// or cash mapping has an empty account_id.
func TestPOSImportConfirm_MissingAccountIDRejected(t *testing.T) {
	pool := testutil.OpenDB(t)
	userID := createTestUser(t, pool)

	h := handler.NewPOSImportHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Confirm, authCtx, map[string]any{
		"filename": "bad.xlsx",
		"imports": []map[string]any{{
			"date": "2026-06-01", "description": "Bad",
			"revenue_mappings":  []map[string]any{{"label": "Rev", "account_id": "", "amount": 100000}},
			"cash_mappings":     []map[string]any{},
			"discount_mappings": []map[string]any{},
			"expense_mappings":  []map[string]any{},
		}},
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing account_id, got %d", rr.Code)
	}
}

// TestPOSImportConfirm_LinesCreated verifies that pos_import_lines rows are
// written for all four line types and that the discount line does NOT alter
// any account balance.
func TestPOSImportConfirm_LinesCreated(t *testing.T) {
	pool := testutil.OpenDB(t)
	accts := setupPOSAccounts(t, pool)
	userID := createTestUser(t, pool)
	filename := "test-lines-" + uuid.New().String()[:8] + ".xlsx"
	cleanupPOSImport(t, pool, filename)

	h := handler.NewPOSImportHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Confirm, authCtx, map[string]any{
		"filename": filename,
		"imports": []map[string]any{{
			"date": "2026-06-10", "description": "Lines test",
			"revenue_mappings":  []map[string]any{{"label": "F&B", "account_id": accts.revenueID.String(), "amount": 800000}},
			"cash_mappings":     []map[string]any{{"label": "Cash", "account_id": accts.cashID.String(), "amount": 780000}},
			"discount_mappings": []map[string]any{{"label": "Disc", "account_id": accts.discountID.String(), "amount": 20000}},
			"expense_mappings":  []map[string]any{{"label": "COGS", "account_id": accts.expenseID.String(), "amount": 100000}},
		}},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Four lines (revenue, cash, discount, expense).
	var lineCount int
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pos_import_lines
		 WHERE import_id IN (SELECT id FROM pos_imports WHERE source_file = $1)`,
		filename).Scan(&lineCount)
	if lineCount != 4 {
		t.Errorf("expected 4 pos_import_lines, got %d", lineCount)
	}

	// Discount line should exist but leave the account balance at 0.
	var discLines int
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pos_import_lines pil
		 JOIN pos_imports pi ON pi.id = pil.import_id
		 WHERE pi.source_file = $1 AND pil.line_type = 'discount'`,
		filename).Scan(&discLines)
	if discLines != 1 {
		t.Errorf("expected 1 discount line, got %d", discLines)
	}
	if bal := getBalance(t, pool, accts.discountID); bal != 0 {
		t.Errorf("discount account balance = %d, want 0", bal)
	}
}
