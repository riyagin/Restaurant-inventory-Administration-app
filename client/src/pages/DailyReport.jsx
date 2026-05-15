import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getDailyReport } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v ?? 0);

const fmtTime = (d) =>
  d ? new Date(d).toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit' }) : '—';

const todayISO = () => new Date().toISOString().split('T')[0];

function SectionHeader({ title, count, total, totalColor }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.75rem' }}>
      <h2 style={{ margin: 0, fontSize: '1rem' }}>
        {title}
        {count != null && (
          <span style={{ marginLeft: '0.5rem', fontSize: '0.78rem', color: '#999', fontWeight: 400 }}>
            ({count})
          </span>
        )}
      </h2>
      {total != null && total > 0 && (
        <span style={{ fontWeight: 700, color: totalColor || '#333', fontSize: '1rem' }}>{idr(total)}</span>
      )}
    </div>
  );
}

function EmptyRow({ cols, label }) {
  return (
    <tr>
      <td colSpan={cols} style={{ textAlign: 'center', color: '#ccc', padding: '1rem', fontSize: '0.85rem' }}>
        {label}
      </td>
    </tr>
  );
}

export default function DailyReport() {
  const [date, setDate]       = useState(todayISO());
  const [data, setData]       = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError]     = useState('');

  useEffect(() => {
    if (!date) return;
    setLoading(true);
    setError('');
    getDailyReport({ date })
      .then(r => setData(r.data))
      .catch(e => setError(e.response?.data?.error || 'Gagal memuat laporan'))
      .finally(() => setLoading(false));
  }, [date]);

  const fmtDate = (d) => d
    ? new Date(d + 'T00:00:00').toLocaleDateString('id-ID', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })
    : '—';

  const summary = data?.summary || {};

  const statusBadge = (status) => {
    const cfg = {
      paid:    { bg: '#e6f9f0', color: '#1b5e45', label: 'Lunas' },
      partial: { bg: '#fff3e0', color: '#e67e22', label: 'Sebagian' },
      unpaid:  { bg: '#fdecea', color: '#c0392b', label: 'Belum' },
    }[status] || { bg: '#f5f5f5', color: '#888', label: status };
    return (
      <span style={{ fontSize: '0.72rem', padding: '0.1rem 0.4rem', borderRadius: '3px', fontWeight: 600, background: cfg.bg, color: cfg.color }}>
        {cfg.label}
      </span>
    );
  };

  return (
    <>
      <div className="page-header">
        <h1>Laporan Harian</h1>
        <Link to="/reports/financial" className="btn btn-secondary">← Laporan Keuangan</Link>
      </div>

      {/* Date picker */}
      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
          <label style={{ fontWeight: 600, fontSize: '0.9rem', color: '#444' }}>Tanggal</label>
          <input
            type="date"
            value={date}
            onChange={e => setDate(e.target.value)}
            style={{ padding: '0.4rem 0.75rem', border: '1px solid #ddd', borderRadius: '6px', fontSize: '0.95rem', fontWeight: 600 }}
          />
          <button className="btn btn-secondary btn-sm" onClick={() => setDate(todayISO())}>Hari Ini</button>
          {date && <span style={{ color: '#888', fontSize: '0.88rem' }}>{fmtDate(date)}</span>}
        </div>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
      {loading && <p style={{ color: '#999', padding: '1rem 0' }}>Memuat...</p>}

      {data && !loading && (
        <>
          {/* Summary cards */}
          <div className="stats-grid" style={{ marginBottom: '1.5rem' }}>
            <div className="stat-card">
              <div className="label">Penjualan POS</div>
              <div className="value" style={{ fontSize: '1.2rem', color: summary.pos_revenue > 0 ? '#27ae60' : '#ccc' }}>
                {idr(summary.pos_revenue)}
              </div>
              <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>
                {data.pos_imports.length} import
              </div>
            </div>
            {summary.manual_sales > 0 && (
              <div className="stat-card">
                <div className="label">Penjualan Manual</div>
                <div className="value" style={{ fontSize: '1.2rem', color: '#27ae60' }}>{idr(summary.manual_sales)}</div>
                <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>
                  {data.sales.length} transaksi
                </div>
              </div>
            )}
            <div className="stat-card">
              <div className="label">Pembelian</div>
              <div className="value" style={{ fontSize: '1.2rem', color: summary.purchases > 0 ? '#2c6fc2' : '#ccc' }}>
                {idr(summary.purchases)}
              </div>
              <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>
                {data.invoices.filter(i => i.invoice_type === 'purchase').length} invoice
              </div>
            </div>
            <div className="stat-card">
              <div className="label">Beban / Pengeluaran</div>
              <div className="value" style={{ fontSize: '1.2rem', color: summary.expenses > 0 ? '#e67e22' : '#ccc' }}>
                {idr(summary.expenses)}
              </div>
              <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>
                {data.invoices.filter(i => i.invoice_type === 'expense').length} invoice
              </div>
            </div>
            <div className="stat-card">
              <div className="label">Pengiriman ke Cabang</div>
              <div className="value" style={{ fontSize: '1.2rem', color: data.dispatches.length > 0 ? '#8e44ad' : '#ccc' }}>
                {data.dispatches.length}
              </div>
              <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>pengiriman</div>
            </div>
          </div>

          {/* POS Imports */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <SectionHeader title="Import POS" count={data.pos_imports.length} total={summary.pos_revenue} totalColor="#27ae60" />
            <table>
              <thead>
                <tr>
                  <th>Deskripsi</th>
                  <th>File</th>
                  <th style={{ textAlign: 'right' }}>Revenue</th>
                  <th style={{ textAlign: 'right' }}>Diskon</th>
                  <th style={{ textAlign: 'right' }}>Kas Diterima</th>
                  <th>Dicatat oleh</th>
                </tr>
              </thead>
              <tbody>
                {data.pos_imports.length === 0
                  ? <EmptyRow cols={6} label="Tidak ada import POS pada tanggal ini" />
                  : data.pos_imports.map(imp => {
                      const lines = imp.lines || [];
                      const revTotal  = lines.filter(l => l.line_type === 'revenue').reduce((s, l) => s + Number(l.amount), 0);
                      const discTotal = lines.filter(l => l.line_type === 'discount').reduce((s, l) => s + Number(l.amount), 0);
                      const cashTotal = lines.filter(l => l.line_type === 'cash').reduce((s, l) => s + Number(l.amount), 0);
                      return (
                        <tr key={imp.id}>
                          <td style={{ fontWeight: 500 }}>{imp.description}</td>
                          <td style={{ color: '#888', fontSize: '0.82rem' }}>{imp.source_file || '—'}</td>
                          <td style={{ textAlign: 'right', fontWeight: 700, color: '#27ae60', whiteSpace: 'nowrap' }}>{idr(revTotal)}</td>
                          <td style={{ textAlign: 'right', color: discTotal < 0 ? '#e74c3c' : '#ccc', whiteSpace: 'nowrap' }}>
                            {discTotal < 0 ? idr(discTotal) : '—'}
                          </td>
                          <td style={{ textAlign: 'right', fontWeight: 600, color: '#2c6fc2', whiteSpace: 'nowrap' }}>{idr(cashTotal)}</td>
                          <td style={{ color: '#888', fontSize: '0.82rem' }}>{imp.created_by_name || '—'}</td>
                        </tr>
                      );
                    })}
              </tbody>
            </table>
          </div>

          {/* Manual Sales */}
          {data.sales.length > 0 && (
            <div className="card" style={{ marginBottom: '1.5rem' }}>
              <SectionHeader title="Penjualan Manual" count={data.sales.length} total={summary.manual_sales} totalColor="#27ae60" />
              <table>
                <thead>
                  <tr>
                    <th>Keterangan</th>
                    <th>Cabang / Divisi</th>
                    <th style={{ textAlign: 'right' }}>Jumlah</th>
                    <th>Dicatat oleh</th>
                  </tr>
                </thead>
                <tbody>
                  {data.sales.map(s => (
                    <tr key={s.id}>
                      <td style={{ color: '#555', fontSize: '0.88rem' }}>{s.description || '—'}</td>
                      <td style={{ fontSize: '0.85rem', color: '#666' }}>
                        {s.branch_name || '—'}{s.division_name ? ` / ${s.division_name}` : ''}
                      </td>
                      <td style={{ textAlign: 'right', fontWeight: 700, color: '#27ae60', whiteSpace: 'nowrap' }}>{idr(s.amount)}</td>
                      <td style={{ color: '#888', fontSize: '0.82rem' }}>{s.created_by_name || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Invoices */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <SectionHeader
              title="Invoice Pembelian & Beban"
              count={data.invoices.length}
              total={summary.purchases + summary.expenses}
              totalColor="#2c6fc2"
            />
            <table>
              <thead>
                <tr>
                  <th>No. Invoice</th>
                  <th>Vendor</th>
                  <th>Jenis</th>
                  <th>Cabang / Divisi</th>
                  <th style={{ textAlign: 'right' }}>Total</th>
                  <th>Bayar</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {data.invoices.length === 0
                  ? <EmptyRow cols={7} label="Tidak ada invoice pada tanggal ini" />
                  : data.invoices.map(inv => (
                      <tr key={inv.id}>
                        <td style={{ fontWeight: 600, fontSize: '0.88rem' }}>{inv.invoice_number}</td>
                        <td style={{ color: '#666', fontSize: '0.88rem' }}>{inv.vendor_name || '—'}</td>
                        <td>
                          <span style={{
                            fontSize: '0.72rem', padding: '0.1rem 0.4rem', borderRadius: '3px', fontWeight: 600,
                            background: inv.invoice_type === 'expense' ? '#fff3e0' : '#e8f4fd',
                            color: inv.invoice_type === 'expense' ? '#e67e22' : '#2c6fc2',
                          }}>
                            {inv.invoice_type === 'expense' ? 'Beban' : 'Pembelian'}
                          </span>
                        </td>
                        <td style={{ fontSize: '0.85rem', color: '#666' }}>
                          {inv.branch_name || '—'}{inv.division_name ? ` / ${inv.division_name}` : ''}
                        </td>
                        <td style={{ textAlign: 'right', fontWeight: 700, whiteSpace: 'nowrap' }}>{idr(inv.total)}</td>
                        <td>{statusBadge(inv.payment_status)}</td>
                        <td>
                          <Link to={`/invoices/view/${inv.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                        </td>
                      </tr>
                    ))}
              </tbody>
            </table>
          </div>

          {/* Dispatches */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <SectionHeader title="Pengiriman ke Cabang" count={data.dispatches.length} />
            <table>
              <thead>
                <tr>
                  <th>Waktu</th>
                  <th>Gudang</th>
                  <th>Cabang / Divisi</th>
                  <th>Catatan</th>
                  <th style={{ textAlign: 'right' }}>Jml Item</th>
                  <th>Oleh</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {data.dispatches.length === 0
                  ? <EmptyRow cols={7} label="Tidak ada pengiriman ke cabang pada tanggal ini" />
                  : data.dispatches.map(d => (
                      <tr key={d.id}>
                        <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmtTime(d.dispatched_at)}</td>
                        <td style={{ fontSize: '0.88rem' }}>{d.warehouse_name}</td>
                        <td style={{ fontSize: '0.85rem', color: '#666' }}>{d.branch_name} / {d.division_name}</td>
                        <td style={{ color: '#888', fontSize: '0.82rem' }}>{d.notes || '—'}</td>
                        <td style={{ textAlign: 'right', fontSize: '0.85rem' }}>{d.item_count} SKU</td>
                        <td style={{ color: '#888', fontSize: '0.82rem' }}>{d.dispatched_by_name || '—'}</td>
                        <td>
                          <Link to={`/dispatches/${d.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                        </td>
                      </tr>
                    ))}
              </tbody>
            </table>
          </div>

          {/* Stock Transfers */}
          {data.transfers.length > 0 && (
            <div className="card" style={{ marginBottom: '1.5rem' }}>
              <SectionHeader title="Transfer Stok Antar Gudang" count={data.transfers.length} />
              <table>
                <thead>
                  <tr>
                    <th>Waktu</th>
                    <th>Dari Gudang</th>
                    <th>Ke Gudang</th>
                    <th style={{ textAlign: 'right' }}>Jml Item</th>
                    <th>Oleh</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {data.transfers.map(t => (
                    <tr key={t.group_id}>
                      <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmtTime(t.transferred_at)}</td>
                      <td style={{ fontSize: '0.88rem' }}>{t.from_warehouse}</td>
                      <td style={{ fontSize: '0.88rem' }}>{t.to_warehouse}</td>
                      <td style={{ textAlign: 'right', fontSize: '0.85rem' }}>{t.distinct_items} SKU ({t.item_count} baris)</td>
                      <td style={{ color: '#888', fontSize: '0.82rem' }}>{t.transferred_by_name || '—'}</td>
                      <td>
                        <Link to={`/transfers/group/${t.group_id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Stock Opname */}
          {data.opnames.length > 0 && (
            <div className="card" style={{ marginBottom: '1.5rem' }}>
              <SectionHeader title="Stok Opname" count={data.opnames.length} />
              <table>
                <thead>
                  <tr>
                    <th>Waktu</th>
                    <th>Gudang</th>
                    <th>Operator</th>
                    <th style={{ textAlign: 'right' }}>Jml Item</th>
                    <th style={{ textAlign: 'right' }}>Total Selisih</th>
                    <th>Catatan</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {data.opnames.map(op => (
                    <tr key={op.id}>
                      <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmtTime(op.created_at)}</td>
                      <td style={{ fontSize: '0.88rem' }}>{op.warehouse_name}</td>
                      <td style={{ fontSize: '0.85rem', color: '#666' }}>{op.operator_name || op.performed_by_name || '—'}</td>
                      <td style={{ textAlign: 'right', fontSize: '0.85rem' }}>{op.item_count}</td>
                      <td style={{ textAlign: 'right', fontWeight: 600, color: op.total_diff > 0 ? '#e74c3c' : '#ccc' }}>
                        {op.total_diff > 0 ? op.total_diff : '—'}
                      </td>
                      <td style={{ color: '#888', fontSize: '0.82rem' }}>{op.notes || '—'}</td>
                      <td>
                        <Link to={`/stock-opname/detail/${op.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Empty state */}
          {data.pos_imports.length === 0 && data.invoices.length === 0 &&
           data.dispatches.length === 0 && data.transfers.length === 0 &&
           data.opnames.length === 0 && data.sales.length === 0 && (
            <div style={{ textAlign: 'center', color: '#bbb', padding: '3rem', fontSize: '0.95rem' }}>
              Tidak ada aktivitas pada tanggal ini.
            </div>
          )}
        </>
      )}
    </>
  );
}
