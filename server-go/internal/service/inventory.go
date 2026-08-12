package service

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// floatToNumeric converts a float64 to pgtype.Numeric.
// pgtype.Numeric.Scan only accepts string/[]byte in pgx/v5; passing a float64
// silently produces an invalid (NULL) Numeric. This helper avoids that pitfall.
func floatToNumeric(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(strconv.FormatFloat(f, 'f', 10, 64))
	return n
}

// StockHistoryParams holds all fields for an InsertStockHistory call.
type StockHistoryParams struct {
	ItemID         uuid.UUID
	WarehouseID    uuid.UUID
	QuantityChange float64
	UnitName       string
	Vendor         string
	Type           string
	Reference      string
	Date           time.Time
	Value          int64
	SourceID       uuid.UUID
	SourceType     string
}

// LotSource identifies what consumed a lot, so a lot's page can say "this went
// out on dispatch DSP-0012" rather than only "5 kg left".
//
// It is passed variadically to FIFODeduct on purpose. Fourteen call sites were
// already deducting stock before per-lot tracing existed; making the argument
// required would have meant editing every one of them in the same change that
// introduces the feature, and the paths that genuinely have no user (an invoice
// being edited, a cancelled purchase unwinding) would have had nothing sensible
// to pass. Omitted, the consumption is still recorded — the lot's arithmetic
// always closes — it simply reads as an adjustment.
type LotSource struct {
	Type      string
	ID        uuid.UUID
	Reference string
	Date      time.Time
}

// FIFODeduct deducts qty (in base unit) from the oldest inventory lots for an item in a warehouse.
// Returns the total IDR value deducted. Must be called with a transaction-scoped *db.Queries.
//
// A fully consumed lot is stamped `depleted_at` and left at quantity 0 rather
// than deleted. Deleting it destroyed the only record that the delivery ever
// existed, which is why per-lot history was impossible before;
// GetInventoryLotsForFIFO filters `quantity > 0`, so the surviving rows are
// invisible to FIFO itself.
func FIFODeduct(ctx context.Context, qtx *db.Queries, itemID, warehouseID uuid.UUID, qty float64, src ...LotSource) (int64, error) {
	lots, err := qtx.GetInventoryLotsForFIFO(ctx, &db.GetInventoryLotsForFIFOParams{
		ItemID:      pgtype.UUID{Bytes: itemID, Valid: true},
		WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
	})
	if err != nil {
		return 0, err
	}

	source := LotSource{}
	if len(src) > 0 {
		source = src[0]
	}
	if source.Date.IsZero() {
		source.Date = time.Now()
	}

	var valueDeducted int64
	remaining := qty

	// Consumption rows are collected and written only once the whole deduction
	// is known to succeed. A run that ends in "stok tidak mencukupi" returns an
	// error and the caller rolls back, but leaving half-written usage rows in the
	// transaction would still be wrong if a caller ever chose to continue.
	type taken struct {
		lotID pgtype.UUID
		qty   float64
		value int64
	}
	var consumed []taken

	const epsilon = 0.001
	for _, lot := range lots {
		if remaining <= 0 {
			break
		}
		lotQty := pgNumericToFloat(lot.Quantity)
		newQty := lotQty - remaining
		if newQty < epsilon {
			// Consume the entire lot (handles exact-match and near-zero remainder)
			valueDeducted += lot.Value
			tookQty := lotQty
			remaining -= lotQty
			if remaining < 0 {
				remaining = 0
			}
			// Stamped and zeroed, not deleted — this row is the only surviving
			// evidence that the delivery happened.
			if err := qtx.DepleteInventoryLot(ctx, lot.ID); err != nil {
				return 0, fmt.Errorf("deplete lot: %w", err)
			}
			consumed = append(consumed, taken{lotID: lot.ID, qty: tookQty, value: lot.Value})
		} else {
			proportion := remaining / lotQty
			deductedValue := int64(float64(lot.Value) * proportion)
			valueDeducted += deductedValue
			tookQty := remaining
			remaining = 0
			if err := qtx.UpdateInventoryLotQuantity(ctx, &db.UpdateInventoryLotQuantityParams{
				ID:       lot.ID,
				Quantity: floatToNumeric(newQty),
				Value:    lot.Value - deductedValue,
			}); err != nil {
				return 0, fmt.Errorf("update lot: %w", err)
			}
			consumed = append(consumed, taken{lotID: lot.ID, qty: tookQty, value: deductedValue})
		}
	}

	if remaining > 0.001 {
		return 0, fmt.Errorf("stok tidak mencukupi: kurang %.4f unit", remaining)
	}

	for _, c := range consumed {
		if err := qtx.InsertLotConsumption(ctx, &db.InsertLotConsumptionParams{
			LotID:       c.lotID,
			ItemID:      pgtype.UUID{Bytes: itemID, Valid: true},
			WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
			Quantity:    floatToNumeric(c.qty),
			Value:       c.value,
			SourceType:  pgtype.Text{String: source.Type, Valid: source.Type != ""},
			SourceID:    pgtype.UUID{Bytes: source.ID, Valid: source.ID != uuid.Nil},
			Reference:   source.Reference,
			Date:        pgtype.Date{Time: source.Date, Valid: true},
		}); err != nil {
			return 0, fmt.Errorf("record lot consumption: %w", err)
		}
	}

	return valueDeducted, nil
}

// FIFOAdd creates a new inventory lot. Must be called with a transaction-scoped *db.Queries.
func FIFOAdd(ctx context.Context, qtx *db.Queries, itemID, warehouseID uuid.UUID, qty float64, unitIndex int32, value int64, date time.Time) error {
	_, err := qtx.CreateInventoryLot(ctx, &db.CreateInventoryLotParams{
		ItemID:      pgtype.UUID{Bytes: itemID, Valid: true},
		WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
		Quantity:    floatToNumeric(qty),
		UnitIndex:   unitIndex,
		Value:       value,
		Date:        pgtype.Date{Time: date, Valid: true},
	})
	return err
}

// InsertStockHistory writes one stock_history row inside a transaction.
func InsertStockHistory(ctx context.Context, qtx *db.Queries, p StockHistoryParams) error {
	_, err := qtx.InsertStockHistory(ctx, &db.InsertStockHistoryParams{
		ItemID:         pgtype.UUID{Bytes: p.ItemID, Valid: true},
		WarehouseID:    pgtype.UUID{Bytes: p.WarehouseID, Valid: true},
		QuantityChange: floatToNumeric(p.QuantityChange),
		UnitName:       p.UnitName,
		Vendor:         pgtype.Text{String: p.Vendor, Valid: p.Vendor != ""},
		Type:           p.Type,
		Reference:      pgtype.Text{String: p.Reference, Valid: p.Reference != ""},
		Date:           pgtype.Date{Time: p.Date, Valid: true},
		Value:          pgtype.Int8{Int64: p.Value, Valid: true},
		SourceID:       pgtype.UUID{Bytes: p.SourceID, Valid: p.SourceID != uuid.Nil},
		SourceType:     pgtype.Text{String: p.SourceType, Valid: p.SourceType != ""},
	})
	return err
}

// pgNumericToFloat converts a pgtype.Numeric to float64.
func pgNumericToFloat(n pgtype.Numeric) float64 {
	if !n.Valid || n.NaN || n.Int == nil {
		return 0
	}
	f, _ := new(big.Float).SetInt(n.Int).Float64()
	if n.Exp > 0 {
		for i := int32(0); i < n.Exp; i++ {
			f *= 10
		}
	} else if n.Exp < 0 {
		for i := n.Exp; i < 0; i++ {
			f /= 10
		}
	}
	return f
}
