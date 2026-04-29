import { useEffect, useState, useCallback } from 'react';
import { getItems, getWarehouses, getInventory, getStockTransfers, createStockTransfer } from '../api';

const emptyRow = () => ({ item_id: '', quantity: '', unit_index: '0' });
const emptyHeader = { from_warehouse_id: '', to_warehouse_id: '', notes: '' };

export default function StockTransfers() {
  const [transfers, setTransfers] = useState([]);
  const [allItems, setAllItems] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [header, setHeader] = useState(emptyHeader);
  const [rows, setRows] = useState([emptyRow()]);
  const [srcInventory, setSrcInventory] = useState([]); // inventory records for source warehouse
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const loadTransfers = () => getStockTransfers().then(r => setTransfers(r.data));

  useEffect(() => {
    Promise.all([getItems(), getWarehouses(), getStockTransfers()]).then(([i, w, t]) => {
      setAllItems(i.data);
      setWarehouses(w.data);
      setTransfers(t.data);
    });
  }, []);

  // When source warehouse changes, load its inventory and reset rows
  useEffect(() => {
    if (!header.from_warehouse_id) { setSrcInventory([]); return; }
    getInventory({ warehouse_id: header.from_warehouse_id }).then(r => setSrcInventory(r.data));
    setRows([emptyRow()]);
  }, [header.from_warehouse_id]);

  // Items that have at least one inventory record in the source warehouse
  const availableItems = allItems.filter(it =>
    srcInventory.some(inv => inv.item_id === it.id)
  );

  // Get inventory records for a specific item in source warehouse
  const getItemStock = (item_id, unit_index) => {
    const rec = srcInventory.find(inv => inv.item_id === item_id && String(inv.unit_index) === String(unit_index));
    return rec ? Number(rec.quantity) : 0;
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

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!header.from_warehouse_id) { setError('Pilih gudang asal'); return; }
    if (!header.to_warehouse_id)   { setError('Pilih gudang tujuan'); return; }
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
      await createStockTransfer({
        from_warehouse_id: header.from_warehouse_id,
        to_warehouse_id: header.to_warehouse_id,
        notes: header.notes || null,
        items: rows.map(r => ({
          item_id: r.item_id,
          quantity: Number(r.quantity),
          unit_index: Number(r.unit_index),
        })),
      });
      setHeader(emptyHeader);
      setRows([emptyRow()]);
      setSrcInventory([]);
      loadTransfers();
    } catch (err) {
      setError(err.response?.data?.error || 'Transfer gagal');
    } finally {
      setLoading(false);
    }
  };

  const fmt = (d) => new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' });

  return (
    <>
      <div className="page-header">
        <h1>Transfer Stok</h1>
      </div>

      <div className="card" style={{ maxWidth: '860px', marginBottom: '1.5rem' }}>
        <h2 style={{ marginBottom: '1.25rem' }}>Transfer Baru</h2>
        {error && <div className="error-msg">{error}</div>}
        <form onSubmit={handleSubmit}>
          <div className="form-row" style={{ marginBottom: '1rem' }}>
            <div className="form-group" style={{ margin: 0 }}>
              <label>Dari Gudang</label>
              <select value={header.from_warehouse_id} onChange={setHeaderField('from_warehouse_id')}>
                <option value="">Pilih gudang...</option>
                {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
              </select>
            </div>
            <div className="form-group" style={{ margin: 0 }}>
              <label>Ke Gudang</label>
              <select value={header.to_warehouse_id} onChange={setHeaderField('to_warehouse_id')}>
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
                  // Only show unit options that have stock in source warehouse
                  const stockedUnits = selectedItem
                    ? selectedItem.units.map((u, ui) => ({ ...u, ui })).filter(u =>
                        srcInventory.some(inv => inv.item_id === row.item_id && inv.unit_index === u.ui)
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
              {loading ? 'Memproses...' : 'Transfer Stok'}
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
              <th>Waktu</th>
              <th>Barang</th>
              <th>Jumlah</th>
              <th>Dari</th>
              <th>Ke</th>
              <th>Oleh</th>
              <th>Catatan</th>
            </tr>
          </thead>
          <tbody>
            {transfers.length === 0 ? (
              <tr><td colSpan={7} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada transfer</td></tr>
            ) : transfers.map(t => (
              <tr key={t.id}>
                <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmt(t.transferred_at)}</td>
                <td>
                  <div style={{ fontWeight: 500 }}>{t.item_name}</div>
                  <div style={{ fontSize: '0.78rem', color: '#aaa' }}>{t.item_code}</div>
                </td>
                <td><span className="badge">{Number(t.quantity).toLocaleString('id-ID')} {t.unit_name}</span></td>
                <td style={{ color: '#555' }}>{t.from_warehouse_name}</td>
                <td style={{ color: '#555' }}>{t.to_warehouse_name}</td>
                <td style={{ fontSize: '0.85rem', color: '#666' }}>{t.transferred_by_name ?? '—'}</td>
                <td style={{ fontSize: '0.85rem', color: '#888', fontStyle: t.notes ? 'normal' : 'italic' }}>{t.notes ?? '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
