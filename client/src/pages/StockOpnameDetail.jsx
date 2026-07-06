import { useEffect, useState, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getStockOpnameById, getAllInventory, updateStockOpname } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);
const fmt = (d) => d ? new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' }) : '—';

// Group current inventory lots by item + unit so an actual count can be
// reconciled against the aggregated system quantity (mirrors StockOpname form).
function groupInventory(inventory) {
  const map = new Map();
  const sorted = [...inventory].reverse(); // oldest-first for FIFO
  for (const rec of sorted) {
    const key = `${rec.item_id}__${rec.unit_index}`;
    if (!map.has(key)) {
      map.set(key, {
        key, item_id: rec.item_id, item_name: rec.item_name, item_code: rec.item_code,
        unit_name: rec.unit_name, unit_index: rec.unit_index, totalQty: 0, totalValue: 0, lots: [],
      });
    }
    const g = map.get(key);
    g.totalQty += Number(rec.quantity);
    g.totalValue += Number(rec.value);
    g.lots.push(rec);
  }
  return Array.from(map.values()).sort((a, b) => a.item_name.localeCompare(b.item_name));
}

function computeGroupWaste(group, actualQty) {
  let waste = 0;
  let remaining = actualQty;
  for (const lot of group.lots) {
    const lotQty = Number(lot.quantity);
    const lotVal = Number(lot.value);
    if (remaining <= 0) waste += lotVal;
    else if (remaining >= lotQty) remaining -= lotQty;
    else { waste += Math.round(lotVal * (lotQty - remaining) / lotQty); remaining = 0; }
  }
  return waste;
}

