import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getInvoice, deleteInvoicePhoto, payInvoice, getAccounts } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);
const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';
const todayStr = new Date().toISOString().split('T')[0];

const STATUS_LABEL = { unpaid: 'Belum Dibayar', paid: 'Lunas', partial: 'Sebagian' };
const STATUS_CLASS  = { unpaid: 'status-unpaid', paid: 'status-paid', partial: 'status-partial' };

const SERVER = 'http://localhost:5000';

function getUser() {
  try { return JSON.parse(localStorage.getItem('user')); } catch { return null; }
}

export default function InvoiceDetail() {
  const { id } = useParams();
  const [invoice, setInvoice]       = useState(null);
  const [loading, setLoading]       = useState(true);
  const [accounts, setAccounts]     = useState([]);
  const [showPayModal, setShowPayModal] = useState(false);
  const [payForm, setPayForm]       = useState({ cash_account_id: '', amount: '' });
  const [paying, setPaying]         = useState(false);
  const [payError, setPayError]     = useState('');

  const currentUser = getUser();
  const isAdmin = currentUser?.role === 'admin';

  const reload = () => getInvoice(id).then(r => { setInvoice(r.data); setLoading(false); });

  useEffect(() => { reload(); }, [id]);
  useEffect(() => { getAccounts().then(r => setAccounts(r.data)); }, []);

  const handleRemovePhoto = async () => {
    if (!confirm('Hapus foto yang dilampirkan?')) return;
    await deleteInvoicePhoto(id);
    reload();
  };

  const openPayModal = (remaining) => {
    setPayForm({ cash_account_id: '', amount: String(remaining) });
    setPayError('');
    setShowPayModal(true);
  };

  const handlePay = async (e) => {
    e.preventDefault();
    setPayError('');
    setPaying(true);
    try {
      await payInvoice(id, {
        cash_account_id: payForm.cash_account_id,
        amount: Number(payForm.amount),
      });
      setShowPayModal(false);
      reload();
    } catch (err) {
      setPayError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setPaying(false);
    }
  };

  // Cash accounts: assets (account_type='asset') that are not system root accounts
  const cashAccounts = accounts.filter(a => a.account_type === 'asset' && !a.is_system);

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Memuat…</div>;
  if (!invoice) return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>Invoice tidak ditemukan.</div>;

  const isExpense = invoice.invoice_type === 'expense';
  const total = invoice.items.reduce((s, it) => s + Number(it.quantity) * Number(it.price), 0);
  const amountPaid = Number(invoice.amount_paid ?? 0);
  const remaining = total - amountPaid;

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.2rem' }}>{invoice.invoice_number}</h1>
          <div style={{ fontSize: '0.85rem', color: '#888' }}>
            {fmt(invoice.date)}
            {invoice.warehouse_name && <> &nbsp;·&nbsp; {invoice.warehouse_name}</>}
            &nbsp;·&nbsp;
            <span style={{ fontWeight: 600, color: isExpense ? '#e67e22' : '#27ae60' }}>
              {isExpense ? 'Pengeluaran' : 'Pembelian'}
            </span>
          </div>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          {invoice.payment_status !== 'paid' && (
            <button onClick={() => openPayModal(remaining)} className="btn btn-primary">
              💳 Bayar Invoice
            </button>
          )}
          {isAdmin && <Link to={`/invoices/edit/${id}`} className="btn btn-secondary">Edit</Link>}
          <Link to="/invoices" className="btn btn-secondary">← Kembali</Link>
        </div>
      </div>

      {/* Summary */}
      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '1.5rem', padding: '0.5rem 0 1rem' }}>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Status</div>
            <span className={`badge ${STATUS_CLASS[invoice.payment_status] ?? ''}`}>{STATUS_LABEL[invoice.payment_status] ?? invoice.payment_status}</span>
          </div>
          {invoice.due_date && (() => {
            const isOverdue = invoice.payment_status !== 'paid' && invoice.due_date.split('T')[0] < todayStr;
            return (
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Jatuh Tempo</div>
                <div style={{ fontWeight: 600, color: isOverdue ? '#e74c3c' : '#333' }}>
                  {fmt(invoice.due_date)}
                  {isOverdue && <span style={{ marginLeft: '0.4rem', fontSize: '0.7rem', background: '#fdecea', color: '#e74c3c', borderRadius: '3px', padding: '0.05rem 0.3rem', fontWeight: 700 }}>LEWAT</span>}
                </div>
              </div>
            );
          })()}
          {invoice.reference_number && (
            <div>
              <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Ref. No.</div>
              <div style={{ fontWeight: 600, fontFamily: 'monospace' }}>{invoice.reference_number}</div>
            </div>
          )}
          {invoice.vendor_name && (
            <div>
              <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Vendor</div>
              <div style={{ fontWeight: 500 }}>{invoice.vendor_name}</div>
            </div>
          )}
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Akun</div>
            <div style={{ fontWeight: 500 }}>{invoice.account_name ?? '—'}</div>
          </div>
          {isExpense && (
            <>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Cabang</div>
                <div style={{ fontWeight: 500 }}>{invoice.branch_name ?? '—'}</div>
              </div>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Divisi</div>
                <div style={{ fontWeight: 500 }}>{invoice.division_name ?? '—'}</div>
              </div>
            </>
          )}
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Total</div>
            <div style={{ fontWeight: 700, fontSize: '1.1rem' }}>{idr(total)}</div>
          </div>
          {invoice.payment_status === 'partial' && (
            <>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Sudah Dibayar</div>
                <div style={{ fontWeight: 700, fontSize: '1.1rem', color: '#27ae60' }}>{idr(amountPaid)}</div>
              </div>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Sisa Tagihan</div>
                <div style={{ fontWeight: 700, fontSize: '1.1rem', color: '#e67e22' }}>{idr(remaining)}</div>
              </div>
            </>
          )}
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: invoice.photo_path ? '1fr 320px' : '1fr', gap: '1.5rem', alignItems: 'start' }}>
        {/* Items table */}
        <div className="card">
          <div className="card-header"><h2>{invoice.items.length} baris item</h2></div>
          <table>
            <thead>
              <tr>
                {isExpense ? (
                  <>
                    <th>Barang</th>
                    <th style={{ textAlign: 'right' }}>Jumlah</th>
                    <th>Satuan</th>
                    <th style={{ textAlign: 'right' }}>Harga Satuan</th>
                    <th style={{ textAlign: 'right' }}>Subtotal</th>
                  </>
                ) : (
                  <>
                    <th>Barang</th>
                    <th>Kode</th>
                    <th>Vendor</th>
                    <th style={{ textAlign: 'right' }}>Jumlah</th>
                    <th>Satuan</th>
                    <th style={{ textAlign: 'right' }}>Harga Satuan</th>
                    <th style={{ textAlign: 'right' }}>Subtotal</th>
                  </>
                )}
              </tr>
            </thead>
            <tbody>
              {invoice.items.map(it => {
                const subtotal = Number(it.quantity) * Number(it.price);
                return isExpense ? (
                  <tr key={it.id}>
                    <td style={{ fontWeight: 500 }}>{it.item_name ?? it.description ?? '—'}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{Number(it.quantity).toLocaleString('id-ID')}</td>
                    <td style={{ color: '#555' }}>{it.units?.[Number(it.unit_index)]?.name ?? '—'}</td>
                    <td style={{ textAlign: 'right', color: '#555' }}>{idr(it.price)}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(subtotal)}</td>
                  </tr>
                ) : (
                  <tr key={it.id}>
                    <td style={{ fontWeight: 500 }}>{it.item_name}</td>
                    <td style={{ color: '#888', fontSize: '0.85rem' }}>{it.item_code}</td>
                    <td style={{ color: '#555' }}>{it.vendor_name}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{Number(it.quantity).toLocaleString('id-ID')}</td>
                    <td style={{ color: '#555' }}>{it.units?.[Number(it.unit_index)]?.name ?? '—'}</td>
                    <td style={{ textAlign: 'right', color: '#555' }}>{idr(it.price)}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(subtotal)}</td>
                  </tr>
                );
              })}
            </tbody>
            <tfoot>
              <tr>
                <td colSpan={isExpense ? 4 : 6} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>Total:</td>
                <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', fontSize: '1.05rem' }}>{idr(total)}</td>
              </tr>
            </tfoot>
          </table>
        </div>

        {/* Payment modal */}
        {showPayModal && (
          <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
            <div className="card" style={{ width: '100%', maxWidth: '420px', padding: '2rem', margin: '1rem' }}>
              <h2 style={{ marginBottom: '0.5rem', fontSize: '1.1rem' }}>Bayar Invoice — {invoice.invoice_number}</h2>
              <div style={{ marginBottom: '1.25rem', display: 'flex', gap: '1.5rem', fontSize: '0.85rem', color: '#666' }}>
                <span>Total: <strong>{idr(total)}</strong></span>
                {amountPaid > 0 && <span>Sudah dibayar: <strong style={{ color: '#27ae60' }}>{idr(amountPaid)}</strong></span>}
                <span>Sisa: <strong style={{ color: remaining > 0 ? '#e67e22' : '#27ae60' }}>{idr(remaining)}</strong></span>
              </div>
              {payError && <div className="error-msg" style={{ marginBottom: '1rem' }}>{payError}</div>}
              <form onSubmit={handlePay}>
                <div className="form-group">
                  <label>Akun Kas / Bank</label>
                  <select
                    value={payForm.cash_account_id}
                    onChange={e => setPayForm(f => ({ ...f, cash_account_id: e.target.value }))}
                    required
                  >
                    <option value="">— Pilih akun —</option>
                    {cashAccounts.map(a => (
                      <option key={a.id} value={a.id}>
                        {a.account_number ? `${a.account_number} · ` : ''}{a.name}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="form-group">
                  <label>Jumlah Dibayar (Rp)</label>
                  <input
                    type="number"
                    value={payForm.amount}
                    onChange={e => setPayForm(f => ({ ...f, amount: e.target.value }))}
                    min={1}
                    required
                  />
                  {payForm.amount && (
                    <small style={{ color: '#888', marginTop: '0.25rem', display: 'block' }}>
                      {idr(Number(payForm.amount))}
                      {Number(payForm.amount) < remaining && (
                        <span style={{ marginLeft: '0.5rem', color: '#e67e22', fontWeight: 600 }}> · Pembayaran sebagian</span>
                      )}
                    </small>
                  )}
                </div>
                <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1.5rem' }}>
                  <button type="submit" className="btn btn-primary" disabled={paying} style={{ flex: 1, justifyContent: 'center' }}>
                    {paying ? 'Memproses...' : 'Konfirmasi Pembayaran'}
                  </button>
                  <button type="button" onClick={() => setShowPayModal(false)} className="btn btn-secondary">Batal</button>
                </div>
              </form>
            </div>
          </div>
        )}

        {/* Photo panel */}
        {invoice.photo_path && (
          <div className="card">
            <div className="card-header" style={{ marginBottom: '0.75rem' }}><h2>Bukti Fisik</h2></div>
            {invoice.photo_path.match(/\.pdf$/i) ? (
              <div style={{ padding: '0.5rem 0' }}>
                <a
                  href={`${SERVER}${invoice.photo_path}`}
                  download
                  className="btn btn-primary"
                  style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}
                >
                  ⬇ Download PDF
                </a>
              </div>
            ) : (
              <>
                <img
                  src={`${SERVER}${invoice.photo_path}`}
                  alt="Receipt"
                  style={{ width: '100%', borderRadius: '6px', border: '1px solid #e8e8e8', objectFit: 'contain', maxHeight: '400px', marginBottom: '0.75rem' }}
                />
                <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                  <a
                    href={`${SERVER}${invoice.photo_path}`}
                    download
                    className="btn btn-primary btn-sm"
                    style={{ textDecoration: 'none' }}
                  >
                    ⬇ Download
                  </a>
                  <a
                    href={`${SERVER}${invoice.photo_path}`}
                    target="_blank"
                    rel="noreferrer"
                    className="btn btn-secondary btn-sm"
                    style={{ textDecoration: 'none' }}
                  >
                    Buka di tab baru
                  </a>
                  <button onClick={handleRemovePhoto} className="btn btn-danger btn-sm">Hapus</button>
                </div>
              </>
            )}
          </div>
        )}
      </div>
    </>
  );
}
