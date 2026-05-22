import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getStats, getDailySalesByBranch } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '—';

const todayStr = new Date().toISOString().split('T')[0];

function offsetDate(dateStr, days) {
  const d = new Date(dateStr);
  d.setDate(d.getDate() + days);
  return d.toISOString().split('T')[0];
}

function DailySalesCard() {
  const [date, setDate] = useState(todayStr);
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    getDailySalesByBranch(date)
      .then(r => { setData(r.data); setLoading(false); })
      .catch(() => setLoading(false));
  }, [date]);

  const isToday = date === todayStr;
  const grandTotal = data ? data.branches.reduce((s, b) => s + b.total, 0) : 0;

  return (
    <div className="card" style={{ marginBottom: '1.5rem' }}>
      <div className="card-header" style={{ alignItems: 'center' }}>
        <h2 style={{ margin: 0 }}>Penjualan Harian per Cabang</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <button
            className="btn btn-secondary btn-sm"
            onClick={() => setDate(d => offsetDate(d, -1))}
            title="Hari sebelumnya"
            style={{ padding: '0.25rem 0.6rem', fontSize: '1rem', lineHeight: 1 }}
          >‹</button>
          <span style={{ fontWeight: 600, fontSize: '0.9rem', minWidth: '120px', textAlign: 'center' }}>
            {fmtDate(date)}{isToday && <span style={{ marginLeft: '0.35rem', fontSize: '0.72rem', background: '#e8f5e9', color: '#2e7d32', borderRadius: '4px', padding: '0.05rem 0.35rem', fontWeight: 700 }}>Hari ini</span>}
          </span>
          <button
            className="btn btn-secondary btn-sm"
            onClick={() => setDate(d => offsetDate(d, 1))}
            disabled={isToday}
            title="Hari berikutnya"
            style={{ padding: '0.25rem 0.6rem', fontSize: '1rem', lineHeight: 1 }}
          >›</button>
        </div>
      </div>

      {loading ? (
        <p style={{ color: '#999', fontSize: '0.88rem', padding: '0.5rem 0' }}>Memuat...</p>
      ) : !data ? (
        <p style={{ color: '#e74c3c', fontSize: '0.88rem' }}>Gagal memuat data.</p>
      ) : (
        <>
          <table>
            <thead>
              <tr>
                <th>Cabang</th>
                <th style={{ textAlign: 'right' }}>POS Import</th>
                <th style={{ textAlign: 'right' }}>Manual</th>
                <th style={{ textAlign: 'right' }}>Total</th>
              </tr>
            </thead>
            <tbody>
              {data.branches.map(b => (
                <tr key={b.branch_id}>
                  <td style={{ fontWeight: 500 }}>{b.branch_name}</td>
                  <td style={{ textAlign: 'right', color: b.pos_revenue > 0 ? '#2c6fc2' : '#bbb', fontWeight: b.pos_revenue > 0 ? 600 : 'normal', fontSize: '0.88rem' }}>
                    {b.pos_revenue > 0 ? idr(b.pos_revenue) : '—'}
                    {b.pos_import_count > 0 && <span style={{ marginLeft: '0.3rem', fontSize: '0.75rem', color: '#888' }}>({b.pos_import_count}x)</span>}
                  </td>
                  <td style={{ textAlign: 'right', color: b.manual_sales > 0 ? '#555' : '#bbb', fontSize: '0.88rem' }}>
                    {b.manual_sales > 0 ? idr(b.manual_sales) : '—'}
                    {b.sale_count > 0 && <span style={{ marginLeft: '0.3rem', fontSize: '0.75rem', color: '#888' }}>({b.sale_count}x)</span>}
                  </td>
                  <td style={{ textAlign: 'right', fontWeight: b.total > 0 ? 700 : 'normal', color: b.total > 0 ? '#1a6632' : '#bbb' }}>
                    {idr(b.total)}
                  </td>
                </tr>
              ))}
              {data.branches.length > 0 && (
                <tr style={{ borderTop: '2px solid #e0e0e0', fontWeight: 700 }}>
                  <td>Total</td>
                  <td style={{ textAlign: 'right', color: '#2c6fc2' }}>{idr(data.branches.reduce((s, b) => s + b.pos_revenue, 0))}</td>
                  <td style={{ textAlign: 'right', color: '#555' }}>{idr(data.branches.reduce((s, b) => s + b.manual_sales, 0))}</td>
                  <td style={{ textAlign: 'right', color: grandTotal > 0 ? '#1a6632' : '#bbb' }}>{idr(grandTotal)}</td>
                </tr>
              )}
            </tbody>
          </table>
          {data.branches.length === 0 && (
            <p style={{ color: '#999', fontSize: '0.88rem', margin: '0.5rem 0 0' }}>Tidak ada cabang terdaftar.</p>
          )}
        </>
      )}
    </div>
  );
}

