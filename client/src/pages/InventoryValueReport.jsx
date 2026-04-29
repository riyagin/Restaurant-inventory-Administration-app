import { useEffect, useState, useCallback } from 'react';
import * as XLSX from 'xlsx';
import { getWarehouses, getInventoryValueReport } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);
const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';

export default function InventoryValueReport() {
  const [warehouses, setWarehouses]   = useState([]);
  const [groups, setGroups]           = useState([]);
  const [loading, setLoading]         = useState(false);
  const [expandedId, setExpandedId]   = useState(null);
  const [filters, setFilters]         = useState({ warehouse_id: 'all', date_from: '', date_to: '' });

  useEffect(() => { getWarehouses().then(r => setWarehouses(r.data)); }, []);

  const load = useCallback(() => {
    setLoading(true);
    const params = {};
    if (filters.warehouse_id && filters.warehouse_id !== 'all') params.warehouse_id = filters.warehouse_id;
    if (filters.date_from) params.date_from = filters.date_from;
    if (filters.date_to)   params.date_to   = filters.date_to;
    getInventoryValueReport(params)
      .then(r => setGroups(r.data))
      .finally(() => setLoading(false));
  }, [filters]);

  useEffect(() => { load(); }, [load]);

  const clearDates = () => setFilters(f => ({ ...f, date_from: '', date_to: '' }));

  const grandValue = groups.reduce((s, g) => s + Number(g.total_value), 0);
  const grandItems = groups.reduce((s, g) => s + g.item_count, 0);

  const downloadExcel = () => {
    const wb = XLSX.utils.book_new();
    const dateRange = [filters.date_from, filters.date_to].filter(Boolean).join(' – ');

    // Sheet 1: Summary per Warehouse
    const sumRows = [
      ['Inventory Value Report — Summary'],
      dateRange ? [`Period: ${dateRange}`] : [],
      [],
      ['Warehouse', 'Items', 'Total Value (IDR)', '% of Total'],
    ].filter(r => r.length);
    for (const g of groups) {
      const pct = grandValue ? ((Number(g.total_value) / grandValue) * 100).toFixed(1) + '%' : '0%';
      sumRows.push([g.warehouse_name, g.item_count, Number(g.total_value), pct]);
    }
    sumRows.push(['TOTAL', grandItems, grandValue, '100%']);
    const ws1 = XLSX.utils.aoa_to_sheet(sumRows);
    ws1['!cols'] = [{ wch: 24 }, { wch: 8 }, { wch: 22 }, { wch: 10 }];
    const r1 = XLSX.utils.decode_range(ws1['!ref']);
    for (let r = 0; r <= r1.e.r; r++) {
      const cell = ws1[XLSX.utils.encode_cell({ r, c: 2 })];
      if (cell && typeof cell.v === 'number') cell.z = '#,##0';
    }
    XLSX.utils.book_append_sheet(wb, ws1, 'Summary');

    // Sheet 2: Item Detail
    const detailRows = [
      ['Inventory Value Report — Item Detail'],
      dateRange ? [`Period: ${dateRange}`] : [],
      [],
      ['Warehouse', 'Item Code', 'Item Name', 'Quantity', 'Unit', 'Value (IDR)', 'Date'],
    ].filter(r => r.length);
    for (const g of groups) {
      for (const it of g.items) {
        detailRows.push([
          g.warehouse_name, it.item_code, it.item_name,
          Number(it.quantity), it.unit_name, Number(it.value), fmt(it.date),
        ]);
      }
      detailRows.push(['', '', 'Subtotal', '', '', Number(g.total_value), '']);
      detailRows.push([]);
    }
    detailRows.push(['', '', 'GRAND TOTAL', '', '', grandValue, '']);
    const ws2 = XLSX.utils.aoa_to_sheet(detailRows);
    ws2['!cols'] = [{ wch: 22 }, { wch: 12 }, { wch: 30 }, { wch: 10 }, { wch: 8 }, { wch: 20 }, { wch: 13 }];
    const r2 = XLSX.utils.decode_range(ws2['!ref']);
    for (let r = 0; r <= r2.e.r; r++) {
      for (const c of [3, 5]) {
        const cell = ws2[XLSX.utils.encode_cell({ r, c })];
        if (cell && typeof cell.v === 'number') cell.z = '#,##0';
      }
    }
    XLSX.utils.book_append_sheet(wb, ws2, 'Item Detail');

    const filename = `inventory-value${dateRange ? '-' + dateRange.replace(' – ', '_') : ''}.xlsx`;
    XLSX.writeFile(wb, filename);
  };

  return (
    <>
      <div className="page-header">
        <h1>Laporan Nilai Inventaris</h1>
        {groups.length > 0 && (
          <button onClick={downloadExcel} className="btn btn-secondary">⬇ Download Excel</button>
        )}
      </div>

      {groups.length > 0 && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem', marginBottom: '1.5rem' }}>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Total Nilai Inventaris</div>
            <div style={{ fontSize: '1.4rem', fontWeight: 700, color: '#4f8ef7' }}>{idr(grandValue)}</div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Total Barang</div>
            <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>{grandItems}</div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Gudang</div>
            <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>{groups.length}</div>
          </div>
        </div>
      )}

      <div className="card">
        <div className="card-header">
          <h2>{loading ? 'Memuat…' : `${groups.length} gudang`}</h2>
          <div className="filters">
            <select value={filters.warehouse_id} onChange={e => setFilters(f => ({ ...f, warehouse_id: e.target.value }))}>
              <option value="all">Semua Gudang</option>
              {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
            </select>
            <input type="date" value={filters.date_from} onChange={e => setFilters(f => ({ ...f, date_from: e.target.value }))} title="Dari tanggal" />
            <input type="date" value={filters.date_to}   onChange={e => setFilters(f => ({ ...f, date_to: e.target.value }))}   title="Sampai tanggal" />
            {(filters.date_from || filters.date_to) && (
              <button type="button" onClick={clearDates} className="btn btn-secondary btn-sm">Hapus filter tanggal</button>
            )}
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th></th>
              <th>Gudang</th>
              <th style={{ textAlign: 'right' }}>Barang</th>
              <th style={{ textAlign: 'right' }}>Total Nilai</th>
              <th style={{ textAlign: 'right' }}>% Total</th>
            </tr>
          </thead>
          <tbody>
            {groups.length === 0 ? (
              <tr><td colSpan={5} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                {loading ? 'Memuat…' : 'Tidak ada catatan inventaris ditemukan'}
              </td></tr>
            ) : groups.map(g => {
              const pct = grandValue ? ((Number(g.total_value) / grandValue) * 100).toFixed(1) : '0';
              const isOpen = expandedId === g.warehouse_id;
              return (
                <>
                  <tr
                    key={g.warehouse_id}
                    onClick={() => setExpandedId(id => id === g.warehouse_id ? null : g.warehouse_id)}
                    style={{ cursor: 'pointer' }}
                    className={isOpen ? 'row-expanded' : ''}
                  >
                    <td style={{ width: '28px', color: '#aaa', fontSize: '0.75rem', userSelect: 'none' }}>
                      {isOpen ? '▼' : '▶'}
                    </td>
                    <td style={{ fontWeight: 600 }}>{g.warehouse_name}</td>
                    <td style={{ textAlign: 'right' }}>
                      <span className="badge">{g.item_count}</span>
                    </td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: '#4f8ef7', fontSize: '1rem' }}>
                      {idr(g.total_value)}
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '0.5rem' }}>
                        <div style={{ width: '80px', height: '6px', background: '#eee', borderRadius: '3px', overflow: 'hidden' }}>
                          <div style={{ width: `${pct}%`, height: '100%', background: '#4f8ef7', borderRadius: '3px' }} />
                        </div>
                        <span style={{ fontSize: '0.82rem', color: '#888', minWidth: '36px', textAlign: 'right' }}>{pct}%</span>
                      </div>
                    </td>
                  </tr>

                  {isOpen && (
                    <tr key={`${g.warehouse_id}-detail`}>
                      <td colSpan={5} style={{ padding: '0.75rem 1.5rem 1.25rem', background: '#f8f9ff' }}>
                        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85rem' }}>
                          <thead>
                            <tr>
                              {['Kode', 'Barang', 'Jumlah', 'Satuan', 'Nilai', 'Tanggal', '% Gudang'].map((h, i) => (
                                <th key={h} style={{
                                  textAlign: ['Nilai', '% Gudang', 'Jumlah'].includes(h) ? 'right' : 'left',
                                  padding: '0.3rem 0.6rem', color: '#888', fontWeight: 600, borderBottom: '1px solid #e8e8e8',
                                }}>{h}</th>
                              ))}
                            </tr>
                          </thead>
                          <tbody>
                            {g.items.map((it, idx) => {
                              const itemPct = Number(g.total_value) ? ((Number(it.value) / Number(g.total_value)) * 100).toFixed(1) : '0';
                              return (
                                <tr key={idx}>
                                  <td style={{ padding: '0.3rem 0.6rem', color: '#888', fontSize: '0.8rem' }}>{it.item_code}</td>
                                  <td style={{ padding: '0.3rem 0.6rem', fontWeight: 500 }}>{it.item_name}</td>
                                  <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right', fontWeight: 600 }}>
                                    {Number(it.quantity).toLocaleString('id-ID')}
                                  </td>
                                  <td style={{ padding: '0.3rem 0.6rem', color: '#555' }}>{it.unit_name}</td>
                                  <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right', fontWeight: 600, color: '#4f8ef7' }}>
                                    {idr(it.value)}
                                  </td>
                                  <td style={{ padding: '0.3rem 0.6rem', color: '#888', fontSize: '0.8rem' }}>{fmt(it.date)}</td>
                                  <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right' }}>
                                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '0.4rem' }}>
                                      <div style={{ width: '60px', height: '5px', background: '#e8e8f0', borderRadius: '3px', overflow: 'hidden' }}>
                                        <div style={{ width: `${itemPct}%`, height: '100%', background: '#4f8ef7', borderRadius: '3px' }} />
                                      </div>
                                      <span style={{ fontSize: '0.75rem', color: '#aaa', minWidth: '32px', textAlign: 'right' }}>{itemPct}%</span>
                                    </div>
                                  </td>
                                </tr>
                              );
                            })}
                          </tbody>
                          <tfoot>
                            <tr>
                              <td colSpan={4} style={{ padding: '0.4rem 0.6rem', textAlign: 'right', fontWeight: 600, color: '#555' }}>Total Gudang:</td>
                              <td style={{ padding: '0.4rem 0.6rem', textAlign: 'right', fontWeight: 700, color: '#4f8ef7' }}>{idr(g.total_value)}</td>
                              <td colSpan={2}></td>
                            </tr>
                          </tfoot>
                        </table>
                      </td>
                    </tr>
                  )}
                </>
              );
            })}
          </tbody>
          {groups.length > 1 && (
            <tfoot>
              <tr>
                <td colSpan={3} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>Total Keseluruhan:</td>
                <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#4f8ef7', fontSize: '1.05rem' }}>{idr(grandValue)}</td>
                <td></td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>
    </>
  );
}
