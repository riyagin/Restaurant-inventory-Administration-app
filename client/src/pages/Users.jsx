import { useEffect, useState } from 'react';
import { getUsers, createUser, updateUser, deleteUser } from '../api';

const currentUser = () => { try { return JSON.parse(localStorage.getItem('user') || '{}'); } catch { return {}; } };

const emptyCreate = { username: '', password: '', confirm: '', role: 'staff' };
const emptyEdit   = { username: '', role: 'staff' };
const emptyPw     = { old_password: '', password: '', confirm: '' };

export default function Users() {
  const [users, setUsers]           = useState([]);
  const [showCreate, setShowCreate] = useState(false);
  const [editTarget, setEditTarget] = useState(null);   // user object being edited
  const [pwTarget, setPwTarget]     = useState(null);   // user object for password change
  const [createForm, setCreateForm] = useState(emptyCreate);
  const [editForm, setEditForm]     = useState(emptyEdit);
  const [pwForm, setPwForm]         = useState(emptyPw);
  const [error, setError]           = useState('');
  const [submitting, setSubmitting] = useState(false);

  const me = currentUser();
  const load = () => getUsers().then(r => setUsers(r.data));
  useEffect(() => { load(); }, []);

  const fmt = (d) => new Date(d).toLocaleDateString('id-ID');

  // ── Create ────────────────────────────────────────────────────────────────
  const openCreate = () => { setCreateForm(emptyCreate); setError(''); setShowCreate(true); };

  const handleCreate = async (e) => {
    e.preventDefault();
    setError('');
    if (createForm.password !== createForm.confirm) { setError('Konfirmasi password tidak cocok'); return; }
    setSubmitting(true);
    try {
      await createUser({ username: createForm.username, password: createForm.password, role: createForm.role });
      setShowCreate(false);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setSubmitting(false);
    }
  };

  // ── Edit (username + role only) ───────────────────────────────────────────
  const openEdit = (u) => { setEditTarget(u); setEditForm({ username: u.username, role: u.role }); setError(''); };

  const handleEdit = async (e) => {
    e.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      await updateUser(editTarget.id, { username: editForm.username, role: editForm.role });
      setEditTarget(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setSubmitting(false);
    }
  };

  // ── Change password ───────────────────────────────────────────────────────
  const openPw = (u) => { setPwTarget(u); setPwForm(emptyPw); setError(''); };

  const handlePw = async (e) => {
    e.preventDefault();
    setError('');
    if (pwForm.password !== pwForm.confirm) { setError('Konfirmasi password baru tidak cocok'); return; }
    if (pwForm.password.length < 6) { setError('Password baru minimal 6 karakter'); return; }
    setSubmitting(true);
    try {
      await updateUser(pwTarget.id, { old_password: pwForm.old_password, password: pwForm.password });
      setPwTarget(null);
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setSubmitting(false);
    }
  };

  // ── Delete ────────────────────────────────────────────────────────────────
  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus pengguna ini?')) return;
    try {
      await deleteUser(id);
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus');
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Pengguna</h1>
        <button onClick={openCreate} className="btn btn-primary">+ Tambah Pengguna</button>
      </div>

      <div className="card" style={{ maxWidth: '640px' }}>
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
              <tr><td colSpan={4} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada pengguna</td></tr>
            ) : users.map(u => (
              <tr key={u.id}>
                <td style={{ fontWeight: 500 }}>
                  {u.username}
                  {u.id === me.id && <span style={{ marginLeft: '0.4rem', fontSize: '0.75rem', color: '#4f8ef7' }}>(kamu)</span>}
                </td>
                <td><span className="badge">{u.role}</span></td>
                <td style={{ color: '#888', fontSize: '0.85rem' }}>{fmt(u.created_at)}</td>
                <td>
                  <div className="actions">
                    <button onClick={() => openEdit(u)} className="btn btn-secondary btn-sm">Edit</button>
                    <button onClick={() => openPw(u)} className="btn btn-secondary btn-sm">Ganti Password</button>
                    <button onClick={() => handleDelete(u.id)} className="btn btn-danger btn-sm" disabled={u.id === me.id}>Hapus</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* ── Create modal ── */}
      {showCreate && (
        <Modal title="Tambah Pengguna Baru" onClose={() => setShowCreate(false)}>
          {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
          <form onSubmit={handleCreate}>
            <div className="form-group">
              <label>Username</label>
              <input value={createForm.username} onChange={e => setCreateForm(f => ({ ...f, username: e.target.value }))} required placeholder="Masukkan username" autoFocus />
            </div>
            <div className="form-group">
              <label>Peran</label>
              <select value={createForm.role} onChange={e => setCreateForm(f => ({ ...f, role: e.target.value }))}>
                <option value="staff">Staff</option>
                <option value="admin">Admin</option>
              </select>
            </div>
            <div className="form-group">
              <label>Password</label>
              <input type="password" value={createForm.password} onChange={e => setCreateForm(f => ({ ...f, password: e.target.value }))} required placeholder="Masukkan password" />
            </div>
            <div className="form-group">
              <label>Konfirmasi Password</label>
              <input type="password" value={createForm.confirm} onChange={e => setCreateForm(f => ({ ...f, confirm: e.target.value }))} required placeholder="Ulangi password" />
            </div>
            <ModalActions>
              <button type="submit" className="btn btn-primary" disabled={submitting} style={{ flex: 1, justifyContent: 'center' }}>
                {submitting ? 'Menyimpan...' : 'Tambah Pengguna'}
              </button>
              <button type="button" onClick={() => setShowCreate(false)} className="btn btn-secondary">Batal</button>
            </ModalActions>
          </form>
        </Modal>
      )}

      {/* ── Edit modal ── */}
      {editTarget && (
        <Modal title={`Edit — ${editTarget.username}`} onClose={() => setEditTarget(null)}>
          {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
          <form onSubmit={handleEdit}>
            <div className="form-group">
              <label>Username</label>
              <input value={editForm.username} onChange={e => setEditForm(f => ({ ...f, username: e.target.value }))} required autoFocus />
            </div>
            <div className="form-group">
              <label>Peran</label>
              <select value={editForm.role} onChange={e => setEditForm(f => ({ ...f, role: e.target.value }))}>
                <option value="staff">Staff</option>
                <option value="admin">Admin</option>
              </select>
            </div>
            <ModalActions>
              <button type="submit" className="btn btn-primary" disabled={submitting} style={{ flex: 1, justifyContent: 'center' }}>
                {submitting ? 'Menyimpan...' : 'Simpan Perubahan'}
              </button>
              <button type="button" onClick={() => setEditTarget(null)} className="btn btn-secondary">Batal</button>
            </ModalActions>
          </form>
        </Modal>
      )}

      {/* ── Change password modal ── */}
      {pwTarget && (
        <Modal title={`Ganti Password — ${pwTarget.username}`} onClose={() => setPwTarget(null)}>
          {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
          <form onSubmit={handlePw}>
            <div className="form-group">
              <label>Password Lama</label>
              <input type="password" value={pwForm.old_password} onChange={e => setPwForm(f => ({ ...f, old_password: e.target.value }))} required placeholder="Masukkan password lama" autoFocus />
            </div>
            <div className="form-group">
              <label>Password Baru</label>
              <input type="password" value={pwForm.password} onChange={e => setPwForm(f => ({ ...f, password: e.target.value }))} required placeholder="Minimal 6 karakter" />
            </div>
            <div className="form-group">
              <label>Konfirmasi Password Baru</label>
              <input type="password" value={pwForm.confirm} onChange={e => setPwForm(f => ({ ...f, confirm: e.target.value }))} required placeholder="Ulangi password baru" />
            </div>
            <ModalActions>
              <button type="submit" className="btn btn-primary" disabled={submitting} style={{ flex: 1, justifyContent: 'center' }}>
                {submitting ? 'Menyimpan...' : 'Ganti Password'}
              </button>
              <button type="button" onClick={() => setPwTarget(null)} className="btn btn-secondary">Batal</button>
            </ModalActions>
          </form>
        </Modal>
      )}
    </>
  );
}

function Modal({ title, onClose, children }) {
  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
      <div className="card" style={{ width: '100%', maxWidth: '400px', padding: '2rem', margin: '1rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1.5rem' }}>
          <h2 style={{ margin: 0, fontSize: '1.05rem' }}>{title}</h2>
          <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa', lineHeight: 1 }}>✕</button>
        </div>
        {children}
      </div>
    </div>
  );
}

function ModalActions({ children }) {
  return (
    <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1.5rem' }}>
      {children}
    </div>
  );
}
