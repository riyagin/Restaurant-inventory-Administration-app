import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { getAccounts, getAccountAdjustments, createAccountAdjustment } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const TYPE_LABEL = { asset: 'Aset', liability: 'Kewajiban', equity: 'Ekuitas', revenue: 'Pendapatan', expense: 'Beban' };
const TYPE_COLOR = { asset: '#e8f5e9', liability: '#fce4ec', equity: '#e8eaf6', revenue: '#e6f9f0', expense: '#fff3e0' };
const TYPE_TEXT  = { asset: '#2e7d32', liability: '#880e4f', equity: '#283593', revenue: '#1b5e45', expense: '#e65100' };

const fmtDT = (d) => new Date(d).toLocaleString('id-ID', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });

export default function AccountAdjustments() {
  const [accounts, setAccounts]         = useState([]);
  const [adjustments, setAdjustments]   = useState([]);
  const [filterAccount, setFilterAccount] = useState('');
  const [loading, setLoading]           = useState(false);
  const [error, setError]               = useState('');
  const [success, setSuccess]           = useState('');

  const [form, setForm] = useState({
    account_id: '',
    sign: '+',       // '+' = credit/increase, '-' = debit/decrease
    amount: '',
    description: '',
  });
  const [submitting, setSubmitting] = useState(false);

  const loadAdjustments = useCallback(() => {
    setLoading(true);
    const params = filterAccount ? { account_id: filterAccount } : {};
    getAccountAdjustments(params)
      .then(r => setAdjustments(r.data))
      .finally(() => setLoading(false));
  }, [filterAccount]);

  useEffect(() => { getAccounts().then(r => setAccounts(r.data)); }, []);
  useEffect(() => { loadAdjustments(); }, [loadAdjustments]);

  // Group accounts by type for the select dropdown
  const groupedAccounts = accounts.reduce((g, a) => {
    if (!g[a.account_type]) g[a.account_type] = [];
    g[a.account_type].push(a);
    return g;
  }, {});

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError(''); setSuccess('');
    const rawAmount = Number(form.amount);
    if (!rawAmount || rawAmount <= 0) { setError('Masukkan jumlah yang valid'); return; }
    const amount = form.sign === '-' ? -rawAmount : rawAmount;
    setSubmitting(true);
    try {
      await createAccountAdjustment({ account_id: form.account_id, amount, description: form.description });
      setSuccess('Penyesuaian berhasil disimpan.');
      setForm({ account_id: '', sign: '+', amount: '', description: '' });
      loadAdjustments();
      // Refresh account list to reflect new balance
      getAccounts().then(r => setAccounts(r.data));
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setSubmitting(false);
    }
  };

  const selectedAccount = accounts.find(a => a.id === form.account_id);

  return (
    <>
      <div className="page-header">
        <h1>Jurnal Manual</h1>
        <Link to="/reports/financial" className="btn btn-secondary">← Laporan Keuangan</Link>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '380px 1fr', gap: '1.5rem', alignItems: 'start' }}>

        {/* ── Form ── */}
        <div className="card">
          <div className="card-header" style={{ marginBottom: '1.25rem' }}>
            <h2>Tambah Penyesuaian</h2>
          </div>

          {error   && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
          {success && <div style={{ background: '#e6f9f0', border: '1px solid #b2dfdb', borderRadius: '6px', padding: '0.75rem 1rem', marginBottom: '1rem', color: '#1b5e45', fontWeight: 500, fontSize: '0.88rem' }}>{success}</div>}

          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label>Akun</label>
              <select
                value={form.account_id}
                onChange={e => setForm(f => ({ ...f, account_id: e.target.value }))}
                required
              >
                <option value="">— Pilih akun —</option>
                {Object.entries(groupedAccounts).map(([type, accts]) => (
                  <optgroup key={type} label={TYPE_LABEL[type] || type}>
                    {accts.map(a => (
                      <option key={a.id} value={a.id}>
                        {a.account_number ? `${a.account_number} · ` : ''}{a.name}
                      </option>
                    ))}
                  </optgroup>
                ))}
              </select>
            </div>

            {selectedAccount && (
              <div style={{ marginBottom: '1rem', padding: '0.6rem 0.9rem', borderRadius: '6px', background: TYPE_COLOR[selectedAccount.account_type] || '#f5f5f5', fontSize: '0.85rem' }}>
                <span style={{ color: TYPE_TEXT[selectedAccount.account_type], fontWeight: 600 }}>
                  {TYPE_LABEL[selectedAccount.account_type]}
                </span>
                <span style={{ color: '#555', marginLeft: '0.75rem' }}>
                  Saldo saat ini: <strong>{idr(selectedAccount.balance)}</strong>
                </span>
              </div>
            )}

            <div className="form-group">
              <label>Jenis &amp; Jumlah (Rp)</label>
              <div style={{ display: 'flex', gap: '0.5rem' }}>
                <select
                  value={form.sign}
                  onChange={e => setForm(f => ({ ...f, sign: e.target.value }))}
                  style={{ width: 'auto', flexShrink: 0, fontWeight: 700, fontSize: '0.82rem', color: form.sign === '+' ? '#27ae60' : '#e74c3c' }}
                >
                  <option value="+">+ Tambah</option>
                  <option value="-">− Kurangi</option>
                </select>
                <input
                  type="number"
                  value={form.amount}
                  onChange={e => setForm(f => ({ ...f, amount: e.target.value }))}
                  placeholder="0"
                  min="1"
                  required
                  style={{ flex: 1 }}
                />
              </div>
              {form.amount && selectedAccount && (
                <small style={{ color: '#888', marginTop: '0.3rem', display: 'block' }}>
                  Saldo baru: <strong style={{ color: form.sign === '+' ? '#27ae60' : '#e74c3c' }}>
                    {idr(selectedAccount.balance + (form.sign === '-' ? -1 : 1) * Number(form.amount))}
                  </strong>
                </small>
              )}
            </div>

            <div className="form-group">
              <label>Keterangan / Alasan</label>
              <input
                type="text"
                value={form.description}
                onChange={e => setForm(f => ({ ...f, description: e.target.value }))}
                placeholder="Contoh: Biaya promosi bulan Januari..."
                required
              />
            </div>

            <button type="submit" className="btn btn-primary" disabled={submitting} style={{ width: '100%', justifyContent: 'center' }}>
              {submitting ? 'Menyimpan...' : 'Simpan Penyesuaian'}
            </button>
          </form>
        </div>

        {/* ── History ── */}
        <div className="card">
          <div className="card-header" style={{ marginBottom: '1rem' }}>
            <h2>Riwayat Penyesuaian {adjustments.length > 0 && `(${adjustments.length})`}</h2>
            <div className="filters">
              <select
                value={filterAccount}
                onChange={e => setFilterAccount(e.target.value)}
                style={{ minWidth: '220px' }}
              >
                <option value="">Semua Akun</option>
                {Object.entries(groupedAccounts).map(([type, accts]) => (
                  <optgroup key={type} label={TYPE_LABEL[type] || type}>
                    {accts.map(a => (
                      <option key={a.id} value={a.id}>
                        {a.account_number ? `${a.account_number} · ` : ''}{a.name}
                      </option>
                    ))}
                  </optgroup>
                ))}
              </select>
              {filterAccount && (
                <button className="btn btn-secondary btn-sm" onClick={() => setFilterAccount('')}>Bersihkan</button>
              )}
            </div>
          </div>

          <table>
            <thead>
              <tr>
                <th>Akun</th>
                <th>Keterangan</th>
                <th style={{ textAlign: 'right' }}>Jumlah</th>
                <th>Oleh</th>
                <th>Waktu</th>
              </tr>
            </thead>
            <tbody>
              {adjustments.length === 0 ? (
                <tr><td colSpan={5} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                  {loading ? 'Memuat...' : 'Belum ada penyesuaian'}
                </td></tr>
              ) : adjustments.map(adj => (
                <tr key={adj.id}>
                  <td>
                    <div style={{ fontWeight: 500, fontSize: '0.88rem' }}>{adj.account_name}</div>
                    <div style={{ display: 'flex', gap: '0.4rem', marginTop: '0.15rem', alignItems: 'center' }}>
                      {adj.account_number && (
                        <span style={{ fontFamily: 'monospace', fontSize: '0.75rem', color: '#4f8ef7' }}>{adj.account_number}</span>
                      )}
                      <span style={{
                        fontSize: '0.7rem', padding: '0.05rem 0.35rem', borderRadius: '3px',
                        background: TYPE_COLOR[adj.account_type] || '#eee',
                        color: TYPE_TEXT[adj.account_type] || '#555',
                        fontWeight: 600,
                      }}>
                        {TYPE_LABEL[adj.account_type] || adj.account_type}
                      </span>
                    </div>
                  </td>
                  <td style={{ color: '#555', fontSize: '0.88rem', maxWidth: '220px' }}>{adj.description}</td>
                  <td style={{ textAlign: 'right', fontWeight: 700, color: adj.amount > 0 ? '#27ae60' : '#e74c3c', whiteSpace: 'nowrap' }}>
                    {adj.amount > 0 ? '+' : ''}{idr(adj.amount)}
                  </td>
                  <td style={{ color: '#888', fontSize: '0.82rem' }}>{adj.created_by_name || '—'}</td>
                  <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmtDT(adj.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </>
  );
}
