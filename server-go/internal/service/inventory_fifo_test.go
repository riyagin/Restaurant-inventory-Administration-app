package service_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/service"
	"inventory-app/server-go/internal/testutil"
)

// TestFIFODeduct_PartialSingleLot confirms that deducting less than a lot's
// quantity reduces quantity and value proportionally without deleting the lot.
func TestFIFODeduct_PartialSingleLot(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, tx, qtx := testutil.OpenTx(t, pool)

	itemID := uuid.New()
	warehouseID := uuid.New()

	if _, err := tx.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'FIFO Partial', 'FPT-001', '[{"name":"kg","ratio":1}]', true)`,
		itemID); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO warehouses (id, name) VALUES ($1, 'WH FPT')`,
		warehouseID); err != nil {
		t.Fatalf("insert warehouse: %v", err)
	}

	// One lot: 10 kg, 100 000 IDR total.
	var lotID pgtype.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 10, 0, 100000, '2026-01-01') RETURNING id`,
		itemID, warehouseID).Scan(&lotID); err != nil {
		t.Fatalf("insert lot: %v", err)
	}

	// Deduct 4 kg — proportional value = (4/10) * 100 000 = 40 000.
	got, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, 4)
	if err != nil {
		t.Fatalf("FIFODeduct: %v", err)
	}
	if got != 40000 {
		t.Errorf("valueDeducted = %d, want 40000", got)
	}

	// Lot must still exist with 6 kg and 60 000 IDR remaining.
	var qty float64
	var val int64
	if err := tx.QueryRow(ctx,
		`SELECT quantity::float8, value FROM inventory WHERE id = $1`, lotID).
		Scan(&qty, &val); err != nil {
		t.Fatalf("scan remaining lot: %v", err)
	}
	if qty < 5.999 || qty > 6.001 {
		t.Errorf("remaining qty = %.4f, want ~6", qty)
	}
	if val != 60000 {
		t.Errorf("remaining value = %d, want 60000", val)
	}
}

// TestFIFODeduct_ExactLotDeleted confirms that consuming an entire lot removes
// the row from inventory.
func TestFIFODeduct_ExactLotDeleted(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, tx, qtx := testutil.OpenTx(t, pool)

	itemID := uuid.New()
	warehouseID := uuid.New()

	_, _ = tx.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'FIFO Exact', 'FEX-001', '[{"name":"pcs","ratio":1}]', true)`,
		itemID)
	_, _ = tx.Exec(ctx,
		`INSERT INTO warehouses (id, name) VALUES ($1, 'WH FEX')`,
		warehouseID)

	var lotID pgtype.UUID
	_ = tx.QueryRow(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 5, 0, 50000, '2026-02-01') RETURNING id`,
		itemID, warehouseID).Scan(&lotID)

	got, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, 5)
	if err != nil {
		t.Fatalf("FIFODeduct: %v", err)
	}
	if got != 50000 {
		t.Errorf("valueDeducted = %d, want 50000", got)
	}

	var count int
	_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM inventory WHERE id = $1`, lotID).Scan(&count)
	if count != 0 {
		t.Errorf("lot still present after full deduction, want deleted")
	}
}

// TestFIFODeduct_MultiLotOldestFirst verifies that the oldest lot (by date)
// is consumed before newer lots.
func TestFIFODeduct_MultiLotOldestFirst(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, tx, qtx := testutil.OpenTx(t, pool)

	itemID := uuid.New()
	warehouseID := uuid.New()

	_, _ = tx.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'FIFO Multi', 'FMU-001', '[{"name":"kg","ratio":1}]', true)`,
		itemID)
	_, _ = tx.Exec(ctx,
		`INSERT INTO warehouses (id, name) VALUES ($1, 'WH FMU')`,
		warehouseID)

	// Lot A — older: Jan 2026, 5 kg, 50 000 IDR (10 000/kg).
	var lotA pgtype.UUID
	_ = tx.QueryRow(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 5, 0, 50000, '2026-01-01') RETURNING id`,
		itemID, warehouseID).Scan(&lotA)

	// Lot B — newer: Mar 2026, 5 kg, 75 000 IDR (15 000/kg).
	var lotB pgtype.UUID
	_ = tx.QueryRow(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 5, 0, 75000, '2026-03-01') RETURNING id`,
		itemID, warehouseID).Scan(&lotB)

	// Deduct 7 kg: all 5 of lot A + 2 from lot B.
	// Expected value: 50 000 + (2/5)*75 000 = 50 000 + 30 000 = 80 000.
	got, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, 7)
	if err != nil {
		t.Fatalf("FIFODeduct: %v", err)
	}
	if got != 80000 {
		t.Errorf("valueDeducted = %d, want 80000", got)
	}

	// Lot A must be deleted.
	var cntA int
	_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM inventory WHERE id = $1`, lotA).Scan(&cntA)
	if cntA != 0 {
		t.Error("lot A must be deleted after full consumption")
	}

	// Lot B must have 3 kg remaining.
	var qtyB float64
	_ = tx.QueryRow(ctx, `SELECT quantity::float8 FROM inventory WHERE id = $1`, lotB).Scan(&qtyB)
	if qtyB < 2.999 || qtyB > 3.001 {
		t.Errorf("lot B remaining = %.4f, want ~3", qtyB)
	}
}

