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

// invoiceUnitFixtures backs the purchase-invoice update tests. The item has two
// units, [dus, pcs] with 10 pcs per dus, so pcs (index 1) is the base unit that
// inventory is denominated in and a line entered in dus must be converted.
type invoiceUnitFixtures struct {
	itemID      uuid.UUID
	warehouseID uuid.UUID
	invAcctID   uuid.UUID
}

func setupInvoiceUnitFixtures(t *testing.T, pool *pgxpool.Pool) invoiceUnitFixtures {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	suffix := uuid.New().String()[:8]

	invAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{
		Name: "InvUpdAcct " + suffix, AccountType: "asset",
	})
	if err != nil {
		t.Fatalf("create inventory account: %v", err)
	}

	itemID, warehouseID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'InvUpdItem-'||$2, 'IUI-'||$2,
		         '[{"name":"dus","perPrev":null},{"name":"pcs","perPrev":10}]', true)`,
		itemID, suffix); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (id, name, inventory_account_id) VALUES ($1, 'WHInvUpd-'||$2, $3)`,
		warehouseID, suffix, invAcct.ID); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}

	t.Cleanup(func() {
		bCtx := context.Background()
		pool.Exec(bCtx, `DELETE FROM invoice_items WHERE item_id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM invoices WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM stock_history WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM inventory WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM warehouses WHERE id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM items WHERE id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM accounts WHERE id = $1`, invAcct.ID)
	})

	return invoiceUnitFixtures{itemID: itemID, warehouseID: warehouseID, invAcctID: invAcct.ID.Bytes}
}

// baseStock sums an item's lots in the warehouse. Every lot is denominated in
// the base unit, so this is the figure a dus-entered line must convert into.
func baseStock(t *testing.T, pool *pgxpool.Pool, itemID, warehouseID uuid.UUID) float64 {
	t.Helper()
	var qty float64
	pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(quantity),0)::float8 FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		itemID, warehouseID).Scan(&qty)
	return qty
}

// createPurchaseInvoice posts a one-line purchase invoice and returns its id.
func createPurchaseInvoice(t *testing.T, h *handler.InvoicesHandler, ctx context.Context, fix invoiceUnitFixtures, line map[string]any) string {
	t.Helper()
	body := map[string]any{
		"invoice_type": "purchase",
		"warehouse_id": fix.warehouseID.String(),
		"date":         "2026-05-01",
		"items":        []map[string]any{line},
	}
	rr := postJSON(t, h.Create, ctx, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create invoice: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, rr, &created)
	if created.ID == "" {
		t.Fatal("create invoice: no id in response")
	}
	return created.ID
}

// TestInvoiceUpdate_ConvertsQuantityToBaseUnit raises a purchase line from 2 dus
// to 3 dus and checks inventory ends at 30 pcs, not 3 — the edit path must
// convert the same way the create path does.
func TestInvoiceUpdate_ConvertsQuantityToBaseUnit(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupInvoiceUnitFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewInvoicesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	invoiceID := createPurchaseInvoice(t, h, ctx, fix, map[string]any{
		"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0, "price": 60000,
	})

	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 20 {
		t.Fatalf("after create, stock = %.4f pcs, want 20 (2 dus × 10)", got)
	}
	// The factor has to be stored on the line, since that is what a later edit
	// or cancellation reverses with.
	var storedFactor float64
	pool.QueryRow(context.Background(),
		`SELECT conversion_factor::float8 FROM invoice_items WHERE invoice_id = $1`, invoiceID).Scan(&storedFactor)
	if storedFactor != 10 {
		t.Errorf("stored conversion_factor = %.2f, want 10", storedFactor)
	}

	rr := callWithID(t, h.Update, ctx, http.MethodPut, invoiceID, map[string]any{
		"warehouse_id": fix.warehouseID.String(),
		"date":         "2026-05-01",
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "quantity": 3.0, "unit_index": 0, "price": 60000},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 30 {
		t.Errorf("after update, stock = %.4f pcs, want 30 (3 dus × 10)", got)
	}
	// Reverse-then-reapply: the inventory account should hold only the revised
	// invoice's value, not the sum of both revisions.
	if bal := getBalance(t, pool, fix.invAcctID); bal != 180000 {
		t.Errorf("inventory account balance = %d, want 180000", bal)
	}
}

// TestInvoiceUpdate_ReversesAtStoredFactor is the regression that matters most
// on the edit path: the item's catalogue changes between the create and the
// edit, and the reversal must still deduct what the line actually added (at its
// stored factor), not what today's catalogue would imply.
func TestInvoiceUpdate_ReversesAtStoredFactor(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupInvoiceUnitFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewInvoicesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	invoiceID := createPurchaseInvoice(t, h, ctx, fix, map[string]any{
		"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0, "price": 60000,
	})
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 20 {
		t.Fatalf("after create, stock = %.4f pcs, want 20", got)
	}

	// The supplier's dus now holds 20 pcs. Only the catalogue moves; the booked
	// line keeps its factor of 10.
	if _, err := pool.Exec(context.Background(),
		`UPDATE items SET units = '[{"name":"dus","perPrev":null},{"name":"pcs","perPrev":20}]' WHERE id = $1`,
		fix.itemID); err != nil {
		t.Fatalf("update item units: %v", err)
	}

	rr := callWithID(t, h.Update, ctx, http.MethodPut, invoiceID, map[string]any{
		"warehouse_id": fix.warehouseID.String(),
		"date":         "2026-05-01",
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0, "price": 60000},
		},
	})
	// Reversing at the new catalogue figure would try to take 40 pcs out of a
	// 20 pcs stock and fail with 422 — so a non-200 here is the bug itself.
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// 20 pcs reversed at the stored factor, 40 pcs re-added at the new one.
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 40 {
		t.Errorf("stock = %.4f pcs, want 40 (reversal at stored 10, re-add at catalogue 20)", got)
	}
}

// TestInvoiceUpdate_HonoursPerLineFactorOverride checks a one-off factor typed
// on the edit form wins over the catalogue for the new line, without being
// written back to the item.
func TestInvoiceUpdate_HonoursPerLineFactorOverride(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupInvoiceUnitFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewInvoicesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	invoiceID := createPurchaseInvoice(t, h, ctx, fix, map[string]any{
		"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0, "price": 60000,
	})

	rr := callWithID(t, h.Update, ctx, http.MethodPut, invoiceID, map[string]any{
		"warehouse_id": fix.warehouseID.String(),
		"date":         "2026-05-01",
		"items": []map[string]any{{
			"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0,
			"price": 60000, "conversion_factor": 7.0,
		}},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 14 {
		t.Errorf("stock = %.4f pcs, want 14 (2 dus × 7 override)", got)
	}
	var storedFactor float64
	pool.QueryRow(context.Background(),
		`SELECT conversion_factor::float8 FROM invoice_items WHERE invoice_id = $1`, invoiceID).Scan(&storedFactor)
	if storedFactor != 7 {
		t.Errorf("stored conversion_factor = %.2f, want 7", storedFactor)
	}

	// The override is a property of this line only — the catalogue is untouched.
	var units string
	pool.QueryRow(context.Background(), `SELECT units::text FROM items WHERE id = $1`, fix.itemID).Scan(&units)
	// jsonb::text renders with spaces, so compare without them.
	if compact := strings.ReplaceAll(units, " ", ""); !strings.Contains(compact, `"perPrev":10`) {
		t.Errorf("item units changed to %s; a per-line override must not write back", units)
	}
}
