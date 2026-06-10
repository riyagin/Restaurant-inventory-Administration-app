package service

import (
	"fmt"
	"testing"
)

// fifoDeductSim replicates the FIFODeduct algorithm without database access.
// Used to verify the proportional-value math in isolation.
func fifoDeductSim(lots []struct{ qty float64; value int64 }, deductQty float64) (valueDeducted int64, err error) {
	remaining := deductQty
	for i := range lots {
		if remaining <= 0 {
			break
		}
		lotQty := lots[i].qty
		if lotQty <= remaining {
			valueDeducted += lots[i].value
			remaining -= lotQty
			lots[i].qty = 0
			lots[i].value = 0
		} else {
			proportion := remaining / lotQty
			deductedValue := int64(float64(lots[i].value) * proportion)
			valueDeducted += deductedValue
			lots[i].qty -= remaining
			lots[i].value -= deductedValue
			remaining = 0
		}
	}
	if remaining > 0.001 {
		err = fmt.Errorf("stok tidak mencukupi: kurang %.4f unit", remaining)
	}
	return
}

// TestFIFODeductLogic exercises the FIFO proportional-value deduction algorithm.
//
// Scenario: lots [10 / 10000], [5 / 5000], [8 / 8000], deduct 22 units.
//   - Lot 1: fully consumed → +10000, remaining = 12
//   - Lot 2: fully consumed → +5000,  remaining = 7
//   - Lot 3: 7 of 8 consumed, proportion 7/8 → +7000, lot3 becomes qty=1, value=1000
//
// Expected: valueDeducted = 22000, lot 3 = {qty:1, value:1000}
func TestFIFODeductLogic(t *testing.T) {
	lots := []struct {
		qty   float64
		value int64
	}{
		{10, 10000},
		{5, 5000},
		{8, 8000},
	}

	valueDeducted, err := fifoDeductSim(lots, 22)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valueDeducted != 22000 {
		t.Errorf("valueDeducted = %d, want 22000", valueDeducted)
	}
	if lots[0].qty != 0 || lots[1].qty != 0 {
		t.Errorf("lots 0 and 1 should be fully consumed, got qty0=%v qty1=%v", lots[0].qty, lots[1].qty)
	}
	if lots[2].qty != 1 {
		t.Errorf("lot 2 qty = %v, want 1", lots[2].qty)
	}
	if lots[2].value != 1000 {
		t.Errorf("lot 2 value = %d, want 1000", lots[2].value)
	}
}

// TestFIFODeductPartial tests partial deduction from a single lot.
func TestFIFODeductPartial(t *testing.T) {
	lots := []struct {
		qty   float64
		value int64
	}{
		{10, 10000},
	}

	valueDeducted, err := fifoDeductSim(lots, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 4/10 of 10000 = 4000
	if valueDeducted != 4000 {
		t.Errorf("valueDeducted = %d, want 4000", valueDeducted)
	}
	// Remaining lot: qty=6, value=6000
	if lots[0].qty != 6 {
		t.Errorf("remaining qty = %v, want 6", lots[0].qty)
	}
	if lots[0].value != 6000 {
		t.Errorf("remaining value = %d, want 6000", lots[0].value)
	}
}

// TestFIFODeductInsufficientStock verifies the error when stock is insufficient.
func TestFIFODeductInsufficientStock(t *testing.T) {
	lots := []struct {
		qty   float64
		value int64
	}{
		{10, 10000},
		{5, 5000},
	}

	_, err := fifoDeductSim(lots, 20)
	if err == nil {
		t.Error("expected insufficient stock error, got nil")
	}
}
