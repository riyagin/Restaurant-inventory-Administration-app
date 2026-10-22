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

// distributionFixtures: one stock item [dus, pcs] with 10 pcs per dus, a
// warehouse, and a branch that owns a division — each with its own account, so
// every destination's share can be read off its balance.
type distributionFixtures struct {
	itemID, warehouseID, branchID, divisionID uuid.UUID
	invAcctID, branchExpID, divExpID          uuid.UUID
}

func setupDistributionFixtures(t *testing.T, pool *pgxpool.Pool) distributionFixtures {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	suffix := uuid.New().String()[:8]

	mk := func(prefix, typ string) uuid.UUID {
		a, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: prefix + " " + suffix, AccountType: typ})
		if err != nil {
			t.Fatalf("create account %s: %v", prefix, err)
		}
		return a.ID.Bytes
	}
	f := distributionFixtures{
		itemID: uuid.New(), warehouseID: uuid.New(), branchID: uuid.New(), divisionID: uuid.New(),
		invAcctID:   mk("DistInv", "asset"),
		branchExpID: mk("DistBrExp", "expense"),
		divExpID:    mk("DistDivExp", "expense"),
	}

	exec := func(what, sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("insert %s: %v", what, err)
		}
	}
	exec("item", `INSERT INTO items (id, name, code, units, is_stock)
		VALUES ($1, 'DistItem-'||$2, 'DSI-'||$2, '[{"name":"dus","perPrev":null},{"name":"pcs","perPrev":10}]', true)`,
		f.itemID, suffix)
	exec("warehouse", `INSERT INTO warehouses (id, name, inventory_account_id) VALUES ($1, 'WHDist-'||$2, $3)`,
		f.warehouseID, suffix, f.invAcctID)
	exec("branch", `INSERT INTO branches (id, name, expense_account_id) VALUES ($1, 'BrDist-'||$2, $3)`,
		f.branchID, suffix, f.branchExpID)
	exec("division", `INSERT INTO divisions (id, branch_id, name, expense_account_id) VALUES ($1, $2, 'DivDist-'||$3, $4)`,
		f.divisionID, f.branchID, suffix, f.divExpID)

	t.Cleanup(func() {
		b := context.Background()
		pool.Exec(b, `DELETE FROM invoices WHERE id IN (SELECT invoice_id FROM invoice_items WHERE item_id = $1)`, f.itemID)
		pool.Exec(b, `DELETE FROM stock_history WHERE warehouse_id = $1`, f.warehouseID)
		pool.Exec(b, `DELETE FROM inventory_lot_consumptions WHERE lot_id IN (SELECT id FROM inventory WHERE warehouse_id = $1)`, f.warehouseID)
		pool.Exec(b, `DELETE FROM inventory WHERE warehouse_id = $1`, f.warehouseID)
		pool.Exec(b, `DELETE FROM warehouses WHERE id = $1`, f.warehouseID)
		pool.Exec(b, `DELETE FROM divisions WHERE id = $1`, f.divisionID)
		pool.Exec(b, `DELETE FROM branches WHERE id = $1`, f.branchID)
		pool.Exec(b, `DELETE FROM items WHERE id = $1`, f.itemID)
		pool.Exec(b, `DELETE FROM accounts WHERE id IN ($1, $2, $3)`, f.invAcctID, f.branchExpID, f.divExpID)
	})
	return f
}

// distributedBody is a one-line distributed invoice: 10 dus split 3 / 5 / 2
// across warehouse, division and bare branch, unless allocs overrides it.
func distributedBody(f distributionFixtures, price int64, allocs []map[string]any) map[string]any {
	if allocs == nil {
		allocs = []map[string]any{
			{"warehouse_id": f.warehouseID.String(), "quantity": 3.0},
			{"branch_id": f.branchID.String(), "division_id": f.divisionID.String(), "quantity": 5.0},
			{"branch_id": f.branchID.String(), "quantity": 2.0},
		}
	}
	return map[string]any{
		"invoice_type":   "purchase",
		"is_distributed": true,
		"date":           "2026-05-01",
		"items": []map[string]any{{
			"item_id": f.itemID.String(), "quantity": 10.0, "unit_index": 0, "price": price,
			"allocations": allocs,
		}},
	}
}

