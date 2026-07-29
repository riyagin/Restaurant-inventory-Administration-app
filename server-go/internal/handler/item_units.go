package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// unitDef is one entry of items.units. The array runs largest → smallest, so the
// last entry is the item's base unit and perPrev on entry i says how many of
// unit i fit in one unit i-1. The first entry has no perPrev.
type unitDef struct {
	Name    string   `json:"name"`
	PerPrev *float64 `json:"perPrev"`
}

func parseUnitDefs(raw []byte) ([]unitDef, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var units []unitDef
	if err := json.Unmarshal(raw, &units); err != nil {
		return nil, err
	}
	return units, nil
}

// unitBaseFactor returns how many base units make up one unit at idx — the
// product of every perPrev below it. The base unit's own factor is 1.
//
// Example: [{dus}, {kaleng, perPrev: 24}] → factor(0) = 24, factor(1) = 1.
func unitBaseFactor(units []unitDef, idx int) float64 {
	if idx < 0 || idx >= len(units) {
		return 1
	}
	factor := 1.0
	for i := idx + 1; i < len(units); i++ {
		if units[i].PerPrev == nil || *units[i].PerPrev <= 0 {
			continue
		}
		factor *= *units[i].PerPrev
	}
	return factor
}

// baseUnitFactor is the []byte-oriented wrapper kept for the opname pricing path.
// Returns 1 when the units cannot be read, which leaves the caller's price
// unchanged rather than scaling it by a guess.
func baseUnitFactor(unitsJSON []byte, idx int32) float64 {
	units, err := parseUnitDefs(unitsJSON)
	if err != nil {
		return 1
	}
	return unitBaseFactor(units, int(idx))
}

// baseUnitName returns the name of an item's base (smallest) unit, for log and
// error text. Empty when the units cannot be read.
func baseUnitName(unitsJSON []byte) string {
	units, err := parseUnitDefs(unitsJSON)
	if err != nil || len(units) == 0 {
		return ""
	}
	return units[len(units)-1].Name
}

// baseUnitIndex is the index every inventory lot of the item must carry — the
// last entry of items.units. Falls back to `fallback` when the units cannot be
// read, so a caller keeps whatever index it already had rather than guessing 0.
func baseUnitIndex(unitsJSON []byte, fallback int32) int32 {
	units, err := parseUnitDefs(unitsJSON)
	if err != nil || len(units) == 0 {
		return fallback
	}
	return int32(len(units) - 1)
}

func normalizeUnitName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// lineConversion is how one transaction line's entered quantity maps onto the
// base-unit quantity inventory is denominated in.
type lineConversion struct {
	Factor       float64 // base units per one entered unit
	BaseIndex    int32   // unit index every inventory lot must carry
	BaseUnitName string
}

// BaseQty restates an entered quantity in base units.
func (c lineConversion) BaseQty(qty float64) float64 { return qty * c.Factor }

// resolveLineConversion works out the factor a purchase or dispatch line is
// booked at.
//
// The catalogue figure comes from items.units, but the operator may override it
// for this one transaction — a supplier's dus holding 20 where the catalogue
// says 24. The override is deliberately not written back to items.units: it
// describes this delivery, not the item.
//
// An override is only honoured for a non-base unit. Picking the base unit means
// the entered quantity already IS the base quantity, so there is nothing to
// scale and a stray factor would silently multiply stock.
func resolveLineConversion(unitsJSON []byte, unitIndex int32, override float64) (lineConversion, error) {
	units, err := parseUnitDefs(unitsJSON)
	if err != nil {
		return lineConversion{}, fmt.Errorf("daftar satuan barang tidak dapat dibaca: %w", err)
	}
	if len(units) == 0 {
		return lineConversion{Factor: 1, BaseIndex: unitIndex}, nil
	}
	baseIndex := int32(len(units) - 1)
	baseName := units[baseIndex].Name

	if int(unitIndex) < 0 || int(unitIndex) >= len(units) {
		return lineConversion{}, fmt.Errorf("satuan pada indeks %d tidak ada pada barang ini", unitIndex)
	}

	factor := unitBaseFactor(units, int(unitIndex))
	if unitIndex != baseIndex && override > 0 {
		factor = override
	}
	if factor <= 0 {
		return lineConversion{}, fmt.Errorf("konversi satuan %q tidak valid", units[unitIndex].Name)
	}
	return lineConversion{Factor: factor, BaseIndex: baseIndex, BaseUnitName: baseName}, nil
}