// TestFIFODeduct_SpansThreeLots ensures correct chaining through three lots.
func TestFIFODeduct_SpansThreeLots(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, tx, qtx := testutil.OpenTx(t, pool)

	itemID := uuid.New()
	warehouseID := uuid.New()

	_, _ = tx.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'FIFO Three', 'FTH-001', '[{"name":"L","ratio":1}]', true)`,
		itemID)
	_, _ = tx.Exec(ctx,
		`INSERT INTO warehouses (id, name) VALUES ($1, 'WH FTH')`,
		warehouseID)

	type lot struct {
		qty int
		val int64
		dt  string
	}
	lots := []lot{
		{2, 20000, "2026-01-01"},
		{3, 36000, "2026-02-01"},
		{4, 56000, "2026-03-01"},
	}
	for i, l := range lots {
		if _, err := tx.Exec(ctx,
			`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
			 VALUES (gen_random_uuid(), $1, $2, $3, 0, $4, $5)`,
			itemID, warehouseID, l.qty, l.val, l.dt); err != nil {
			t.Fatalf("insert lot %d: %v", i, err)
		}
	}

	// Deduct 7: lot1 fully (2) + lot2 fully (3) + 2 from lot3.
	// 20000 + 36000 + (2/4)*56000 = 20000 + 36000 + 28000 = 84000.
	got, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, 7)
	if err != nil {
		t.Fatalf("FIFODeduct: %v", err)
	}
	if got != 84000 {
		t.Errorf("valueDeducted = %d, want 84000", got)
	}

	var remaining int
	_ = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		itemID, warehouseID).Scan(&remaining)
	if remaining != 1 {
		t.Errorf("expected 1 surviving lot, got %d", remaining)
	}
}

// TestFIFODeduct_InsufficientStock confirms an error is returned when total
// available quantity is less than the requested deduction.
func TestFIFODeduct_InsufficientStock(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, tx, qtx := testutil.OpenTx(t, pool)

	itemID := uuid.New()
	warehouseID := uuid.New()

	_, _ = tx.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'FIFO Low', 'FLW-001', '[{"name":"kg","ratio":1}]', true)`,
		itemID)
	_, _ = tx.Exec(ctx,
		`INSERT INTO warehouses (id, name) VALUES ($1, 'WH FLW')`,
		warehouseID)
	_, _ = tx.Exec(ctx,
		`INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
		 VALUES (gen_random_uuid(), $1, $2, 3, 0, 30000, '2026-01-01')`,
		itemID, warehouseID)

	_, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, 10)
	if err == nil {
		t.Fatal("expected insufficient stock error, got nil")
	}
}

// TestFIFOAdd_CreatesLot verifies that FIFOAdd persists a lot with the
// correct quantity and value.
func TestFIFOAdd_CreatesLot(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, tx, qtx := testutil.OpenTx(t, pool)

	itemID := uuid.New()
	warehouseID := uuid.New()

	_, _ = tx.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'FIFO Add', 'FAD-001', '[{"name":"kg","ratio":1}]', true)`,
		itemID)
	_, _ = tx.Exec(ctx,
		`INSERT INTO warehouses (id, name) VALUES ($1, 'WH FAD')`,
		warehouseID)

	lotDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, 12.5, 0, 125000, lotDate); err != nil {
		t.Fatalf("FIFOAdd: %v", err)
	}

	var qty float64
	var val int64
	if err := tx.QueryRow(ctx,
		`SELECT quantity::float8, value FROM inventory
		 WHERE item_id = $1 AND warehouse_id = $2`,
		itemID, warehouseID).Scan(&qty, &val); err != nil {
		t.Fatalf("query new lot: %v", err)
	}
	if qty < 12.499 || qty > 12.501 {
		t.Errorf("lot qty = %.4f, want 12.5", qty)
	}
	if val != 125000 {
		t.Errorf("lot value = %d, want 125000", val)
	}
}

// TestFIFOAddThenDeduct exercises the round-trip: add two lots at different
// prices then deduct across both to confirm FIFO ordering holds.
func TestFIFOAddThenDeduct(t *testing.T) {
	pool := testutil.OpenDB(t)
	ctx, tx, qtx := testutil.OpenTx(t, pool)

	itemID := uuid.New()
	warehouseID := uuid.New()

	_, _ = tx.Exec(ctx,
		`INSERT INTO items (id, name, code, units, is_stock)
		 VALUES ($1, 'FIFO RoundTrip', 'FRT-001', '[{"name":"kg","ratio":1}]', true)`,
		itemID)
	_, _ = tx.Exec(ctx,
		`INSERT INTO warehouses (id, name) VALUES ($1, 'WH FRT')`,
		warehouseID)

	// Add lot 1: 10 kg, value 80 000 (8 000/kg) on Jan 1.
	if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, 10, 0, 80000,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("FIFOAdd lot1: %v", err)
	}
	// Add lot 2: 10 kg, value 100 000 (10 000/kg) on Feb 1.
	if err := service.FIFOAdd(ctx, qtx, itemID, warehouseID, 10, 0, 100000,
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("FIFOAdd lot2: %v", err)
	}

	// Deduct 15: all 10 from lot1 + 5 from lot2.
	// 80000 + (5/10)*100000 = 80000 + 50000 = 130000.
	got, err := service.FIFODeduct(ctx, qtx, itemID, warehouseID, 15)
	if err != nil {
		t.Fatalf("FIFODeduct: %v", err)
	}
	if got != 130000 {
		t.Errorf("valueDeducted = %d, want 130000", got)
	}

	var remaining int
	_ = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM inventory WHERE item_id = $1 AND warehouse_id = $2`,
		itemID, warehouseID).Scan(&remaining)
	if remaining != 1 {
		t.Errorf("expected 1 remaining lot, got %d", remaining)
	}
}
