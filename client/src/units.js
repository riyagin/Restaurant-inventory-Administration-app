// Unit conversion helpers, mirroring server-go/internal/handler/item_units.go.
//
// items.units runs largest → smallest. The last entry is the item's base unit —
// the one every inventory lot is denominated in — and `perPrev` on entry i says
// how many of unit i fit in one unit i-1. The first entry has no perPrev.

export const baseUnitIndex = (units) => (units?.length ? units.length - 1 : 0);

export const baseUnitName = (units) => (units?.length ? units[units.length - 1].name : '');

export const unitName = (units, idx) => units?.[Number(idx)]?.name ?? '';

/** How many base units one unit at `idx` holds, per the item's own catalogue. */
export function catalogFactor(units, idx) {
  const i = Number(idx);
  if (!units?.length || i < 0 || i >= units.length) return 1;
  let factor = 1;
  for (let j = i + 1; j < units.length; j++) {
    const perPrev = Number(units[j].perPrev);
    if (perPrev > 0) factor *= perPrev;
  }
  return factor;
}

export const isBaseUnit = (units, idx) => Number(idx) === baseUnitIndex(units);

/**
 * The factor a transaction line is actually booked at: the operator's one-off
 * override when they set one, else the catalogue figure. Picking the base unit
 * means there is nothing to convert, so any override is ignored — the same rule
 * the backend applies.
 */
export function effectiveFactor(units, idx, override) {
  if (!units?.length || isBaseUnit(units, idx)) return 1;
  const o = Number(override);
  return o > 0 ? o : catalogFactor(units, idx);
}

/** True when the line carries an override that differs from the catalogue. */
export function isOverridden(units, idx, override) {
  if (!units?.length || isBaseUnit(units, idx)) return false;
  const o = Number(override);
  return o > 0 && o !== catalogFactor(units, idx);
}

export const formatQty = (n) =>
  Number(n).toLocaleString('id-ID', { maximumFractionDigits: 3 });