export default function Dashboard() {
  const [stats, setStats] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    getStats().then(r => setStats(r.data)).catch(err => setError(err.response?.data?.error || err.message || 'Gagal memuat dasbor'));
  }, []);

  if (error) return <p style={{ padding: '2rem', color: '#e74c3c' }}>Error: {error}</p>;
  if (!stats) return <p style={{ padding: '2rem', color: '#999' }}>Memuat...</p>;

  const outstanding = stats.outstandingInvoices || [];
  const overdueCount = outstanding.filter(inv => inv.due_date && inv.due_date < todayStr).length;

  return (
    <>
      <div className="page-header">
        <h1>Dasbor</h1>
      </div>

      {/* Stat cards */}
      <div className="stats-grid" style={{ marginBottom: '1.5rem' }}>
        <div className="stat-card">
          <div className="label">Total Produk</div>
          <div className="value">{stats.totalItems}</div>
          <div style={{ fontSize: '0.8rem', color: '#aaa', marginTop: '0.25rem' }}>
            {stats.totalInventoryRecords} catatan inventaris
          </div>
        </div>

        <div className="stat-card">
          <div className="label">Nilai Inventaris Global</div>
          <div className="value" style={{ fontSize: '1.25rem' }}>{idr(stats.totalInventoryValue)}</div>
        </div>

        <div className="stat-card">
          <div className="label">Pembelian Hari Ini</div>
          <div className="value" style={{ fontSize: '1.25rem', color: stats.todayPurchasesTotal > 0 ? '#2c6fc2' : '#ccc' }}>
            {idr(stats.todayPurchasesTotal)}
          </div>
          {stats.todayPurchasesCount > 0 && (
            <div style={{ fontSize: '0.8rem', color: '#aaa', marginTop: '0.25rem' }}>
              {stats.todayPurchasesCount} invoice
            </div>
          )}
        </div>

        <Link to="/invoices?status=unpaid" className="stat-card" style={{ textDecoration: 'none', cursor: 'pointer', background: outstanding.length > 0 ? '#fff8e1' : undefined, borderColor: outstanding.length > 0 ? '#ffe082' : undefined }}>
          <div className="label" style={{ color: outstanding.length > 0 ? '#b45309' : undefined }}>Invoice Belum Lunas</div>
          <div className="value" style={{ color: outstanding.length > 0 ? '#b45309' : '#ccc' }}>
            {outstanding.length}
          </div>
          {overdueCount > 0 && (
            <div style={{ fontSize: '0.8rem', color: '#e74c3c', fontWeight: 600, marginTop: '0.25rem' }}>
              {overdueCount} lewat jatuh tempo
            </div>
          )}
        </Link>
      </div>

      {/* Daily sales by branch */}
      <DailySalesCard />

      {/* Outstanding invoices */}
      {outstanding.length > 0 && (
        <div className="card" style={{ marginBottom: '1.5rem' }}>
          <div className="card-header">
            <h2>Invoice Belum Lunas ({outstanding.length})</h2>
            <Link to="/invoices" className="btn btn-secondary btn-sm">Lihat Semua Invoice</Link>
          </div>
          <table>
            <thead>
              <tr>
                <th>No. Invoice</th>
                <th>Vendor</th>
                <th>Tanggal</th>
                <th>Jatuh Tempo</th>
                <th style={{ textAlign: 'right' }}>Total</th>
                <th style={{ textAlign: 'right' }}>Sisa</th>
                <th>Status</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {outstanding.map(inv => {
                const remaining = Number(inv.total) - Number(inv.amount_paid);
                const isOverdue = inv.due_date && inv.due_date < todayStr;
                return (
                  <tr key={inv.id} style={{ background: isOverdue ? '#fff5f5' : undefined }}>
                    <td style={{ fontWeight: 600 }}>{inv.invoice_number}</td>
                    <td style={{ color: '#666', fontSize: '0.88rem' }}>{inv.vendor_name || '—'}</td>
                    <td style={{ color: '#888', fontSize: '0.85rem' }}>{fmtDate(inv.date)}</td>
                    <td style={{ fontSize: '0.85rem', fontWeight: isOverdue ? 700 : 'normal', color: isOverdue ? '#e74c3c' : '#555', whiteSpace: 'nowrap' }}>
                      {inv.due_date ? fmtDate(inv.due_date) : '—'}
                      {isOverdue && (
                        <span style={{ marginLeft: '0.35rem', fontSize: '0.7rem', background: '#fdecea', color: '#e74c3c', borderRadius: '3px', padding: '0.05rem 0.3rem', fontWeight: 700 }}>
                          LEWAT
                        </span>
                      )}
                    </td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(inv.total)}</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: '#e67e22' }}>{idr(remaining)}</td>
                    <td>
                      <span style={{
                        display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px',
                        fontSize: '0.75rem', fontWeight: 600,
                        background: inv.payment_status === 'partial' ? '#fff3e0' : '#fdecea',
                        color: inv.payment_status === 'partial' ? '#e67e22' : '#c0392b',
                      }}>
                        {inv.payment_status === 'partial' ? 'Sebagian' : 'Belum Dibayar'}
                      </span>
                    </td>
                    <td>
                      <Link to={`/invoices/view/${inv.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {outstanding.length === 0 && (
        <div style={{ background: '#e6f9f0', border: '1px solid #b2dfdb', borderRadius: '8px', padding: '1rem 1.5rem', color: '#1b5e45', fontWeight: 500, fontSize: '0.9rem', marginBottom: '1.5rem' }}>
          Semua invoice sudah lunas.
        </div>
      )}

      {/* Recent activity */}
      {(stats.recentActivity || []).length > 0 && (
        <div className="card">
          <div className="card-header" style={{ marginBottom: '0.75rem' }}>
            <h2>Aktivitas Terakhir</h2>
          </div>
          <table>
            <thead>
              <tr>
                <th>Waktu</th>
                <th>Pengguna</th>
                <th>Aksi</th>
                <th>Keterangan</th>
              </tr>
            </thead>
            <tbody>
              {stats.recentActivity.map(a => (
                <tr key={a.id}>
                  <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>
                    {new Date(a.created_at).toLocaleString('id-ID')}
                  </td>
                  <td style={{ fontWeight: 500, fontSize: '0.88rem' }}>{a.username || '—'}</td>
                  <td>
                    <span style={{
                      display: 'inline-block', padding: '0.1rem 0.45rem', borderRadius: '4px', fontSize: '0.75rem', fontWeight: 600,
                      background: a.action === 'create' ? '#e8f5e9' : a.action === 'delete' ? '#fdecea' : '#e8f0fe',
                      color:      a.action === 'create' ? '#2e7d32' : a.action === 'delete' ? '#c0392b' : '#3949ab',
                    }}>
                      {a.action}
                    </span>
                  </td>
                  <td style={{ color: '#555', fontSize: '0.85rem' }}>{a.description}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
