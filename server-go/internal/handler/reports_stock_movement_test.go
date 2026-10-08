package handler_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	"inventory-app/server-go/internal/testutil"
)

type stockMovementCell struct {
	OpnameQty     float64 `json:"opname_quantity"`
	OpnameValue   int64   `json:"opname_value"`
	DispatchQty   float64 `json:"dispatch_quantity"`
	DispatchValue int64   `json:"dispatch_value"`
}

type stockMovementResp struct {
	Mode    string `json:"mode"`
	Summary struct {
		OpnameGain int64 `json:"opname_gain_value"`
		OpnameLoss int64 `json:"opname_loss_value"`
		Dispatch   int64 `json:"dispatch_value"`
		Net        int64 `json:"net_value"`
		ItemCount  int   `json:"item_count"`
		DayCount   int   `json:"day_count"`
		OpnameDays int   `json:"opname_days"`
	} `json:"summary"`
	Days []struct {
		Date            string `json:"date"`
		OpnameGainValue int64  `json:"opname_gain_value"`
		OpnameLossValue int64  `json:"opname_loss_value"`
		DispatchValue   int64  `json:"dispatch_value"`
	} `json:"days"`
	Items []struct {
		ItemID     string                       `json:"item_id"`
		UnitName   string                       `json:"unit_name"`
		Days       map[string]stockMovementCell `json:"days"`
		TotalQty   float64                      `json:"total_quantity"`
		TotalValue int64                        `json:"total_value"`
		ActiveDays int                          `json:"active_days"`
	} `json:"items"`
	Opnames []struct {
		ID   string `json:"id"`
		Date string `json:"date"`
	} `json:"opnames"`
}

