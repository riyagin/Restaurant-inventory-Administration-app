import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import * as XLSX from 'xlsx';
import { getBranches, getDivisions, getExpenseReport } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);
const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';

const STATUS_LABEL = { unpaid: 'Belum Dibayar', paid: 'Lunas', partial: 'Sebagian' };
const STATUS_CLASS  = { unpaid: 'status-unpaid', paid: 'status-paid', partial: 'status-partial' };

const TAB_STYLE = (active) => ({
  padding: '0.35rem 0.9rem', borderRadius: '5px', fontSize: '0.82rem', fontWeight: 600,
  cursor: 'pointer', border: 'none',
  background: active ? '#4f8ef7' : 'transparent',
  color: active ? '#fff' : '#888',
});

export default function ExpenseReport() {
  const [branches, setBranches]       = useState([]);
  const [divisions, setDivisions]     = useState([]);
  const [filters, setFilters]         = useState({ branch_id: '', division_id: '', date_from: '', date_to: '' });
  const [groups, setGroups]           = useState([]);
  const [expandedKey, setExpandedKey] = useState(null);
  // per-group tab: 'items' | 'invoices'
  const [groupTab, setGroupTab]       = useState({});
  const [loading, setLoading]         = useState(false);

  useEffect(() => { getBranches().then(r => setBranches(r.data)); }, []);

  const onBranchChange = (e) => {
    const val = e.target.value;
    setFilters(f => ({ ...f, branch_id: val, division_id: '' }));
    setDivisions([]);
    if (val) getDivisions({ branch_id: val }).then(r => setDivisions(r.data));
  };

  const load = useCallback(() => {
    setLoading(true);
    const params = {};
    if (filters.branch_id)   params.branch_id   = filters.branch_id;
    if (filters.division_id) params.division_id = filters.division_id;
    if (filters.date_from)   params.date_from   = filters.date_from;
    if (filters.date_to)     params.date_to     = filters.date_to;
    getExpenseReport(params)
      .then(r => setGroups(r.data))
      .finally(() => setLoading(false));
  }, [filters]);

  useEffect(() => { load(); }, [load]);

  const grandTotal = groups.reduce((s, g) => s + Number(g.total_amount), 0);
  const grandCount = groups.reduce((s, g) => s + g.invoice_count, 0);

  const clearDates = () => setFilters(f => ({ ...f, date_from: '', date_to: '' }));

  const getTab = (key) => groupTab[key] ?? 'items';
  const setTab = (key, tab) => setGroupTab(t => ({ ...t, [key]: tab }));

  const downloadExcel = () => {
    const wb = XLSX.utils.book_new();
    const dateRange = [filters.date_from, filters.date_to].filter(Boolean).join(' – ');

    // ── Sheet 1: Item Usage ──
    const itemRows = [
      ['Expense Report — Item Usage'],
      dateRange ? [`Period: ${dateRange}`] : [],
      [],
      ['Branch', 'Division', 'Item / Description', 'Total Qty', 'Total Value (IDR)'],
    ].filter(r => r.length);

    for (const g of groups) {
      for (const it of g.item_usage) {
        itemRows.push([g.branch_name, g.division_name, it.description, Number(it.total_qty), Number(it.total_value)]);
      }
      itemRows.push(['', '', 'Subtotal', '', Number(g.total_amount)]);
      itemRows.push([]);
    }
    itemRows.push(['', '', 'GRAND TOTAL', '', grandTotal]);

    const ws1 = XLSX.utils.aoa_to_sheet(itemRows);
    ws1['!cols'] = [{ wch: 20 }, { wch: 20 }, { wch: 32 }, { wch: 12 }, { wch: 20 }];
    const r1 = XLSX.utils.decode_range(ws1['!ref']);
    for (let r = 0; r <= r1.e.r; r++) {
      for (const c of [3, 4]) {
        const cell = ws1[XLSX.utils.encode_cell({ r, c })];
        if (cell && typeof cell.v === 'number') cell.z = '#,##0';
      }
    }
    XLSX.utils.book_append_sheet(wb, ws1, 'Item Usage');

    // ── Sheet 2: Invoices ──
    const invRows = [
      ['Expense Report — Invoices'],
      dateRange ? [`Period: ${dateRange}`] : [],
      [],
      ['Branch', 'Division', 'Invoice No.', 'Date', 'Source', 'Status', 'Amount (IDR)'],
    ].filter(r => r.length);

    for (const g of groups) {
      for (const inv of g.invoices) {
        invRows.push([
          g.branch_name, g.division_name,
          inv.invoice_number, fmt(inv.date),
          inv.dispatch_id ? 'Dispatch' : 'Manual',
          STATUS_LABEL[inv.payment_status] ?? inv.payment_status,
          Number(inv.total),
        ]);
      }
      invRows.push(['', '', '', '', '', 'Subtotal', Number(g.total_amount)]);
      invRows.push([]);
    }
    invRows.push(['', '', '', '', '', 'GRAND TOTAL', grandTotal]);

    const ws2 = XLSX.utils.aoa_to_sheet(invRows);
    ws2['!cols'] = [{ wch: 20 }, { wch: 20 }, { wch: 14 }, { wch: 13 }, { wch: 12 }, { wch: 16 }, { wch: 20 }];
    const r2 = XLSX.utils.decode_range(ws2['!ref']);
    for (let r = 0; r <= r2.e.r; r++) {
      const cell = ws2[XLSX.utils.encode_cell({ r, c: 6 })];
      if (cell && typeof cell.v === 'number') cell.z = '#,##0';
    }
    XLSX.utils.book_append_sheet(wb, ws2, 'Invoices');

    const filename = `expense-report${dateRange ? '-' + dateRange.replace(' – ', '_') : ''}.xlsx`;
    XLSX.writeFile(wb, filename);
  };

  return (
    <>
      <div className="page-header">
        <h1>Laporan Pengeluaran</h1>
        {groups.length > 0 && (
          <button onClick={downloadExcel} className="btn btn-secondary">
            ⬇ Download Excel
          </button>
        )}
      </div>

      {/* Summary cards */}
      {groups.length > 0 && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem', marginBottom: '1.5rem' }}>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Total Pengeluaran</div>
            <div style={{ fontSize: '1.4rem', fontWeight: 700, color: '#e74c3c' }}>{idr(grandTotal)}</div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Total Invoice</div>
            <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>{grandCount}</div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Cabang / Divisi</div>
            <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>{groups.length}</div>
          </div>
        </div>
      )}

      <div className="card">
        <div className="card-header">
          <h2>{loading ? 'Memuat…' : `${groups.length} grup`}</h2>
          <div className="filters">
            <select value={filters.branch_id} onChange={onBranchChange}>
              <option value="">Semua Cabang</option>
              {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
            </select>
            <select value={filters.division_id} onChange={e => setFilters(f => ({ ...f, division_id: e.target.value }))} disabled={!filters.branch_id}>
              <option value="">Semua Divisi</option>
              {divisions.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
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
              <th>Cabang</th>
              <th>Divisi</th>
              <th style={{ textAlign: 'right' }}>Invoice</th>
              <th style={{ textAlign: 'right' }}>Total</th>
            </tr>
          </thead>
          <tbody>
            {groups.length === 0 ? (
              <tr><td colSpan={5} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                {loading ? 'Memuat…' : 'Tidak ada invoice pengeluaran ditemukan'}
              </td></tr>
            ) : groups.map(g => {
              const key = `${g.branch_id}::${g.division_id}`;
              const isOpen = expandedKey === key;
              const tab = getTab(key);
              return (
                <>
                  <tr
                    key={key}
                    onClick={() => setExpandedKey(k => k === key ? null : key)}
                    style={{ cursor: 'pointer' }}
                    className={isOpen ? 'row-expanded' : ''}
                  >
                    <td style={{ width: '28px', color: '#aaa', fontSize: '0.75rem', userSelect: 'none' }}>
                      {isOpen ? '▼' : '▶'}
                    </td>
                    <td style={{ fontWeight: 600 }}>{g.branch_name}</td>
                    <td style={{ color: '#555' }}>{g.division_name}</td>
                    <td style={{ textAlign: 'right' }}>
                      <span className="badge">{g.invoice_count}</span>
                    </td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: '#e74c3c', fontSize: '1rem' }}>
                      {idr(g.total_amount)}
                    </td>
                  </tr>

                  {isOpen && (
                    <tr key={`${key}-detail`}>
                      <td colSpan={5} style={{ padding: '0.75rem 1.5rem 1.25rem', background: '#f8f9ff' }}>

                        {/* Tab bar */}
                        <div style={{ display: 'flex', gap: '0.25rem', marginBottom: '0.75rem', background: '#f0f0f0', borderRadius: '7px', padding: '0.25rem', width: 'fit-content' }}
                             onClick={e => e.stopPropagation()}>
                          <button style={TAB_STYLE(tab === 'items')}   onClick={() => setTab(key, 'items')}>Pemakaian Barang</button>
                          <button style={TAB_STYLE(tab === 'invoices')} onClick={() => setTab(key, 'invoices')}>Invoice</button>
                        </div>

                        {/* ── Item Usage tab ── */}
                        {tab === 'items' && (
                          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85rem' }}>
                            <thead>
                              <tr>
                                {['Barang / Deskripsi', 'Total Qty', 'Total Nilai'].map((h, i) => (
                                  <th key={h} style={{ textAlign: i > 0 ? 'right' : 'left', padding: '0.3rem 0.6rem', color: '#888', fontWeight: 600, borderBottom: '1px solid #e8e8e8' }}>{h}</th>
                                ))}
                              </tr>
                            </thead>
                            <tbody>
                              {g.item_usage.length === 0 ? (
                                <tr><td colSpan={3} style={{ padding: '0.75rem 0.6rem', color: '#bbb', fontStyle: 'italic' }}>Tidak ada barang</td></tr>
                              ) : g.item_usage.map((it, idx) => (
                                <tr key={idx}>
                                  <td style={{ padding: '0.3rem 0.6rem', fontWeight: 500 }}>{it.description ?? '—'}</td>
                                  <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right', fontWeight: 600 }}>
                                    {Number(it.total_qty).toLocaleString('id-ID')}
                                  </td>
                                  <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right', color: '#555' }}>
                                    {idr(it.total_value)}
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                            <tfoot>
                              <tr>
                                <td colSpan={2} style={{ padding: '0.4rem 0.6rem', textAlign: 'right', fontWeight: 600, color: '#555' }}>Total:</td>
                                <td style={{ padding: '0.4rem 0.6rem', textAlign: 'right', fontWeight: 700, color: '#e74c3c' }}>{idr(g.total_amount)}</td>
                              </tr>
                            </tfoot>
                          </table>
                        )}

                        {/* ── Invoices tab ── */}
                        {tab === 'invoices' && (
                          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85rem' }}>
                            <thead>
                              <tr>
                                {['No. Invoice', 'Tanggal', 'Sumber', 'Status', 'Total', ''].map((h, i) => (
                                  <th key={h} style={{ textAlign: h === 'Total' ? 'right' : 'left', padding: '0.3rem 0.6rem', color: '#888', fontWeight: 600, borderBottom: '1px solid #e8e8e8' }}>{h}</th>
                                ))}
                              </tr>
                            </thead>
                            <tbody>
                              {g.invoices.map(inv => (
                                <tr key={inv.id}>
                                  <td style={{ padding: '0.3rem 0.6rem', fontWeight: 600 }}>{inv.invoice_number}</td>
                                  <td style={{ padding: '0.3rem 0.6rem', color: '#555' }}>{fmt(inv.date)}</td>
                                  <td style={{ padding: '0.3rem 0.6rem' }}>
                                    {inv.dispatch_id ? (
                                      <Link to={`/dispatches/${inv.dispatch_id}`} style={{ fontSize: '0.75rem', fontWeight: 600, padding: '0.1rem 0.45rem', borderRadius: '4px', background: '#f3e8ff', color: '#8b5cf6', textDecoration: 'none' }}>
                                        Dispatch
                                      </Link>
                                    ) : (
                                      <span style={{ fontSize: '0.75rem', fontWeight: 600, padding: '0.1rem 0.45rem', borderRadius: '4px', background: '#fff3e0', color: '#f57c00' }}>
                                        Manual
                                      </span>
                                    )}
                                  </td>
                                  <td style={{ padding: '0.3rem 0.6rem' }}>
                                    <span className={`badge ${STATUS_CLASS[inv.payment_status] ?? ''}`}>
                                      {STATUS_LABEL[inv.payment_status] ?? inv.payment_status}
                                    </span>
                                  </td>
                                  <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right', fontWeight: 600 }}>{idr(inv.total)}</td>
                                  <td style={{ padding: '0.3rem 0.6rem' }}>
                                    <Link to={`/invoices/view/${inv.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                            <tfoot>
                              <tr>
                                <td colSpan={4} style={{ padding: '0.4rem 0.6rem', textAlign: 'right', fontWeight: 600, color: '#555' }}>Subtotal:</td>
                                <td style={{ padding: '0.4rem 0.6rem', textAlign: 'right', fontWeight: 700, color: '#e74c3c' }}>{idr(g.total_amount)}</td>
                                <td></td>
                              </tr>
                            </tfoot>
                          </table>
                        )}

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
                <td colSpan={4} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>Total Keseluruhan:</td>
                <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#e74c3c', fontSize: '1.05rem' }}>{idr(grandTotal)}</td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>
    </>
  );
}
