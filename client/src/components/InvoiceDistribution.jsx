import { unitName } from '../units';
import { ALLOC_TOLERANCE, allocQty, newAllocation, unallocated } from '../invoiceAllocation';

// Step 2 of a faktur distribusi: say where each purchased line goes.
//
// A line's quantity is split across any number of destinations — a warehouse
// (goes into stock) or a branch / division (consumed on arrival) — in whatever
// proportion is needed, never assumed even. The LAST destination row of a line
// is "auto" until the user types into it: it always shows what is still needed
// to reach the line's full quantity, so adding a destination never leaves the
// operator doing the subtraction themselves.

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const fmtQty = (x) => new Intl.NumberFormat('id-ID', { maximumFractionDigits: 4 }).format(x);

export default function InvoiceDistribution({ rows, setRows, itemById, warehouses, branches, divisions }) {
  const updateRow = (ri, fn) => setRows(rs => rs.map((r, i) => (i === ri ? fn(r) : r)));

  const setDest = (ri, ai) => (e) => {
    const dest = e.target.value;
    updateRow(ri, r => ({ ...r, allocations: r.allocations.map((a, i) => (i === ai ? { ...a, dest } : a)) }));
  };

  // Typing into a row takes it out of auto: the operator's number wins.
  const setQty = (ri, ai) => (e) => {
    const quantity = e.target.value;
    updateRow(ri, r => ({
      ...r,
      allocations: r.allocations.map((a, i) => (i === ai ? { ...a, quantity, auto: false } : a)),
    }));
  };

  // The current last row is frozen at the value it was showing and the new row
  // becomes the auto one — so it shows what is left after everything above it.
  const addAllocation = (ri) => updateRow(ri, r => ({
    ...r,
    allocations: [
      ...r.allocations.map((a, i) => (a.auto ? { ...a, quantity: String(allocQty(r, i)), auto: false } : a)),
      newAllocation(),
    ],
  }));

  // Whatever row ends up last goes back to auto, so it absorbs the quantity the
  // removed row was carrying.
  const removeAllocation = (ri, ai) => updateRow(ri, r => {
    const rest = r.allocations.filter((_, i) => i !== ai);
    return { ...r, allocations: rest.map((a, i) => (i === rest.length - 1 ? { ...a, auto: true } : a)) };
  });

  const resetAuto = (ri, ai) => updateRow(ri, r => ({
    ...r,
    allocations: r.allocations.map((a, i) => (i === ai ? { ...a, auto: true } : a)),
  }));

  const destLabel = (dest) => {
    const [kind, a, b] = dest.split(':');
    if (kind === 'w') return `Gudang · ${warehouses.find(w => w.id === a)?.name ?? '?'}`;
    const branch = branches.find(x => x.id === a)?.name ?? '?';
    if (kind === 'b') return `${branch} (cabang)`;
    return `${branch} — ${divisions.find(d => d.id === b)?.name ?? '?'}`;
  };

  // Value per destination across the whole invoice — what each place will be
  // charged with, which is the number people actually check.
  const perDest = new Map();
  rows.forEach(r => {
    const qty = Number(r.quantity) || 0;
    const lineValue = qty * (Number(r.price) || 0);
    if (!r.item_id || qty <= 0) return;
    r.allocations.forEach((a, ai) => {
      if (!a.dest) return;
      const q = allocQty(r, ai);
      perDest.set(a.dest, (perDest.get(a.dest) ?? 0) + lineValue * q / qty);
    });
  });

  const muted = { color: 'var(--ink-2, #888)', fontSize: '0.8rem' };

  return (
    <div style={{ marginBottom: '1.5rem' }}>
      <div style={{ marginBottom: '0.35rem', fontWeight: 600, fontSize: '0.9rem', color: '#444' }}>
        2. Distribusi ke Tujuan
      </div>
      <div style={{ ...muted, marginBottom: '0.9rem' }}>
        Tentukan ke mana setiap item dikirim. Satu item boleh dibagi ke beberapa gudang atau cabang dengan jumlah
        berbeda — baris tujuan terakhir otomatis menampilkan sisa yang belum dialokasikan.
        Gudang menambah stok; cabang/divisi langsung dibebankan sebagai pemakaian.
      </div>

      {rows.map((row, ri) => {
        const item = itemById(row.item_id);
        const qty = Number(row.quantity) || 0;
        if (!item || qty <= 0) {
          return (
            <div key={ri} style={{ ...cardStyle, ...muted }}>
              Baris {ri + 1}: pilih item dan isi kuantitas terlebih dahulu.
            </div>
          );
        }
        const unit = unitName(item.units, row.unit_index);
        const lineValue = qty * (Number(row.price) || 0);
        const left = unallocated(row);
        const complete = Math.abs(left) <= ALLOC_TOLERANCE;

        return (
          <div key={ri} style={cardStyle}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: '0.75rem', flexWrap: 'wrap', marginBottom: '0.5rem' }}>
              <div>
                <strong>{item.name}</strong>
                <span style={{ ...muted, marginLeft: '0.5rem' }}>
                  {fmtQty(qty)} {unit} · {idr(lineValue)}
                  {!item.is_stock && ' · non-stok (hanya ke cabang)'}
                </span>
              </div>
              <span style={{
                fontSize: '0.78rem', fontWeight: 600, padding: '0.15rem 0.55rem', borderRadius: '999px',
                background: complete ? '#e6f6ec' : left > 0 ? '#fff4e0' : '#fdecec',
                color: complete ? '#1e7a3e' : left > 0 ? '#9a5b00' : '#b42318',
              }}>
                {complete
                  ? 'Teralokasi penuh'
                  : left > 0
                    ? `Sisa ${fmtQty(left)} ${unit}`
                    : `Kelebihan ${fmtQty(-left)} ${unit}`}
              </span>
            </div>

            <table className="invoice-items-table">
              <thead>
                <tr>
                  <th>Tujuan</th>
                  <th style={{ width: '150px' }}>Jumlah ({unit})</th>
                  <th style={{ width: '130px', textAlign: 'right' }}>Nilai</th>
                  <th style={{ width: '40px' }}></th>
                </tr>
              </thead>
              <tbody>
                {row.allocations.map((a, ai) => {
                  const q = allocQty(row, ai);
                  const isLast = ai === row.allocations.length - 1;
                  return (
                    <tr key={ai}>
                      <td>
                        <select value={a.dest} onChange={setDest(ri, ai)} style={{ width: '100%' }}>
                          <option value="">Pilih tujuan...</option>
                          <optgroup label="Gudang (masuk stok)">
                            {warehouses.map(w => (
                              <option key={w.id} value={`w:${w.id}`} disabled={!item.is_stock}>{w.name}</option>
                            ))}
                          </optgroup>
                          {branches.map(b => (
                            <optgroup key={b.id} label={`Cabang ${b.name} (pemakaian)`}>
                              <option value={`b:${b.id}`}>{b.name} — tanpa divisi</option>
                              {divisions.filter(d => d.branch_id === b.id).map(d => (
                                <option key={d.id} value={`d:${b.id}:${d.id}`}>{b.name} — {d.name}</option>
                              ))}
                            </optgroup>
                          ))}
                        </select>
                      </td>
                      <td>
                        <input
                          type="number" min="0" step="any"
                          value={a.auto ? (q ? String(q) : '') : a.quantity}
                          placeholder={a.auto ? '0' : ''}
                          onChange={setQty(ri, ai)}
                          style={{ width: '100%', ...(a.auto ? { background: '#f4f8ff', fontStyle: 'italic' } : {}) }}
                          title={a.auto ? 'Otomatis: sisa yang belum dialokasikan' : undefined}
                        />
                        {isLast && (a.auto
                          ? <div style={{ ...muted, fontSize: '0.7rem', marginTop: '0.15rem' }}>otomatis = sisa</div>
                          : !complete && (
                            <button type="button" onClick={() => resetAuto(ri, ai)}
                              style={{ fontSize: '0.7rem', marginTop: '0.15rem', padding: 0, border: 'none', background: 'none', color: 'var(--accent, #4f8ef7)', cursor: 'pointer' }}>
                              isi dengan sisa
                            </button>
                          ))}
                      </td>
                      <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>{idr(qty ? lineValue * q / qty : 0)}</td>
                      <td>
                        {row.allocations.length > 1 && (
                          <button type="button" onClick={() => removeAllocation(ri, ai)} className="btn btn-danger btn-sm">✕</button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <button type="button" onClick={() => addAllocation(ri)} className="btn btn-secondary btn-sm" style={{ marginTop: '0.4rem' }}>
              + Tambah Tujuan
            </button>
          </div>
        );
      })}

      {perDest.size > 0 && (
        <div style={{ ...cardStyle, background: '#fafbfc' }}>
          <div style={{ fontWeight: 600, fontSize: '0.85rem', marginBottom: '0.4rem' }}>Ringkasan per Tujuan</div>
          <table className="invoice-items-table">
            <tbody>
              {[...perDest.entries()].map(([dest, value]) => (
                <tr key={dest}>
                  <td>{destLabel(dest)}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600, whiteSpace: 'nowrap' }}>{idr(value)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

const cardStyle = {
  border: '1px solid var(--line, #e3e6eb)',
  borderRadius: '8px',
  padding: '0.75rem 0.9rem',
  marginBottom: '0.75rem',
};
