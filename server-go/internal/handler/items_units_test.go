package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/testutil"
)

// putItem calls ItemsHandler.Update with {id} bound on the chi route context.
func putItem(t *testing.T, h *handler.ItemsHandler, ctx context.Context, id uuid.UUID, body any) *httptest.ResponseRecorder {
	t.Helper()
	bs, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(bs))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	h.Update(rr, req)
	return rr
}

// setupUnitItem creates a stock item with [dus, pack/12] plus one warehouse.
func setupUnitItem(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.New().String()[:8]
	itemID := uuid.New()
	warehouseID := uuid.New()

	if _, err := pool.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'UnitItem-'||$2, 'UNI-'||$2,
		         '[{"name":"dus","perPrev":null},{"name":"pack","perPrev":12}]', true)`,
		itemID, suffix); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (id, name) VALUES ($1, 'WHUnit-'||$2)`,
		warehouseID, suffix); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}

	t.Cleanup(func() {
		bCtx := context.Background()
		pool.Exec(bCtx, `DELETE FROM inventory WHERE item_id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM activity_log WHERE entity_id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM warehouses WHERE id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM items WHERE id = $1`, itemID)
	})
	return itemID, warehouseID
}

// Appending a smaller unit demotes "pack" from base to middle. The 5 packs on
// hand must come back as 50 pcs at the new base index, with value untouched —
// same goods, same money, finer unit.
func TestItemUpdate_AppendSmallerUnitRescalesInventory(t *testing.T) {
	pool := testutil.OpenDB(t)
	itemID, warehouseID := setupUnitItem(t, pool)
	userID := createTestUser(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 5, 1, 172000, '2026-01-01')`,
		itemID, warehouseID); err != nil {
		t.Fatalf("insert lot: %v", err)
	}

	h := handler.NewItemsHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(ctx, testClaims(userID))

	rr := putItem(t, h, authCtx, itemID, map[string]any{
		"name": "UnitItem", "code": "UNI", "is_stock": true,
		"units": []map[string]any{
			{"name": "dus", "perPrev": nil},
			{"name": "pack", "perPrev": 12},
			{"name": "pcs", "perPrev": 10},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var qty float64
	var unitIdx int32
	var value int64
	if err := pool.QueryRow(ctx,
		`SELECT quantity::float8, unit_index, value FROM inventory WHERE item_id = $1`,
		itemID).Scan(&qty, &unitIdx, &value); err != nil {
		t.Fatalf("read lot: %v", err)
	}

	if qty < 49.999 || qty > 50.001 {
		t.Errorf("quantity = %.4f, want ~50 (5 pack × 10 pcs)", qty)
	}
	if unitIdx != 2 {
		t.Errorf("unit_index = %d, want 2 (new base 'pcs')", unitIdx)
	}
	if value != 172000 {
		t.Errorf("value = %d, want 172000 (unchanged)", value)
	}
}

// Editing an item without touching its units must leave stock exactly as it was.
func TestItemUpdate_NameOnlyLeavesInventoryAlone(t *testing.T) {
	pool := testutil.OpenDB(t)
	itemID, warehouseID := setupUnitItem(t, pool)
	userID := createTestUser(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 5, 1, 172000, '2026-01-01')`,
		itemID, warehouseID); err != nil {
		t.Fatalf("insert lot: %v", err)
	}

	h := handler.NewItemsHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(ctx, testClaims(userID))

	rr := putItem(t, h, authCtx, itemID, map[string]any{
		"name": "Renamed", "code": "UNI", "is_stock": true,
		"units": []map[string]any{
			{"name": "dus", "perPrev": nil},
			{"name": "pack", "perPrev": 12},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var qty float64
	var unitIdx int32
	pool.QueryRow(ctx,
		`SELECT quantity::float8, unit_index FROM inventory WHERE item_id = $1`,
		itemID).Scan(&qty, &unitIdx)
	if qty < 4.999 || qty > 5.001 || unitIdx != 1 {
		t.Errorf("lot = %.4f @ idx %d, want 5 @ idx 1 (untouched)", qty, unitIdx)
	}
}

// Removing the unit that stock is denominated in is rejected, and the rejection
// must roll back the item row too — a committed unit list with unconverted lots
// is exactly the corruption this feature exists to prevent.
func TestItemUpdate_DroppingStockedUnitIsRejectedAtomically(t *testing.T) {
	pool := testutil.OpenDB(t)
	itemID, warehouseID := setupUnitItem(t, pool)
	userID := createTestUser(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 5, 1, 172000, '2026-01-01')`,
		itemID, warehouseID); err != nil {
		t.Fatalf("insert lot: %v", err)
	}

	h := handler.NewItemsHandler(pool, db.New(pool))
	authCtx := middleware.ContextWithClaims(ctx, testClaims(userID))

	rr := putItem(t, h, authCtx, itemID, map[string]any{
		"name": "UnitItem", "code": "UNI", "is_stock": true,
		"units": []map[string]any{{"name": "dus", "perPrev": nil}},
	})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rr.Code, rr.Body.String())
	}

	var nUnits int
	pool.QueryRow(ctx, `SELECT jsonb_array_length(units) FROM items WHERE id = $1`, itemID).Scan(&nUnits)
	if nUnits != 2 {
		t.Errorf("units length = %d, want 2 (update rolled back)", nUnits)
	}
}
