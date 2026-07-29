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

// dispatchUnitFixtures backs the dispatch edit tests. Like the invoice ones the
// item is [dus, pcs] at 10 pcs per dus, so pcs is the base unit inventory is
// held in. One lot of 100 pcs worth 100 000 makes the unit cost a round 1 000.
type dispatchUnitFixtures struct {
	itemID      uuid.UUID
	warehouseID uuid.UUID
	branchID    uuid.UUID
	divisionID  uuid.UUID
	invAcctID   uuid.UUID
	expAcctID   uuid.UUID
}

func setupDispatchUnitFixtures(t *testing.T, pool *pgxpool.Pool) dispatchUnitFixtures {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	suffix := uuid.New().String()[:8]

	invAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: "DUInv " + suffix, AccountType: "asset"})
	if err != nil {
		t.Fatalf("create inventory account: %v", err)
	}
	brExpAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: "DUBrExp " + suffix, AccountType: "expense"})
	if err != nil {
		t.Fatalf("create branch expense account: %v", err)
	}
	brRevAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: "DUBrRev " + suffix, AccountType: "revenue"})
	if err != nil {
		t.Fatalf("create branch revenue account: %v", err)
	}
	divExpAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: "DUDivExp " + suffix, AccountType: "expense"})
	if err != nil {
		t.Fatalf("create division expense account: %v", err)
	}

	itemID, warehouseID := uuid.New(), uuid.New()
	branchID, divisionID := uuid.New(), uuid.New()

	if _, err := pool.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'DUItem-'||$2, 'DUI-'||$2,
		         '[{"name":"dus","perPrev":null},{"name":"pcs","perPrev":10}]', true)`,
		itemID, suffix); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (id, name, inventory_account_id) VALUES ($1, 'WHDU-'||$2, $3)`,
		warehouseID, suffix, invAcct.ID); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO branches (id, name, revenue_account_id, expense_account_id)
		 VALUES ($1, 'BrDU-'||$2, $3, $4)`, branchID, suffix, brRevAcct.ID, brExpAcct.ID); err != nil {
		t.Fatalf("insert branch: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO divisions (id, branch_id, name, expense_account_id)
		 VALUES ($1, $2, 'DivDU-'||$3, $4)`, divisionID, branchID, suffix, divExpAcct.ID); err != nil {
		t.Fatalf("insert division: %v", err)
	}
	// One lot: 100 pcs at 1 000/pcs.
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 100, 1, 100000, '2026-01-01')`,
		itemID, warehouseID); err != nil {
		t.Fatalf("insert lot: %v", err)
	}

	t.Cleanup(func() {
		bCtx := context.Background()
		pool.Exec(bCtx,
			`DELETE FROM invoice_items WHERE invoice_id IN (
			     SELECT id FROM invoices WHERE dispatch_id IN
			         (SELECT id FROM dispatches WHERE warehouse_id = $1))`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM invoices WHERE dispatch_id IN (SELECT id FROM dispatches WHERE warehouse_id = $1)`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM dispatch_items WHERE dispatch_id IN (SELECT id FROM dispatches WHERE warehouse_id = $1)`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM dispatches WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM stock_history WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM inventory WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM divisions WHERE id = $1`, divisionID)
		pool.Exec(bCtx, `DELETE FROM branches WHERE id = $1`, branchID)
		pool.Exec(bCtx, `DELETE FROM warehouses WHERE id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM items WHERE id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM accounts WHERE id IN ($1,$2,$3,$4)`,
			invAcct.ID, brExpAcct.ID, brRevAcct.ID, divExpAcct.ID)
	})

	return dispatchUnitFixtures{
		itemID: itemID, warehouseID: warehouseID, branchID: branchID, divisionID: divisionID,
		invAcctID: invAcct.ID.Bytes, expAcctID: divExpAcct.ID.Bytes,
	}
}

