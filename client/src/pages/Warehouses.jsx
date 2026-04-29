import { useEffect, useState } from 'react';
import { getWarehouses, createWarehouse, updateWarehouse, deleteWarehouse } from '../api';

export default function Warehouses() {
  const [warehouses, setWarehouses] = useState([]);
  const [newName, setNewName]       = useState('');
  const [newAcctNum, setNewAcctNum] = useState('');
  const [editId, setEditId]         = useState(null);
  const [editName, setEditName]     = useState('');
  const [error, setError]           = useState('');

  const load = () => getWarehouses().then(r => setWarehouses(r.data));
  useEffect(() => { load(); }, []);

  const handleAdd = async (e) => {
    e.preventDefault();
    setError('');
    try {
      await createWarehouse({ name: newName, account_number: Number(newAcctNum) });
      setNewName('');
      setNewAcctNum('');
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    }
  };

  const handleEdit = async (id) => {
    setError('');
    try {
      await updateWarehouse(id, { name: editName });
      setEditId(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus gudang ini?')) return;
    setError('');
    try {
      await deleteWarehouse(id);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Tidak bisa dihapus: gudang sedang digunakan');
    }
  };

  const startEdit = (w) => { setEditId(w.id); setEditName(w.name); };

  return (
    <>
      <div className="page-header">
        <h1>Gudang</h1>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 320px', gap: '1.5rem', alignItems: 'start' }}>
        <div className="card">
          {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
          <table>
            <thead>
              <tr>
                <th>Nama Gudang</th>
                <th>Akun Persediaan</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {warehouses.length === 0 ? (
                <tr><td colSpan={3} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada gudang</td></tr>
              ) : warehouses.map(w => (
                <tr key={w.id}>
                  <td>
                    {editId === w.id ? (
                      <input
                        value={editName}
                        onChange={e => setEditName(e.target.value)}
                        style={{ padding: '0.35rem 0.6rem', border: '1px solid #4f8ef7', borderRadius: '6px', fontSize: '0.9rem', width: '100%' }}
                        autoFocus
                      />
                    ) : <span style={{ fontWeight: 500 }}>{w.name}</span>}
                  </td>
                  <td style={{ fontSize: '0.85rem', color: '#555' }}>
                    {w.inventory_account_number
                      ? <span><span style={{ fontFamily: 'monospace', color: '#4f8ef7' }}>{w.inventory_account_number}</span> · {w.inventory_account_name}</span>
                      : <span style={{ color: '#ccc' }}>—</span>}
                  </td>
                  <td>
                    <div className="actions">
                      {editId === w.id ? (
                        <>
                          <button onClick={() => handleEdit(w.id)} className="btn btn-primary btn-sm">Simpan</button>
                          <button onClick={() => setEditId(null)} className="btn btn-secondary btn-sm">Batal</button>
                        </>
                      ) : (
                        <>
                          <button onClick={() => startEdit(w)} className="btn btn-secondary btn-sm">Edit</button>
                          <button onClick={() => handleDelete(w.id)} className="btn btn-danger btn-sm">Hapus</button>
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
          <h2 style={{ marginBottom: '1.25rem', fontSize: '1rem' }}>Tambah Gudang</h2>
          <form onSubmit={handleAdd}>
            <div className="form-group">
              <label>Nama Gudang</label>
              <input
                value={newName}
                onChange={e => setNewName(e.target.value)}
                placeholder="mis. Gudang Utama, Gudang B..."
                required
              />
            </div>
            <div className="form-group">
              <label>No. Akun Persediaan</label>
              <input
                value={newAcctNum}
                onChange={e => setNewAcctNum(e.target.value)}
                placeholder="mis. 11000, 11100..."
                maxLength={5}
                required
              />
              <small style={{ color: '#888', marginTop: '0.25rem', display: 'block' }}>
                Harus dalam rentang 10000–19999 (Aset)
              </small>
            </div>
            <button type="submit" className="btn btn-primary" style={{ width: '100%', justifyContent: 'center' }}>
              Tambah Gudang
            </button>
          </form>
        </div>
      </div>
    </>
  );
}