// TestDistributedInvoice_Lifecycle walks a faktur distribusi through create,
// a price-only edit, a re-split, and deletion, checking at each step that
// stock landed only in the warehouse share and that each destination's account
// carries exactly its share — then that deletion leaves nothing behind.
func TestDistributedInvoice_Lifecycle(t *testing.T) {
	pool := testutil.OpenDB(t)
	f := setupDistributionFixtures(t, pool)
	userID := createTestUser(t, pool)
	h := handler.NewInvoicesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	check := func(step string, stock float64, inv, div, br int64) {
		t.Helper()
		if got := baseStock(t, pool, f.itemID, f.warehouseID); got != stock {
			t.Errorf("%s: warehouse stock = %.2f pcs, want %.2f", step, got, stock)
		}
		if got := getBalance(t, pool, f.invAcctID); got != inv {
			t.Errorf("%s: inventory account = %d, want %d", step, got, inv)
		}
		if got := getBalance(t, pool, f.divExpID); got != div {
			t.Errorf("%s: division expense = %d, want %d", step, got, div)
		}
		if got := getBalance(t, pool, f.branchExpID); got != br {
			t.Errorf("%s: branch expense = %d, want %d", step, got, br)
		}
	}

	// 1. Create: 10 dus × 50 000 = 500 000, split 3 / 5 / 2.
	rr := postJSON(t, h.Create, ctx, distributedBody(f, 50000, nil))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, rr, &created)
	check("after create", 30, 150000, 250000, 100000)

	rr = callWithID(t, h.Get, ctx, http.MethodGet, created.ID, nil)
	var detail struct {
		IsDistributed bool             `json:"is_distributed"`
		Allocations   []map[string]any `json:"allocations"`
	}
	decodeJSON(t, rr, &detail)
	if !detail.IsDistributed || len(detail.Allocations) != 3 {
		t.Fatalf("get: is_distributed=%v, %d allocations; want true, 3", detail.IsDistributed, len(detail.Allocations))
	}

	// 2. Price-only edit to 60 000: same split, so lots are revalued in place.
	upd := distributedBody(f, 60000, nil)
	rr = callWithID(t, h.Update, ctx, http.MethodPut, created.ID, upd)
	if rr.Code != http.StatusOK {
		t.Fatalf("reprice: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	check("after reprice", 30, 180000, 300000, 120000)
	var lotValue int64
	pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(value),0)::bigint FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		f.itemID, f.warehouseID).Scan(&lotValue)
	if lotValue != 180000 {
		t.Errorf("after reprice: lot value = %d, want 180000 (must agree with the account)", lotValue)
	}

	// 3. Re-split 4 / 4 / 2: a full reverse and repost.
	upd = distributedBody(f, 60000, []map[string]any{
		{"warehouse_id": f.warehouseID.String(), "quantity": 4.0},
		{"branch_id": f.branchID.String(), "division_id": f.divisionID.String(), "quantity": 4.0},
		{"branch_id": f.branchID.String(), "quantity": 2.0},
	})
	rr = callWithID(t, h.Update, ctx, http.MethodPut, created.ID, upd)
	if rr.Code != http.StatusOK {
		t.Fatalf("re-split: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	check("after re-split", 40, 240000, 240000, 120000)

	// 4. An allocation that does not add up to the line is refused outright.
	bad := distributedBody(f, 60000, []map[string]any{
		{"warehouse_id": f.warehouseID.String(), "quantity": 4.0},
		{"branch_id": f.branchID.String(), "quantity": 5.0},
	})
	if rr = callWithID(t, h.Update, ctx, http.MethodPut, created.ID, bad); rr.Code != http.StatusBadRequest {
		t.Errorf("short allocation: expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	check("after refused edit", 40, 240000, 240000, 120000)

	// 5. Delete: every share is credited back and the stock comes out.
	if rr = callWithID(t, h.Delete, ctx, http.MethodDelete, created.ID, nil); rr.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
	check("after delete", 0, 0, 0, 0)
}

// TestDistributedInvoice_SharesSumToLine checks the rounding remainder lands on
// the last allocation: 3 × 10 001 split three ways must still total 30 003.
func TestDistributedInvoice_SharesSumToLine(t *testing.T) {
	pool := testutil.OpenDB(t)
	f := setupDistributionFixtures(t, pool)
	userID := createTestUser(t, pool)
	h := handler.NewInvoicesHandler(pool, db.New(pool))
	ctx := middleware.ContextWithClaims(context.Background(), testClaims(userID))

	body := distributedBody(f, 10001, nil)
	line := body["items"].([]map[string]any)[0]
	line["quantity"] = 3.0
	line["allocations"] = []map[string]any{
		{"warehouse_id": f.warehouseID.String(), "quantity": 1.0},
		{"branch_id": f.branchID.String(), "division_id": f.divisionID.String(), "quantity": 1.0},
		{"branch_id": f.branchID.String(), "quantity": 1.0},
	}
	rr := postJSON(t, h.Create, ctx, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	total := getBalance(t, pool, f.invAcctID) + getBalance(t, pool, f.divExpID) + getBalance(t, pool, f.branchExpID)
	if total != 30003 {
		t.Errorf("destination shares sum to %d, want 30003", total)
	}

	// A non-stock destination check: the item is stock, so a warehouse is fine,
	// but a division from another branch must be refused.
	other := uuid.New()
	pool.Exec(context.Background(), `INSERT INTO branches (id, name) VALUES ($1, 'BrDistOther-'||$2)`, other, other.String()[:8])
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM branches WHERE id = $1`, other) })
	bad := distributedBody(f, 10001, []map[string]any{
		{"branch_id": other.String(), "division_id": f.divisionID.String(), "quantity": 10.0},
	})
	if rr = postJSON(t, h.Create, ctx, bad); rr.Code != http.StatusBadRequest {
		t.Errorf("foreign division: expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}
