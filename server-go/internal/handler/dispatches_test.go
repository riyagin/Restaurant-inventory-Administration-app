package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/testutil"
)

// dispatchFixtures holds all IDs created for a dispatch integration test.
type dispatchFixtures struct {
	itemID      uuid.UUID
	warehouseID uuid.UUID
	branchID    uuid.UUID
	invAcctID   uuid.UUID
	expAcctID   uuid.UUID
}

// setupDispatchFixtures inserts accounts, item, warehouse, branch, and two
// inventory lots. Cleanup is registered with t.Cleanup.
func setupDispatchFixtures(t *testing.T, pool *pgxpool.Pool) dispatchFixtures {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)

	suffix := uuid.New().String()[:8]

	invAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{
		Name: "InvAcct " + suffix, AccountType: "asset",
	})
	if err != nil {
		t.Fatalf("create inventory account: %v", err)
	}
	expAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{
		Name: "ExpAcct " + suffix, AccountType: "expense",
	})
	if err != nil {
		t.Fatalf("create expense account: %v", err)
	}
	revAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{
		Name: "RevAcct " + suffix, AccountType: "revenue",
	})
	if err != nil {
		t.Fatalf("create revenue account: %v", err)
	}

	itemID := uuid.New()
	warehouseID := uuid.New()
	branchID := uuid.New()

	if _, err := pool.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'DispatchItem-'||$2, 'DIT-'||$2, '[{"name":"kg","ratio":1}]', true)`,
		itemID, suffix); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (id, name, inventory_account_id) VALUES ($1, 'WHDisp-'||$2, $3)`,
		warehouseID, suffix, invAcct.ID); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO branches (id, name, revenue_account_id, expense_account_id)
		 VALUES ($1, 'BrDisp-'||$2, $3, $4)`,
		branchID, suffix, revAcct.ID, expAcct.ID); err != nil {
		t.Fatalf("insert branch: %v", err)
	}

	// Lot A: Jan 2026, 5 kg, value 50 000 (10 000/kg).
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 5, 0, 50000, '2026-01-01')`,
		itemID, warehouseID); err != nil {
		t.Fatalf("insert lot A: %v", err)
	}
	// Lot B: Mar 2026, 5 kg, value 75 000 (15 000/kg).
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 5, 0, 75000, '2026-03-01')`,
		itemID, warehouseID); err != nil {
		t.Fatalf("insert lot B: %v", err)
	}

	fix := dispatchFixtures{
		itemID: itemID, warehouseID: warehouseID, branchID: branchID,
		invAcctID: invAcct.ID.Bytes, expAcctID: expAcct.ID.Bytes,
	}

	t.Cleanup(func() {
		bCtx := context.Background()
		pool.Exec(bCtx,
			`DELETE FROM invoice_items WHERE invoice_id IN (
			     SELECT id FROM invoices WHERE dispatch_id IN
			         (SELECT id FROM dispatches WHERE warehouse_id = $1))`, warehouseID)
		pool.Exec(bCtx,
			`DELETE FROM invoices WHERE dispatch_id IN (SELECT id FROM dispatches WHERE warehouse_id = $1)`,
			warehouseID)
		pool.Exec(bCtx,
			`DELETE FROM dispatch_items WHERE dispatch_id IN (SELECT id FROM dispatches WHERE warehouse_id = $1)`,
			warehouseID)
		pool.Exec(bCtx, `DELETE FROM dispatches WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM stock_history WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM inventory WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM warehouses WHERE id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM branches WHERE id = $1`, branchID)
		pool.Exec(bCtx, `DELETE FROM items WHERE id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM accounts WHERE id IN ($1, $2, $3)`,
			invAcct.ID, expAcct.ID, revAcct.ID)
	})
	return fix
}

// TestDispatchCreate_FIFODeductionAndAccountBalance dispatches 7 kg across
// two lots (5 kg Jan + 5 kg Mar) and verifies:
//  1. Older lot fully consumed (deleted).
//  2. Newer lot has 3 kg / 45 000 IDR remaining.
//  3. Branch expense account +80 000, warehouse inventory account −80 000.
func TestDispatchCreate_FIFODeductionAndAccountBalance(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupDispatchFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewDispatchesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Create, ctx, map[string]any{
		"branch_id":    fix.branchID.String(),
		"warehouse_id": fix.warehouseID.String(),
		"items": []map[string]any{{
			"item_id": fix.itemID.String(), "quantity": 7.0,
			"unit_index": 0, "unit_name": "kg",
		}},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	bCtx := context.Background()

	// ── Inventory lots ────────────────────────────────────────────────────────
	var lotCount int
	pool.QueryRow(bCtx,
		`SELECT COUNT(*) FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		fix.itemID, fix.warehouseID).Scan(&lotCount)
	if lotCount != 1 {
		t.Errorf("lot count = %d, want 1", lotCount)
	}

	var remQty float64
	var remVal int64
	pool.QueryRow(bCtx,
		`SELECT quantity::float8, value FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		fix.itemID, fix.warehouseID).Scan(&remQty, &remVal)
	if remQty < 2.999 || remQty > 3.001 {
		t.Errorf("remaining lot qty = %.4f, want ~3", remQty)
	}
	if remVal != 45000 {
		t.Errorf("remaining lot value = %d, want 45000", remVal)
	}

	// ── Account balances ──────────────────────────────────────────────────────
	// Total FIFO value = 50 000 (all of lot A) + (2/5)×75 000 = 80 000.
	expBal := getBalance(t, pool, fix.expAcctID)
	invBal := getBalance(t, pool, fix.invAcctID)
	if expBal != 80000 {
		t.Errorf("expense account balance = %d, want 80000", expBal)
	}
	if invBal != -80000 {
		t.Errorf("inventory account balance = %d, want -80000", invBal)
	}
}

// TestDispatchCreate_InsufficientStock confirms 422 when dispatch qty > stock.
func TestDispatchCreate_InsufficientStock(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupDispatchFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewDispatchesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Create, ctx, map[string]any{
		"branch_id":    fix.branchID.String(),
		"warehouse_id": fix.warehouseID.String(),
		"items": []map[string]any{{
			"item_id": fix.itemID.String(), "quantity": 999.0,
			"unit_index": 0, "unit_name": "kg",
		}},
	})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestDispatchCreate_AutoInvoiceLinked verifies an expense invoice is
// auto-created and linked to the dispatch.
func TestDispatchCreate_AutoInvoiceLinked(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupDispatchFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewDispatchesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Create, ctx, map[string]any{
		"branch_id":    fix.branchID.String(),
		"warehouse_id": fix.warehouseID.String(),
		"items": []map[string]any{{
			"item_id": fix.itemID.String(), "quantity": 2.0,
			"unit_index": 0, "unit_name": "kg",
		}},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	dispMap, _ := resp["dispatch"].(map[string]any)
	dispatchIDStr, _ := dispMap["id"].(string)
	if dispatchIDStr == "" {
		t.Fatal("dispatch.id missing from response")
	}

	var invoiceCount int
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM invoices WHERE dispatch_id = $1`, dispatchIDStr).
		Scan(&invoiceCount)
	if invoiceCount != 1 {
		t.Errorf("expected 1 linked invoice, got %d", invoiceCount)
	}
}
