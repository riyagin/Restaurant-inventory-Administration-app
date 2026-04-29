import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getStats } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '—';

const todayStr = new Date().toISOString().split('T')[0];

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