export default function StockOpnameDetail() {
  const { id } = useParams();
  const [opname, setOpname] = useState(null);
  const [loading, setLoading] = useState(true);

  // ── Edit (correction) state ──
  const [editing, setEditing]   = useState(false);
  const [inventory, setInventory] = useState([]);
  const [actuals, setActuals]   = useState({});
  const [saving, setSaving]     = useState(false);
  const [editError, setEditError] = useState('');

  const load = useCallback(() => {
    return getStockOpnameById(id).then(r => { setOpname(r.data); setLoading(false); });
  }, [id]);

  useEffect(() => { load(); }, [load]);

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Memuat…</div>;
  if (!opname) return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>Opname tidak ditemukan.</div>;

  const totalWaste = opname.items.reduce((s, it) => s + Number(it.waste_value), 0);

  // ── Edit helpers ──
  const groups = groupInventory(inventory);

  const startEdit = () => {
    setEditError('');
    setActuals({});
    getAllInventory({ warehouse_id: opname.warehouse_id }).then(rows => {
      setInventory(rows);
      setEditing(true);
    });
  };

  const cancelEdit = () => { setEditing(false); setActuals({}); setEditError(''); };

  const setActual = (key, val) => setActuals(a => ({ ...a, [key]: val }));

  const diffOf = (g) => {
    const a = actuals[g.key];
    if (a === '' || a === undefined) return null;
    return Number(a) - g.totalQty;
  };

  const changed = groups.filter(g => { const d = diffOf(g); return d !== null && d !== 0; });

  const submitEdit = async () => {
    setEditError('');
    if (!changed.length) { setEditError('Tidak ada perubahan — semua aktual sesuai dengan sistem.'); return; }
    setSaving(true);
    try {
      await updateStockOpname(id, {
        items: changed.map(g => ({
          item_id: g.item_id, unit_index: g.unit_index,
          unit_name: g.unit_name, actual_quantity: Number(actuals[g.key]),
        })),
      });
      await load();
      cancelEdit();
    } catch (err) {
      setEditError(err.response?.data?.error || 'Gagal menyimpan koreksi.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.2rem' }}>Stock Opname</h1>
          <div style={{ fontSize: '0.85rem', color: '#888' }}>
            {opname.warehouse_name} &nbsp;·&nbsp; {fmt(opname.performed_at)}
            {opname.updated_at && (
              <span> &nbsp;·&nbsp; <em style={{ color: '#b9770e' }}>diubah {fmt(opname.updated_at)}</em></span>
            )}
          </div>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          {!editing && (
            <button className="btn btn-primary" onClick={startEdit}>✎ Koreksi</button>
          )}
          <Link to="/stock-opname" className="btn btn-secondary">← Kembali</Link>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '1.5rem', padding: '0.5rem 0 1rem' }}>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Gudang</div>
            <span className="badge">{opname.warehouse_name}</span>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Penanggung Jawab</div>
            <div style={{ fontWeight: 500 }}>{opname.pic_name ?? <span style={{color:'#aaa',fontStyle:'italic'}}>—</span>}</div>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Pelaksana</div>
            <div style={{ fontWeight: 500 }}>{opname.operator_name ?? <span style={{color:'#aaa',fontStyle:'italic'}}>—</span>}</div>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Dicatat Oleh</div>
            <div style={{ fontWeight: 500 }}>{opname.performed_by_name ?? '—'}</div>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Barang Disesuaikan</div>
            <span className="badge">{opname.items.length}</span>
          </div>
          {totalWaste > 0 && (
            <div>
              <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Total Susut</div>
              <div style={{ fontWeight: 700, color: '#e74c3c' }}>{idr(totalWaste)}</div>
            </div>
          )}
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Catatan</div>
            <div style={{ color: opname.notes ? '#333' : '#aaa', fontStyle: opname.notes ? 'normal' : 'italic' }}>{opname.notes ?? '—'}</div>
          </div>
        </div>
      </div>

      {/* ── Correction (edit) panel ── */}
      {editing && (
        <div className="card" style={{ marginBottom: '1.5rem', border: '1px solid #f0c674' }}>
          <div className="card-header"><h2>Koreksi Opname</h2></div>
          <p style={{ fontSize: '0.85rem', color: '#888', margin: '0 0 1rem' }}>
            Masukkan jumlah aktual yang benar. Selisih terhadap jumlah sistem saat ini akan dicatat
            sebagai baris koreksi baru (tidak menimpa catatan asli).
          </p>
          {editError && <div className="error-msg" style={{ marginBottom: '1rem' }}>{editError}</div>}
          {groups.length === 0 ? (
            <p style={{ color: '#999', fontSize: '0.9rem' }}>Tidak ada catatan inventaris di gudang ini.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Barang</th>
                  <th>Satuan</th>
                  <th style={{ textAlign: 'right' }}>Qty Sistem</th>
                  <th style={{ textAlign: 'center' }}>Qty Aktual</th>
                  <th style={{ textAlign: 'right' }}>Selisih</th>
                  <th style={{ textAlign: 'right' }}>Nilai Susut</th>
                </tr>
              </thead>
              <tbody>
                {groups.map(g => {
                  const d = diffOf(g);
                  const hasInput = actuals[g.key] !== undefined && actuals[g.key] !== '';
                  const wasteVal = (hasInput && d !== null && d < 0) ? computeGroupWaste(g, Number(actuals[g.key])) : 0;
                  return (
                    <tr key={g.key}>
                      <td style={{ fontWeight: 500 }}>{g.item_name}</td>
                      <td style={{ color: '#555' }}>{g.unit_name}</td>
                      <td style={{ textAlign: 'right', fontWeight: 600 }}>{g.totalQty.toLocaleString('id-ID')}</td>
                      <td style={{ textAlign: 'center', width: '120px' }}>
                        <input
                          type="number" min="0" step="any"
                          value={actuals[g.key] ?? ''}
                          onChange={e => setActual(g.key, e.target.value)}
                          placeholder={g.totalQty.toLocaleString('id-ID')}
                          style={{ width: '100%', padding: '0.35rem 0.5rem', border: '1px solid #ddd', borderRadius: '5px', fontSize: '0.9rem', textAlign: 'right' }}
                        />
                      </td>
                      <td style={{ textAlign: 'right', fontWeight: 600, color: d === null || d === 0 ? '#aaa' : d > 0 ? '#27ae60' : '#e74c3c' }}>
                        {d === null || d === 0 ? '—' : (d > 0 ? '+' : '') + d.toLocaleString('id-ID')}
                      </td>
                      <td style={{ textAlign: 'right', color: wasteVal > 0 ? '#e74c3c' : d !== null && d > 0 ? '#27ae60' : '#aaa', fontWeight: wasteVal > 0 ? 600 : 400 }}>
                        {wasteVal > 0 ? idr(wasteVal) : d !== null && d > 0 ? '+stok' : '—'}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
          <div style={{ marginTop: '1.25rem', display: 'flex', gap: '0.75rem', justifyContent: 'flex-end' }}>
            <button className="btn btn-secondary" onClick={cancelEdit} disabled={saving}>Batal</button>
            <button className="btn btn-primary" onClick={submitEdit} disabled={saving || changed.length === 0}>
              {saving ? 'Menyimpan…' : `Simpan Koreksi${changed.length ? ` (${changed.length})` : ''}`}
            </button>
          </div>
        </div>
      )}

      <div className="card">
        <div className="card-header"><h2>{opname.items.length} baris penyesuaian</h2></div>
        <table>
          <thead>
            <tr>
              <th>Barang</th>
              <th>Kode</th>
              <th>Satuan</th>
              <th style={{ textAlign: 'right' }}>Tercatat</th>
              <th style={{ textAlign: 'right' }}>Aktual</th>
              <th style={{ textAlign: 'right' }}>Selisih</th>
              <th style={{ textAlign: 'right' }}>Nilai Susut</th>
            </tr>
          </thead>
          <tbody>
            {opname.items.map(it => {
              const diff = Number(it.difference);
              return (
                <tr key={it.id} style={it.is_correction ? { background: '#fffaf0' } : undefined}>
                  <td style={{ fontWeight: 500 }}>
                    {it.item_name}
                    {it.is_correction && (
                      <span className="badge" style={{ marginLeft: '0.5rem', background: '#f39c12', color: '#fff', fontSize: '0.7rem' }}>Koreksi</span>
                    )}
                  </td>
                  <td style={{ color: '#888', fontSize: '0.85rem' }}>{it.item_code}</td>
                  <td style={{ color: '#555' }}>{it.unit_name}</td>
                  <td style={{ textAlign: 'right' }}>{Number(it.recorded_quantity).toLocaleString('id-ID')}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{Number(it.actual_quantity).toLocaleString('id-ID')}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600, color: diff >= 0 ? '#27ae60' : '#e74c3c' }}>
                    {diff > 0 ? '+' : ''}{diff.toLocaleString('id-ID')}
                  </td>
                  <td style={{ textAlign: 'right', color: Number(it.waste_value) > 0 ? '#e74c3c' : '#aaa' }}>
                    {Number(it.waste_value) > 0 ? idr(it.waste_value) : '—'}
                  </td>
                </tr>
              );
            })}
          </tbody>
          {totalWaste > 0 && (
            <tfoot>
              <tr>
                <td colSpan={6} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>Total Susut:</td>
                <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#e74c3c' }}>{idr(totalWaste)}</td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>
    </>
  );
}
