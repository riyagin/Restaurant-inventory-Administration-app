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

// manualEntryFixtures backs the manual lot entry tests: one item, one warehouse
// with its own inventory account, and no stock to begin with.
type manualEntryFixtures struct {
	itemID      uuid.UUID
	warehouseID uuid.UUID
	invAcctID   uuid.UUID
}

func setupManualEntryFixtures(t *testing.T, pool *pgxpool.Pool) manualEntryFixtures {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	suffix := uuid.New().String()[:8]

	invAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: "ManInv " + suffix, AccountType: "asset"})
	if err != nil {
		t.Fatalf("create inventory account: %v", err)
	}

	itemID, warehouseID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'ManItem-'||$2, 'MNI-'||$2, '[{"name":"kg","perPrev":null}]', true)`,
		itemID, suffix); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (id, name, inventory_account_id) VALUES ($1, 'WHMan-'||$2, $3)`,
		warehouseID, suffix, invAcct.ID); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}

	t.Cleanup(func() {
		bCtx := context.Background()
		pool.Exec(bCtx, `DELETE FROM stock_history WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM inventory WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM warehouses WHERE id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM items WHERE id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM accounts WHERE id = $1`, invAcct.ID)
	})

	return manualEntryFixtures{itemID: itemID, warehouseID: warehouseID, invAcctID: invAcct.ID.Bytes}
}

// historyTotals sums the movement log for an item: the figures that must agree
// with what is physically on hand.
func historyTotals(t *testing.T, pool *pgxpool.Pool, itemID, warehouseID uuid.UUID) (qty float64, value int64, rows int) {
	t.Helper()
	pool.QueryRow(context.Background(), `
		SELECT COALESCE(SUM(quantity_change),0)::float8, COALESCE(SUM(value),0)::bigint, COUNT(*)::int
		FROM stock_history WHERE item_id = $1 AND warehouse_id = $2`,
		itemID, warehouseID).Scan(&qty, &value, &rows)
	return qty, value, rows
}

// lotID returns the single inventory lot for the item, failing if there is not
// exactly one.
func lotID(t *testing.T, pool *pgxpool.Pool, itemID, warehouseID uuid.UUID) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`SELECT id::text FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		itemID, warehouseID).Scan(&id); err != nil {
		t.Fatalf("expected exactly one lot: %v", err)
	}
	return id
}

// TestManualInventory_MovementLogMatchesStock walks a manual lot through create,
// edit and delete, checking after each step that the movement log still sums to
// what is physically on hand — and that the ledger moves with it.
func TestManualInventory_MovementLogMatchesStock(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupManualEntryFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewInventoryHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	invBefore := getBalance(t, pool, fix.invAcctID)

	// ── Create: 10 kg worth 100 000 ───────────────────────────────────────────
	rr := postJSON(t, h.Create, ctx, map[string]any{
		"item_id": fix.itemID.String(), "warehouse_id": fix.warehouseID.String(),
		"quantity": 10.0, "unit_index": 0, "unit_name": "kg", "value": 100000, "date": "2026-05-01",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	qty, value, rows := historyTotals(t, pool, fix.itemID, fix.warehouseID)
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != qty {
		t.Errorf("after create: on hand %.4f but history sums to %.4f", got, qty)
	}
	if value != 100000 || rows != 1 {
		t.Errorf("after create: history value %d over %d row(s), want 100000 over 1", value, rows)
	}
	if bal := getBalance(t, pool, fix.invAcctID); bal != invBefore+100000 {
		t.Errorf("after create: inventory account moved %d, want +100000", bal-invBefore)
	}

	// ── Edit: down to 6 kg worth 60 000 ───────────────────────────────────────
	lot := lotID(t, pool, fix.itemID, fix.warehouseID)
	rr = callWithID(t, h.Update, ctx, http.MethodPut, lot, map[string]any{
		"quantity": 6.0, "value": 60000, "date": "2026-05-01",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	qty, value, rows = historyTotals(t, pool, fix.itemID, fix.warehouseID)
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != qty {
		t.Errorf("after edit: on hand %.4f but history sums to %.4f", got, qty)
	}
	if rows != 2 {
		t.Errorf("after edit: %d history row(s), want 2 — an edit must leave a movement", rows)
	}
	if value != 60000 {
		t.Errorf("after edit: history value %d, want 60000", value)
	}
	if bal := getBalance(t, pool, fix.invAcctID); bal != invBefore+60000 {
		t.Errorf("after edit: inventory account moved %d, want +60000", bal-invBefore)
	}

	// ── Delete ────────────────────────────────────────────────────────────────
	rr = callWithID(t, h.Delete, ctx, http.MethodDelete, lot, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	qty, value, rows = historyTotals(t, pool, fix.itemID, fix.warehouseID)
	if qty != 0 || value != 0 {
		t.Errorf("after delete: history sums to %.4f qty / %d value, want 0 / 0", qty, value)
	}
	if rows != 3 {
		t.Errorf("after delete: %d history row(s), want 3 — a deletion must leave a movement", rows)
	}
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 0 {
		t.Errorf("after delete: on hand %.4f, want 0", got)
	}
	if bal := getBalance(t, pool, fix.invAcctID); bal != invBefore {
		t.Errorf("after delete: inventory account = %d, want %d (back to start)", bal, invBefore)
	}
}

// TestManualInventory_NotLabelledAsPurchase keeps a hand-entered lot out of the
// purchase bucket: it has no invoice, vendor or price behind it, and the item
// detail page groups its flow breakdown by this label.
func TestManualInventory_NotLabelledAsPurchase(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupManualEntryFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewInventoryHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Create, ctx, map[string]any{
		"item_id": fix.itemID.String(), "warehouse_id": fix.warehouseID.String(),
		"quantity": 3.0, "unit_index": 0, "unit_name": "kg", "value": 30000,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var moveType, sourceType string
	pool.QueryRow(context.Background(),
		`SELECT type, COALESCE(source_type,'') FROM stock_history WHERE item_id = $1`,
		fix.itemID).Scan(&moveType, &sourceType)
	if moveType != "manual_in" {
		t.Errorf("movement type = %q, want \"manual_in\"", moveType)
	}
	if sourceType != "inventory" {
		t.Errorf("source_type = %q, want \"inventory\" so the row is traceable", sourceType)
	}
}

// TestAccountAdjustment_ZeroRejected stops a record being stored that claims an
// adjustment happened when no journal entry stands behind it.
func TestAccountAdjustment_ZeroRejected(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupManualEntryFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewAccountAdjustmentsHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Create, ctx, map[string]any{
		"account_id": fix.invAcctID.String(), "amount": 0, "description": "tanpa nilai",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM account_adjustments WHERE account_id = $1`, fix.invAcctID).Scan(&count)
	if count != 0 {
		t.Errorf("%d adjustment row(s) stored, want 0", count)
	}
}
