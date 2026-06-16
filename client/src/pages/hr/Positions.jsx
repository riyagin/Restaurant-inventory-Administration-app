import { useEffect, useState } from 'react';
import { getPositions, createPosition, updatePosition, deletePosition } from '../../api';

export default function Positions() {
  const [rows, setRows]     = useState([]);
  const [name, setName]     = useState('');
  const [editId, setEditId] = useState(null);
  const [editForm, setEditForm] = useState({ name: '', is_active: true });
  const [error, setError]   = useState('');
  const [submitting, setSubmitting] = useState(false);

  const load = () => getPositions().then(r => setRows(r.data)).catch(() => setRows([]));
  useEffect(() => { load(); }, []);

  const handleCreate = async (e) => {
    e.preventDefault();
    setError('');
    if (!name.trim()) { setError('Nama jabatan wajib diisi'); return; }
    setSubmitting(true);
    try {
      await createPosition({ name: name.trim() });
      setName('');
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menambah jabatan');
    } finally {
      setSubmitting(false);
    }
  };

  const openEdit = (p) => { setEditId(p.id); setEditForm({ name: p.name, is_active: p.is_active }); setError(''); };

  const handleUpdate = async (e) => {
    e.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      await updatePosition(editId, { name: editForm.name.trim(), is_active: editForm.is_active });
      setEditId(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal memperbarui jabatan');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus jabatan ini?')) return;
    try {
      await deletePosition(id);
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus jabatan');
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Jabatan</h1>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      <div className="card" style={{ maxWidth: 640, marginBottom: '1rem' }}>
        <form onSubmit={handleCreate} style={{ display: 'flex', gap: '0.5rem', alignItems: 'flex-end' }}>
          <div className="form-group" style={{ margin: 0, flex: 1 }}>
            <label>Nama Jabatan Baru</label>
            <input value={name} onChange={e => setName(e.target.value)} placeholder="mis. Kasir" />
          </div>
          <button type="submit" className="btn btn-primary" disabled={submitting}>+ Tambah</button>
        </form>
      </div>

      <div className="card" style={{ maxWidth: 640 }}>
        <table>
          <thead>
            <tr><th>Nama</th><th>Status</th><th></th></tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr><td colSpan={3} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada jabatan</td></tr>
            ) : rows.map(p => (
              <tr key={p.id}>
                <td style={{ fontWeight: 500 }}>{p.name}</td>
                <td>
                  <span className="badge" style={{ background: p.is_active ? '#e6f4ea' : '#fce8e6', color: p.is_active ? '#1e7e34' : '#c5221f' }}>
                    {p.is_active ? 'Aktif' : 'Nonaktif'}
                  </span>
                </td>
                <td>
                  <div className="actions">
                    <button onClick={() => openEdit(p)} className="btn btn-secondary btn-sm">Edit</button>
                    <button onClick={() => handleDelete(p.id)} className="btn btn-danger btn-sm">Hapus</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {editId && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '100%', maxWidth: 400, padding: '2rem', margin: '1rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1.5rem' }}>
              <h2 style={{ margin: 0, fontSize: '1.05rem' }}>Edit Jabatan</h2>
              <button onClick={() => setEditId(null)} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
            </div>
            <form onSubmit={handleUpdate}>
              <div className="form-group">
                <label>Nama</label>
                <input value={editForm.name} onChange={e => setEditForm(f => ({ ...f, name: e.target.value }))} required autoFocus />
              </div>
              <div className="form-group">
                <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <input type="checkbox" checked={editForm.is_active} onChange={e => setEditForm(f => ({ ...f, is_active: e.target.checked }))} />
                  Aktif
                </label>
              </div>
              <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1.5rem' }}>
                <button type="submit" className="btn btn-primary" disabled={submitting} style={{ flex: 1, justifyContent: 'center' }}>
                  {submitting ? 'Menyimpan...' : 'Simpan'}
                </button>
                <button type="button" onClick={() => setEditId(null)} className="btn btn-secondary">Batal</button>
              </div>
            </form>
          </div>
        </div>
      )}
    </>
  );
}
