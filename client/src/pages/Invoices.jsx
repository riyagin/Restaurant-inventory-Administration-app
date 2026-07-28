import { useEffect, useState, useCallback } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { getInvoices, deleteInvoice, payInvoice, getAccounts, getBranches, getDivisions, getVendors } from '../api';

function getUser() {
  try { return JSON.parse(localStorage.getItem('user')); } catch { return null; }
}

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const STATUS_LABEL = { unpaid: 'Belum Dibayar', paid: 'Lunas', partial: 'Sebagian', dispatched: 'Pengiriman' };
const STATUS_CLASS  = { unpaid: 'status-unpaid', paid: 'status-paid', partial: 'status-partial', dispatched: 'status-dispatched' };
// Each status carries a shape as well as a hue, so the chips stay separable
// without colour vision (and in grayscale print).
const STATUS_GLYPH  = { unpaid: '○', paid: '✓', partial: '◐', dispatched: '→' };

function StatusBadge({ status }) {
  return (
    <span className={`badge ${STATUS_CLASS[status] ?? ''}`}>
      <span className="glyph" aria-hidden="true">{STATUS_GLYPH[status] ?? '·'}</span>
      {STATUS_LABEL[status] ?? status}
    </span>
  );
}

const PAGE_SIZE = 25;
const todayStr = new Date().toISOString().split('T')[0];
const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';

const VALID_STATUS = ['all', 'unpaid', 'partial', 'paid', 'dispatched'];

