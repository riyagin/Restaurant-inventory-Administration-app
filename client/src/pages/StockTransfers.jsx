import { useEffect, useMemo, useRef, useState } from 'react';
import {
  getItems, getWarehouses, getAllInventory, getStockTransfers,
  createStockTransfer, updateStockTransfer, deleteStockTransfer,
} from '../api';

const emptyRow = () => ({ item_id: '', quantity: '', unit_index: '0' });
const emptyHeader = { from_warehouse_id: '', to_warehouse_id: '', notes: '' };

export default function StockTransfers() {
  const [transfers, setTransfers] = useState([]);
  const [allItems, setAllItems] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [header, setHeader] = useState(emptyHeader);
  const [rows, setRows] = useState([emptyRow()]);
  const [srcInventory, setSrcInventory] = useState([]); // inventory records for source warehouse
  const [expandedId, setExpandedId] = useState(null);
  const [editingId, setEditingId] = useState(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  // When populating the form for an edit, suppress the source-warehouse effect
  // that would otherwise wipe the rows we just loaded.
  const loadingEditRef = useRef(false);

  const isEditing = !!editingId;
  const loadTransfers = () => getStockTransfers().then(r => setTransfers(r.data));

  useEffect(() => {
    Promise.all([getItems(), getWarehouses(), getStockTransfers()]).then(([i, w, t]) => {
      setAllItems(i.data);
      setWarehouses(w.data);
      setTransfers(t.data);
    });
  }, []);

  // When source warehouse changes, load its inventory (and reset rows unless we
  // are populating the form for an edit).
  useEffect(() => {
    if (!header.from_warehouse_id) {
      setSrcInventory([]);
      if (!loadingEditRef.current) setRows([emptyRow()]);
      return;
    }
    getAllInventory({ warehouse_id: header.from_warehouse_id }).then(setSrcInventory);
    if (!loadingEditRef.current) setRows([emptyRow()]);
    loadingEditRef.current = false;
  }, [header.from_warehouse_id]);

  // Group the flat transfer rows into one entry per transfer (group_id). Edits
  // append signed correction rows, so quantities are netted per item+unit and
  // any item that nets to zero (fully removed on an edit) is dropped.
  const groups = useMemo(() => {
    const map = new Map();
    for (const t of transfers) {
      const gid = t.group_id;
      if (!map.has(gid)) {
        map.set(gid, {
          group_id: gid,
          from_warehouse_id: t.from_warehouse_id,
          from_warehouse_name: t.from_warehouse_name,
          to_warehouse_id: t.to_warehouse_id,
          to_warehouse_name: t.to_warehouse_name,
          notes: t.notes,
          status: t.status || 'active',
          transferred_at: t.transferred_at,
          transferred_by_name: t.transferred_by_name,
          _items: new Map(),
        });
      }
      const g = map.get(gid);
      const key = `${t.item_id}|${t.unit_index}`;
      const existing = g._items.get(key);
      if (existing) {
        existing.quantity += Number(t.quantity);
      } else {
        g._items.set(key, {
          id: t.id, item_id: t.item_id, item_name: t.item_name,
          quantity: Number(t.quantity), unit_index: t.unit_index, unit_name: t.unit_name,
        });
      }
    }
    return Array.from(map.values()).map(({ _items, ...g }) => ({
      ...g,
      items: Array.from(_items.values()).filter(it => Math.abs(it.quantity) > 1e-9),
    }));
  }, [transfers]);

  // Items with stock in the source warehouse, plus any already on the edited rows.
  const availableItems = allItems.filter(it =>
    srcInventory.some(inv => inv.item_id === it.id) || rows.some(r => r.item_id === it.id)
  );

  // Stock of an item+unit in the source warehouse.
  const getItemStock = (item_id, unit_index) => {
    return srcInventory
      .filter(inv => inv.item_id === item_id && String(inv.unit_index) === String(unit_index))
      .reduce((sum, inv) => sum + Number(inv.quantity), 0);
  };

  const setHeaderField = (field) => (e) => {
    const val = e.target.value;
    setHeader(h => ({ ...h, [field]: val }));
  };

  const setRow = (index, field) => (e) => {
    const val = e.target.value;
    setRows(rs => rs.map((r, i) => {
      if (i !== index) return r;
      const updated = { ...r, [field]: val };
      if (field === 'item_id') {
        // Default to the unit_index that has stock in source warehouse
        const stockRec = srcInventory.find(inv => inv.item_id === val);
        updated.unit_index = stockRec ? String(stockRec.unit_index) : '0';
        updated.quantity = '';
      }
      if (field === 'unit_index') {
        updated.quantity = '';
      }
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
    setError('');
  };

  const startEdit = (g) => {
    setError('');
    setEditingId(g.group_id);
    // Suppress the source-warehouse effect's row reset while we populate the form.
    loadingEditRef.current = true;
    setHeader({
      from_warehouse_id: g.from_warehouse_id ?? '',
      to_warehouse_id: g.to_warehouse_id ?? '',
      notes: g.notes ?? '',
    });
    setRows(g.items.map(it => ({
      item_id: it.item_id,
      quantity: String(Number(it.quantity)),
      unit_index: String(it.unit_index),
    })));
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  const handleDelete = async (g) => {
    if (!window.confirm('Batalkan transfer ini? Stok akan dikembalikan ke gudang asal dan pembukuan direverse. Catatan tetap disimpan.')) return;
    setError('');
    try {
      await deleteStockTransfer(g.group_id);
      if (editingId === g.group_id) resetForm();
      loadTransfers();
    } catch (err) {
      setError(err.response?.data?.error || 'Pembatalan gagal');
    }
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!header.from_warehouse_id) { setError('Pilih gudang asal'); return; }
    if (!header.to_warehouse_id)   { setError('Pilih gudang tujuan'); return; }
    if (header.from_warehouse_id === header.to_warehouse_id) { setError('Gudang asal dan tujuan harus berbeda'); return; }
    for (const [i, row] of rows.entries()) {
      if (!row.item_id) { setError(`Baris ${i + 1}: pilih barang`); return; }
      if (!row.quantity || Number(row.quantity) <= 0) { setError(`Baris ${i + 1}: jumlah harus lebih dari 0`); return; }
      // When editing, the existing transfer already holds stock at the destination,
      // so the source availability understates what can be transferred; let the
      // backend enforce real availability via FIFO.
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
      const items = rows.map(r => {
        const item = allItems.find(it => it.id === r.item_id);
        const unitName = item?.units[Number(r.unit_index)]?.name ?? '';
        return { item_id: r.item_id, quantity: Number(r.quantity), unit_index: Number(r.unit_index), unit_name: unitName };
      });
      if (isEditing) {
        await updateStockTransfer(editingId, { notes: header.notes || null, items });
      } else {
        await createStockTransfer({
          from_warehouse_id: header.from_warehouse_id,
          to_warehouse_id: header.to_warehouse_id,
          notes: header.notes || null,
          items,
        });
      }
      resetForm();
      loadTransfers();
    } catch (err) {
      setError(err.response?.data?.error || (isEditing ? 'Perubahan gagal' : 'Transfer gagal'));
    } finally {
      setLoading(false);
    }
  };

  const fmt = (d) => new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' });

  return (
    <>
      <div className="page-header">
        <h1>Transfer Gudang</h1>
      </div>

      <div className="card" style={{ maxWidth: '860px', marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.25rem' }}>
          <h2 style={{ margin: 0 }}>{isEditing ? 'Edit Transfer' : 'Transfer Baru'}</h2>
          {isEditing && (
            <button type="button" onClick={resetForm} className="btn btn-secondary btn-sm">Batal Edit</button>
          )}
        </div>
        {error && <div className="error-msg">{error}</div>}
        <form onSubmit={handleSubmit}>
          <div className="form-row" style={{ marginBottom: '1rem' }}>
            <div className="form-group" style={{ margin: 0 }}>
              <label>Dari Gudang {isEditing && <span style={{ color: '#aaa', fontWeight: 400 }}>(tidak dapat diubah)</span>}</label>
              <select value={header.from_warehouse_id} onChange={setHeaderField('from_warehouse_id')} disabled={isEditing}>
                <option value="">Pilih gudang...</option>
                {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
              </select>
            </div>
            <div className="form-group" style={{ margin: 0 }}>
              <label>Ke Gudang {isEditing && <span style={{ color: '#aaa', fontWeight: 400 }}>(tidak dapat diubah)</span>}</label>
              <select value={header.to_warehouse_id} onChange={setHeaderField('to_warehouse_id')} disabled={isEditing}>
                <option value="">Pilih gudang...</option>
                {warehouses.filter(w => w.id !== header.from_warehouse_id).map(w => (
                  <option key={w.id} value={w.id}>{w.name}</option>
                ))}
              </select>
            </div>
            <div className="form-group" style={{ margin: 0 }}>
              <label>Catatan <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
              <input value={header.notes} onChange={setHeaderField('notes')} placeholder="Alasan transfer..." />
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
                  // Only show unit options that have stock in source warehouse (plus
                  // the row's current unit when editing).
                  const stockedUnits = selectedItem
                    ? selectedItem.units.map((u, ui) => ({ ...u, ui })).filter(u =>
                        srcInventory.some(inv => inv.item_id === row.item_id && inv.unit_index === u.ui) ||
                        String(u.ui) === String(row.unit_index)
                      )
                    : [];

                  return (
                    <tr key={i}>
                      <td style={{ minWidth: '220px' }}>
                        <select
                          value={row.item_id}
                          onChange={setRow(i, 'item_id')}
                          style={{ width: '100%' }}
                          disabled={!header.from_warehouse_id}
                        >
                          <option value="">
                            {header.from_warehouse_id ? 'Pilih barang...' : 'Pilih gudang asal terlebih dahulu'}
                          </option>
                          {availableItems.map(it => (
                            <option key={it.id} value={it.id}>{it.name}</option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: '130px' }}>
                        <select
                          value={row.unit_index}
                          onChange={setRow(i, 'unit_index')}
                          disabled={!selectedItem}
                          style={{ width: '100%' }}
                        >
                          {stockedUnits.length > 0
                            ? stockedUnits.map(u => (
                                <option key={u.ui} value={String(u.ui)}>{u.name}</option>
                              ))
                            : <option value="0">—</option>
                          }
                        </select>
                      </td>
                      <td style={{ minWidth: '140px' }}>
                        <input
                          type="number"
                          min="0.001"
                          step="any"
                          value={row.quantity}
                          onChange={setRow(i, 'quantity')}
                          placeholder="0"
                          style={{ width: '100%' }}
                          disabled={!selectedItem}
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
            <button type="button" onClick={addRow} className="btn btn-secondary" disabled={!header.from_warehouse_id}>
              + Tambah Baris
            </button>
            <button type="submit" className="btn btn-primary" disabled={loading}>
              {loading ? 'Memproses...' : isEditing ? 'Simpan Perubahan' : 'Transfer Gudang'}
            </button>
          </div>
        </form>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>Riwayat Transfer</h2>
        </div>
        <table>
          <thead>
            <tr>
              <th></th>
              <th>Waktu</th>
              <th>Dari</th>
              <th>Ke</th>
              <th>Barang</th>
              <th>Oleh</th>
              <th>Catatan</th>
            </tr>
          </thead>
          <tbody>
            {groups.length === 0 ? (
              <tr><td colSpan={7} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada transfer</td></tr>
            ) : groups.map(g => (
              <>
                <tr
                  key={g.group_id}
                  onClick={() => setExpandedId(prev => prev === g.group_id ? null : g.group_id)}
                  style={{ cursor: 'pointer' }}
                  className={expandedId === g.group_id ? 'row-expanded' : ''}
                >
                  <td style={{ width: '28px', color: '#aaa', fontSize: '0.75rem', userSelect: 'none' }}>
                    {expandedId === g.group_id ? '▼' : '▶'}
                  </td>
                  <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmt(g.transferred_at)}</td>
                  <td style={{ color: '#555' }}>{g.from_warehouse_name}</td>
                  <td style={{ color: '#555' }}>{g.to_warehouse_name}</td>
                  <td>
                    <span className="badge">{g.items.length} item{g.items.length !== 1 ? 's' : ''}</span>
                    {g.status === 'cancelled' && (
                      <span className="badge" style={{ marginLeft: '0.35rem', background: '#fdecea', color: '#c0392b' }}>Dibatalkan</span>
                    )}
                  </td>
                  <td style={{ fontSize: '0.85rem', color: '#666' }}>{g.transferred_by_name ?? '—'}</td>
                  <td style={{ fontSize: '0.85rem', color: '#888', fontStyle: g.notes ? 'normal' : 'italic' }}>{g.notes ?? '—'}</td>
                </tr>
                {expandedId === g.group_id && (
                  <tr key={`${g.group_id}-items`}>
                    <td colSpan={7} style={{ padding: '0.75rem 1.5rem 1rem', background: '#f8f9ff' }}>
                      <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#666', marginBottom: '0.5rem', textTransform: 'uppercase', letterSpacing: '0.4px' }}>
                        Barang Ditransfer
                      </div>
                      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85rem' }}>
                        <thead>
                          <tr>
                            {['Barang', 'Jumlah', 'Satuan'].map(h => (
                              <th key={h} style={{ textAlign: 'left', padding: '0.3rem 0.6rem', color: '#888', fontWeight: 600, borderBottom: '1px solid #e8e8e8' }}>{h}</th>
                            ))}
                          </tr>
                        </thead>
                        <tbody>
                          {g.items.map(it => (
                            <tr key={it.id}>
                              <td style={{ padding: '0.3rem 0.6rem', fontWeight: 500 }}>{it.item_name}</td>
                              <td style={{ padding: '0.3rem 0.6rem', fontWeight: 600 }}>{Number(it.quantity).toLocaleString('id-ID')}</td>
                              <td style={{ padding: '0.3rem 0.6rem', color: '#555' }}>{it.unit_name}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                      <div style={{ marginTop: '0.85rem', display: 'flex', gap: '0.5rem' }}>
                        {g.status === 'cancelled' ? (
                          <span style={{ fontSize: '0.82rem', color: '#c0392b', fontStyle: 'italic' }}>
                            Transfer ini telah dibatalkan. Stok telah dikembalikan dan pembukuan direverse.
                          </span>
                        ) : (
                          <>
                            <button type="button" className="btn btn-secondary btn-sm"
                              onClick={(e) => { e.stopPropagation(); startEdit(g); }}>
                              Edit
                            </button>
                            <button type="button" className="btn btn-danger btn-sm"
                              onClick={(e) => { e.stopPropagation(); handleDelete(g); }}>
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
