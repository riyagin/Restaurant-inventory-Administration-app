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

// opnameFixtures holds IDs for stock-opname integration tests.
type opnameFixtures struct {
	itemID      uuid.UUID
	warehouseID uuid.UUID
	invAcctID   uuid.UUID
}

func setupOpnameFixtures(t *testing.T, pool *pgxpool.Pool) opnameFixtures {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	suffix := uuid.New().String()[:8]

	invAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{
		Name: "InvAcctOpname-" + suffix, AccountType: "asset",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	itemID := uuid.New()
	warehouseID := uuid.New()

	if _, err := pool.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'OpnameItem-'||$2, 'OPN-'||$2, '[{"name":"kg","ratio":1}]', true)`,
		itemID, suffix); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (id, name, inventory_account_id) VALUES ($1, 'WHOpn-'||$2, $3)`,
		warehouseID, suffix, invAcct.ID); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}

	fix := opnameFixtures{itemID: itemID, warehouseID: warehouseID, invAcctID: invAcct.ID.Bytes}

	t.Cleanup(func() {
		bCtx := context.Background()
		pool.Exec(bCtx,
			`DELETE FROM stock_opname_items WHERE opname_id IN (SELECT id FROM stock_opname WHERE warehouse_id = $1)`,
			warehouseID)
		pool.Exec(bCtx, `DELETE FROM stock_opname WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM stock_history WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM inventory WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM warehouses WHERE id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM items WHERE id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM accounts WHERE id = $1`, invAcct.ID)
	})
	return fix
}

// TestStockOpname_Loss verifies that when actual < recorded:
//  1. Inventory lots are FIFO-deducted by the loss amount.
//  2. The opname_item records the correct difference and waste_value.
func TestStockOpname_Loss(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupOpnameFixtures(t, pool)
	userID := createTestUser(t, pool)
	ctx := context.Background()

	// 10 kg in one lot, value 100 000.
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 10, 0, 100000, '2026-01-01')`,
		fix.itemID, fix.warehouseID); err != nil {
		t.Fatalf("insert lot: %v", err)
	}

	// Opname says only 6 kg found → 4 kg loss.
	h := handler.NewStockOpnameHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(ctx, testClaims(userID))

	rr := postJSON(t, h.Create, authCtx, map[string]any{
		"warehouse_id": fix.warehouseID.String(),
		"items": []map[string]any{{
			"item_id": fix.itemID.String(), "unit_index": 0,
			"unit_name": "kg", "actual_quantity": 6.0,
		}},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Inventory should now be 6 kg.
	var totalQty float64
	pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(quantity::float8), 0) FROM inventory
		 WHERE item_id = $1 AND warehouse_id = $2`,
		fix.itemID, fix.warehouseID).Scan(&totalQty)
	if totalQty < 5.999 || totalQty > 6.001 {
		t.Errorf("inventory qty after loss = %.4f, want ~6", totalQty)
	}

	// Opname item must record −4 difference and 40 000 waste value (4/10 × 100 000).
	var diff float64
	var wasteVal int64
	pool.QueryRow(ctx,
		`SELECT difference::float8, waste_value
		 FROM stock_opname_items soi
		 JOIN stock_opname so ON so.id = soi.opname_id
		 WHERE soi.item_id = $1 AND so.warehouse_id = $2
		 ORDER BY so.performed_at DESC LIMIT 1`,
		fix.itemID, fix.warehouseID).Scan(&diff, &wasteVal)

	if diff > -3.999 || diff < -4.001 {
		t.Errorf("opname difference = %.4f, want ~-4", diff)
	}
	if wasteVal != 40000 {
		t.Errorf("waste_value = %d, want 40000", wasteVal)
	}
}

