import { useEffect, useState } from 'react';
import { getAccounts, createAccount, updateAccount, deleteAccount } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const emptyForm = { name: '', balance: '' };

export default function Accounts() {
  const [accounts, setAccounts] = useState([]);
  const [form, setForm] = useState(emptyForm);
  const [editId, setEditId] = useState(null);
  const [editForm, setEditForm] = useState(emptyForm);
  const [error, setError] = useState('');

  const load = () => getAccounts().then(r => setAccounts(r.data));
  useEffect(() => { load(); }, []);

  const handleAdd = async (e) => {
    e.preventDefault();
    setError('');
    try {
      await createAccount({ name: form.name, balance: Number(form.balance) || 0 });
      setForm(emptyForm);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Something went wrong');
    }
  };

  const handleEdit = async (id) => {
    setError('');
    try {
      await updateAccount(id, { name: editForm.name, balance: Number(editForm.balance) || 0 });
      setEditId(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Something went wrong');
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Delete this account?')) return;
    setError('');
    try {
      await deleteAccount(id);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Cannot delete: account is in use');
    }
  };

  const startEdit = (a) => {
    setEditId(a.id);
    setEditForm({ name: a.name, balance: String(a.balance) });
  };

  return (
    <>
      <div className="page-header">
        <h1>Accounts</h1>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 340px', gap: '1.5rem', alignItems: 'start' }}>
        <div className="card">
          {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
          <table>
            <thead>
              <tr>
                <th>Account Name</th>
                <th style={{ textAlign: 'right' }}>Balance</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {accounts.length === 0 ? (
                <tr><td colSpan={3} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>No accounts yet</td></tr>
              ) : accounts.map(a => (
                <tr key={a.id}>
                  <td>
                    {editId === a.id ? (
                      <input
                        value={editForm.name}
                        onChange={e => setEditForm(f => ({ ...f, name: e.target.value }))}
                        style={{ padding: '0.35rem 0.6rem', border: '1px solid #4f8ef7', borderRadius: '6px', fontSize: '0.9rem', width: '100%' }}
                        autoFocus
                      />
                    ) : a.name}
                  </td>
                  <td style={{ textAlign: 'right', fontWeight: 600, color: Number(a.balance) < 0 ? '#e74c3c' : '#27ae60' }}>
                    {editId === a.id ? (
                      <input
                        type="number"
                        value={editForm.balance}
                        onChange={e => setEditForm(f => ({ ...f, balance: e.target.value }))}
                        style={{ padding: '0.35rem 0.6rem', border: '1px solid #4f8ef7', borderRadius: '6px', fontSize: '0.9rem', width: '140px', textAlign: 'right' }}
                      />
                    ) : idr(a.balance)}
                  </td>
                  <td>
                    <div className="actions">
                      {editId === a.id ? (
                        <>
                          <button onClick={() => handleEdit(a.id)} className="btn btn-primary btn-sm">Save</button>
                          <button onClick={() => setEditId(null)} className="btn btn-secondary btn-sm">Cancel</button>
                        </>
                      ) : (
                        <>
                          <button onClick={() => startEdit(a)} className="btn btn-secondary btn-sm">Edit</button>
                          <button onClick={() => handleDelete(a.id)} className="btn btn-danger btn-sm">Delete</button>
                        </>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="card">
          <h2 style={{ marginBottom: '1.25rem', fontSize: '1rem' }}>Add Account</h2>
          <form onSubmit={handleAdd}>
            <div className="form-group">
              <label>Account Name</label>
              <input
                value={form.name}
                onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
                placeholder="e.g. Kas Besar, BCA..."
                required
              />
            </div>
            <div className="form-group">
              <label>Initial Balance (Rp)</label>
              <input
                type="number"
                value={form.balance}
                onChange={e => setForm(f => ({ ...f, balance: e.target.value }))}
                placeholder="0"
              />
              {form.balance > 0 && (
                <small style={{ color: '#888', marginTop: '0.25rem', display: 'block' }}>
                  {idr(Number(form.balance))}
                </small>
              )}
            </div>
            <button type="submit" className="btn btn-primary" style={{ width: '100%', justifyContent: 'center' }}>
              Add Account
            </button>
          </form>
        </div>
      </div>
    </>
  );
}
