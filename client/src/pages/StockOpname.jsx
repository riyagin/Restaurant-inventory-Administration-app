import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import XLSX from 'xlsx-js-style';
import {
  getWarehouses, getAllInventory, createStockOpname, getStockOpname,
  getStockOpnameDrafts, createStockOpnameDraft, updateStockOpnameDraft, deleteStockOpnameDraft,
} from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const fmt = (d) => d ? new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' }) : '—';

function groupInventory(inventory) {
  const map = new Map();
  const sorted = [...inventory].reverse(); // oldest-first for FIFO
  for (const rec of sorted) {
    const key = `${rec.item_id}__${rec.unit_index}`;
    if (!map.has(key)) {
      map.set(key, {
        key,
        item_id: rec.item_id,
        item_name: rec.item_name,
        item_code: rec.item_code,
        unit_name: rec.unit_name,
        unit_index: rec.unit_index,
        totalQty: 0,
        totalValue: 0,
        lots: [],
      });
    }
    const g = map.get(key);
    g.totalQty   += Number(rec.quantity);
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
    if (remaining <= 0) {
      waste += lotVal;
    } else if (remaining >= lotQty) {
      remaining -= lotQty;
    } else {
      const consumed = lotQty - remaining;
      waste += Math.round(lotVal * consumed / lotQty);
      remaining = 0;
    }
  }
  return waste;
}

