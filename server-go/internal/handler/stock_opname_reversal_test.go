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

// opnameReversalFixtures backs the "count to 0 by mistake, then put it back"
// tests. One lot of 10 units worth 100 000 makes the unit cost a round 10 000.
type opnameReversalFixtures struct {
	itemID      uuid.UUID
	warehouseID uuid.UUID
	invAcctID   uuid.UUID
	varAcctID   uuid.UUID
}

func setupOpnameReversalFixtures(t *testing.T, pool *pgxpool.Pool) opnameReversalFixtures {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	suffix := uuid.New().String()[:8]

	invAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: "SOInv " + suffix, AccountType: "asset"})
	if err != nil {
		t.Fatalf("create inventory account: %v", err)
	}

	// The variance account is a singleton the handler looks up by name, so reuse
	// whichever one the database already has rather than making a second.
	varianceID, err := q.GetInventoryVarianceAccountID(ctx)
	if err != nil {
		created, cerr := q.CreateAccount(ctx, &db.CreateAccountParams{
			Name: "Selisih Persediaan", AccountType: "expense",
		})
		if cerr != nil {
			t.Fatalf("create variance account: %v", cerr)
		}
		varianceID = created.ID
		t.Cleanup(func() {
			pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, created.ID)
		})
	}

	itemID, warehouseID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'SOItem-'||$2, 'SOI-'||$2, '[{"name":"kg","perPrev":null}]', true)`,
		itemID, suffix); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (id, name, inventory_account_id) VALUES ($1, 'WHSO-'||$2, $3)`,
		warehouseID, suffix, invAcct.ID); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 10, 0, 100000, '2026-01-01')`,
		itemID, warehouseID); err != nil {
		t.Fatalf("insert lot: %v", err)
	}

	t.Cleanup(func() {
		bCtx := context.Background()
		pool.Exec(bCtx, `DELETE FROM stock_opname_items WHERE item_id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM stock_opname WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM stock_history WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM inventory WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM warehouses WHERE id = $1`, warehouseID)
		pool.Exec(bCtx, `DELETE FROM items WHERE id = $1`, itemID)
		pool.Exec(bCtx, `DELETE FROM accounts WHERE id = $1`, invAcct.ID)
	})

	return opnameReversalFixtures{
		itemID: itemID, warehouseID: warehouseID,
		invAcctID: invAcct.ID.Bytes, varAcctID: uuid.UUID(varianceID.Bytes),
	}
}

// countToZero submits an opname that counts the item down to zero and returns
// the new opname id.
func countToZero(t *testing.T, h *handler.StockOpnameHandler, ctx context.Context, fix opnameReversalFixtures) string {
	t.Helper()
	rr := postJSON(t, h.Create, ctx, map[string]any{
		"warehouse_id": fix.warehouseID.String(),
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "unit_index": 0, "unit_name": "kg", "actual_quantity": 0.0},
		},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create opname: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, rr, &created)
	if created.ID == "" {
		t.Fatal("create opname: no id in response")
	}
	return created.ID
}

// netWaste sums an opname's recorded waste for an item across the original row
// and every correction — the figure the reports and the next reversal both read.
func netWaste(t *testing.T, pool *pgxpool.Pool, opnameID string, itemID uuid.UUID) int64 {
	t.Helper()
	var v int64
	pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(waste_value),0) FROM stock_opname_items WHERE opname_id = $1 AND item_id = $2`,
		opnameID, itemID).Scan(&v)
	return v
}

