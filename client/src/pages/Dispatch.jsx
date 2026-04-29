import { useEffect, useState } from 'react';
import { getItems, getWarehouses, getInventory, getBranches, getDivisions, getDispatches, createDispatch } from '../api';

const emptyRow = () => ({ item_id: '', quantity: '', unit_index: '0' });
const emptyHeader = { warehouse_id: '', branch_id: '', division_id: '', notes: '' };

export default function Dispatch() {
  const [dispatches, setDispatches] = useState([]);
  const [allItems, setAllItems] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [branches, setBranches] = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [srcInventory, setSrcInventory] = useState([]);
  const [header, setHeader] = useState(emptyHeader);
  const [rows, setRows] = useState([emptyRow()]);
  const [expandedId, setExpandedId] = useState(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const loadDispatches = () => getDispatches().then(r => setDispatches(r.data));

  useEffect(() => {
    Promise.all([getItems(), getWarehouses(), getBranches(), getDispatches()]).then(([i, w, b, d]) => {
      setAllItems(i.data);
      setWarehouses(w.data);
      setBranches(b.data);
      setDispatches(d.data);
    });
  }, []);

  // Load inventory when source warehouse changes
  useEffect(() => {
    if (!header.warehouse_id) { setSrcInventory([]); setRows([emptyRow()]); return; }
    getInventory({ warehouse_id: header.warehouse_id }).then(r => setSrcInventory(r.data));
    setRows([emptyRow()]);
  }, [header.warehouse_id]);

  // Load divisions when branch changes
  useEffect(() => {
    if (!header.branch_id) { setDivisions([]); return; }
    getDivisions({ branch_id: header.branch_id }).then(r => setDivisions(r.data));
    setHeader(h => ({ ...h, division_id: '' }));
  }, [header.branch_id]);

  const availableItems = allItems.filter(it => srcInventory.some(inv => inv.item_id === it.id));

  const getItemStock = (item_id, unit_index) => {
    const rec = srcInventory.find(inv => inv.item_id === item_id && String(inv.unit_index) === String(unit_index));
    return rec ? Number(rec.quantity) : 0;
  };

  const setHeaderField = (field) => (e) => setHeader(h => ({ ...h, [field]: e.target.value }));

  const setRow = (index, field) => (e) => {
    const val = e.target.value;
    setRows(rs => rs.map((r, i) => {
      if (i !== index) return r;
      const updated = { ...r, [field]: val };
      if (field === 'item_id') {
        const stockRec = srcInventory.find(inv => inv.item_id === val);
        updated.unit_index = stockRec ? String(stockRec.unit_index) : '0';
        updated.quantity = '';
      }
      if (field === 'unit_index') updated.quantity = '';
      return updated;
    }));
  };

  const addRow = () => setRows(rs => [...rs, emptyRow()]);
  const removeRow = (index) => setRows(rs => rs.filter((_, i) => i !== index));

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!header.warehouse_id) { setError('Pilih gudang asal'); return; }
    if (!header.branch_id)    { setError('Pilih cabang'); return; }
    if (!header.division_id)  { setError('Pilih divisi'); return; }
    for (const [i, row] of rows.entries()) {
      if (!row.item_id) { setError(`Baris ${i + 1}: pilih barang`); return; }
      if (!row.quantity || Number(row.quantity) <= 0) { setError(`Baris ${i + 1}: jumlah harus lebih dari 0`); return; }
      const available = getItemStock(row.item_id, row.unit_index);
      if (Number(row.quantity) > available) {
        const item = allItems.find(it => it.id === row.item_id);
        const unitName = item?.units[Number(row.unit_index)]?.name ?? '';
        setError(`Baris ${i + 1}: stok "${item?.name}" tidak cukup (tersedia: ${available.toLocaleString('id-ID')} ${unitName})`);
        return;
      }
    }
    setLoading(true);
    try {
      await createDispatch({
        warehouse_id: header.warehouse_id,
        branch_id: header.branch_id,
        division_id: header.division_id,
        notes: header.notes || null,
        items: rows.map(r => ({ item_id: r.item_id, quantity: Number(r.quantity), unit_index: Number(r.unit_index) })),
      });
      setHeader(emptyHeader);
      setRows([emptyRow()]);
      setSrcInventory([]);
      setDivisions([]);
      loadDispatches();
    } catch (err) {
      setError(err.response?.data?.error || 'Pengiriman gagal');
    } finally {
      setLoading(false);
    }
  };

  const fmt = (d) => new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' });

  return (
    <>
      <div className="page-header">
        <h1>Pengiriman ke Cabang</h1>
      </div>

      {/* Dispatch form */}
      <div className="card" style={{ maxWidth: '900px', marginBottom: '1.5rem' }}>
        <h2 style={{ marginBottom: '1.25rem' }}>Pengiriman Baru</h2>
        {error && <div className="error-msg">{error}</div>}
        <form onSubmit={handleSubmit}>
          <div className="form-row" style={{ marginBottom: '1rem' }}>
            <div className="form-group" style={{ margin: 0 }}>
              <label>Dari Gudang</label>
              <select value={header.warehouse_id} onChange={setHeaderField('warehouse_id')}>
                <option value="">Pilih gudang...</option>
                {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
              </select>
            </div>
            <div className="form-group" style={{ margin: 0 }}>
              <label>Cabang</label>
              <select value={header.branch_id} onChange={setHeaderField('branch_id')}>
                <option value="">Pilih cabang...</option>
                {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
              </select>
            </div>
            <div className="form-group" style={{ margin: 0 }}>
              <label>Divisi</label>
              <select value={header.division_id} onChange={setHeaderField('division_id')} disabled={!header.branch_id}>
                <option value="">{header.branch_id ? 'Pilih divisi...' : 'Pilih cabang terlebih dahulu'}</option>
                {divisions.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
              </select>
            </div>
          </div>

          <div className="form-group" style={{ marginBottom: '1rem' }}>
            <label>Catatan <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
            <input value={header.notes} onChange={setHeaderField('notes')} placeholder="Alasan atau deskripsi..." />
          </div>

          <div style={{ overflowX: 'auto', marginBottom: '0.5rem' }}>
            <table className="invoice-items-table">
              <thead>
                <tr>
                  <th>Barang</th>
                  <th>Satuan</th>
                  <th>Jumlah</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, i) => {
                  const selectedItem = allItems.find(it => it.id === row.item_id);
                  const available = row.item_id ? getItemStock(row.item_id, row.unit_index) : null;
                  const stockedUnits = selectedItem
                    ? selectedItem.units.map((u, ui) => ({ ...u, ui })).filter(u =>
                        srcInventory.some(inv => inv.item_id === row.item_id && inv.unit_index === u.ui)
                      )
                    : [];
                  return (
                    <tr key={i}>
                      <td style={{ minWidth: '220px' }}>
                        <select value={row.item_id} onChange={setRow(i, 'item_id')} style={{ width: '100%' }} disabled={!header.warehouse_id}>
                          <option value="">{header.warehouse_id ? 'Select item...' : 'Select warehouse first'}</option>
                          {availableItems.map(it => <option key={it.id} value={it.id}>{it.name}</option>)}
                        </select>
                      </td>
                      <td style={{ minWidth: '130px' }}>
                        <select value={row.unit_index} onChange={setRow(i, 'unit_index')} disabled={!selectedItem} style={{ width: '100%' }}>
                          {stockedUnits.length > 0
                            ? stockedUnits.map(u => <option key={u.ui} value={String(u.ui)}>{u.name}</option>)
                            : <option value="0">—</option>}
                        </select>
                      </td>
                      <td style={{ minWidth: '150px' }}>
                        <input
                          type="number" min="0.001" step="any"
                          value={row.quantity} onChange={setRow(i, 'quantity')}
                          placeholder="0" style={{ width: '100%' }} disabled={!selectedItem}
                        />
                        {available !== null && (
                          <div style={{ fontSize: '0.75rem', color: '#888', marginTop: '2px' }}>
                            Tersedia: {available.toLocaleString('id-ID')}
                          </div>
                        )}
                      </td>
                      <td>
                        {rows.length > 1 && (
                          <button type="button" onClick={() => removeRow(i)} className="btn btn-danger btn-sm">✕</button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '0.75rem' }}>
            <button type="button" onClick={addRow} className="btn btn-secondary" disabled={!header.warehouse_id}>+ Tambah Baris</button>
            <button type="submit" className="btn btn-primary" disabled={loading}>
              {loading ? 'Memproses...' : 'Kirim Stok'}
            </button>
          </div>
        </form>
      </div>

      {/* Dispatch history */}
      <div className="card">
        <div className="card-header"><h2>Riwayat Pengiriman</h2></div>
        <table>
          <thead>
            <tr>
              <th></th>
              <th>Waktu</th>
              <th>Dari</th>
              <th>Cabang</th>
              <th>Divisi</th>
              <th>Barang</th>
              <th>Oleh</th>
              <th>Catatan</th>
            </tr>
          </thead>
          <tbody>
            {dispatches.length === 0 ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada pengiriman</td></tr>
            ) : dispatches.map(d => (
              <>
                <tr
                  key={d.id}
                  onClick={() => setExpandedId(prev => prev === d.id ? null : d.id)}
                  style={{ cursor: 'pointer' }}
                  className={expandedId === d.id ? 'row-expanded' : ''}
                >
                  <td style={{ width: '28px', color: '#aaa', fontSize: '0.75rem', userSelect: 'none' }}>
                    {expandedId === d.id ? '▼' : '▶'}
                  </td>
                  <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmt(d.dispatched_at)}</td>
                  <td style={{ color: '#555' }}>{d.warehouse_name}</td>
                  <td style={{ fontWeight: 500 }}>{d.branch_name}</td>
                  <td style={{ color: '#555' }}>{d.division_name}</td>
                  <td><span className="badge">{d.items.length} item{d.items.length !== 1 ? 's' : ''}</span></td>
                  <td style={{ fontSize: '0.85rem', color: '#666' }}>{d.dispatched_by_name ?? '—'}</td>
                  <td style={{ fontSize: '0.85rem', color: '#888', fontStyle: d.notes ? 'normal' : 'italic' }}>{d.notes ?? '—'}</td>
                </tr>
                {expandedId === d.id && (
                  <tr key={`${d.id}-items`}>
                    <td colSpan={8} style={{ padding: '0.75rem 1.5rem 1rem', background: '#f8f9ff' }}>
                      <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#666', marginBottom: '0.5rem', textTransform: 'uppercase', letterSpacing: '0.4px' }}>
                        Barang Terkirim
                      </div>
                      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85rem' }}>
                        <thead>
                          <tr>
                            {['Barang', 'Kode', 'Jumlah', 'Satuan'].map(h => (
                              <th key={h} style={{ textAlign: 'left', padding: '0.3rem 0.6rem', color: '#888', fontWeight: 600, borderBottom: '1px solid #e8e8e8' }}>{h}</th>
                            ))}
                          </tr>
                        </thead>
                        <tbody>
                          {d.items.map(it => (
                            <tr key={it.id}>
                              <td style={{ padding: '0.3rem 0.6rem', fontWeight: 500 }}>{it.item_name}</td>
                              <td style={{ padding: '0.3rem 0.6rem', color: '#aaa', fontSize: '0.8rem' }}>{it.item_code}</td>
                              <td style={{ padding: '0.3rem 0.6rem', fontWeight: 600 }}>{Number(it.quantity).toLocaleString('id-ID')}</td>
                              <td style={{ padding: '0.3rem 0.6rem', color: '#555' }}>{it.unit_name}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </td>
                  </tr>
                )}
              </>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