// TestStockOpname_Surplus verifies that when actual > recorded a new lot is
// added and total inventory quantity equals the actual count.
func TestStockOpname_Surplus(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupOpnameFixtures(t, pool)
	userID := createTestUser(t, pool)
	ctx := context.Background()

	// Seed a prior purchase invoice so GetItemLastPrice returns a price.
	itemLastPriceSetup(t, pool, fix.itemID, fix.warehouseID, 10000)

	// Existing stock: 5 kg.
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 5, 0, 50000, '2026-01-01')`,
		fix.itemID, fix.warehouseID); err != nil {
		t.Fatalf("insert lot: %v", err)
	}

	// Opname says 8 kg found → 3 kg surplus.
	h := handler.NewStockOpnameHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(ctx, testClaims(userID))

	rr := postJSON(t, h.Create, authCtx, map[string]any{
		"warehouse_id": fix.warehouseID.String(),
		"items": []map[string]any{{
			"item_id": fix.itemID.String(), "unit_index": 0,
			"unit_name": "kg", "actual_quantity": 8.0,
		}},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Total inventory should be 8 kg in 2 lots.
	var totalQty float64
	pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(quantity::float8), 0) FROM inventory
		 WHERE item_id = $1 AND warehouse_id = $2`,
		fix.itemID, fix.warehouseID).Scan(&totalQty)
	if totalQty < 7.999 || totalQty > 8.001 {
		t.Errorf("inventory qty after surplus = %.4f, want ~8", totalQty)
	}

	var lotCount int
	pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		fix.itemID, fix.warehouseID).Scan(&lotCount)
	if lotCount != 2 {
		t.Errorf("expected 2 lots after surplus, got %d", lotCount)
	}
}

// TestStockOpname_NoChange confirms no inventory mutation when actual equals
// recorded, and the opname item records zero difference.
func TestStockOpname_NoChange(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupOpnameFixtures(t, pool)
	userID := createTestUser(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 7, 0, 70000, '2026-01-01')`,
		fix.itemID, fix.warehouseID); err != nil {
		t.Fatalf("insert lot: %v", err)
	}

	h := handler.NewStockOpnameHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(ctx, testClaims(userID))

	rr := postJSON(t, h.Create, authCtx, map[string]any{
		"warehouse_id": fix.warehouseID.String(),
		"items": []map[string]any{{
			"item_id": fix.itemID.String(), "unit_index": 0,
			"unit_name": "kg", "actual_quantity": 7.0,
		}},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var qty float64
	var lots int
	pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(quantity::float8), 0) FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		fix.itemID, fix.warehouseID).Scan(&qty)
	pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		fix.itemID, fix.warehouseID).Scan(&lots)

	if qty < 6.999 || qty > 7.001 {
		t.Errorf("inventory qty = %.4f, want 7 (unchanged)", qty)
	}
	if lots != 1 {
		t.Errorf("lot count = %d, want 1 (unchanged)", lots)
	}
}

// itemLastPriceSetup inserts a minimal purchase invoice + invoice_item so that
// GetItemLastPrice returns a known price for surplus lot valuation.
func itemLastPriceSetup(t *testing.T, pool *pgxpool.Pool, itemID, warehouseID uuid.UUID, pricePerUnit int64) {
	t.Helper()
	ctx := context.Background()

	var vendorID uuid.UUID
	pool.QueryRow(ctx,
		`INSERT INTO vendors (id, name) VALUES (gen_random_uuid(), 'TestVendorOpname') RETURNING id`).
		Scan(&vendorID)

	var invID uuid.UUID
	pool.QueryRow(ctx,
		`INSERT INTO invoices (id, invoice_number, date, invoice_type, payment_status, amount_paid, warehouse_id, vendor_id)
		 VALUES (gen_random_uuid(), 'OPNAME-PRICE-TEST', '2026-01-01', 'purchase', 'paid', 0, $1, $2)
		 RETURNING id`,
		warehouseID, vendorID).Scan(&invID)
	pool.Exec(ctx,
		`INSERT INTO invoice_items (id, invoice_id, item_id, quantity, unit_index, price)
		 VALUES (gen_random_uuid(), $1, $2, 1, 0, $3)`,
		invID, itemID, pricePerUnit)

	t.Cleanup(func() {
		bCtx := context.Background()
		pool.Exec(bCtx, `DELETE FROM invoice_items WHERE invoice_id = $1`, invID)
		pool.Exec(bCtx, `DELETE FROM invoices WHERE id = $1`, invID)
		pool.Exec(bCtx, `DELETE FROM vendors WHERE id = $1`, vendorID)
	})
}
