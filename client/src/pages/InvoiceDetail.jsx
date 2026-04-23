import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getInvoice, deleteInvoicePhoto } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);
const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';

const STATUS_LABEL = { unpaid: 'Belum Dibayar', paid: 'Lunas', partial: 'Sebagian' };
const STATUS_CLASS  = { unpaid: 'status-unpaid', paid: 'status-paid', partial: 'status-partial' };

const SERVER = 'http://localhost:5000';

export default function InvoiceDetail() {
  const { id } = useParams();
  const [invoice, setInvoice] = useState(null);
  const [loading, setLoading] = useState(true);

  const reload = () => getInvoice(id).then(r => { setInvoice(r.data); setLoading(false); });

  useEffect(() => { reload(); }, [id]);

  const handleRemovePhoto = async () => {
    if (!confirm('Remove the attached photo?')) return;
    await deleteInvoicePhoto(id);
    reload();
  };

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Loading…</div>;
  if (!invoice) return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>Invoice not found.</div>;

  const isExpense = invoice.invoice_type === 'expense';
  const total = invoice.items.reduce((s, it) => s + Number(it.quantity) * Number(it.price), 0);

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
              {isExpense ? 'Expense' : 'Purchase'}
            </span>
          </div>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <Link to={`/invoices/edit/${id}`} className="btn btn-secondary">Edit</Link>
          <Link to="/invoices" className="btn btn-secondary">← Back</Link>
        </div>
      </div>

      {/* Summary */}
      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '1.5rem', padding: '0.5rem 0 1rem' }}>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Status</div>
            <span className={`badge ${STATUS_CLASS[invoice.payment_status] ?? ''}`}>{STATUS_LABEL[invoice.payment_status] ?? invoice.payment_status}</span>
          </div>
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
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Account</div>
            <div style={{ fontWeight: 500 }}>{invoice.account_name ?? '—'}</div>
          </div>
          {isExpense && (
            <>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Branch</div>
                <div style={{ fontWeight: 500 }}>{invoice.branch_name ?? '—'}</div>
              </div>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Division</div>
                <div style={{ fontWeight: 500 }}>{invoice.division_name ?? '—'}</div>
              </div>
            </>
          )}
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Total</div>
            <div style={{ fontWeight: 700, fontSize: '1.1rem' }}>{idr(total)}</div>
          </div>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: invoice.photo_path ? '1fr 320px' : '1fr', gap: '1.5rem', alignItems: 'start' }}>
        {/* Items table */}
        <div className="card">
          <div className="card-header"><h2>{invoice.items.length} line item{invoice.items.length !== 1 ? 's' : ''}</h2></div>
          <table>
            <thead>
              <tr>
                {isExpense ? (
                  <>
                    <th>Item</th>
                    <th style={{ textAlign: 'right' }}>Qty</th>
                    <th>Unit</th>
                    <th style={{ textAlign: 'right' }}>Unit Price</th>
                    <th style={{ textAlign: 'right' }}>Subtotal</th>
                  </>
                ) : (
                  <>
                    <th>Item</th>
                    <th>Code</th>
                    <th>Vendor</th>
                    <th style={{ textAlign: 'right' }}>Qty</th>
                    <th>Unit</th>
                    <th style={{ textAlign: 'right' }}>Unit Price</th>
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
                    Open in new tab
                  </a>
                  <button onClick={handleRemovePhoto} className="btn btn-danger btn-sm">Remove</button>
                </div>
              </>
            )}
          </div>
        )}
      </div>
    </>
  );
}