export default function Invoices() {
  const currentUser = getUser();
  const isAdmin = currentUser?.role === 'admin';

  // The dashboard links here with ?status=unpaid — honour it as the initial filter.
  const [searchParams] = useSearchParams();
  const initialStatus = VALID_STATUS.includes(searchParams.get('status')) ? searchParams.get('status') : 'all';

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

  const [branches,   setBranches]   = useState([]);
  const [divisions,  setDivisions]  = useState([]);
  const [vendors,    setVendors]    = useState([]);

  const [search,     setSearch]     = useState('');
  const [status,     setStatus]     = useState(initialStatus);
  const [type,       setType]       = useState('all');
  const [dateFrom,   setDateFrom]   = useState('');
  const [dateTo,     setDateTo]     = useState('');
  const [branchFilter,        setBranchFilter]        = useState('');
  const [divisionNameFilter,  setDivisionNameFilter]  = useState('');
  const [vendorFilter,        setVendorFilter]        = useState('');

  const load = useCallback(() => {
    setLoading(true);
    const params = { page, limit: PAGE_SIZE };
    if (search)                params.search      = search;
    if (status !== 'all')      params.status      = status;
    if (type   !== 'all')      params.type        = type;
    if (dateFrom)              params.date_from   = dateFrom;
    if (dateTo)                params.date_to     = dateTo;
    if (branchFilter)       params.branch_id      = branchFilter;
    if (divisionNameFilter) params.division_name  = divisionNameFilter;
    if (vendorFilter)       params.vendor_id      = vendorFilter;
    getInvoices(params)
      .then(r => {
        setInvoices(r.data.invoices);
        setTotal(r.data.total);
        setOutTotal(r.data.outstanding_total ?? 0);
        setOutCount(r.data.outstanding_count ?? 0);
      })
      .finally(() => setLoading(false));
  }, [search, status, type, dateFrom, dateTo, branchFilter, divisionNameFilter, vendorFilter, page]);

  useEffect(() => { load(); }, [load]);
  useEffect(() => {
    getAccounts().then(r => setAccounts(r.data));
    getBranches().then(r => setBranches(r.data)).catch(() => {});
    getDivisions().then(r => setDivisions(r.data)).catch(() => {});
    getVendors().then(r => setVendors(r.data)).catch(() => {});
  }, []);

  // Esc closes the payment dialog
  useEffect(() => {
    if (!payTarget) return;
    const onKey = (e) => { if (e.key === 'Escape') setPayTarget(null); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [payTarget]);

  // Reset to page 1 whenever filters change
  const setFilter = (setter) => (val) => { setter(val); setPage(1); };

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus invoice ini beserta semua itemnya?')) return;
    await deleteInvoice(id);
    load();
  };

  const openPay = (inv) => {
    const invTotal = Number(inv.total_amount);
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
    setDateFrom(''); setDateTo('');
    setBranchFilter(''); setDivisionNameFilter(''); setVendorFilter('');
    setPage(1);
  };

  const totalPages  = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const hasFilters  = search || status !== 'all' || type !== 'all' || dateFrom || dateTo || branchFilter || divisionNameFilter || vendorFilter;
  const pageStart   = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1;
  const pageEnd     = Math.min(page * PAGE_SIZE, total);

  return (
    <>
      <div className="page-header">
        <h1>Invoice</h1>
        <Link to="/invoices/new" className="btn btn-primary">+ Invoice Baru</Link>
      </div>

      {outstandingCount > 0 && (
        <div className="notice">
          <span className="notice-label">
            <span aria-hidden="true">◐</span>
            {outstandingCount} invoice belum lunas
          </span>
          <span className="notice-value">{idr(outstandingTotal)}</span>
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
            {vendors.length > 0 && (
              <select value={vendorFilter} onChange={e => setFilter(setVendorFilter)(e.target.value)} title="Filter vendor">
                <option value="">Semua Vendor</option>
                <option value="none">Tanpa Vendor</option>
                {vendors.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}
              </select>
            )}
            <input type="date" value={dateFrom} onChange={e => setFilter(setDateFrom)(e.target.value)} title="Dari tanggal" />
            <input type="date" value={dateTo}   onChange={e => setFilter(setDateTo)(e.target.value)}   title="Sampai tanggal" />
            {branches.length > 0 && (
              <select value={branchFilter} onChange={e => setFilter(setBranchFilter)(e.target.value)} title="Filter cabang">
                <option value="">Semua Cabang</option>
                {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
              </select>
            )}
            {divisions.length > 0 && (
              <select value={divisionNameFilter} onChange={e => setFilter(setDivisionNameFilter)(e.target.value)} title="Filter divisi">
                <option value="">Semua Divisi</option>
                {[...new Set(divisions.map(d => d.name))].sort().map(name => (
                  <option key={name} value={name}>{name}</option>
                ))}
              </select>
            )}
            {hasFilters && (
              <button type="button" onClick={clearFilters} className="btn btn-secondary btn-sm">Bersihkan</button>
            )}
          </div>
        </div>

        <div style={{overflowX: 'auto'}}>
        <table style={{minWidth: '900px'}}>
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
              <th className="num">Total</th>
              {/* fits "Belum Dibayar" on one line so the chip keeps its pill shape */}
              <th style={{width: '8.5rem'}}>Status</th>
              <th><span className="sr-only">Aksi</span></th>
            </tr>
          </thead>
          <tbody>
            {invoices.length === 0 ? (
              <tr><td colSpan={11}>
                <div className="empty-state">
                  {loading ? 'Memuat…' : hasFilters ? (
                    <>
                      <div className="empty-title">Tidak ada invoice yang cocok</div>
                      <div className="empty-hint">Filter yang aktif menyaring semua data. Longgarkan rentang tanggal atau kata kunci pencarian.</div>
                      <button type="button" onClick={clearFilters} className="btn btn-secondary btn-sm">Bersihkan filter</button>
                    </>
                  ) : (
                    <>
                      <div className="empty-title">Belum ada invoice</div>
                      <div className="empty-hint">Catat pembelian dari vendor atau pengeluaran cabang agar masuk ke buku besar.</div>
                      <Link to="/invoices/new" className="btn btn-primary btn-sm">+ Invoice Baru</Link>
                    </>
                  )}
                </div>
              </td></tr>
            ) : invoices.map(inv => {
              const isOverdue = inv.due_date && inv.payment_status !== 'paid' && inv.due_date < todayStr;
              return (
              <tr key={inv.id}>
                <td className="cell-key">{inv.invoice_number}</td>
                <td className="cell-meta">{inv.reference_number ?? <span className="cell-empty">—</span>}</td>
                <td>
                  <span className={`type-tag ${inv.invoice_type === 'expense' ? 'type-expense' : 'type-purchase'}`}>
                    {inv.invoice_type === 'expense' ? 'Pengeluaran' : 'Pembelian'}
                  </span>
                </td>
                <td className="cell-meta" style={{whiteSpace:'nowrap'}}>{fmtDate(inv.date)}</td>
                <td className={`cell-meta${isOverdue ? ' overdue-date' : ''}`} style={{whiteSpace:'nowrap'}}>
                  {inv.due_date ? fmtDate(inv.due_date) : <span className="cell-empty">—</span>}
                  {isOverdue && <span className="overdue-flag">LEWAT</span>}
                </td>
                <td className="cell-body">
                  {(inv.invoice_type === 'expense'
                    ? [inv.branch_name, inv.division_name].filter(Boolean).join(' / ')
                    : inv.warehouse_name) || <span className="cell-empty">—</span>}
                </td>
                <td className="cell-meta">{inv.vendor_name ?? <span className="cell-empty">—</span>}</td>
                <td className="cell-meta">{inv.account_name ?? <span className="cell-empty">—</span>}</td>
                <td className="cell-key num">
                  {idr(inv.total_amount)}
                  {inv.payment_status === 'partial' && Number(inv.amount_paid) > 0 && (
                    <span className="cell-sub">
                      Sisa {idr(Number(inv.total_amount) - Number(inv.amount_paid))}
                    </span>
                  )}
                </td>
                <td style={{whiteSpace:'nowrap'}}><StatusBadge status={inv.payment_status} /></td>
                <td style={{whiteSpace:'nowrap'}}>
                  <div className="actions">
                    <Link to={`/invoices/view/${inv.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                    {inv.payment_status !== 'paid' && inv.payment_status !== 'dispatched' && (
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
        </div>

        {totalPages > 1 && (
          <nav className="pagination" aria-label="Navigasi halaman">
            <span className="pagination-info">Halaman {page} dari {totalPages}</span>
            <div className="pagination-pages">
              <button className="page-btn" onClick={() => setPage(1)} disabled={page === 1} aria-label="Halaman pertama">«</button>
              <button className="page-btn" onClick={() => setPage(p => p - 1)} disabled={page === 1}>‹ Sebelumnya</button>

              {Array.from({ length: totalPages }, (_, i) => i + 1)
                .filter(p => p === 1 || p === totalPages || Math.abs(p - page) <= 2)
                .reduce((acc, p, idx, arr) => {
                  if (idx > 0 && p - arr[idx - 1] > 1) acc.push('…');
                  acc.push(p);
                  return acc;
                }, [])
                .map((p, idx) =>
                  p === '…'
                    ? <span key={`ellipsis-${idx}`} className="page-ellipsis">…</span>
                    : <button
                        key={p}
                        className="page-btn"
                        aria-current={p === page ? 'page' : undefined}
                        aria-label={`Halaman ${p}`}
                        onClick={() => setPage(p)}
                      >{p}</button>
                )
              }

              <button className="page-btn" onClick={() => setPage(p => p + 1)} disabled={page === totalPages}>Berikutnya ›</button>
              <button className="page-btn" onClick={() => setPage(totalPages)} disabled={page === totalPages} aria-label="Halaman terakhir">»</button>
            </div>
          </nav>
        )}
      </div>

      {payTarget && (() => {
        const cashAccounts = accounts.filter(a => a.account_type === 'asset' && !a.is_system);
        const invTotal = Number(payTarget.total_amount);
        const amountPaid = Number(payTarget.amount_paid ?? 0);
        const remaining = invTotal - amountPaid;
        return (
          <div className="modal-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) setPayTarget(null); }}>
            <div className="card modal-card" role="dialog" aria-modal="true" aria-labelledby="pay-title">
              <h2 id="pay-title">Bayar Invoice — {payTarget.invoice_number}</h2>
              <div className="modal-summary">
                <span>Total: <strong>{idr(invTotal)}</strong></span>
                {amountPaid > 0 && <span>Sudah dibayar: <strong>{idr(amountPaid)}</strong></span>}
                <span>Sisa: <strong>{idr(remaining)}</strong></span>
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
                    <small className="form-hint">
                      {idr(Number(payForm.amount))}
                      {Number(payForm.amount) < remaining && (
                        <span className="hint-partial"> · Pembayaran sebagian</span>
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
