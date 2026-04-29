import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import * as XLSX from 'xlsx';
import { getWarehouses, getInventory, createStockOpname, getStockOpname } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const fmt = (d) => d ? new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' }) : '—';

export default function StockOpname() {
  const [warehouses, setWarehouses]   = useState([]);
  const [warehouseId, setWarehouseId] = useState('');
  const [inventory, setInventory]     = useState([]);
  const [actuals, setActuals]         = useState({});   // inventory_id → actual qty string
  const [notes, setNotes]             = useState('');
  const [operatorName, setOperatorName] = useState('');
  const [picName, setPicName]         = useState('');
  const [history, setHistory]         = useState([]);
  const [expandedId, setExpandedId]   = useState(null);
  const [error, setError]             = useState('');
  const [loading, setLoading]         = useState(false);

  useEffect(() => {
    getWarehouses().then(r => setWarehouses(r.data));
    getStockOpname().then(r => setHistory(r.data));
  }, []);

  const handleWarehouseChange = (e) => {
    const wid = e.target.value;
    setWarehouseId(wid);
    setActuals({});
    setError('');
    if (wid) getInventory({ warehouse_id: wid }).then(r => setInventory(r.data));
    else setInventory([]);
  };

  const setActual = (id, val) => setActuals(a => ({ ...a, [id]: val }));

  const diff = (rec) => {
    const actual = actuals[rec.id];
    if (actual === '' || actual === undefined) return null;
    return Number(actual) - Number(rec.quantity);
  };

  const hasChanges = inventory.some(rec => {
    const d = diff(rec);
    return d !== null && d !== 0;
  });

  const totalWaste = inventory.reduce((sum, rec) => {
    const d = diff(rec);
    if (d === null || d >= 0) return sum;
    const wasteVal = Math.round(Math.abs(Number(rec.value)) * Math.abs(d) / Number(rec.quantity));
    return sum + wasteVal;
  }, 0);

  const downloadTemplate = () => {
    const warehouseName = warehouses.find(w => w.id === warehouseId)?.name ?? 'Warehouse';
    const today = new Date().toLocaleDateString('id-ID', { dateStyle: 'long' });

    // Columns: No. | Item Name | Unit | Actual Qty
    const headerRow  = ['No.', 'Item Name', 'Unit', 'Actual Qty'];
    const dataRows   = inventory.map((rec, i) => [i + 1, rec.item_name, rec.unit_name, '']);

    const aoa = [
      [`Stock Opname — ${warehouseName}`],
      [`Tanggal: ${today}`],
      [`Person in Charge: _______________________________   Executor: _______________________________`],
      [],
      headerRow,
      ...dataRows,
    ];

    const ws = XLSX.utils.aoa_to_sheet(aoa);
    ws['!cols'] = [{ wch: 5 }, { wch: 34 }, { wch: 12 }, { wch: 14 }];

    // Merge title cell across all columns
    ws['!merges'] = [
      { s: { r: 0, c: 0 }, e: { r: 0, c: 3 } },
      { s: { r: 1, c: 0 }, e: { r: 1, c: 3 } },
      { s: { r: 2, c: 0 }, e: { r: 2, c: 3 } },
    ];

    const thin = { style: 'thin', color: { rgb: '000000' } };
    const border = { top: thin, bottom: thin, left: thin, right: thin };

    const headerStyle = {
      font:      { bold: true },
      fill:      { fgColor: { rgb: 'D9E1F2' }, patternType: 'solid' },
      alignment: { horizontal: 'center', vertical: 'center', wrapText: true },
      border,
    };
    const cellStyle = {
      alignment: { vertical: 'center', wrapText: false },
      border,
    };
    const numStyle  = { ...cellStyle, alignment: { ...cellStyle.alignment, horizontal: 'center' } };

    // Apply styles to header row (row index 4)
    headerRow.forEach((_, c) => {
      const addr = XLSX.utils.encode_cell({ r: 4, c });
      if (!ws[addr]) ws[addr] = { t: 's', v: headerRow[c] };
      ws[addr].s = headerStyle;
    });

    // Apply styles to data rows
    dataRows.forEach((row, ri) => {
      row.forEach((_, c) => {
        const addr = XLSX.utils.encode_cell({ r: 5 + ri, c });
        if (!ws[addr]) ws[addr] = { t: 's', v: '' };
        ws[addr].s = c === 0 ? numStyle : cellStyle;
      });
    });

    // Row heights
    ws['!rows'] = [
      { hpt: 22 }, { hpt: 18 }, { hpt: 18 }, { hpt: 6 }, { hpt: 22 },
      ...dataRows.map(() => ({ hpt: 20 })),
    ];

    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, ws, 'Stock Opname');
    XLSX.writeFile(wb, `opname-template-${warehouseName.replace(/\s+/g, '-')}.xlsx`, { cellStyles: true });
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    const changedItems = inventory
      .filter(rec => {
        const d = diff(rec);
        return d !== null && d !== 0;
      })
      .map(rec => ({
        inventory_id: rec.id,
        item_id: rec.item_id,
        unit_index: rec.unit_index,
        actual_quantity: Number(actuals[rec.id]),
      }));

    if (!changedItems.length) { setError('Tidak ada perubahan — semua aktual sesuai dengan catatan.'); return; }

    setLoading(true);
    try {
      await createStockOpname({ warehouse_id: warehouseId, notes: notes || null, operator_name: operatorName || null, pic_name: picName || null, items: changedItems });
      setActuals({});
      setNotes('');
      setOperatorName('');
      setPicName('');
      // Reload inventory and history
      getInventory({ warehouse_id: warehouseId }).then(r => setInventory(r.data));
      getStockOpname().then(r => setHistory(r.data));
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Stock Opname</h1>
      </div>

      {/* Opname form */}
      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div className="card-header" style={{ marginBottom: '1rem' }}>
          <h2>Opname Baru</h2>
          <div className="filters">
            <select value={warehouseId} onChange={handleWarehouseChange}>
              <option value="">Pilih gudang...</option>
              {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
            </select>
            {inventory.length > 0 && (
              <button type="button" onClick={downloadTemplate} className="btn btn-secondary">
                ⬇ Download Template
              </button>
            )}
          </div>
        </div>

        {!warehouseId ? (
          <p style={{ color: '#999', fontSize: '0.9rem', padding: '0.5rem 0' }}>Pilih gudang untuk memulai stock opname.</p>
        ) : inventory.length === 0 ? (
          <p style={{ color: '#999', fontSize: '0.9rem', padding: '0.5rem 0' }}>Tidak ada catatan inventaris di gudang ini.</p>
        ) : (
          <form onSubmit={handleSubmit}>
            {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

            <table>
              <thead>
                <tr>
                  <th>Barang</th>
                  <th>Kode</th>
                  <th>Satuan</th>
                  <th style={{ textAlign: 'right' }}>Qty Sistem</th>
                  <th style={{ textAlign: 'right' }}>Nilai Sistem</th>
                  <th style={{ textAlign: 'center' }}>Qty Aktual</th>
                  <th style={{ textAlign: 'right' }}>Selisih</th>
                  <th style={{ textAlign: 'right' }}>Nilai Susut</th>
                </tr>
              </thead>
              <tbody>
                {inventory.map(rec => {
                  const d = diff(rec);
                  const hasInput = actuals[rec.id] !== undefined && actuals[rec.id] !== '';
                  const wasteVal = (hasInput && d !== null && d < 0)
                    ? Math.round(Math.abs(Number(rec.value)) * Math.abs(d) / Number(rec.quantity))
                    : 0;
                  return (
                    <tr key={rec.id}>
                      <td style={{ fontWeight: 500 }}>{rec.item_name}</td>
                      <td style={{ color: '#aaa', fontSize: '0.82rem' }}>{rec.item_code}</td>
                      <td style={{ color: '#555' }}>{rec.unit_name}</td>
                      <td style={{ textAlign: 'right', fontWeight: 600 }}>
                        {Number(rec.quantity).toLocaleString('id-ID')}
                      </td>
                      <td style={{ textAlign: 'right', color: '#666' }}>{idr(rec.value)}</td>
                      <td style={{ textAlign: 'center', width: '120px' }}>
                        <input
                          type="number"
                          min="0"
                          step="any"
                          value={actuals[rec.id] ?? ''}
                          onChange={e => setActual(rec.id, e.target.value)}
                          placeholder={Number(rec.quantity).toLocaleString('id-ID')}
                          style={{ width: '100%', padding: '0.35rem 0.5rem', border: '1px solid #ddd', borderRadius: '5px', fontSize: '0.9rem', textAlign: 'right' }}
                        />
                      </td>
                      <td style={{ textAlign: 'right', fontWeight: 600, color: d === null || d === 0 ? '#aaa' : d > 0 ? '#27ae60' : '#e74c3c' }}>
                        {d === null || d === 0 ? '—' : (d > 0 ? '+' : '') + d.toLocaleString('id-ID')}
                      </td>
                      <td style={{ textAlign: 'right', color: wasteVal > 0 ? '#e74c3c' : '#aaa', fontWeight: wasteVal > 0 ? 600 : 400 }}>
                        {wasteVal > 0 ? idr(wasteVal) : '—'}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
              {totalWaste > 0 && (
                <tfoot>
                  <tr>
                    <td colSpan={7} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>
                      Total Nilai Susut:
                    </td>
                    <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#e74c3c' }}>
                      {idr(totalWaste)}
                    </td>
                  </tr>
                </tfoot>
              )}
            </table>

            <div style={{ marginTop: '1.25rem', display: 'flex', gap: '1rem', alignItems: 'flex-end' }}>
              <div className="form-group" style={{ flex: 1, margin: 0 }}>
                <label>Penanggung Jawab <span style={{ color: '#aaa', fontWeight: 400 }}>(supervisor)</span></label>
                <input value={picName} onChange={e => setPicName(e.target.value)} placeholder="mis. Ahmad Fauzi..." />
              </div>
              <div className="form-group" style={{ flex: 1, margin: 0 }}>
                <label>Pelaksana <span style={{ color: '#aaa', fontWeight: 400 }}>(penghitung)</span></label>
                <input value={operatorName} onChange={e => setOperatorName(e.target.value)} placeholder="mis. Budi Santoso..." />
              </div>
              <div className="form-group" style={{ flex: 1, margin: 0 }}>
                <label>Catatan <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
                <input value={notes} onChange={e => setNotes(e.target.value)} placeholder="mis. Penghitungan stok bulanan..." />
              </div>
              <button
                type="submit"
                className="btn btn-primary"
                disabled={loading || !hasChanges}
                style={{ whiteSpace: 'nowrap' }}
              >
                {loading ? 'Menyimpan…' : 'Konfirmasi Opname'}
              </button>
            </div>
          </form>
        )}
      </div>

      {/* Opname history */}
      <div className="card">
        <div className="card-header"><h2>Riwayat Opname</h2></div>
        <table>
          <thead>
            <tr>
              <th></th>
              <th>Waktu</th>
              <th>Gudang</th>
              <th>Barang disesuaikan</th>
              <th>PIC</th>
              <th>Pelaksana</th>
              <th>Dicatat oleh</th>
              <th>Catatan</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {history.length === 0 ? (
              <tr><td colSpan={9} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada opname</td></tr>
            ) : history.map(op => (
              <>
                <tr
                  key={op.id}
                  onClick={() => setExpandedId(p => p === op.id ? null : op.id)}
                  style={{ cursor: 'pointer' }}
                  className={expandedId === op.id ? 'row-expanded' : ''}
                >
                  <td style={{ width: '28px', color: '#aaa', fontSize: '0.75rem', userSelect: 'none' }}>
                    {expandedId === op.id ? '▼' : '▶'}
                  </td>
                  <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmt(op.performed_at)}</td>
                  <td style={{ fontWeight: 500 }}>{op.warehouse_name}</td>
                  <td><span className="badge">{op.items.length} item{op.items.length !== 1 ? 's' : ''}</span></td>
                  <td style={{ fontSize: '0.85rem', color: '#555' }}>{op.pic_name ?? <span style={{color:'#bbb',fontStyle:'italic'}}>—</span>}</td>
                  <td style={{ fontSize: '0.85rem', color: '#555' }}>{op.operator_name ?? <span style={{color:'#bbb',fontStyle:'italic'}}>—</span>}</td>
                  <td style={{ fontSize: '0.85rem', color: '#666' }}>{op.performed_by_name ?? '—'}</td>
                  <td style={{ fontSize: '0.85rem', color: '#888', fontStyle: op.notes ? 'normal' : 'italic' }}>{op.notes ?? '—'}</td>
                  <td><Link to={`/stock-opname/detail/${op.id}`} className="btn btn-secondary btn-sm">Lihat</Link></td>
                </tr>
                {expandedId === op.id && (
                  <tr key={`${op.id}-items`}>
                    <td colSpan={9} style={{ padding: '0.75rem 1.5rem 1rem', background: '#f8f9ff' }}>
                      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85rem' }}>
                        <thead>
                          <tr>
                            {['Barang', 'Satuan', 'Tercatat', 'Aktual', 'Selisih', 'Nilai Susut'].map(h => (
                              <th key={h} style={{ textAlign: h === 'Barang' || h === 'Satuan' ? 'left' : 'right', padding: '0.3rem 0.6rem', color: '#888', fontWeight: 600, borderBottom: '1px solid #e8e8e8' }}>{h}</th>
                            ))}
                          </tr>
                        </thead>
                        <tbody>
                          {op.items.map(it => (
                            <tr key={it.id}>
                              <td style={{ padding: '0.3rem 0.6rem', fontWeight: 500 }}>{it.item_name}</td>
                              <td style={{ padding: '0.3rem 0.6rem', color: '#555' }}>{it.unit_name}</td>
                              <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right' }}>{Number(it.recorded_quantity).toLocaleString('id-ID')}</td>
                              <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right' }}>{Number(it.actual_quantity).toLocaleString('id-ID')}</td>
                              <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right', fontWeight: 600, color: Number(it.difference) >= 0 ? '#27ae60' : '#e74c3c' }}>
                                {Number(it.difference) > 0 ? '+' : ''}{Number(it.difference).toLocaleString('id-ID')}
                              </td>
                              <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right', color: Number(it.waste_value) > 0 ? '#e74c3c' : '#aaa' }}>
                                {Number(it.waste_value) > 0 ? idr(it.waste_value) : '—'}
                              </td>
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