// TestStockMovement_Modes checks that the opname view sees only opname rows,
// the combined view adds dispatch rows, legacy type labels are picked up by
// source_type, reversals net out, and nothing else (purchases) leaks in.
func TestStockMovement_Modes(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx := context.Background()
	q := db.New(pool)
	suffix := uuid.New().String()[:8]

	invAcct, err := q.CreateAccount(ctx, &db.CreateAccountParams{Name: "Inv Gerak " + suffix, AccountType: "asset"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	itemID, warehouseID, opnameID := uuid.New(), uuid.New(), uuid.New()
	// Two units, largest first: the report must label quantities with the base
	// unit (the last one), not the first.
	if _, err := pool.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'GerakItem '||$2, 'GRK-'||$2, '[{"name":"dus","perPrev":null},{"name":"pcs","perPrev":12}]', true)`,
		itemID, suffix); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (id, name, inventory_account_id) VALUES ($1, 'WHGerak '||$2, $3)`,
		warehouseID, suffix, invAcct.ID); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO stock_opname (id, warehouse_id, performed_at) VALUES ($1, $2, '2026-03-02')`,
		opnameID, warehouseID); err != nil {
		t.Fatalf("insert opname: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM stock_history WHERE warehouse_id = $1`, warehouseID)
		pool.Exec(c, `DELETE FROM stock_opname WHERE id = $1`, opnameID)
		pool.Exec(c, `DELETE FROM warehouses WHERE id = $1`, warehouseID)
		pool.Exec(c, `DELETE FROM items WHERE id = $1`, itemID)
		pool.Exec(c, `DELETE FROM accounts WHERE id = $1`, invAcct.ID)
	})

	type row struct {
		date, typ, source string
		sourceID          *uuid.UUID
		qty               float64
		value             int64
	}
	for _, r := range []row{
		// 2026-03-01: legacy dispatch label + a partial cancellation → net −4
		{"2026-03-01", "pemakaian", "dispatch", nil, -5, -5000},
		{"2026-03-01", "dispatch_cancel", "dispatch", nil, 1, 1000},
		// 2026-03-02: opname loss −3, corrected the same day by +1 → net −2
		{"2026-03-02", "opname", "opname", &opnameID, -3, -3000},
		{"2026-03-02", "opname", "opname", &opnameID, 1, 1000},
		// 2026-03-03: legacy "SO" label, a surplus
		{"2026-03-03", "SO", "opname", &opnameID, 2, 2000},
		// a purchase must appear in neither mode
		{"2026-03-02", "invoice", "invoice", nil, 50, 50000},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, type, date, source_id, source_type, value)
			 VALUES ($1, $2, $3, '', $4, $5, $6, $7, $8)`,
			itemID, warehouseID, r.qty, r.typ, r.date, r.sourceID, r.source, r.value); err != nil {
			t.Fatalf("insert history: %v", err)
		}
	}

	h := handler.NewAnalyticsHandler(pool, q)
	call := func(mode string) stockMovementResp {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/reports/stock-movement?date_from=2026-03-01&date_to=2026-03-04&warehouse_id="+warehouseID.String()+"&mode="+mode, nil)
		rr := httptest.NewRecorder()
		h.StockMovement(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("mode %q: expected 200, got %d: %s", mode, rr.Code, rr.Body.String())
		}
		var out stockMovementResp
		decodeJSON(t, rr, &out)
		return out
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

	// ── opname only ──
	op := call("opname")
	if op.Mode != "opname" || len(op.Items) != 1 {
		t.Fatalf("opname: mode=%q items=%d, want opname/1", op.Mode, len(op.Items))
	}
	it := op.Items[0]
	if it.UnitName != "pcs" {
		t.Errorf("unit_name = %q, want base unit pcs", it.UnitName)
	}
	if _, ok := it.Days["2026-03-01"]; ok {
		t.Errorf("opname mode includes the dispatch day")
	}
	if c := it.Days["2026-03-02"]; !near(c.OpnameQty, -2) || c.OpnameValue != -2000 {
		t.Errorf("03-02 opname = %v / %d, want -2 / -2000", c.OpnameQty, c.OpnameValue)
	}
	if c := it.Days["2026-03-03"]; !near(c.OpnameQty, 2) {
		t.Errorf("legacy SO row not picked up: %v", c.OpnameQty)
	}
	if !near(it.TotalQty, 0) || it.TotalValue != 0 || it.ActiveDays != 2 {
		t.Errorf("opname totals = %v / %d / %d days, want 0 / 0 / 2", it.TotalQty, it.TotalValue, it.ActiveDays)
	}
	if op.Summary.OpnameLoss != -2000 || op.Summary.OpnameGain != 2000 || op.Summary.Dispatch != 0 {
		t.Errorf("opname summary = %+v", op.Summary)
	}
	if op.Summary.DayCount != 4 || len(op.Days) != 4 || op.Summary.OpnameDays != 2 {
		t.Errorf("days: count=%d len=%d opname_days=%d, want 4/4/2", op.Summary.DayCount, len(op.Days), op.Summary.OpnameDays)
	}
	if len(op.Opnames) != 2 || op.Opnames[0].ID != opnameID.String() {
		t.Errorf("opnames = %+v, want the session on both its days", op.Opnames)
	}

	// ── opname + dispatch ──
	all := call("opname_dispatch")
	it = all.Items[0]
	if c := it.Days["2026-03-01"]; !near(c.DispatchQty, -4) || c.DispatchValue != -4000 || c.OpnameQty != 0 {
		t.Errorf("03-01 = %+v, want dispatch -4 / -4000", c)
	}
	if !near(it.TotalQty, -4) || it.TotalValue != -4000 || it.ActiveDays != 3 {
		t.Errorf("combined totals = %v / %d / %d days, want -4 / -4000 / 3", it.TotalQty, it.TotalValue, it.ActiveDays)
	}
	if all.Summary.Dispatch != -4000 || all.Summary.Net != -4000 {
		t.Errorf("combined summary = %+v", all.Summary)
	}

	// ── validation ──
	for _, qs := range []string{
		"date_from=2026-03-01&date_to=2026-03-04&mode=bogus",
		"date_from=2026-01-01&date_to=2026-06-30",
		"date_from=2026-03-01&date_to=2026-03-04&warehouse_id=nope",
	} {
		rr := httptest.NewRecorder()
		h.StockMovement(rr, httptest.NewRequest(http.MethodGet, "/api/reports/stock-movement?"+qs, nil))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", qs, rr.Code)
		}
	}
}
