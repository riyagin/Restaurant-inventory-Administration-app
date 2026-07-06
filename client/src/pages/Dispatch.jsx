import { useEffect, useRef, useState } from 'react';
import { getItems, getWarehouses, getAllInventory, getBranches, getDivisions, getDispatches, createDispatch, updateDispatch, deleteDispatch } from '../api';

const emptyRow = () => ({ item_id: '', quantity: '', unit_index: '0' });
const today = () => new Date().toISOString().slice(0, 10);
const emptyHeader = { warehouse_id: '', branch_id: '', division_id: '', notes: '', dispatch_date: today() };

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
  const [editingId, setEditingId] = useState(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  // When populating the form for an edit, suppress the warehouse/branch effects
  // that would otherwise wipe the rows and selected division.
  const loadingEditRef = useRef(false);

  const isEditing = !!editingId;
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
    if (!header.warehouse_id) { setSrcInventory([]); if (!loadingEditRef.current) setRows([emptyRow()]); return; }
    getAllInventory({ warehouse_id: header.warehouse_id }).then(setSrcInventory);
    if (!loadingEditRef.current) setRows([emptyRow()]);
  }, [header.warehouse_id]);

  // Load divisions when branch changes
  useEffect(() => {
    if (!header.branch_id) { setDivisions([]); return; }
    getDivisions({ branch_id: header.branch_id }).then(r => setDivisions(r.data));
    if (!loadingEditRef.current) setHeader(h => ({ ...h, division_id: '' }));
    // Both effects fire on an edit-load; clear the guard in this (later) one.
    loadingEditRef.current = false;
  }, [header.branch_id]);

  // Show items available in the warehouse, plus any already on the edited rows.
  const availableItems = allItems.filter(it =>
    srcInventory.some(inv => inv.item_id === it.id) || rows.some(r => r.item_id === it.id)
  );

  const getItemStock = (item_id, unit_index) => {
    return srcInventory
      .filter(inv => inv.item_id === item_id && String(inv.unit_index) === String(unit_index))
      .reduce((sum, inv) => sum + Number(inv.quantity), 0);
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

  const resetForm = () => {
    setEditingId(null);
    setHeader(emptyHeader);
    setRows([emptyRow()]);
    setSrcInventory([]);
    setDivisions([]);
    setError('');
  };

  const startEdit = (d) => {
    setError('');
    setEditingId(d.id);
    // Suppress the reset side-effects of the warehouse/branch effects while we
    // populate the form from the existing dispatch.
    loadingEditRef.current = true;
    setHeader({
      warehouse_id: d.warehouse_id ?? '',
      branch_id: d.branch_id ?? '',
      division_id: d.division_id ?? '',
      notes: d.notes ?? '',
      // Preload the dispatch's own date so editing (e.g. just the notes) does
      // not silently move it to today.
      dispatch_date: d.dispatched_at ? new Date(d.dispatched_at).toISOString().slice(0, 10) : today(),
    });
    setRows(d.items.map(it => ({
      item_id: it.item_id,
      quantity: String(Number(it.quantity)),
      unit_index: String(it.unit_index),
    })));
    // Ensure the division dropdown is populated for the target branch.
    if (d.branch_id) getDivisions({ branch_id: d.branch_id }).then(r => setDivisions(r.data));
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  const handleDelete = async (d) => {
    if (!window.confirm('Batalkan pengiriman ini? Stok akan dikembalikan dan pembukuan direverse. Catatan tetap disimpan.')) return;
    setError('');
    try {
      await deleteDispatch(d.id);
      if (editingId === d.id) resetForm();
      loadDispatches();
    } catch (err) {
      setError(err.response?.data?.error || 'Pembatalan gagal');
    }
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!header.warehouse_id) { setError('Pilih gudang asal'); return; }
    if (!header.branch_id)    { setError('Pilih cabang'); return; }
    if (!header.division_id)  { setError('Pilih divisi'); return; }
    for (const [i, row] of rows.entries()) {
      if (!row.item_id) { setError(`Baris ${i + 1}: pilih barang`); return; }
      if (!row.quantity || Number(row.quantity) <= 0) { setError(`Baris ${i + 1}: jumlah harus lebih dari 0`); return; }
      // When editing, the current dispatch already holds stock, so the
      // available figure understates what can be dispatched; let the backend
      // enforce real availability via FIFO.
      if (!isEditing) {
        const available = getItemStock(row.item_id, row.unit_index);
        if (Number(row.quantity) > available) {
          const item = allItems.find(it => it.id === row.item_id);
          const unitName = item?.units[Number(row.unit_index)]?.name ?? '';
          setError(`Baris ${i + 1}: stok "${item?.name}" tidak cukup (tersedia: ${available.toLocaleString('id-ID')} ${unitName})`);
          return;
        }
      }
    }
    setLoading(true);
    try {
      const payload = {
        warehouse_id: header.warehouse_id,
        branch_id: header.branch_id,
        division_id: header.division_id,
        notes: header.notes || null,
        dispatch_date: header.dispatch_date || today(),
        items: rows.map(r => {
          const item = allItems.find(it => it.id === r.item_id);
          const unitName = item?.units[Number(r.unit_index)]?.name ?? '';
          return { item_id: r.item_id, quantity: Number(r.quantity), unit_index: Number(r.unit_index), unit_name: unitName };
        }),
      };
      if (isEditing) {
        await updateDispatch(editingId, payload);
      } else {
        await createDispatch(payload);
      }
      resetForm();
      loadDispatches();
    } catch (err) {
      setError(err.response?.data?.error || (isEditing ? 'Perubahan gagal' : 'Pengiriman gagal'));
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
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.25rem' }}>
          <h2 style={{ margin: 0 }}>{isEditing ? 'Edit Pengiriman' : 'Pengiriman Baru'}</h2>
          {isEditing && (
            <button type="button" onClick={resetForm} className="btn btn-secondary btn-sm">Batal Edit</button>
          )}
        </div>
        {error && <div className="error-msg">{error}</div>}
        <form onSubmit={handleSubmit}>
          <div className="form-row" style={{ marginBottom: '1rem' }}>
            <div className="form-group" style={{ margin: 0 }}>
              <label>Dari Gudang {isEditing && <span style={{ color: '#aaa', fontWeight: 400 }}>(tidak dapat diubah)</span>}</label>
              <select value={header.warehouse_id} onChange={setHeaderField('warehouse_id')} disabled={isEditing}>
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

          <div className="form-row" style={{ marginBottom: '1rem' }}>
            <div className="form-group" style={{ margin: 0 }}>
              <label>Catatan <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
              <input value={header.notes} onChange={setHeaderField('notes')} placeholder="Alasan atau deskripsi..." />
            </div>
            <div className="form-group" style={{ margin: 0, minWidth: '170px', maxWidth: '200px' }}>
              <label>Tanggal Pengiriman</label>
              <input type="date" value={header.dispatch_date} onChange={setHeaderField('dispatch_date')} max={today()} />
            </div>
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
                        srcInventory.some(inv => inv.item_id === row.item_id && inv.unit_index === u.ui) ||
                        String(u.ui) === String(row.unit_index)
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
                          max={!isEditing && available !== null ? available : undefined}
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
              {loading ? 'Memproses...' : isEditing ? 'Simpan Perubahan' : 'Kirim Stok'}
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
                  <td>
                    <span className="badge">{d.items.length} item{d.items.length !== 1 ? 's' : ''}</span>
                    {d.status === 'cancelled' && (
                      <span className="badge" style={{ marginLeft: '0.35rem', background: '#fdecea', color: '#c0392b' }}>Dibatalkan</span>
                    )}
                  </td>
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
                      <div style={{ marginTop: '0.85rem', display: 'flex', gap: '0.5rem' }}>
                        {d.status === 'cancelled' ? (
                          <span style={{ fontSize: '0.82rem', color: '#c0392b', fontStyle: 'italic' }}>
                            Pengiriman ini telah dibatalkan. Stok telah dikembalikan dan pembukuan direverse.
                          </span>
                        ) : (
                          <>
                            <button type="button" className="btn btn-secondary btn-sm"
                              onClick={(e) => { e.stopPropagation(); startEdit(d); }}>
                              Edit
                            </button>
                            <button type="button" className="btn btn-danger btn-sm"
                              onClick={(e) => { e.stopPropagation(); handleDelete(d); }}>
                              Batalkan
                            </button>
                          </>
                        )}
                      </div>
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