// TestOpnameUpdate_RestoresZeroedItemAtWrittenOffCost is the case this feature
// exists for: an item is counted to 0 by mistake, which deletes its lots, and
// the correction puts both the stock and its original value back — leaving no
// residue in the variance account.
func TestOpnameUpdate_RestoresZeroedItemAtWrittenOffCost(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupOpnameReversalFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewStockOpnameHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	invBefore := getBalance(t, pool, fix.invAcctID)
	varBefore := getBalance(t, pool, fix.varAcctID)

	opnameID := countToZero(t, h, ctx, fix)

	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 0 {
		t.Fatalf("after opname, stock = %.4f, want 0", got)
	}
	if got := getBalance(t, pool, fix.invAcctID); got != invBefore-100000 {
		t.Fatalf("after opname, inventory balance moved by %d, want -100000", got-invBefore)
	}
	if got := netWaste(t, pool, opnameID, fix.itemID); got != 100000 {
		t.Fatalf("recorded waste = %d, want 100000", got)
	}

	// Put all 10 kg back.
	rr := callWithID(t, h.Update, ctx, http.MethodPut, opnameID, map[string]any{
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "unit_index": 0, "unit_name": "kg", "actual_quantity": 10.0},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("correction: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 10 {
		t.Errorf("after correction, stock = %.4f, want 10", got)
	}
	// The write-off and its reversal must use the same cost, so both accounts
	// land exactly where they started.
	if got := getBalance(t, pool, fix.invAcctID); got != invBefore {
		t.Errorf("inventory balance = %d, want %d (back to pre-opname)", got, invBefore)
	}
	if got := getBalance(t, pool, fix.varAcctID); got != varBefore {
		t.Errorf("variance balance = %d, want %d (no residue)", got, varBefore)
	}
	// The restoration is booked as negative waste, so the opname nets to zero
	// loss rather than still claiming 100 000 was lost.
	if got := netWaste(t, pool, opnameID, fix.itemID); got != 0 {
		t.Errorf("net waste after correction = %d, want 0", got)
	}
	// The original row is preserved; the correction is appended.
	var rowCount, correctionCount int
	pool.QueryRow(context.Background(),
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE is_correction) FROM stock_opname_items WHERE opname_id = $1`,
		opnameID).Scan(&rowCount, &correctionCount)
	if rowCount != 2 || correctionCount != 1 {
		t.Errorf("rows = %d (%d corrections), want 2 (1)", rowCount, correctionCount)
	}
}

// TestOpnameUpdate_PartialRestoreIsProRated restores only part of what was
// written off, so exactly that share of the value comes back.
func TestOpnameUpdate_PartialRestoreIsProRated(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupOpnameReversalFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewStockOpnameHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	invBefore := getBalance(t, pool, fix.invAcctID)
	opnameID := countToZero(t, h, ctx, fix)

	// Only 4 of the 10 kg actually existed.
	rr := callWithID(t, h.Update, ctx, http.MethodPut, opnameID, map[string]any{
		"items": []map[string]any{
			{"item_id": fix.itemID.String(), "unit_index": 0, "unit_name": "kg", "actual_quantity": 4.0},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("correction: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 4 {
		t.Errorf("stock = %.4f, want 4", got)
	}
	// 4 of 10 kg back at 10 000/kg → inventory down only 60 000 overall.
	if got := getBalance(t, pool, fix.invAcctID); got != invBefore-60000 {
		t.Errorf("inventory balance moved by %d, want -60000", got-invBefore)
	}
	if got := netWaste(t, pool, opnameID, fix.itemID); got != 60000 {
		t.Errorf("net waste = %d, want 60000 (6 kg still written off)", got)
	}
}

// TestOpnameUpdate_RepeatedRestoreCannotDoubleReverse guards the arithmetic:
// once the write-off has been fully reversed there is nothing left to reverse,
// so a further increase is a plain surplus and must not hand back the same
// value a second time.
func TestOpnameUpdate_RepeatedRestoreCannotDoubleReverse(t *testing.T) {
	pool := testutil.OpenDB(t)
	fix := setupOpnameReversalFixtures(t, pool)
	userID := createTestUser(t, pool)

	h := handler.NewStockOpnameHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	invBefore := getBalance(t, pool, fix.invAcctID)
	opnameID := countToZero(t, h, ctx, fix)

	restore := func(qty float64) {
		t.Helper()
		rr := callWithID(t, h.Update, ctx, http.MethodPut, opnameID, map[string]any{
			"items": []map[string]any{
				{"item_id": fix.itemID.String(), "unit_index": 0, "unit_name": "kg", "actual_quantity": qty},
			},
		})
		if rr.Code != http.StatusOK {
			t.Fatalf("correction to %.0f: expected 200, got %d: %s", qty, rr.Code, rr.Body.String())
		}
	}

	restore(6)  // reverses 60 000 of the 100 000 written off
	restore(10) // reverses the remaining 40 000

	if got := netWaste(t, pool, opnameID, fix.itemID); got != 0 {
		t.Fatalf("net waste = %d, want 0 after full restore", got)
	}
	if got := getBalance(t, pool, fix.invAcctID); got != invBefore {
		t.Fatalf("inventory balance = %d, want %d after full restore", got, invBefore)
	}

	// Nothing is written off any more. Going above the original count is a
	// genuine surplus: the item has no purchase history here, so it is valued at
	// 0 rather than re-using the already-reversed cost.
	restore(12)
	if got := baseStock(t, pool, fix.itemID, fix.warehouseID); got != 12 {
		t.Errorf("stock = %.4f, want 12", got)
	}
	if got := getBalance(t, pool, fix.invAcctID); got != invBefore {
		t.Errorf("inventory balance = %d, want %d — the reversal must not pay out twice", got, invBefore)
	}
}