export default function StockOpname() {
  // Screen: 'warehouse-select' | 'form'
  const [screen, setScreen]             = useState('warehouse-select');
  const [warehouses, setWarehouses]     = useState([]);
  const [selectedWh, setSelectedWh]     = useState(null); // { id, name }
  const [inventory, setInventory]       = useState([]);
  const [actuals, setActuals]           = useState({});
  const [picName, setPicName]           = useState('');
  const [operatorName, setOperatorName] = useState('');
  const [notes, setNotes]               = useState('');
  const [history, setHistory]           = useState([]);
  const [drafts, setDrafts]             = useState([]);
  const [currentDraftId, setCurrentDraftId] = useState(null);
  const [expandedId, setExpandedId]     = useState(null);
  const [error, setError]               = useState('');
  const [loading, setLoading]           = useState(false);
  const [saveMsg, setSaveMsg]           = useState('');

  const groups = groupInventory(inventory);

  const reloadData = useCallback(() => {
    getStockOpname().then(r => setHistory(r.data));
    getStockOpnameDrafts().then(r => setDrafts(r.data));
  }, []);

  useEffect(() => {
    getWarehouses().then(r => setWarehouses(r.data));
    reloadData();
  }, [reloadData]);

  // ── Warehouse selection ──────────────────────────────────────────────────────

  const selectWarehouse = (wh) => {
    setSelectedWh(wh);
    setActuals({});
    setError('');
    setCurrentDraftId(null);
    setPicName('');
    setOperatorName('');
    setNotes('');
    getAllInventory({ warehouse_id: wh.id }).then(rows => {
      setInventory(rows);
      setScreen('form');
    });
  };

  const resumeDraft = (draft) => {
    const wh = warehouses.find(w => w.id === draft.warehouse_id);
    if (!wh) return;
    setSelectedWh(wh);
    setPicName(draft.pic_name ?? '');
    setOperatorName(draft.operator_name ?? '');
    setNotes(draft.notes ?? '');
    setCurrentDraftId(draft.id);
    setError('');

    const draftActuals = {};
    if (Array.isArray(draft.items)) {
      for (const it of draft.items) {
        const key = `${it.item_id}__${it.unit_index}`;
        draftActuals[key] = String(it.actual_qty);
      }
    }

    getAllInventory({ warehouse_id: wh.id }).then(rows => {
      setInventory(rows);
      setActuals(draftActuals);
      setScreen('form');
    });
  };

  const backToWarehouseSelect = () => {
    setScreen('warehouse-select');
    setSelectedWh(null);
    setInventory([]);
    setActuals({});
    setCurrentDraftId(null);
    setError('');
  };

  // ── Derived values ───────────────────────────────────────────────────────────

  const setActual = (key, val) => setActuals(a => ({ ...a, [key]: val }));

  const diff = (group) => {
    const actual = actuals[group.key];
    if (actual === '' || actual === undefined) return null;
    return Number(actual) - group.totalQty;
  };

  const hasChanges = groups.some(g => {
    const d = diff(g);
    return d !== null && d !== 0;
  });

  const totalWaste = groups.reduce((sum, g) => {
    const d = diff(g);
    if (d === null || d >= 0) return sum;
    return sum + computeGroupWaste(g, Number(actuals[g.key]));
  }, 0);

  // ── Download template ────────────────────────────────────────────────────────

  const downloadTemplate = () => {
    const warehouseName = selectedWh?.name ?? 'Warehouse';
    const todayStr = new Date().toLocaleDateString('id-ID', { dateStyle: 'long' });
    const COLS = 4;
    const headerRow = ['No.', 'Nama Barang', 'Satuan', 'Qty Aktual'];
    const dataRows  = groups.map((g, i) => [i + 1, g.item_name, g.unit_name ?? '', '']);
    const aoa = [
      [`Stock Opname — ${warehouseName}`, '', '', ''],
      [`Tanggal: ${todayStr}`, '', '', ''],
      [`Person in Charge: _______________________________   Pelaksana: _______________________________`, '', '', ''],
      ['', '', '', ''],
      headerRow,
      ...dataRows,
    ];
    const ws = XLSX.utils.aoa_to_sheet(aoa);
    ws['!cols'] = [{ wch: 5 }, { wch: 32 }, { wch: 12 }, { wch: 14 }];
    ws['!merges'] = [
      { s: { r: 0, c: 0 }, e: { r: 0, c: COLS - 1 } },
      { s: { r: 1, c: 0 }, e: { r: 1, c: COLS - 1 } },
      { s: { r: 2, c: 0 }, e: { r: 2, c: COLS - 1 } },
    ];
    const thin   = { style: 'thin',   color: { rgb: 'AAAAAA' } };
    const medium = { style: 'medium', color: { rgb: '000000' } };
    const border = { top: thin, bottom: thin, left: thin, right: thin };
    const outerBorder = { top: medium, bottom: medium, left: medium, right: medium };
    const setCell = (addr, t, v, s) => { ws[addr] = { t, v, s }; };
    setCell('A1', 's', `Stock Opname — ${warehouseName}`, { font: { bold: true, sz: 13 }, alignment: { horizontal: 'center', vertical: 'center' } });
    setCell('A2', 's', `Tanggal: ${todayStr}`, { font: { sz: 10 }, alignment: { horizontal: 'left', vertical: 'center' } });
    setCell('A3', 's', 'Person in Charge: _______________________________   Pelaksana: _______________________________', { font: { sz: 9 }, alignment: { horizontal: 'left', vertical: 'center' } });
    const headerStyle = { font: { bold: true, sz: 10 }, alignment: { horizontal: 'center', vertical: 'center', wrapText: true }, border: outerBorder };
    headerRow.forEach((v, c) => { setCell(XLSX.utils.encode_cell({ r: 4, c }), 's', v, headerStyle); });
    const base = { font: { sz: 10 }, border };
    const colStyles = [
      { ...base, alignment: { horizontal: 'center', vertical: 'center' } },
      { ...base, alignment: { horizontal: 'left',   vertical: 'center', wrapText: true } },
      { ...base, alignment: { horizontal: 'center', vertical: 'center' } },
      { ...base, alignment: { horizontal: 'right',  vertical: 'center' } },
    ];
    dataRows.forEach((row, ri) => {
      for (let c = 0; c < COLS; c++) {
        const addr = XLSX.utils.encode_cell({ r: 5 + ri, c });
        if (!ws[addr]) ws[addr] = { t: c === 0 ? 'n' : 's', v: row[c] ?? '' };
        ws[addr].s = colStyles[c];
      }
    });
    ws['!rows'] = [{ hpt: 26 }, { hpt: 16 }, { hpt: 16 }, { hpt: 6 }, { hpt: 22 }, ...dataRows.map(() => ({ hpt: 20 }))];
    ws['!pageSetup'] = { paperSize: 9, orientation: 'portrait', fitToPage: true, fitToWidth: 1, fitToHeight: 0 };
    ws['!margins'] = { left: 0.5, right: 0.5, top: 0.75, bottom: 0.75, header: 0.3, footer: 0.3 };
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, ws, 'Stock Opname');
    XLSX.writeFile(wb, `opname-${warehouseName.replace(/\s+/g, '-')}.xlsx`);
  };

  // ── Build items payload ──────────────────────────────────────────────────────

  const buildDraftItems = () =>
    groups
      .filter(g => actuals[g.key] !== undefined && actuals[g.key] !== '')
      .map(g => ({
        item_id:    g.item_id,
        unit_index: g.unit_index,
        unit_name:  g.unit_name,
        actual_qty: Number(actuals[g.key]),
      }));

  const buildSubmitItems = () =>
    groups
      .filter(g => {
        const d = diff(g);
        return d !== null && d !== 0;
      })
      .map(g => ({
        item_id:         g.item_id,
        unit_index:      g.unit_index,
        unit_name:       g.unit_name,
        actual_quantity: Number(actuals[g.key]),
      }));

  // ── Save progress ────────────────────────────────────────────────────────────

  const handleSaveDraft = async () => {
    setSaveMsg('');
    setError('');
    const items = buildDraftItems();
    const payload = {
      pic_name:      picName,
      operator_name: operatorName,
      notes,
      items,
    };
    try {
      if (currentDraftId) {
        await updateStockOpnameDraft(currentDraftId, payload);
      } else {
        const res = await createStockOpnameDraft({ warehouse_id: selectedWh.id, ...payload });
        setCurrentDraftId(res.data.id);
      }
      setSaveMsg('Progress berhasil disimpan.');
      reloadData();
      setTimeout(() => setSaveMsg(''), 3000);
    } catch {
      setError('Gagal menyimpan progress.');
    }
  };

  // ── Submit ───────────────────────────────────────────────────────────────────

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    const changedItems = buildSubmitItems();
    if (!changedItems.length) {
      setError('Tidak ada perubahan — semua aktual sesuai dengan catatan.');
      return;
    }
    setLoading(true);
    try {
      await createStockOpname({
        warehouse_id:  selectedWh.id,
        notes:         notes || null,
        operator_name: operatorName || null,
        pic_name:      picName || null,
        draft_id:      currentDraftId || '',
        items:         changedItems,
      });
      reloadData();
      backToWarehouseSelect();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setLoading(false);
    }
  };

  const handleDiscardDraft = async (id, e) => {
    e.stopPropagation();
    if (!confirm('Hapus draft ini?')) return;
    try {
      await deleteStockOpnameDraft(id);
      reloadData();
      if (currentDraftId === id) backToWarehouseSelect();
    } catch {
      // ignore
    }
  };

  // ── Render ───────────────────────────────────────────────────────────────────

  return (
    <>
      <div className="page-header">
        <h1>Stock Opname</h1>
      </div>

      {screen === 'warehouse-select' ? (
        <>
          {/* Active drafts */}
          {drafts.length > 0 && (
            <div className="card" style={{ marginBottom: '1.5rem' }}>
              <div className="card-header"><h2>Draft Tersimpan</h2></div>
              <table>
                <thead>
                  <tr>
                    <th>Gudang</th>
                    <th>PIC</th>
                    <th>Pelaksana</th>
                    <th>Catatan</th>
                    <th>Terakhir disimpan</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {drafts.map(d => (
                    <tr
                      key={d.id}
                      style={{ cursor: 'pointer' }}
                      onClick={() => resumeDraft(d)}
                    >
                      <td style={{ fontWeight: 500 }}>{d.warehouse_name}</td>
                      <td style={{ fontSize: '0.85rem', color: '#555' }}>{d.pic_name ?? <span style={{ color: '#bbb', fontStyle: 'italic' }}>—</span>}</td>
                      <td style={{ fontSize: '0.85rem', color: '#555' }}>{d.operator_name ?? <span style={{ color: '#bbb', fontStyle: 'italic' }}>—</span>}</td>
                      <td style={{ fontSize: '0.85rem', color: '#888' }}>{d.notes ?? '—'}</td>
                      <td style={{ fontSize: '0.82rem', color: '#999' }}>{fmt(d.updated_at)}</td>
                      <td>
                        <div style={{ display: 'flex', gap: '0.5rem' }}>
                          <button className="btn btn-primary btn-sm" onClick={e => { e.stopPropagation(); resumeDraft(d); }}>Lanjutkan</button>
                          <button className="btn btn-danger btn-sm" onClick={e => handleDiscardDraft(d.id, e)}>Hapus</button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Warehouse selection */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <div className="card-header"><h2>Opname Baru — Pilih Gudang</h2></div>
            {warehouses.length === 0 ? (
              <p style={{ color: '#999', fontSize: '0.9rem', padding: '0.5rem 0' }}>Tidak ada gudang tersedia.</p>
            ) : (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.75rem', paddingTop: '0.25rem' }}>
                {warehouses.map(wh => (
                  <button
                    key={wh.id}
                    className="btn btn-secondary"
                    style={{ minWidth: '140px', padding: '0.75rem 1.25rem', fontSize: '0.95rem', fontWeight: 500 }}
                    onClick={() => selectWarehouse(wh)}
                  >
                    {wh.name}
                  </button>
                ))}
              </div>
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
                      <td style={{ fontSize: '0.85rem', color: '#555' }}>{op.pic_name ?? <span style={{ color: '#bbb', fontStyle: 'italic' }}>—</span>}</td>
                      <td style={{ fontSize: '0.85rem', color: '#555' }}>{op.operator_name ?? <span style={{ color: '#bbb', fontStyle: 'italic' }}>—</span>}</td>
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
                                <tr key={it.id} style={it.is_correction ? { background: '#fffaf0' } : undefined}>
                                  <td style={{ padding: '0.3rem 0.6rem', fontWeight: 500 }}>
                                    {it.item_name}
                                    {it.is_correction && (
                                      <span className="badge" style={{ marginLeft: '0.4rem', background: '#f39c12', color: '#fff', fontSize: '0.68rem' }}>Koreksi</span>
                                    )}
                                  </td>
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
      ) : (
        /* ── Form screen ─────────────────────────────────────────────────────── */
        <div className="card">
          {/* Header bar */}
          <div className="card-header" style={{ marginBottom: '1.25rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
              <button className="btn btn-secondary btn-sm" onClick={backToWarehouseSelect}>← Kembali</button>
              <h2 style={{ margin: 0 }}>
                Opname — <span style={{ color: '#555', fontWeight: 400 }}>{selectedWh?.name}</span>
                {currentDraftId && <span style={{ marginLeft: '0.75rem', fontSize: '0.8rem', color: '#888', fontWeight: 400 }}>(draft tersimpan)</span>}
              </h2>
            </div>
            {groups.length > 0 && (
              <button type="button" onClick={downloadTemplate} className="btn btn-secondary">
                ⬇ Download Template
              </button>
            )}
          </div>

          {groups.length === 0 ? (
            <p style={{ color: '#999', fontSize: '0.9rem', padding: '0.5rem 0' }}>Tidak ada catatan inventaris di gudang ini.</p>
          ) : (
            <form onSubmit={handleSubmit}>
              {/* ── Top fields ── */}
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '1rem', marginBottom: '1.5rem' }}>
                <div className="form-group" style={{ margin: 0 }}>
                  <label>
                    Penanggung Jawab{' '}
                    <span style={{ color: '#aaa', fontWeight: 400 }}>(supervisor)</span>
                  </label>
                  <input
                    value={picName}
                    onChange={e => setPicName(e.target.value)}
                    placeholder="mis. Ahmad Fauzi..."
                  />
                </div>
                <div className="form-group" style={{ margin: 0 }}>
                  <label>
                    Pelaksana{' '}
                    <span style={{ color: '#aaa', fontWeight: 400 }}>(penghitung)</span>
                  </label>
                  <input
                    value={operatorName}
                    onChange={e => setOperatorName(e.target.value)}
                    placeholder="mis. Budi Santoso..."
                  />
                </div>
                <div className="form-group" style={{ margin: 0 }}>
                  <label>
                    Catatan{' '}
                    <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span>
                  </label>
                  <input
                    value={notes}
                    onChange={e => setNotes(e.target.value)}
                    placeholder="mis. Penghitungan stok bulanan..."
                  />
                </div>
              </div>

              {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
              {saveMsg && (
                <div style={{ marginBottom: '1rem', padding: '0.6rem 1rem', background: '#eafaf1', color: '#1e8449', borderRadius: '6px', fontSize: '0.9rem', fontWeight: 500 }}>
                  {saveMsg}
                </div>
              )}

              {/* ── Item table ── */}
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
                  {groups.map(g => {
                    const d = diff(g);
                    const hasInput = actuals[g.key] !== undefined && actuals[g.key] !== '';
                    const wasteVal = (hasInput && d !== null && d < 0)
                      ? computeGroupWaste(g, Number(actuals[g.key]))
                      : 0;
                    return (
                      <tr key={g.key}>
                        <td style={{ fontWeight: 500 }}>{g.item_name}</td>
                        <td style={{ color: '#aaa', fontSize: '0.82rem' }}>{g.item_code}</td>
                        <td style={{ color: '#555' }}>{g.unit_name}</td>
                        <td style={{ textAlign: 'right', fontWeight: 600 }}>
                          {g.totalQty.toLocaleString('id-ID')}
                        </td>
                        <td style={{ textAlign: 'right', color: '#666' }}>{idr(g.totalValue)}</td>
                        <td style={{ textAlign: 'center', width: '120px' }}>
                          <input
                            type="number"
                            min="0"
                            step="any"
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

              {/* ── Action buttons ── */}
              <div style={{ marginTop: '1.5rem', display: 'flex', gap: '0.75rem', justifyContent: 'flex-end' }}>
                <button
                  type="button"
                  className="btn btn-secondary"
                  onClick={handleSaveDraft}
                >
                  Simpan Progress
                </button>
                <button
                  type="submit"
                  className="btn btn-primary"
                  disabled={loading || !hasChanges}
                >
                  {loading ? 'Menyimpan…' : 'Konfirmasi Opname'}
                </button>
              </div>
            </form>
          )}
        </div>
      )}
    </>
  );
}