// storedConversionFactor reads back the factor a line was booked at. Rows
// written before the column existed hold 1, which is exactly right: their
// quantity is what reached inventory unconverted.
func storedConversionFactor(n pgtype.Numeric) float64 {
	f := numericToFloat64(n)
	if f <= 0 {
		return 1
	}
	return f
}

// lotRescale computes how to restate an inventory lot when an item's unit list
// changes from oldUnits to newUnits.
//
// It exists because nothing in the deduction path converts units: FIFODeduct
// compares a requested quantity directly against inventory.quantity, so every
// lot of an item must be denominated in the same unit — the base (last) one.
// Appending a smaller unit moves the base down a level, and any lot left at the
// old index is then silently read as the wrong unit. A lot of 2 "ball" under
// [ball, pack/40] means 80 packs; read as 2 it under-reports stock 40-fold.
//
// Returns the multiplier for the lot's quantity and its new unit_index.
//
// The old unit is located in the new array by name, which is what makes an
// append work: "pack" keeps its identity even as it stops being the base.
// A pure rename has no name to match, so equal-length arrays fall back to
// matching by position. Anything else — the lot's unit deleted outright — is an
// error rather than a guess, because there is no conversion that preserves the
// quantity.
func lotRescale(oldUnits, newUnits []unitDef, oldIdx int32) (float64, int32, error) {
	if len(newUnits) == 0 {
		return 0, 0, fmt.Errorf("daftar satuan baru kosong")
	}
	if int(oldIdx) < 0 || int(oldIdx) >= len(oldUnits) {
		return 0, 0, fmt.Errorf("satuan lama pada indeks %d tidak ditemukan", oldIdx)
	}

	oldName := normalizeUnitName(oldUnits[oldIdx].Name)
	matched := -1
	for i, u := range newUnits {
		if normalizeUnitName(u.Name) == oldName && oldName != "" {
			matched = i
			break
		}
	}

	// Rename: same shape, so position still identifies the unit.
	if matched < 0 && len(newUnits) == len(oldUnits) {
		matched = int(oldIdx)
	}
	if matched < 0 {
		return 0, 0, fmt.Errorf("satuan %q tidak ada lagi pada daftar satuan baru", oldUnits[oldIdx].Name)
	}

	return unitBaseFactor(newUnits, matched), int32(len(newUnits) - 1), nil
}

// rescaleInventoryForUnits restates every inventory lot of an item into the new
// base unit. Value is deliberately left alone: the same physical goods bought
// for the same money are merely being counted in a smaller unit.
//
// Returns the number of lots adjusted.
func rescaleInventoryForUnits(ctx context.Context, tx pgx.Tx, itemID uuid.UUID, oldUnits, newUnits []unitDef) (int, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, quantity::float8, unit_index FROM inventory WHERE item_id = $1`, itemID)
	if err != nil {
		return 0, err
	}

	type lot struct {
		id       uuid.UUID
		quantity float64
		unitIdx  int32
	}
	var lots []lot
	for rows.Next() {
		var l lot
		if err := rows.Scan(&l.id, &l.quantity, &l.unitIdx); err != nil {
			rows.Close()
			return 0, err
		}
		lots = append(lots, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	adjusted := 0
	for _, l := range lots {
		factor, idx, err := lotRescale(oldUnits, newUnits, l.unitIdx)
		if err != nil {
			return 0, err
		}
		if factor == 1 && idx == l.unitIdx {
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE inventory SET quantity = $1, unit_index = $2 WHERE id = $3`,
			l.quantity*factor, idx, l.id); err != nil {
			return 0, err
		}
		adjusted++
	}
	return adjusted, nil
}
