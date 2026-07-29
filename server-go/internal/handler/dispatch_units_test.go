package handler_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/testutil"
)

// multiUnitFixtures is the dispatch fixture set for an item counted in two
// units: a dus of 24 kaleng, with kaleng as the base unit stock is held in.
type multiUnitFixtures struct {
	itemID      uuid.UUID
	warehouseID uuid.UUID
	branchID    uuid.UUID
}

// setupMultiUnitFixtures inserts one item with units [dus, kaleng/24] and a
// single lot of 100 kaleng worth 200 000 (2 000/kaleng).
func setupMultiUnitFixtures(t *testing.T, pool *pgxpool.Pool) multiUnitFixtures {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	suffix := uuid.New().String()[:8]

	invAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: "InvAcctU " + suffix, AccountType: "asset"})
	if err != nil {
		t.Fatalf("create inventory account: %v", err)
	}
	expAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: "ExpAcctU " + suffix, AccountType: "expense"})
	if err != nil {
		t.Fatalf("create expense account: %v", err)
	}
	revAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: "RevAcctU " + suffix, AccountType: "revenue"})
	if err != nil {
		t.Fatalf("create revenue account: %v", err)
	}

	itemID, warehouseID, branchID := uuid.New(), uuid.New(), uuid.New()

	if _, err := pool.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'UnitItem-'||$2, 'UNI-'||$2,
		         '[{"name":"dus"},{"name":"kaleng","perPrev":24}]', true)`,
		itemID, suffix); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (id, name, inventory_account_id) VALUES ($1, 'WHUnit-'||$2, $3)`,
		warehouseID, suffix, invAcct.ID); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO branches (id, name, revenue_account_id, expense_account_id)
		 VALUES ($1, 'BrUnit-'||$2, $3, $4)`,
		branchID, suffix, revAcct.ID, expAcct.ID); err != nil {
		t.Fatalf("insert branch: %v", err)
	}
	// Stock is held in the base unit: 100 kaleng at unit_index 1.
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 100, 1, 200000, '2026-01-01')`,
		itemID, warehouseID); err != nil {
		t.Fatalf("insert lot: %v", err)
	}

	t.Cleanup(func() {
		bCtx := context.Background()
		pool.Exec(bCtx, `DELETE FROM invoice_items WHERE invoice_id IN (
		    SELECT id FROM invoices WHERE dispatch_id IN
		        (SELECT id FROM dispatches WHERE warehouse_id = $1))`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM invoice_items WHERE item_id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM invoices WHERE dispatch_id IN (SELECT id FROM dispatches WHERE warehouse_id = $1)`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM invoices WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM dispatch_items WHERE dispatch_id IN (SELECT id FROM dispatches WHERE warehouse_id = $1)`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM dispatches WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM stock_history WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM inventory WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM warehouses WHERE id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM branches WHERE id = $1`, branchID)
		pool.Exec(bCtx, `DELETE FROM items WHERE id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM accounts WHERE id IN ($1, $2, $3)`, invAcct.ID, expAcct.ID, revAcct.ID)
	})

	return multiUnitFixtures{itemID: itemID, warehouseID: warehouseID, branchID: branchID}
}

// dispatchOneLine posts a single-line dispatch, optionally with a one-off
// conversion factor (0 = use the item's own units).
func dispatchOneLine(t *testing.T, pool *pgxpool.Pool, fix multiUnitFixtures, qty float64, unitIndex int, factor float64) {
	t.Helper()
	userID := createTestUser(t, pool)
	h := handler.NewDispatchesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	rr := postJSON(t, h.Create, ctx, map[string]any{
		"branch_id":    fix.branchID.String(),
		"warehouse_id": fix.warehouseID.String(),
		"items": []map[string]any{{
			"item_id": fix.itemID.String(), "quantity": qty,
			"unit_index": unitIndex, "unit_name": "dus",
			"conversion_factor": factor,
		}},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

// remainingStock is the item's on-hand quantity, which is always denominated in
// the base unit.
func remainingStock(t *testing.T, pool *pgxpool.Pool, fix multiUnitFixtures) float64 {
	t.Helper()
	var qty float64
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(quantity),0)::float8 FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		fix.itemID, fix.warehouseID).Scan(&qty); err != nil {
		t.Fatalf("read remaining stock: %v", err)
	}
	return qty
}

// TestDispatchCreate_ConvertsLargerUnitToBase dispatches 2 dus of an item whose
// dus holds 24 kaleng, and checks that stock moves in kaleng — 48, not 2.
func TestDispatchCreate_ConvertsLargerUnitToBase(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupMultiUnitFixtures(t, pool)

	dispatchOneLine(t, pool, fix, 2, 0, 0)

	if got := remainingStock(t, pool, fix); got != 52 {
		t.Errorf("remaining stock = %v kaleng, want 52 (100 − 2 dus × 24)", got)
	}

	// The line keeps the unit it was entered in, plus the rate it was booked at.
	var qty, factor float64
	var unitIndex int32
	if err := pool.QueryRow(context.Background(),
		`SELECT quantity::float8, unit_index, conversion_factor::float8
		 FROM dispatch_items WHERE item_id = $1`, fix.itemID).Scan(&qty, &unitIndex, &factor); err != nil {
		t.Fatalf("read dispatch item: %v", err)
	}
	if qty != 2 || unitIndex != 0 || factor != 24 {
		t.Errorf("dispatch_items = (qty %v, unit_index %d, factor %v), want (2, 0, 24)", qty, unitIndex, factor)
	}

	// Stock history is the movement log for inventory, so it is in base units.
	var change float64
	var unitName string
	if err := pool.QueryRow(context.Background(),
		`SELECT quantity_change::float8, unit_name FROM stock_history
		 WHERE item_id = $1 AND type = 'dispatch'`, fix.itemID).Scan(&change, &unitName); err != nil {
		t.Fatalf("read stock history: %v", err)
	}
	if change != -48 || unitName != "kaleng" {
		t.Errorf("stock_history = (%v, %q), want (-48, \"kaleng\")", change, unitName)
	}
}

// TestDispatchCreate_OneOffConversionOverride sends a dus holding 20 rather than
// the catalogued 24. The dispatch must move 20 kaleng and record the rate it
// used, while the item master keeps saying 24.
func TestDispatchCreate_OneOffConversionOverride(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupMultiUnitFixtures(t, pool)

	dispatchOneLine(t, pool, fix, 1, 0, 20)

	if got := remainingStock(t, pool, fix); got != 80 {
		t.Errorf("remaining stock = %v kaleng, want 80 (100 − 1 dus × 20)", got)
	}

	var factor float64
	if err := pool.QueryRow(context.Background(),
		`SELECT conversion_factor::float8 FROM dispatch_items WHERE item_id = $1`,
		fix.itemID).Scan(&factor); err != nil {
		t.Fatalf("read dispatch item: %v", err)
	}
	if factor != 20 {
		t.Errorf("stored conversion_factor = %v, want 20", factor)
	}

	// The override is scoped to the transaction — the catalogue is untouched.
	var units string
	if err := pool.QueryRow(context.Background(),
		`SELECT units::text FROM items WHERE id = $1`, fix.itemID).Scan(&units); err != nil {
		t.Fatalf("read item units: %v", err)
	}
	if !strings.Contains(units, `"perPrev": 24`) && !strings.Contains(units, `"perPrev":24`) {
		t.Errorf("items.units = %s, want the catalogue rate of 24 left intact", units)
	}
}

// TestDispatchCreate_BaseUnitIgnoresOverride guards the rule that an override is
// meaningless on the base unit: the quantity typed already is the base quantity,
// so a stray factor must not scale the movement.
func TestDispatchCreate_BaseUnitIgnoresOverride(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupMultiUnitFixtures(t, pool)

	dispatchOneLine(t, pool, fix, 10, 1, 24)

	if got := remainingStock(t, pool, fix); got != 90 {
		t.Errorf("remaining stock = %v kaleng, want 90 (100 − 10 kaleng)", got)
	}
}
