import { useEffect, useMemo, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getVendorHistory } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(v) || 0);
const num = (v) => Number(v ?? 0).toLocaleString('id-ID', { maximumFractionDigits: 3 });
const fmt = (d) => (d ? new Date(d).toLocaleDateString('id-ID', { dateStyle: 'medium' }) : '—');
const todayStr = new Date().toISOString().split('T')[0];

const STATUS_LABEL = { unpaid: 'Belum Dibayar', paid: 'Lunas', partial: 'Sebagian' };
const STATUS_CLASS = { unpaid: 'status-unpaid', paid: 'status-paid', partial: 'status-partial' };
const TYPE_LABEL   = { purchase: 'Pembelian', expense: 'Biaya' };

const TABS = [
  ['invoice', 'Invoice'],
  ['barang',  'Barang Dibeli'],
];

const dash = <span style={{ color: '#ccc' }}>—</span>;

// Invoices settled straight from a cash account keep amount_paid = 0 and only
// carry payment_status, so 'paid' has to be read as "fully paid" here.
const paidOf      = (inv) => (inv.payment_status === 'paid' ? Number(inv.total_amount) : Number(inv.amount_paid));
const remainingOf = (inv) => (inv.payment_status === 'paid' ? 0 : Number(inv.total_amount) - Number(inv.amount_paid));

const avgPrice = (r) => {
  const qty = Number(r.total_quantity ?? 0);
  return qty > 0 ? Number(r.total_spend ?? 0) / qty : null;
};

function Stat({ label, value, sub, color, labelColor }) {
  return (
    <div className="card" style={{ padding: '1.15rem' }}>
      <div style={{ fontSize: '0.72rem', color: labelColor ?? '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.35rem' }}>
        {label}
      </div>
      <div style={{ fontSize: '1.25rem', fontWeight: 700, color: color ?? '#333' }}>{value}</div>
      {sub && <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>{sub}</div>}
    </div>
  );
}

const Empty = ({ children, cols }) => (
  <tr><td colSpan={cols} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>{children}</td></tr>
);

