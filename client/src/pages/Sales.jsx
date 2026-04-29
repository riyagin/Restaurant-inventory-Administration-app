import { useEffect, useRef, useState } from 'react';
import { getAccounts, getBranches, getDivisions, getSales, createSale, deleteSale } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const today = new Date().toISOString().split('T')[0];
const emptyForm = { account_id: '', amount: '', description: '', date: today, branch_id: '', division_id: '' };

export default function Sales() {
  const [sales, setSales] = useState([]);
  const [accounts, setAccounts] = useState([]);
  const [branches, setBranches] = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [form, setForm] = useState(emptyForm);
  const [filterAccount, setFilterAccount] = useState('all');
  const [carouselIdx, setCarouselIdx] = useState(0);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const loadSales = (account_id) =>
    getSales(account_id && account_id !== 'all' ? { account_id } : {}).then(r => setSales(r.data));

  useEffect(() => {
    getAccounts().then(r => setAccounts(r.data));
    getBranches().then(r => setBranches(r.data));
    loadSales();
  }, []);

  const set = (field) => (e) => setForm(f => ({ ...f, [field]: e.target.value }));

  const handleBranchChange = (e) => {
    const branch_id = e.target.value;
    setForm(f => ({ ...f, branch_id, division_id: '' }));
    if (branch_id) {
      getDivisions({ branch_id }).then(r => setDivisions(r.data));
    } else {
      setDivisions([]);
    }
  };

  const cashAccounts = accounts.filter(a => a.account_type === 'asset' && !a.is_system);
  const CAROUSEL_PAGE = 4;
  const safeIdx = cashAccounts.length > 0 ? carouselIdx % cashAccounts.length : 0;
  const visibleAccounts = cashAccounts.length <= CAROUSEL_PAGE
    ? cashAccounts
    : Array.from({ length: CAROUSEL_PAGE }, (_, i) => cashAccounts[(safeIdx + i) % cashAccounts.length]);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await createSale({
        account_id: form.account_id,
        amount: Number(form.amount),
        description: form.description || undefined,
        date: form.date,
        branch_id: form.branch_id || undefined,
        division_id: form.division_id || undefined,
      });
      setForm(emptyForm);
      setDivisions([]);
      getAccounts().then(r => setAccounts(r.data));
      loadSales(filterAccount);
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus catatan penjualan ini? Saldo akun akan dikembalikan.')) return;
    try {
      await deleteSale(id);
      getAccounts().then(r => setAccounts(r.data));
      loadSales(filterAccount);
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus');
    }
  };

  const handleFilterChange = (e) => {
    setFilterAccount(e.target.value);
    loadSales(e.target.value);
  };

  const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';

  const totalShown = sales.reduce((s, r) => s + Number(r.amount), 0);

  return (
    <>
      <div className="page-header">
        <h1>Penjualan</h1>
      </div>

      {/* Account balance carousel */}
      {cashAccounts.length > 0 && (
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginBottom: '1.5rem' }}>
          <button
            onClick={() => setCarouselIdx(i => (i - 1 + cashAccounts.length) % cashAccounts.length)}
            disabled={cashAccounts.length <= CAROUSEL_PAGE}
            style={{ background: 'none', border: '1px solid #ddd', borderRadius: '50%', width: '32px', height: '32px', cursor: cashAccounts.length > CAROUSEL_PAGE ? 'pointer' : 'default', fontSize: '1rem', color: '#555', flexShrink: 0 }}
          >‹</button>

          <div style={{ flex: 1, display: 'grid', gridTemplateColumns: `repeat(${Math.min(cashAccounts.length, CAROUSEL_PAGE)}, 1fr)`, gap: '0.75rem', overflow: 'hidden' }}>
            {visibleAccounts.map((a, i) => (
              <div key={a.id} className={`stat-card${Number(a.balance) < 0 ? ' warning' : ''}`} style={{ margin: 0 }}>
                <div className="label" style={{ fontSize: '0.8rem', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{a.name}</div>
                <div className="value" style={{ fontSize: '1.1rem', color: Number(a.balance) < 0 ? '#e67e22' : '#1a1a2e' }}>
                  {idr(a.balance)}
                </div>
              </div>
            ))}
          </div>

          <button
            onClick={() => setCarouselIdx(i => (i + 1) % cashAccounts.length)}
            disabled={cashAccounts.length <= CAROUSEL_PAGE}
            style={{ background: 'none', border: '1px solid #ddd', borderRadius: '50%', width: '32px', height: '32px', cursor: cashAccounts.length > CAROUSEL_PAGE ? 'pointer' : 'default', fontSize: '1rem', color: '#555', flexShrink: 0 }}
          >›</button>
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 340px', gap: '1.5rem', alignItems: 'start' }}>
        {/* Sales history */}
        <div className="card">
          <div className="card-header">
            <h2>{sales.length} catatan{filterAccount !== 'all' ? '' : ' total'} · {idr(totalShown)}</h2>
            <div className="filters">
              <select value={filterAccount} onChange={handleFilterChange}>
                <option value="all">Semua Akun</option>
                {cashAccounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select>
            </div>
          </div>
          <table>
            <thead>
              <tr>
                <th>Tanggal</th>
                <th>Akun</th>
                <th style={{ textAlign: 'right' }}>Jumlah</th>
                <th>Cabang / Divisi</th>
                <th>Deskripsi</th>
                <th>Dicatat oleh</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {sales.length === 0 ? (
                <tr><td colSpan={7} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada penjualan</td></tr>
              ) : sales.map(s => (
                <tr key={s.id}>
                  <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(s.date)}</td>
                  <td style={{ fontWeight: 500 }}>{s.account_name}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600, color: '#27ae60' }}>{idr(s.amount)}</td>
                  <td style={{ fontSize: '0.85rem', color: '#555' }}>
                    {s.branch_name
                      ? <>{s.branch_name}{s.division_name && <span style={{ color: '#888' }}> / {s.division_name}</span>}</>
                      : <span style={{ color: '#ccc', fontStyle: 'italic' }}>—</span>}
                  </td>
                  <td style={{ color: '#555', fontSize: '0.88rem' }}>{s.description ?? <span style={{ color: '#bbb', fontStyle: 'italic' }}>—</span>}</td>
                  <td style={{ color: '#888', fontSize: '0.82rem' }}>{s.created_by_name ?? '—'}</td>
                  <td>
                    <button onClick={() => handleDelete(s.id)} className="btn btn-danger btn-sm">Hapus</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {/* Add sale form */}
        <div className="card">
          <h2 style={{ marginBottom: '1.25rem', fontSize: '1rem' }}>Catat Penjualan</h2>
          {error && <div className="error-msg">{error}</div>}
          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label>Akun Kas / Bank</label>
              <select value={form.account_id} onChange={set('account_id')} required>
                <option value="">Pilih akun...</option>
                {cashAccounts.map(a => (
                  <option key={a.id} value={a.id}>{a.name} ({idr(a.balance)})</option>
                ))}
              </select>
            </div>
            <div className="form-group">
              <label>Jumlah (Rp)</label>
              <input
                type="number"
                min="1"
                value={form.amount}
                onChange={set('amount')}
                required
                placeholder="0"
              />
              {form.amount > 0 && (
                <small style={{ color: '#888', marginTop: '0.25rem', display: 'block' }}>
                  {idr(Number(form.amount))}
                </small>
              )}
            </div>
            <div className="form-group">
              <label>Cabang <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
              <select value={form.branch_id} onChange={handleBranchChange}>
                <option value="">— Pilih cabang —</option>
                {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
              </select>
            </div>
            {form.branch_id && (
              <div className="form-group">
                <label>Divisi <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
                <select value={form.division_id} onChange={set('division_id')}>
                  <option value="">— Pilih divisi —</option>
                  {divisions.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
                </select>
              </div>
            )}
            <div className="form-group">
              <label>Deskripsi <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
              <input
                value={form.description}
                onChange={set('description')}
                placeholder="mis. Penjualan tunai, Pesanan online..."
              />
            </div>
            <div className="form-group">
              <label>Tanggal</label>
              <input type="date" value={form.date} onChange={set('date')} required />
            </div>
            <button
              type="submit"
              className="btn btn-primary"
              style={{ width: '100%', justifyContent: 'center' }}
              disabled={loading}
            >
              {loading ? 'Menyimpan...' : 'Catat Penjualan'}
            </button>
          </form>
        </div>
      </div>
    </>
  );
}
