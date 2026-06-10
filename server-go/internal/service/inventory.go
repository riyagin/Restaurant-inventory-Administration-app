package service

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

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

// FIFODeduct deducts qty (in base unit) from the oldest inventory lots for an item in a warehouse.
// Returns the total IDR value deducted. Must be called with a transaction-scoped *db.Queries.
func FIFODeduct(ctx context.Context, qtx *db.Queries, itemID, warehouseID uuid.UUID, qty float64) (int64, error) {
	lots, err := qtx.GetInventoryLotsForFIFO(ctx, &db.GetInventoryLotsForFIFOParams{
		ItemID:      pgtype.UUID{Bytes: itemID, Valid: true},
		WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
	})
	if err != nil {
		return 0, err
	}

	var valueDeducted int64
	remaining := qty

	for _, lot := range lots {
		if remaining <= 0 {
			break
		}
		lotQty := pgNumericToFloat(lot.Quantity)
		if lotQty <= remaining {
			valueDeducted += lot.Value
			remaining -= lotQty
			if err := qtx.DeleteInventoryLot(ctx, lot.ID); err != nil {
				return 0, fmt.Errorf("delete lot: %w", err)
			}
		} else {
			proportion := remaining / lotQty
			deductedValue := int64(float64(lot.Value) * proportion)
			valueDeducted += deductedValue
			newQty := lotQty - remaining
			remaining = 0
			var newQtyNumeric pgtype.Numeric
			_ = newQtyNumeric.Scan(newQty)
			if err := qtx.UpdateInventoryLotQuantity(ctx, &db.UpdateInventoryLotQuantityParams{
				ID:       lot.ID,
				Quantity: newQtyNumeric,
				Value:    lot.Value - deductedValue,
			}); err != nil {
				return 0, fmt.Errorf("update lot: %w", err)
			}
		}
	}

	if remaining > 0.001 {
		return 0, fmt.Errorf("stok tidak mencukupi: kurang %.4f unit", remaining)
	}
	return valueDeducted, nil
}

// FIFOAdd creates a new inventory lot. Must be called with a transaction-scoped *db.Queries.
func FIFOAdd(ctx context.Context, qtx *db.Queries, itemID, warehouseID uuid.UUID, qty float64, unitIndex int32, value int64, date time.Time) error {
	var qtyNumeric pgtype.Numeric
	_ = qtyNumeric.Scan(qty)
	_, err := qtx.CreateInventoryLot(ctx, &db.CreateInventoryLotParams{
		ItemID:      pgtype.UUID{Bytes: itemID, Valid: true},
		WarehouseID: pgtype.UUID{Bytes: warehouseID, Valid: true},
		Quantity:    qtyNumeric,
		UnitIndex:   unitIndex,
		Value:       value,
		Date:        pgtype.Date{Time: date, Valid: true},
	})
	return err
}

// InsertStockHistory writes one stock_history row inside a transaction.
func InsertStockHistory(ctx context.Context, qtx *db.Queries, p StockHistoryParams) error {
	var qtyChangeNumeric pgtype.Numeric
	_ = qtyChangeNumeric.Scan(p.QuantityChange)
	_, err := qtx.InsertStockHistory(ctx, &db.InsertStockHistoryParams{
		ItemID:         pgtype.UUID{Bytes: p.ItemID, Valid: true},
		WarehouseID:    pgtype.UUID{Bytes: p.WarehouseID, Valid: true},
		QuantityChange: qtyChangeNumeric,
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
