import { baseUnitName, catalogFactor, effectiveFactor, formatQty, isBaseUnit, isOverridden, unitName } from '../units';

/**
 * Shows what a line entered in a larger unit becomes in the item's smallest
 * unit — the unit stock is actually kept in — and lets the operator correct the
 * rate for this transaction alone.
 *
 * The override is deliberately transaction-scoped: a supplier's dus holding 20
 * where the catalogue says 24 describes that delivery, not the item, so it is
 * never written back to the item master.
 *
 * Renders nothing when the chosen unit already is the base unit: there is no
 * conversion to show and nothing to correct.
 */
export default function UnitConversion({ units, unitIndex, quantity, factor, onFactorChange, verb = 'masuk stok' }) {
  if (!units?.length || isBaseUnit(units, unitIndex)) return null;

  const from = unitName(units, unitIndex);
  const base = baseUnitName(units);
  const standard = catalogFactor(units, unitIndex);
  const active = effectiveFactor(units, unitIndex, factor);
  const overridden = isOverridden(units, unitIndex, factor);
  const qty = Number(quantity);
  const hasQty = quantity !== '' && !isNaN(qty) && qty > 0;

  return (
    <div style={{
      marginTop: '0.35rem', padding: '0.35rem 0.5rem', borderRadius: '5px',
      background: overridden ? '#fff8e6' : '#f4f7ff',
      border: `1px solid ${overridden ? '#f0d290' : '#dfe7fb'}`,
      fontSize: '0.72rem', color: '#555', lineHeight: 1.5,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.3rem', flexWrap: 'wrap' }}>
        <span>1 {from} =</span>
        <input
          type="number" min="0" step="any"
          value={factor ?? ''}
          onChange={(e) => onFactorChange(e.target.value)}
          placeholder={String(standard)}
          title="Berlaku untuk transaksi ini saja — tidak mengubah data barang"
          style={{
            width: '4.5rem', padding: '0.1rem 0.3rem', fontSize: '0.72rem',
            textAlign: 'right', border: '1px solid #ccd', borderRadius: '4px',
          }}
        />
        <span>{base}</span>
        {overridden && (
          <>
            <span style={{ color: '#a97a12' }}>(standar {formatQty(standard)})</span>
            <button
              type="button"
              onClick={() => onFactorChange('')}
              style={{
                fontSize: '0.68rem', padding: '0 0.35rem', cursor: 'pointer',
                border: '1px solid #e2c98a', borderRadius: '4px', background: '#fff', color: '#a97a12',
              }}
            >Kembalikan</button>
          </>
        )}
      </div>
      <div style={{ marginTop: '0.15rem', color: hasQty ? '#333' : '#aaa' }}>
        {hasQty
          ? <>→ {formatQty(qty)} {from} = <strong>{formatQty(qty * active)} {base}</strong> {verb}</>
          : <>→ isi jumlah untuk melihat konversi ke {base}</>}
      </div>
    </div>
  );
}
