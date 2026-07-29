package handler

import "testing"

func ratio(f float64) *float64 { return &f }

// units [dus, pack/12] — the AGARMERAH shape.
func twoUnits() []unitDef {
	return []unitDef{{Name: "dus"}, {Name: "pack", PerPrev: ratio(12)}}
}

// units [dus, pack/12, pcs/10] — a smaller unit appended below the old base.
func threeUnits() []unitDef {
	return []unitDef{{Name: "dus"}, {Name: "pack", PerPrev: ratio(12)}, {Name: "pcs", PerPrev: ratio(10)}}
}

func TestUnitBaseFactor(t *testing.T) {
	u := threeUnits()
	for idx, want := range map[int]float64{0: 120, 1: 10, 2: 1} {
		if got := unitBaseFactor(u, idx); got != want {
			t.Errorf("unitBaseFactor(%d) = %v, want %v", idx, got, want)
		}
	}
	// Out of range degrades to 1 rather than guessing.
	if got := unitBaseFactor(u, 9); got != 1 {
		t.Errorf("unitBaseFactor(out of range) = %v, want 1", got)
	}
}

// The feature: appending a smaller unit demotes the old base to a middle unit,
// and lots held in it must be restated into the new base.
func TestLotRescale_AppendSmallerUnit(t *testing.T) {
	cases := []struct {
		name       string
		oldIdx     int32
		wantFactor float64
	}{
		{"lot held in the old base (pack) becomes pcs", 1, 10},
		{"lot held in the top unit (dus) becomes pcs", 0, 120},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			factor, newIdx, err := lotRescale(twoUnits(), threeUnits(), tc.oldIdx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if factor != tc.wantFactor {
				t.Errorf("factor = %v, want %v", factor, tc.wantFactor)
			}
			if newIdx != 2 {
				t.Errorf("newIdx = %d, want 2 (new base)", newIdx)
			}
		})
	}
}

// An unchanged unit list must never move stock.
func TestLotRescale_NoChangeIsNoOp(t *testing.T) {
	factor, newIdx, err := lotRescale(twoUnits(), twoUnits(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if factor != 1 || newIdx != 1 {
		t.Errorf("factor/newIdx = %v/%d, want 1/1", factor, newIdx)
	}
}

// Renaming a unit keeps its position, so the lot is matched positionally and
// left alone rather than rejected.
func TestLotRescale_RenameMatchesByPosition(t *testing.T) {
	renamed := []unitDef{{Name: "dus"}, {Name: "pak", PerPrev: ratio(12)}}
	factor, newIdx, err := lotRescale(twoUnits(), renamed, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if factor != 1 || newIdx != 1 {
		t.Errorf("factor/newIdx = %v/%d, want 1/1", factor, newIdx)
	}
}

// Deleting the unit a lot is denominated in has no quantity-preserving
// conversion, so it must fail loudly instead of silently mis-scaling.
func TestLotRescale_DroppedUnitIsRejected(t *testing.T) {
	if _, _, err := lotRescale(twoUnits(), []unitDef{{Name: "dus"}}, 1); err == nil {
		t.Fatal("expected an error when the lot's unit is removed, got nil")
	}
}

// A ratio correction restates lots held above the base: 12 → 24 per dus means a
// lot counted in dus is now worth twice as many packs.
func TestLotRescale_RatioChange(t *testing.T) {
	wider := []unitDef{{Name: "dus"}, {Name: "pack", PerPrev: ratio(24)}}
	factor, _, err := lotRescale(twoUnits(), wider, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if factor != 24 {
		t.Errorf("factor = %v, want 24", factor)
	}
}
