import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { getInvoices, deleteInvoice, payInvoice, getAccounts } from '../api';

function getUser() {
  try { return JSON.parse(localStorage.getItem('user')); } catch { return null; }
}

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const STATUS_LABEL = { unpaid: 'Belum Dibayar', paid: 'Lunas', partial: 'Sebagian' };
const STATUS_CLASS  = { unpaid: 'status-unpaid', paid: 'status-paid', partial: 'status-partial' };

const PAGE_SIZE = 25;
const todayStr = new Date().toISOString().split('T')[0];
const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';

export default function Invoices() {
  const currentUser = getUser();
  const isAdmin = currentUser?.role === 'admin';

  const [invoices, setInvoices]           = useState([]);
  const [total, setTotal]                 = useState(0);
  const [outstandingTotal, setOutTotal]   = useState(0);
  const [outstandingCount, setOutCount]   = useState(0);
  const [page, setPage]                   = useState(1);
  const [loading, setLoading]             = useState(false);

  const [accounts, setAccounts]           = useState([]);
  const [payTarget, setPayTarget]         = useState(null); // invoice being paid
  const [payForm, setPayForm]             = useState({ cash_account_id: '', amount: '' });
  const [paying, setPaying]               = useState(false);
  const [payError, setPayError]           = useState('');

  const [search,   setSearch]   = useState('');
  const [status,   setStatus]   = useState('all');
  const [type,     setType]     = useState('all');
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo,   setDateTo]   = useState('');

  const load = useCallback(() => {
    setLoading(true);
    const params = { page, limit: PAGE_SIZE };
    if (search)            params.search    = search;
    if (status !== 'all')  params.status    = status;
    if (type   !== 'all')  params.type      = type;
    if (dateFrom)          params.date_from = dateFrom;
    if (dateTo)            params.date_to   = dateTo;
    getInvoices(params)
      .then(r => {
        setInvoices(r.data.invoices);
        setTotal(r.data.total);
        setOutTotal(r.data.outstanding_total ?? 0);
        setOutCount(r.data.outstanding_count ?? 0);
      })
      .finally(() => setLoading(false));
  }, [search, status, type, dateFrom, dateTo, page]);

  useEffect(() => { load(); }, [load]);
  useEffect(() => { getAccounts().then(r => setAccounts(r.data)); }, []);

  // Reset to page 1 whenever filters change
  const setFilter = (setter) => (val) => { setter(val); setPage(1); };

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus invoice ini beserta semua itemnya?')) return;
    await deleteInvoice(id);
    load();
  };

  const openPay = (inv) => {
    const invTotal = Number(inv.total);
    const amountPaid = Number(inv.amount_paid ?? 0);
    setPayTarget(inv);
    setPayForm({ cash_account_id: '', amount: String(invTotal - amountPaid) });
    setPayError('');
  };

  const handlePay = async (e) => {
    e.preventDefault();
    setPayError('');
    setPaying(true);
    try {
      await payInvoice(payTarget.id, {
        cash_account_id: payForm.cash_account_id,
        amount: Number(payForm.amount),
      });
      setPayTarget(null);
      load();
    } catch (err) {
      setPayError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setPaying(false);
    }
  };

  const clearFilters = () => {
    setSearch(''); setStatus('all'); setType('all');
    setDateFrom(''); setDateTo(''); setPage(1);
  };

  const totalPages  = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const hasFilters  = search || status !== 'all' || type !== 'all' || dateFrom || dateTo;
  const pageStart   = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1;
  const pageEnd     = Math.min(page * PAGE_SIZE, total);

  return (
    <>
      <div className="page-header">
        <h1>Invoice</h1>
        <Link to="/invoices/new" className="btn btn-primary">+ Invoice Baru</Link>
      </div>

      {outstandingCount > 0 && (
        <div style={{
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
          background: '#fff8e1', border: '1px solid #ffe082', borderRadius: '8px',
          padding: '0.9rem 1.25rem', marginBottom: '1.25rem',
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
            <span style={{ fontSize: '1.1rem' }}>⚠</span>
            <span style={{ fontWeight: 600, color: '#b45309' }}>
              {outstandingCount} invoice belum lunas
            </span>
          </div>
          <span style={{ fontWeight: 700, fontSize: '1.1rem', color: '#b45309' }}>
            {idr(outstandingTotal)}
          </span>
        </div>
      )}

      <div className="card">
        <div className="card-header">
          <h2>
            {loading ? 'Memuat…' : (
              total === 0 ? 'Tidak ada invoice' : `${pageStart}–${pageEnd} dari ${total} invoice`
            )}
          </h2>
          <div className="filters">
            <input
              placeholder="Cari no. invoice, ref, vendor…"
              value={search}
              onChange={e => setFilter(setSearch)(e.target.value)}
              style={{ minWidth: '220px' }}
            />
            <select value={type} onChange={e => setFilter(setType)(e.target.value)}>
              <option value="all">Semua Tipe</option>
              <option value="purchase">Pembelian</option>
              <option value="expense">Pengeluaran</option>
            </select>
            <select value={status} onChange={e => setFilter(setStatus)(e.target.value)}>
              <option value="all">Semua Status</option>
              <option value="unpaid">Belum Dibayar</option>
              <option value="partial">Sebagian</option>
              <option value="paid">Lunas</option>
            </select>
            <input type="date" value={dateFrom} onChange={e => setFilter(setDateFrom)(e.target.value)} title="Dari tanggal" />
            <input type="date" value={dateTo}   onChange={e => setFilter(setDateTo)(e.target.value)}   title="Sampai tanggal" />
            {hasFilters && (
              <button type="button" onClick={clearFilters} className="btn btn-secondary btn-sm">Bersihkan</button>
            )}
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>No. Invoice</th>
              <th>Ref. No.</th>
              <th>Tipe</th>
              <th>Tanggal</th>
              <th>Jatuh Tempo</th>
              <th>Gudang / Cabang</th>
              <th>Vendor</th>
              <th>Akun</th>
              <th>Total</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {invoices.length === 0 ? (
              <tr><td colSpan={11} style={{textAlign:'center',color:'#999',padding:'2rem'}}>
                {loading ? 'Memuat…' : 'Tidak ada invoice ditemukan'}
              </td></tr>
            ) : invoices.map(inv => {
              const isOverdue = inv.due_date && inv.payment_status !== 'paid' && inv.due_date < todayStr;
              return (
              <tr key={inv.id}>
                <td style={{fontWeight:600}}>{inv.invoice_number}</td>
                <td style={{color:'#888',fontSize:'0.85rem'}}>{inv.reference_number ?? '—'}</td>
                <td>
                  <span style={{
                    display:'inline-block',padding:'0.1rem 0.45rem',borderRadius:'4px',fontSize:'0.75rem',fontWeight:600,
                    background: inv.invoice_type === 'expense' ? '#fff3e0' : '#e6f9f0',
                    color: inv.invoice_type === 'expense' ? '#f57c00' : '#27ae60',
                  }}>
                    {inv.invoice_type === 'expense' ? 'Pengeluaran' : 'Pembelian'}
                  </span>
                </td>
                <td style={{color:'#888',fontSize:'0.85rem'}}>{fmtDate(inv.date)}</td>
                <td style={{
                  fontWeight: isOverdue ? 700 : 'normal',
                  color: isOverdue ? '#e74c3c' : inv.due_date ? '#555' : '#ccc',
                  fontSize: '0.85rem',
                  whiteSpace: 'nowrap',
                }}>
                  {inv.due_date ? fmtDate(inv.due_date) : '—'}
                  {isOverdue && <span style={{marginLeft:'0.35rem',fontSize:'0.7rem',background:'#fdecea',color:'#e74c3c',borderRadius:'3px',padding:'0.05rem 0.3rem',fontWeight:700}}>LEWAT</span>}
                </td>
                <td>
                  {inv.invoice_type === 'expense'
                    ? <span style={{color:'#555'}}>{[inv.branch_name, inv.division_name].filter(Boolean).join(' / ') || '—'}</span>
                    : <span style={{color:'#555'}}>{inv.warehouse_name ?? '—'}</span>
                  }
                </td>
                <td style={{color:'#888',fontSize:'0.85rem'}}>{inv.vendor_name ?? '—'}</td>
                <td style={{color:'#888'}}>{inv.account_name ?? '—'}</td>
                <td style={{fontWeight:600}}>
                  {idr(inv.total)}
                  {inv.payment_status === 'partial' && Number(inv.amount_paid) > 0 && (
                    <div style={{fontSize:'0.75rem',color:'#e67e22',fontWeight:400}}>
                      Sisa {idr(Number(inv.total) - Number(inv.amount_paid))}
                    </div>
                  )}
                </td>
                <td>
                  <span className={`badge ${STATUS_CLASS[inv.payment_status]}`}>
                    {STATUS_LABEL[inv.payment_status]}
                  </span>
                </td>
                <td>
                  <div className="actions">
                    <Link to={`/invoices/view/${inv.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                    {inv.payment_status !== 'paid' && (
                      <button onClick={() => openPay(inv)} className="btn btn-primary btn-sm">Bayar</button>
                    )}
                    <Link to={`/invoices/edit/${inv.id}`} className="btn btn-secondary btn-sm">Edit</Link>
                    {isAdmin && <button onClick={() => handleDelete(inv.id)} className="btn btn-danger btn-sm">Hapus</button>}
                  </div>
                </td>
              </tr>
              );
            })}
          </tbody>
        </table>

        {totalPages > 1 && (
          <div style={{display:'flex',alignItems:'center',justifyContent:'space-between',padding:'0.75rem 1.5rem',borderTop:'1px solid #f0f0f0'}}>
            <span style={{fontSize:'0.85rem',color:'#888'}}>
              Halaman {page} dari {totalPages}
            </span>
            <div style={{display:'flex',gap:'0.35rem'}}>
              <button
                className="btn btn-secondary btn-sm"
                onClick={() => setPage(1)}
                disabled={page === 1}
              >«</button>
              <button
                className="btn btn-secondary btn-sm"
                onClick={() => setPage(p => p - 1)}
                disabled={page === 1}
              >‹ Sebelumnya</button>

              {Array.from({ length: totalPages }, (_, i) => i + 1)
                .filter(p => p === 1 || p === totalPages || Math.abs(p - page) <= 2)
                .reduce((acc, p, idx, arr) => {
                  if (idx > 0 && p - arr[idx - 1] > 1) acc.push('…');
                  acc.push(p);
                  return acc;
                }, [])
                .map((p, idx) =>
                  p === '…'
                    ? <span key={`ellipsis-${idx}`} style={{padding:'0 0.3rem',color:'#aaa',lineHeight:'2'}}>…</span>
                    : <button
                        key={p}
                        className="btn btn-sm"
                        style={{
                          background: p === page ? '#4f8ef7' : undefined,
                          color: p === page ? '#fff' : undefined,
                          border: p === page ? 'none' : undefined,
                        }}
                        onClick={() => setPage(p)}
                      >{p}</button>
                )
              }

              <button
                className="btn btn-secondary btn-sm"
                onClick={() => setPage(p => p + 1)}
                disabled={page === totalPages}
              >Berikutnya ›</button>
              <button
                className="btn btn-secondary btn-sm"
                onClick={() => setPage(totalPages)}
                disabled={page === totalPages}
              >»</button>
            </div>
          </div>
        )}
      </div>

      {payTarget && (() => {
        const cashAccounts = accounts.filter(a => a.account_type === 'asset' && !a.is_system);
        const invTotal = Number(payTarget.total);
        const amountPaid = Number(payTarget.amount_paid ?? 0);
        const remaining = invTotal - amountPaid;
        return (
          <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
            <div className="card" style={{ width: '100%', maxWidth: '420px', padding: '2rem', margin: '1rem' }}>
              <h2 style={{ marginBottom: '0.5rem', fontSize: '1.1rem' }}>Bayar Invoice — {payTarget.invoice_number}</h2>
              <div style={{ marginBottom: '1.25rem', display: 'flex', gap: '1.5rem', fontSize: '0.85rem', color: '#666' }}>
                <span>Total: <strong>{idr(invTotal)}</strong></span>
                {amountPaid > 0 && <span>Sudah dibayar: <strong style={{ color: '#27ae60' }}>{idr(amountPaid)}</strong></span>}
                <span>Sisa: <strong style={{ color: '#e67e22' }}>{idr(remaining)}</strong></span>
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
                    max={remaining}
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
                  <button type="button" onClick={() => setPayTarget(null)} className="btn btn-secondary">Batal</button>
                </div>
              </form>
            </div>
          </div>
        );
      })()}
    </>
  );
}
