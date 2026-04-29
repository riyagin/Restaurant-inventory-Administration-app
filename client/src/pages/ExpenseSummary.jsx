import { useEffect, useState, useCallback } from 'react';
import * as XLSX from 'xlsx';
import { getExpenseSummaryReport } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);
const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';

export default function ExpenseSummary() {
  const [groups, setGroups]   = useState([]);
  const [loading, setLoading] = useState(false);
  const [expandedId, setExpandedId] = useState(null);
  const [filters, setFilters] = useState({ date_from: '', date_to: '' });

  const load = useCallback(() => {
    setLoading(true);
    const params = {};
    if (filters.date_from) params.date_from = filters.date_from;
    if (filters.date_to)   params.date_to   = filters.date_to;
    getExpenseSummaryReport(params)
      .then(r => setGroups(r.data))
      .finally(() => setLoading(false));
  }, [filters]);

  useEffect(() => { load(); }, [load]);

  const clearDates = () => setFilters({ date_from: '', date_to: '' });

  const grandTotal  = groups.reduce((s, g) => s + Number(g.total_amount), 0);
  const grandCount  = groups.reduce((s, g) => s + g.invoice_count, 0);

  const downloadExcel = () => {
    const wb = XLSX.utils.book_new();
    const dateRange = [filters.date_from, filters.date_to].filter(Boolean).join(' – ');

    // Sheet 1: Branch Summary
    const branchRows = [
      ['Expense Summary — By Branch'],
      dateRange ? [`Period: ${dateRange}`] : [],
      [],
      ['Branch', 'Invoices', 'Total Amount (IDR)', '% of Total'],
    ].filter(r => r.length);
    for (const g of groups) {
      const pct = grandTotal ? ((Number(g.total_amount) / grandTotal) * 100).toFixed(1) + '%' : '0%';
      branchRows.push([g.branch_name, g.invoice_count, Number(g.total_amount), pct]);
    }
    branchRows.push(['TOTAL', grandCount, grandTotal, '100%']);
    const ws1 = XLSX.utils.aoa_to_sheet(branchRows);
    ws1['!cols'] = [{ wch: 24 }, { wch: 10 }, { wch: 22 }, { wch: 10 }];
    const r1 = XLSX.utils.decode_range(ws1['!ref']);
    for (let r = 0; r <= r1.e.r; r++) {
      const cell = ws1[XLSX.utils.encode_cell({ r, c: 2 })];
      if (cell && typeof cell.v === 'number') cell.z = '#,##0';
    }
    XLSX.utils.book_append_sheet(wb, ws1, 'Branch Summary');

    // Sheet 2: Division Breakdown
    const divRows = [
      ['Expense Summary — By Division'],
      dateRange ? [`Period: ${dateRange}`] : [],
      [],
      ['Branch', 'Division', 'Invoices', 'Total Amount (IDR)', '% of Branch'],
    ].filter(r => r.length);
    for (const g of groups) {
      for (const d of g.divisions) {
        const pct = Number(g.total_amount) ? ((Number(d.total_amount) / Number(g.total_amount)) * 100).toFixed(1) + '%' : '0%';
        divRows.push([g.branch_name, d.division_name, d.invoice_count, Number(d.total_amount), pct]);
      }
      divRows.push(['', 'Branch Total', g.invoice_count, Number(g.total_amount), '']);
      divRows.push([]);
    }
    divRows.push(['', 'GRAND TOTAL', grandCount, grandTotal, '']);
    const ws2 = XLSX.utils.aoa_to_sheet(divRows);
    ws2['!cols'] = [{ wch: 22 }, { wch: 22 }, { wch: 10 }, { wch: 22 }, { wch: 12 }];
    const r2 = XLSX.utils.decode_range(ws2['!ref']);
    for (let r = 0; r <= r2.e.r; r++) {
      const cell = ws2[XLSX.utils.encode_cell({ r, c: 3 })];
      if (cell && typeof cell.v === 'number') cell.z = '#,##0';
    }
    XLSX.utils.book_append_sheet(wb, ws2, 'Division Breakdown');

    const filename = `expense-summary${dateRange ? '-' + dateRange.replace(' – ', '_') : ''}.xlsx`;
    XLSX.writeFile(wb, filename);
  };

  return (
    <>
      <div className="page-header">
        <h1>Ringkasan Pengeluaran</h1>
        {groups.length > 0 && (
          <button onClick={downloadExcel} className="btn btn-secondary">⬇ Download Excel</button>
        )}
      </div>

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
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Cabang</div>
            <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>{groups.length}</div>
          </div>
          {grandCount > 0 && (
            <div className="card" style={{ padding: '1.25rem' }}>
              <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Rata-rata/Invoice</div>
              <div style={{ fontSize: '1.4rem', fontWeight: 700, color: '#e67e22' }}>{idr(Math.round(grandTotal / grandCount))}</div>
            </div>
          )}
        </div>
      )}

      <div className="card">
        <div className="card-header">
          <h2>{loading ? 'Memuat…' : `${groups.length} cabang`}</h2>
          <div className="filters">
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
              <th style={{ textAlign: 'right' }}>Invoice</th>
              <th style={{ textAlign: 'right' }}>Total</th>
              <th style={{ textAlign: 'right' }}>% Total</th>
            </tr>
          </thead>
          <tbody>
            {groups.length === 0 ? (
              <tr><td colSpan={5} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                {loading ? 'Memuat…' : 'Tidak ada invoice pengeluaran ditemukan'}
              </td></tr>
            ) : groups.map(g => {
              const pct = grandTotal ? ((Number(g.total_amount) / grandTotal) * 100).toFixed(1) : '0';
              const isOpen = expandedId === g.branch_id;
              return (
                <>
                  <tr
                    key={g.branch_id}
                    onClick={() => setExpandedId(id => id === g.branch_id ? null : g.branch_id)}
                    style={{ cursor: 'pointer' }}
                    className={isOpen ? 'row-expanded' : ''}
                  >
                    <td style={{ width: '28px', color: '#aaa', fontSize: '0.75rem', userSelect: 'none' }}>
                      {isOpen ? '▼' : '▶'}
                    </td>
                    <td style={{ fontWeight: 600 }}>{g.branch_name}</td>
                    <td style={{ textAlign: 'right' }}>
                      <span className="badge">{g.invoice_count}</span>
                    </td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: '#e74c3c', fontSize: '1rem' }}>
                      {idr(g.total_amount)}
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '0.5rem' }}>
                        <div style={{ width: '80px', height: '6px', background: '#eee', borderRadius: '3px', overflow: 'hidden' }}>
                          <div style={{ width: `${pct}%`, height: '100%', background: '#e74c3c', borderRadius: '3px' }} />
                        </div>
                        <span style={{ fontSize: '0.82rem', color: '#888', minWidth: '36px', textAlign: 'right' }}>{pct}%</span>
                      </div>
                    </td>
                  </tr>

                  {isOpen && g.divisions.length > 0 && (
                    <tr key={`${g.branch_id}-divs`}>
                      <td colSpan={5} style={{ padding: '0.5rem 1.5rem 1rem', background: '#f8f9ff' }}>
                        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85rem' }}>
                          <thead>
                            <tr>
                              {['Divisi', 'Invoice', 'Jumlah', '% Cabang'].map((h, i) => (
                                <th key={h} style={{
                                  textAlign: i > 0 ? 'right' : 'left',
                                  padding: '0.3rem 0.6rem', color: '#888', fontWeight: 600, borderBottom: '1px solid #e8e8e8',
                                }}>{h}</th>
                              ))}
                            </tr>
                          </thead>
                          <tbody>
                            {g.divisions.map(d => {
                              const divPct = Number(g.total_amount) ? ((Number(d.total_amount) / Number(g.total_amount)) * 100).toFixed(1) : '0';
                              return (
                                <tr key={d.division_id}>
                                  <td style={{ padding: '0.3rem 0.6rem', fontWeight: 500 }}>{d.division_name}</td>
                                  <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right' }}>
                                    <span className="badge">{d.invoice_count}</span>
                                  </td>
                                  <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right', fontWeight: 600, color: '#e74c3c' }}>
                                    {idr(d.total_amount)}
                                  </td>
                                  <td style={{ padding: '0.3rem 0.6rem', textAlign: 'right' }}>
                                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '0.4rem' }}>
                                      <div style={{ width: '60px', height: '5px', background: '#e8e8f0', borderRadius: '3px', overflow: 'hidden' }}>
                                        <div style={{ width: `${divPct}%`, height: '100%', background: '#e74c3c', borderRadius: '3px' }} />
                                      </div>
                                      <span style={{ fontSize: '0.75rem', color: '#aaa', minWidth: '32px', textAlign: 'right' }}>{divPct}%</span>
                                    </div>
                                  </td>
                                </tr>
                              );
                            })}
                          </tbody>
                          <tfoot>
                            <tr>
                              <td style={{ padding: '0.4rem 0.6rem', fontWeight: 600, color: '#555' }}>Total Cabang</td>
                              <td style={{ padding: '0.4rem 0.6rem', textAlign: 'right' }}>
                                <span className="badge">{g.invoice_count}</span>
                              </td>
                              <td style={{ padding: '0.4rem 0.6rem', textAlign: 'right', fontWeight: 700, color: '#e74c3c' }}>
                                {idr(g.total_amount)}
                              </td>
                              <td></td>
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
                <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#e74c3c', fontSize: '1.05rem' }}>{idr(grandTotal)}</td>
                <td></td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>
    </>
  );
}
