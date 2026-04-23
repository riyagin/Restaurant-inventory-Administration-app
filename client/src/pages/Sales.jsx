import { useEffect, useState } from 'react';
import { getAccounts, getSales, createSale, deleteSale } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const today = new Date().toISOString().split('T')[0];
const emptyForm = { account_id: '', amount: '', description: '', date: today };

export default function Sales() {
  const [sales, setSales] = useState([]);
  const [accounts, setAccounts] = useState([]);
  const [form, setForm] = useState(emptyForm);
  const [filterAccount, setFilterAccount] = useState('all');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const loadSales = (account_id) =>
    getSales(account_id && account_id !== 'all' ? { account_id } : {}).then(r => setSales(r.data));

  useEffect(() => {
    getAccounts().then(r => setAccounts(r.data));
    loadSales();
  }, []);

  const set = (field) => (e) => setForm(f => ({ ...f, [field]: e.target.value }));

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
      });
      setForm(emptyForm);
      getAccounts().then(r => setAccounts(r.data));
      loadSales(filterAccount);
    } catch (err) {
      setError(err.response?.data?.error || 'Something went wrong');
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Delete this sale record? The account balance will be reversed.')) return;
    try {
      await deleteSale(id);
      getAccounts().then(r => setAccounts(r.data));
      loadSales(filterAccount);
    } catch (err) {
      alert(err.response?.data?.error || 'Failed to delete');
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
        <h1>Sales</h1>
      </div>

      {/* Account balance summary */}
      {accounts.length > 0 && (
        <div className="stats-grid" style={{ marginBottom: '1.5rem' }}>
          {accounts.map(a => (
            <div key={a.id} className={`stat-card${Number(a.balance) < 0 ? ' warning' : ''}`}>
              <div className="label">{a.name}</div>
              <div className="value" style={{ fontSize: '1.3rem', color: Number(a.balance) < 0 ? '#e67e22' : '#1a1a2e' }}>
                {idr(a.balance)}
              </div>
            </div>
          ))}
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 340px', gap: '1.5rem', alignItems: 'start' }}>
        {/* Sales history */}
        <div className="card">
          <div className="card-header">
            <h2>{sales.length} record{sales.length !== 1 ? 's' : ''}{filterAccount !== 'all' ? '' : ' total'} · {idr(totalShown)}</h2>
            <div className="filters">
              <select value={filterAccount} onChange={handleFilterChange}>
                <option value="all">All Accounts</option>
                {accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select>
            </div>
          </div>
          <table>
            <thead>
              <tr>
                <th>Date</th>
                <th>Account</th>
                <th style={{ textAlign: 'right' }}>Amount</th>
                <th>Description</th>
                <th>Recorded by</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {sales.length === 0 ? (
                <tr><td colSpan={6} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>No sales recorded yet</td></tr>
              ) : sales.map(s => (
                <tr key={s.id}>
                  <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(s.date)}</td>
                  <td style={{ fontWeight: 500 }}>{s.account_name}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600, color: '#27ae60' }}>{idr(s.amount)}</td>
                  <td style={{ color: '#555', fontSize: '0.88rem' }}>{s.description ?? <span style={{ color: '#bbb', fontStyle: 'italic' }}>—</span>}</td>
                  <td style={{ color: '#888', fontSize: '0.82rem' }}>{s.created_by_name ?? '—'}</td>
                  <td>
                    <button onClick={() => handleDelete(s.id)} className="btn btn-danger btn-sm">Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {/* Add sale form */}
        <div className="card">
          <h2 style={{ marginBottom: '1.25rem', fontSize: '1rem' }}>Record Sale</h2>
          {error && <div className="error-msg">{error}</div>}
          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label>Account</label>
              <select value={form.account_id} onChange={set('account_id')} required>
                <option value="">Select account...</option>
                {accounts.map(a => (
                  <option key={a.id} value={a.id}>{a.name} ({idr(a.balance)})</option>
                ))}
              </select>
            </div>
            <div className="form-group">
              <label>Amount (Rp)</label>
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
              <label>Description <span style={{ color: '#aaa', fontWeight: 400 }}>(optional)</span></label>
              <input
                value={form.description}
                onChange={set('description')}
                placeholder="e.g. Cash sale, Online order..."
              />
            </div>
            <div className="form-group">
              <label>Date</label>
              <input type="date" value={form.date} onChange={set('date')} required />
            </div>
            <button
              type="submit"
              className="btn btn-primary"
              style={{ width: '100%', justifyContent: 'center' }}
              disabled={loading}
            >
              {loading ? 'Saving...' : 'Record Sale'}
            </button>
          </form>
        </div>
      </div>
    </>
  );
}