// createUnitDispatch dispatches one line and returns the new dispatch id.
func createUnitDispatch(t *testing.T, h *handler.DispatchesHandler, ctx context.Context, fix dispatchUnitFixtures, line map[string]any) string {
	t.Helper()
	rr := postJSON(t, h.Create, ctx, map[string]any{
		"branch_id":     fix.branchID.String(),
		"division_id":   fix.divisionID.String(),
		"warehouse_id":  fix.warehouseID.String(),
		"dispatch_date": "2026-05-01",
		"items":         []map[string]any{line},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create dispatch: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Dispatch struct {
			ID string `json:"id"`
		} `json:"dispatch"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if resp.Dispatch.ID == "" {
		t.Fatal("create dispatch: no id in response")
	}
	return resp.Dispatch.ID
}

// TestDispatchUpdate_QuantityDeltaInBaseUnits raises a line from 2 dus to 3 dus
// and checks only the 10-pcs difference moves: stock down by 10 more, expense up
// by its FIFO value, and a dispatch_edit history row recording the delta.
func TestDispatchUpdate_QuantityDeltaInBaseUnits(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupDispatchUnitFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewDispatchesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	dispatchID := createUnitDispatch(t, h, ctx, fix, map[string]any{
		"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0, "unit_name": "dus",
	})
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 80 {
		t.Fatalf("after create, stock = %.4f pcs, want 80 (100 − 2 dus × 10)", got)
	}
	if bal := getBalance(t, pool, fix.expAcctID); bal != 20000 {
		t.Fatalf("after create, expense balance = %d, want 20000", bal)
	}

	rr := callWithID(t, h.Update, ctx, http.MethodPut, dispatchID, map[string]any{
		"branch_id":     fix.branchID.String(),
		"division_id":   fix.divisionID.String(),
		"dispatch_date": "2026-05-01",
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "quantity": 3.0, "unit_index": 0, "unit_name": "dus"},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 70 {
		t.Errorf("after update, stock = %.4f pcs, want 70 (one more dus deducted)", got)
	}
	if bal := getBalance(t, pool, fix.expAcctID); bal != 30000 {
		t.Errorf("expense balance = %d, want 30000", bal)
	}

	// The line is stored as typed, with the factor alongside it.
	var qty, factor float64
	pool.QueryRow(context.Background(),
		`SELECT quantity::float8, conversion_factor::float8 FROM dispatch_items WHERE dispatch_id = $1`,
		dispatchID).Scan(&qty, &factor)
	if qty != 3 || factor != 10 {
		t.Errorf("dispatch_items = %.2f × %.2f, want 3 × 10", qty, factor)
	}

	// The correction is appended as its own movement, leaving the original intact.
	var editQty float64
	pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(quantity_change),0)::float8 FROM stock_history
		 WHERE source_id = $1 AND type = 'dispatch_edit'`, dispatchID).Scan(&editQty)
	if editQty != -10 {
		t.Errorf("dispatch_edit quantity_change total = %.4f, want -10", editQty)
	}
}

// TestDispatchUpdate_FactorOnlyChangeMovesStock covers the case a naive diff
// misses: the typed quantity is unchanged, but the operator corrected how many
// pcs a dus holds, so the base quantity — and therefore stock — must move.
func TestDispatchUpdate_FactorOnlyChangeMovesStock(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupDispatchUnitFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewDispatchesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	dispatchID := createUnitDispatch(t, h, ctx, fix, map[string]any{
		"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0, "unit_name": "dus",
	})
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 80 {
		t.Fatalf("after create, stock = %.4f pcs, want 80", got)
	}

	// Same 2 dus, but this supplier's dus held 15 pcs.
	rr := callWithID(t, h.Update, ctx, http.MethodPut, dispatchID, map[string]any{
		"branch_id":     fix.branchID.String(),
		"division_id":   fix.divisionID.String(),
		"dispatch_date": "2026-05-01",
		"items": []map[string]any{{
			"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0,
			"unit_name": "dus", "conversion_factor": 15.0,
		}},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 70 {
		t.Errorf("stock = %.4f pcs, want 70 (2×15 = 30 pcs dispatched, not 20)", got)
	}
	if bal := getBalance(t, pool, fix.expAcctID); bal != 30000 {
		t.Errorf("expense balance = %d, want 30000", bal)
	}
	var factor float64
	pool.QueryRow(context.Background(),
		`SELECT conversion_factor::float8 FROM dispatch_items WHERE dispatch_id = $1`, dispatchID).Scan(&factor)
	if factor != 15 {
		t.Errorf("stored conversion_factor = %.2f, want 15", factor)
	}
}

// TestDispatchUpdate_ReduceReturnsStock lowers a line and checks the returned
// quantity comes back in base units at the value it was booked at.
func TestDispatchUpdate_ReduceReturnsStock(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupDispatchUnitFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewDispatchesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	dispatchID := createUnitDispatch(t, h, ctx, fix, map[string]any{
		"item_id": fix.itemID.String(), "quantity": 3.0, "unit_index": 0, "unit_name": "dus",
	})
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 70 {
		t.Fatalf("after create, stock = %.4f pcs, want 70", got)
	}

	rr := callWithID(t, h.Update, ctx, http.MethodPut, dispatchID, map[string]any{
		"branch_id":     fix.branchID.String(),
		"division_id":   fix.divisionID.String(),
		"dispatch_date": "2026-05-01",
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "quantity": 1.0, "unit_index": 0, "unit_name": "dus"},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 90 {
		t.Errorf("stock = %.4f pcs, want 90 (2 dus returned)", got)
	}
	if bal := getBalance(t, pool, fix.expAcctID); bal != 10000 {
		t.Errorf("expense balance = %d, want 10000", bal)
	}
	// Returned lots must be denominated in the base unit, or FIFO will later
	// read them as the wrong unit.
	var badUnits int
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM inventory WHERE item_id = $1 AND warehouse_id = $2 AND unit_index <> 1`,
		fix.itemID, fix.warehouseID).Scan(&badUnits)
	if badUnits != 0 {
		t.Errorf("%d returned lot(s) are not in the base unit", badUnits)
	}
}

// TestDispatchUpdate_CancelledRejected confirms a cancelled dispatch is frozen:
// its stock and ledger were already reversed, so a later edit would double-count.
func TestDispatchUpdate_CancelledRejected(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupDispatchUnitFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewDispatchesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	dispatchID := createUnitDispatch(t, h, ctx, fix, map[string]any{
		"item_id": fix.itemID.String(), "quantity": 2.0, "unit_index": 0, "unit_name": "dus",
	})

	rr := callWithID(t, h.Delete, ctx, http.MethodDelete, dispatchID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	// Cancelling returns the full base quantity that left.
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 100 {
		t.Errorf("after cancel, stock = %.4f pcs, want 100", got)
	}
	if bal := getBalance(t, pool, fix.expAcctID); bal != 0 {
		t.Errorf("after cancel, expense balance = %d, want 0", bal)
	}

	rr = callWithID(t, h.Update, ctx, http.MethodPut, dispatchID, map[string]any{
		"branch_id":   fix.branchID.String(),
		"division_id": fix.divisionID.String(),
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "quantity": 1.0, "unit_index": 0, "unit_name": "dus"},
		},
	})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("editing a cancelled dispatch: expected 422, got %d: %s", rr.Code, rr.Body.String())
	}
}
