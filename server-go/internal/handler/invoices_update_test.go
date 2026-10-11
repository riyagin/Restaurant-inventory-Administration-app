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
	"inventory-app/server-go/internal/service"
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

// lotValue sums the value of an item's remaining lots in the warehouse.
func lotValue(t *testing.T, pool *pgxpool.Pool, itemID, warehouseID uuid.UUID) int64 {
	t.Helper()
	var v int64
	pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(value),0)::bigint FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		itemID, warehouseID).Scan(&v)
	return v
}

func invoiceStatus(t *testing.T, pool *pgxpool.Pool, invoiceID string) (string, int64) {
	t.Helper()
	var status string
	var paid int64
	pool.QueryRow(context.Background(),
		`SELECT payment_status, amount_paid FROM invoices WHERE id = $1`, invoiceID).Scan(&status, &paid)
	return status, paid
}

// TestInvoiceUpdate_PaidInvoicePriceCorrection covers the case the edit used to
// refuse: the invoice is already paid and most of its goods have been used when
// the supplier's real price turns up. The edit must go through without trying
// to pull the consumed stock back out, revalue only what is still on hand, and
// re-derive the payment status from what was paid against the new total.
func TestInvoiceUpdate_PaidInvoicePriceCorrection(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupInvoiceUnitFixtures(t, pool)
	userID := createTestUser(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	cash, err := q.CreateAccount(ctx, &db.CreateAccountParams{
		Name: "InvUpdCash " + uuid.New().String()[:8], AccountType: "asset",
	})
	if err != nil {
		t.Fatalf("create cash account: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET balance = 10000000 WHERE id = $1`, cash.ID); err != nil {
		t.Fatalf("fund cash account: %v", err)
	}

	h := handler.NewInvoicesHandler(pool, q)
	actx := middleware.ContextWithClaims(ctx, testClaims(userID))

	// 2 dus × 60.000 = 120.000 for 20 pcs (6.000/pcs), paid in full on entry.
	rr := postJSON(t, h.Create, actx, map[string]any{
		"invoice_type":   "purchase",
		"warehouse_id":   fix.warehouseID.String(),
		"date":           "2026-05-01",
		"payment_status": "paid",
		"account_id":     uuid.UUID(cash.ID.Bytes).String(),
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0, "price": 60000},
		},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create invoice: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, rr, &created)
	invoiceID := created.ID

	// 15 of the 20 pcs are used before the real price is known.
	if _, err := service.FIFODeduct(ctx, q, fix.itemID, fix.warehouseID, 15); err != nil {
		t.Fatalf("consume stock: %v", err)
	}
	if got := lotValue(t, pool, fix.itemID, fix.warehouseID); got != 30000 {
		t.Fatalf("after consumption, lot value = %d, want 30000", got)
	}

	edit := func(price int) {
		t.Helper()
		rr := callWithID(t, h.Update, actx, http.MethodPut, invoiceID, map[string]any{
			"warehouse_id": fix.warehouseID.String(),
			"date":         "2026-05-01",
			"account_id":   uuid.UUID(cash.ID.Bytes).String(),
			"items": []map[string]any{
				{"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0, "price": price},
			},
		})
		if rr.Code != http.StatusOK {
			t.Fatalf("update to %d: expected 200, got %d: %s", price, rr.Code, rr.Body.String())
		}
	}

	// Real price 66.000/dus: +12.000 overall, +600/pcs. The 5 pcs on hand take
	// +3.000; the 15 already used went out at the old cost, so their +9.000 is a
	// variance, not inventory.
	edit(66000)
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 5 {
		t.Errorf("stock = %.4f pcs, want 5 (a price correction must not move stock)", got)
	}
	if got := lotValue(t, pool, fix.itemID, fix.warehouseID); got != 33000 {
		t.Errorf("lot value = %d, want 33000", got)
	}
	if bal := getBalance(t, pool, fix.invAcctID); bal != 123000 {
		t.Errorf("inventory account = %d, want 123000 (120000 + the on-hand share 3000)", bal)
	}
	if status, paid := invoiceStatus(t, pool, invoiceID); status != "partial" || paid != 120000 {
		t.Errorf("status = %s paid %d, want partial / 120000 (132000 owed)", status, paid)
	}

	// Revised down below what was paid: -32.000, the on-hand share -8.000.
	edit(50000)
	if got := lotValue(t, pool, fix.itemID, fix.warehouseID); got != 25000 {
		t.Errorf("lot value = %d, want 25000", got)
	}
	if bal := getBalance(t, pool, fix.invAcctID); bal != 115000 {
		t.Errorf("inventory account = %d, want 115000", bal)
	}
	if status, _ := invoiceStatus(t, pool, invoiceID); status != "paid" {
		t.Errorf("status = %s, want paid (120000 paid against 100000)", status)
	}
}

// TestInvoiceUpdate_PaidInvoiceKeepsVendor refuses moving a paid invoice to a
// different vendor: its settlement already debited the original vendor's
// payable, which would be left showing a payment against nothing.
func TestInvoiceUpdate_PaidInvoiceKeepsVendor(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupInvoiceUnitFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewInvoicesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	invoiceID := createPurchaseInvoice(t, h, ctx, fix, map[string]any{
		"item_id": fix.itemID.String(), "quantity": 1.0, "unit_index": 0, "price": 60000,
	})
	if _, err := pool.Exec(context.Background(),
		`UPDATE invoices SET amount_paid = 60000, payment_status = 'paid' WHERE id = $1`, invoiceID); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	rr := callWithID(t, h.Update, ctx, http.MethodPut, invoiceID, map[string]any{
		"warehouse_id": fix.warehouseID.String(),
		"vendor_id":    uuid.New().String(),
		"date":         "2026-05-01",
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "quantity": 1.0, "unit_index": 0, "price": 60000},
		},
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestInvoiceUpdate_DateMoveReversesInOriginalPeriod moves an invoice from May
// to June. The reversal must land in May, where the original entry is, so May
// nets to zero and June carries the revised amount — dating it on the new date
// left May overstated and June short by the original total.
func TestInvoiceUpdate_DateMoveReversesInOriginalPeriod(t *testing.T) {
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
		"date":         "2026-06-15",
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0, "price": 65000},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	monthNet := func(month string) int64 {
		var net int64
		pool.QueryRow(context.Background(),
			`SELECT COALESCE(SUM(jl.amount),0)::bigint
			   FROM journal_lines jl JOIN journal_entries je ON je.id = jl.entry_id
			  WHERE je.source_id = $1 AND jl.account_id = $2
			    AND to_char(je.entry_date, 'YYYY-MM') = $3`,
			invoiceID, fix.invAcctID, month).Scan(&net)
		return net
	}
	if got := monthNet("2026-05"); got != 0 {
		t.Errorf("May net on inventory = %d, want 0 (original and its reversal)", got)
	}
	if got := monthNet("2026-06"); got != 130000 {
		t.Errorf("June net on inventory = %d, want 130000 (the revised invoice only)", got)
	}
}
