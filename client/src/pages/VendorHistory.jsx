import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getVendorHistory } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';
const todayStr = new Date().toISOString().split('T')[0];

const STATUS_LABEL = { unpaid: 'Belum Dibayar', paid: 'Lunas', partial: 'Sebagian' };
const STATUS_CLASS  = { unpaid: 'status-unpaid', paid: 'status-paid', partial: 'status-partial' };

export default function VendorHistory() {
  const { id } = useParams();
  const [data, setData]       = useState(null);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter]   = useState('all'); // 'all' | 'unpaid' | 'partial' | 'paid'

  useEffect(() => {
    getVendorHistory(id)
      .then(r => setData(r.data))
      .finally(() => setLoading(false));
  }, [id]);

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Memuat…</div>;
  if (!data)   return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>Vendor tidak ditemukan.</div>;

  const { vendor, invoices, summary } = data;

  const visible = filter === 'all' ? invoices : invoices.filter(inv => inv.payment_status === filter);

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.2rem' }}>{vendor.name}</h1>
          <div style={{ fontSize: '0.85rem', color: '#888' }}>Riwayat Pembayaran Vendor</div>
        </div>
        <Link to="/vendors" className="btn btn-secondary">← Kembali</Link>
      </div>

      {/* Summary cards */}
      <div className="stats-grid" style={{ marginBottom: '1.5rem' }}>
        <div className="stat-card">
          <div className="label">Total Ditagih</div>
          <div className="value" style={{ fontSize: '1.2rem' }}>{idr(summary.totalInvoiced)}</div>
          <div style={{ fontSize: '0.8rem', color: '#aaa', marginTop: '0.2rem' }}>{invoices.length} invoice</div>
        </div>
        <div className="stat-card">
          <div className="label">Total Dibayar</div>
          <div className="value" style={{ fontSize: '1.2rem', color: '#27ae60' }}>{idr(summary.totalPaid)}</div>
        </div>
        <div className={`stat-card${summary.totalOutstanding > 0 ? ' warning' : ''}`}>
          <div className="label" style={{ color: summary.totalOutstanding > 0 ? '#b45309' : undefined }}>Sisa Hutang</div>
          <div className="value" style={{ fontSize: '1.2rem', color: summary.totalOutstanding > 0 ? '#e67e22' : '#27ae60' }}>
            {idr(summary.totalOutstanding)}
          </div>
        </div>
        <div className="stat-card">
          <div className="label">Belum Lunas</div>
          <div className="value" style={{ color: invoices.filter(i => i.payment_status !== 'paid').length > 0 ? '#e67e22' : '#27ae60' }}>
            {invoices.filter(i => i.payment_status !== 'paid').length}
          </div>
          <div style={{ fontSize: '0.8rem', color: '#aaa', marginTop: '0.2rem' }}>invoice</div>
        </div>
      </div>

      {/* Invoice table */}
      <div className="card">
        <div className="card-header">
          <h2>{visible.length} invoice{filter !== 'all' ? ` · ${STATUS_LABEL[filter]}` : ''}</h2>
          <div className="filters">
            <select value={filter} onChange={e => setFilter(e.target.value)}>
              <option value="all">Semua Status</option>
              <option value="unpaid">Belum Dibayar</option>
              <option value="partial">Sebagian</option>
              <option value="paid">Lunas</option>
            </select>
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>No. Invoice</th>
              <th>Ref. No.</th>
              <th>Tanggal</th>
              <th>Jatuh Tempo</th>
              <th>Akun Pembayaran</th>
              <th style={{ textAlign: 'right' }}>Total</th>
              <th style={{ textAlign: 'right' }}>Dibayar</th>
              <th style={{ textAlign: 'right' }}>Sisa</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {visible.length === 0 ? (
              <tr><td colSpan={10} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Tidak ada invoice</td></tr>
            ) : visible.map(inv => {
              const total     = Number(inv.total);
              const paid      = Number(inv.amount_paid);
              const remaining = total - paid;
              const isOverdue = inv.due_date && inv.payment_status !== 'paid' && inv.due_date.split('T')[0] < todayStr;
              return (
                <tr key={inv.id}>
                  <td style={{ fontWeight: 600 }}>{inv.invoice_number}</td>
                  <td style={{ color: '#888', fontSize: '0.85rem' }}>{inv.reference_number ?? '—'}</td>
                  <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(inv.date)}</td>
                  <td style={{ fontSize: '0.85rem', whiteSpace: 'nowrap', fontWeight: isOverdue ? 700 : 'normal', color: isOverdue ? '#e74c3c' : '#555' }}>
                    {fmt(inv.due_date)}
                    {isOverdue && (
                      <span style={{ marginLeft: '0.35rem', fontSize: '0.7rem', background: '#fdecea', color: '#e74c3c', borderRadius: '3px', padding: '0.05rem 0.3rem', fontWeight: 700 }}>LEWAT</span>
                    )}
                  </td>
                  <td style={{ color: '#888', fontSize: '0.85rem' }}>{inv.account_name ?? '—'}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(total)}</td>
                  <td style={{ textAlign: 'right', color: paid > 0 ? '#27ae60' : '#ccc', fontWeight: 500 }}>{idr(paid)}</td>
                  <td style={{ textAlign: 'right', color: remaining > 0 ? '#e67e22' : '#27ae60', fontWeight: remaining > 0 ? 700 : 400 }}>
                    {idr(remaining)}
                  </td>
                  <td>
                    <span className={`badge ${STATUS_CLASS[inv.payment_status] ?? ''}`}>
                      {STATUS_LABEL[inv.payment_status] ?? inv.payment_status}
                    </span>
                  </td>
                  <td>
                    <Link to={`/invoices/view/${inv.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                  </td>
                </tr>
              );
            })}
          </tbody>
          {visible.length > 0 && (
            <tfoot>
              <tr style={{ borderTop: '2px solid #e8e8e8', background: '#fafafa' }}>
                <td colSpan={5} style={{ fontWeight: 600, color: '#555', paddingTop: '0.6rem' }}>Subtotal ({visible.length} invoice)</td>
                <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.6rem' }}>
                  {idr(visible.reduce((s, i) => s + Number(i.total), 0))}
                </td>
                <td style={{ textAlign: 'right', fontWeight: 700, color: '#27ae60', paddingTop: '0.6rem' }}>
                  {idr(visible.reduce((s, i) => s + Number(i.amount_paid), 0))}
                </td>
                <td style={{ textAlign: 'right', fontWeight: 700, color: '#e67e22', paddingTop: '0.6rem' }}>
                  {idr(visible.reduce((s, i) => s + (Number(i.total) - Number(i.amount_paid)), 0))}
                </td>
                <td colSpan={2} />
              </tr>
            </tfoot>
          )}
        </table>
      </div>
    </>
  );
}