export default function VendorHistory() {
  const { id } = useParams();
  const [data, setData]       = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError]     = useState('');
  const [tab, setTab]         = useState('invoice');
  const [filter, setFilter]   = useState('all'); // 'all' | 'unpaid' | 'partial' | 'paid'
  const [itemSearch, setItemSearch] = useState('');

  useEffect(() => {
    setLoading(true);
    setError('');
    getVendorHistory(id)
      .then((r) => setData(r.data))
      .catch((err) => setError(err.response?.data?.error || 'Gagal memuat data vendor.'))
      .finally(() => setLoading(false));
  }, [id]);

  const items = data?.items ?? [];

  const visible = useMemo(() => {
    const invoices = data?.invoices ?? [];
    return filter === 'all' ? invoices : invoices.filter((inv) => inv.payment_status === filter);
  }, [data, filter]);

  const visibleItems = useMemo(() => {
    const rows = data?.items ?? [];
    const q = itemSearch.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((r) =>
      r.item_name.toLowerCase().includes(q) || (r.item_code ?? '').toLowerCase().includes(q));
  }, [data, itemSearch]);

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Memuat…</div>;
  if (error)   return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>{error}</div>;
  if (!data)   return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>Vendor tidak ditemukan.</div>;

  const { vendor, summary } = data;
  const itemSpend = items.reduce((s, r) => s + Number(r.total_spend), 0);

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.2rem' }}>{vendor.name}</h1>
          <div style={{ fontSize: '0.85rem', color: '#888' }}>Riwayat Aktivitas Vendor</div>
        </div>
        <Link to="/vendors" className="btn btn-secondary">← Kembali</Link>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', gap: '1rem', marginBottom: '1.25rem' }}>
        <Stat
          label="Total Ditagih"
          value={idr(summary.total_invoiced)}
          sub={`${summary.invoice_count} invoice`}
        />
        <Stat label="Total Dibayar" value={idr(summary.total_paid)} color="#27ae60" />
        <Stat
          label="Sisa Hutang"
          value={idr(summary.total_outstanding)}
          color={summary.total_outstanding > 0 ? '#e67e22' : '#27ae60'}
          labelColor={summary.total_outstanding > 0 ? '#b45309' : undefined}
          sub={`${summary.unpaid_count} invoice belum lunas`}
        />
        <Stat
          label="Jenis Barang"
          value={summary.item_count}
          sub={`total ${idr(itemSpend)}`}
          color="#8b5cf6"
        />
      </div>

      <div style={{ display: 'flex', gap: '0.4rem', marginBottom: '1rem', flexWrap: 'wrap' }}>
        {TABS.map(([key, label]) => (
          <button
            key={key}
            type="button"
            onClick={() => setTab(key)}
            className={tab === key ? 'btn btn-primary btn-sm' : 'btn btn-secondary btn-sm'}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === 'invoice' && (
        <div className="card">
          <div className="card-header">
            <h2>{visible.length} invoice{filter !== 'all' ? ` · ${STATUS_LABEL[filter]}` : ''}</h2>
            <div className="filters">
              <select value={filter} onChange={(e) => setFilter(e.target.value)}>
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
                <th>Jenis</th>
                <th>Tanggal</th>
                <th>Jatuh Tempo</th>
                <th>Akun Pembayaran</th>
                <th style={{ textAlign: 'right' }}>Nilai Vendor</th>
                <th style={{ textAlign: 'right' }}>Total Invoice</th>
                <th style={{ textAlign: 'right' }}>Dibayar</th>
                <th style={{ textAlign: 'right' }}>Sisa</th>
                <th>Status</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {visible.length === 0 ? (
                <Empty cols={12}>Tidak ada invoice</Empty>
              ) : visible.map((inv) => {
                const total     = Number(inv.total_amount);
                const paid      = paidOf(inv);
                const remaining = remainingOf(inv);
                const isOverdue = inv.due_date && inv.payment_status !== 'paid' && inv.due_date.split('T')[0] < todayStr;
                return (
                  <tr key={inv.id}>
                    <td style={{ fontWeight: 600 }}>
                      {inv.invoice_number}
                      {!inv.is_primary && (
                        <span
                          title="Invoice ini milik vendor lain; hanya sebagian barisnya berasal dari vendor ini."
                          style={{ marginLeft: '0.4rem', fontSize: '0.68rem', background: '#eef2ff', color: '#4f46e5', borderRadius: '3px', padding: '0.05rem 0.3rem', fontWeight: 700 }}
                        >
                          SEBAGIAN
                        </span>
                      )}
                    </td>
                    <td style={{ color: '#888', fontSize: '0.85rem' }}>{inv.reference_number ?? dash}</td>
                    <td style={{ color: '#666', fontSize: '0.85rem' }}>{TYPE_LABEL[inv.invoice_type] ?? inv.invoice_type}</td>
                    <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(inv.date)}</td>
                    <td style={{ fontSize: '0.85rem', whiteSpace: 'nowrap', fontWeight: isOverdue ? 700 : 'normal', color: isOverdue ? '#e74c3c' : '#555' }}>
                      {fmt(inv.due_date)}
                      {isOverdue && (
                        <span style={{ marginLeft: '0.35rem', fontSize: '0.7rem', background: '#fdecea', color: '#e74c3c', borderRadius: '3px', padding: '0.05rem 0.3rem', fontWeight: 700 }}>LEWAT</span>
                      )}
                    </td>
                    <td style={{ color: '#888', fontSize: '0.85rem' }}>{inv.account_name ?? dash}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(inv.vendor_amount)}</td>
                    <td style={{ textAlign: 'right', color: '#888' }}>{idr(total)}</td>
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
                  <td colSpan={6} style={{ fontWeight: 600, color: '#555', paddingTop: '0.6rem' }}>Subtotal ({visible.length} invoice)</td>
                  <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.6rem' }}>
                    {idr(visible.reduce((s, i) => s + Number(i.vendor_amount), 0))}
                  </td>
                  <td style={{ textAlign: 'right', fontWeight: 700, color: '#888', paddingTop: '0.6rem' }}>
                    {idr(visible.reduce((s, i) => s + Number(i.total_amount), 0))}
                  </td>
                  <td style={{ textAlign: 'right', fontWeight: 700, color: '#27ae60', paddingTop: '0.6rem' }}>
                    {idr(visible.reduce((s, i) => s + paidOf(i), 0))}
                  </td>
                  <td style={{ textAlign: 'right', fontWeight: 700, color: '#e67e22', paddingTop: '0.6rem' }}>
                    {idr(visible.reduce((s, i) => s + remainingOf(i), 0))}
                  </td>
                  <td colSpan={2} />
                </tr>
              </tfoot>
            )}
          </table>
        </div>
      )}

      {tab === 'barang' && (
        <div className="card">
          <div className="card-header">
            <h2>{visibleItems.length} barang dibeli dari vendor ini</h2>
            <div className="filters">
              <input
                type="text"
                placeholder="Cari barang…"
                value={itemSearch}
                onChange={(e) => setItemSearch(e.target.value)}
              />
            </div>
          </div>

          <table>
            <thead>
              <tr>
                <th>Barang</th>
                <th>Satuan</th>
                <th style={{ textAlign: 'right' }}>Harga Terakhir</th>
                <th>Tgl Terakhir</th>
                <th style={{ textAlign: 'right' }}>Harga Rata-rata</th>
                <th style={{ textAlign: 'right' }}>Terendah</th>
                <th style={{ textAlign: 'right' }}>Tertinggi</th>
                <th style={{ textAlign: 'right' }}>Total Qty</th>
                <th style={{ textAlign: 'right' }}>Total Belanja</th>
                <th style={{ textAlign: 'right' }}>Beli</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {visibleItems.length === 0 ? (
                <Empty cols={11}>Belum ada barang dibeli dari vendor ini.</Empty>
              ) : visibleItems.map((r) => {
                const avg  = avgPrice(r);
                const path = r.is_stock ? `/items/stock/${r.item_id}` : `/items/history/${r.item_id}`;
                return (
                  <tr key={`${r.item_id}-${r.unit_index}`}>
                    <td>
                      <Link to={path} style={{ color: '#4f8ef7', textDecoration: 'none', fontWeight: 600 }}>{r.item_name}</Link>
                      {r.item_code && <div style={{ fontSize: '0.75rem', color: '#aaa' }}>{r.item_code}</div>}
                    </td>
                    <td style={{ color: '#666' }}>{r.unit_name ?? dash}</td>
                    <td style={{ textAlign: 'right', fontWeight: 700 }}>{idr(r.last_price)}</td>
                    <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(r.last_purchase_date)}</td>
                    <td style={{ textAlign: 'right', color: '#555' }}>{avg == null ? dash : idr(avg)}</td>
                    <td style={{ textAlign: 'right', color: '#27ae60' }}>{idr(r.min_price)}</td>
                    <td style={{ textAlign: 'right', color: '#e74c3c' }}>{idr(r.max_price)}</td>
                    <td style={{ textAlign: 'right' }}>{num(r.total_quantity)}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(r.total_spend)}</td>
                    <td style={{ textAlign: 'right', color: '#888' }}>{r.purchase_count}</td>
                    <td>
                      <Link to={path} className="btn btn-secondary btn-sm">Riwayat Harga</Link>
                    </td>
                  </tr>
                );
              })}
            </tbody>
            {visibleItems.length > 0 && (
              <tfoot>
                <tr style={{ borderTop: '2px solid #e8e8e8', background: '#fafafa' }}>
                  <td colSpan={8} style={{ fontWeight: 600, color: '#555', paddingTop: '0.6rem' }}>
                    Total ({visibleItems.length} baris)
                  </td>
                  <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.6rem' }}>
                    {idr(visibleItems.reduce((s, r) => s + Number(r.total_spend), 0))}
                  </td>
                  <td colSpan={2} />
                </tr>
              </tfoot>
            )}
          </table>
        </div>
      )}
    </>
  );
}
