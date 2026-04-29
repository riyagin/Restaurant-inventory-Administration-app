import { useEffect, useState } from 'react';
import { getUsers, createUser, updateUser, deleteUser } from '../api';

const empty = { username: '', password: '', role: 'staff' };

export default function Users() {
  const [users, setUsers] = useState([]);
  const [form, setForm] = useState(empty);
  const [editId, setEditId] = useState(null);
  const [error, setError] = useState('');
  const currentUser = JSON.parse(localStorage.getItem('user') || '{}');

  const load = () => getUsers().then(r => setUsers(r.data));

  useEffect(() => { load(); }, []);

  const set = (field) => (e) => setForm(f => ({ ...f, [field]: e.target.value }));

  const startEdit = (u) => {
    setEditId(u.id);
    setForm({ username: u.username, password: '', role: u.role });
    setError('');
  };

  const cancel = () => { setEditId(null); setForm(empty); setError(''); };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    try {
      const payload = { ...form };
      if (editId && !payload.password) delete payload.password;
      if (editId) await updateUser(editId, payload);
      else await createUser(payload);
      cancel();
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus pengguna ini?')) return;
    try {
      await deleteUser(id);
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus');
    }
  };

  const fmt = (d) => new Date(d).toLocaleDateString('id-ID');

  return (
    <>
      <div className="page-header">
        <h1>Pengguna</h1>
      </div>

      <div style={{display:'grid',gridTemplateColumns:'1fr 340px',gap:'1.5rem',alignItems:'start'}}>
        <div className="card">
          <table>
            <thead>
              <tr>
                <th>Username</th>
                <th>Peran</th>
                <th>Dibuat</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {users.length === 0 ? (
                <tr><td colSpan={4} style={{textAlign:'center',color:'#999',padding:'2rem'}}>Belum ada pengguna</td></tr>
              ) : users.map(u => (
                <tr key={u.id}>
                  <td style={{fontWeight:500}}>
                    {u.username}
                    {u.id === currentUser.id && <span style={{marginLeft:'0.4rem',fontSize:'0.75rem',color:'#4f8ef7'}}>(kamu)</span>}
                  </td>
                  <td><span className="badge">{u.role}</span></td>
                  <td style={{color:'#888',fontSize:'0.85rem'}}>{fmt(u.created_at)}</td>
                  <td>
                    <div className="actions">
                      <button onClick={() => startEdit(u)} className="btn btn-secondary btn-sm">Edit</button>
                      <button onClick={() => handleDelete(u.id)} className="btn btn-danger btn-sm" disabled={u.id === currentUser.id}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="card">
          <h2 style={{marginBottom:'1.25rem',fontSize:'1rem'}}>{editId ? 'Edit Pengguna' : 'Tambah Pengguna'}</h2>
          {error && <div className="error-msg">{error}</div>}
          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label>Username</label>
              <input value={form.username} onChange={set('username')} required placeholder="Masukkan username" />
            </div>
            <div className="form-group">
              <label>{editId ? 'Password Baru' : 'Password'}</label>
              <input
                type="password"
                value={form.password}
                onChange={set('password')}
                required={!editId}
                placeholder={editId ? 'Kosongkan untuk tetap sama' : 'Masukkan password'}
              />
            </div>
            <div className="form-group">
              <label>Peran</label>
              <select value={form.role} onChange={set('role')}>
                <option value="staff">Staff</option>
                <option value="admin">Admin</option>
              </select>
            </div>
            <div className="form-actions">
              <button type="submit" className="btn btn-primary">{editId ? 'Simpan Perubahan' : 'Tambah Pengguna'}</button>
              {editId && <button type="button" onClick={cancel} className="btn btn-secondary">Batal</button>}
            </div>
          </form>
        </div>
      </div>
    </>
  );
}
