import { useEffect, useState } from 'react';
import { getWageComponents, createWageComponent, updateWageComponent, deleteWageComponent } from '../../api';

const TYPE_LABELS = {
  allowance: 'Tunjangan',
  bonus: 'Bonus',
  deduction: 'Potongan',
};

const emptyForm = { name: '', type: 'allowance', is_fixed: true, is_active: true };

export default function WageComponents() {
  const [rows, setRows]   = useState([]);
  const [form, setForm]   = useState(emptyForm);
  const [editId, setEditId] = useState(null);
  const [editForm, setEditForm] = useState(emptyForm);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const load = () => getWageComponents().then(r => setRows(r.data || [])).catch(() => setRows([]));
  useEffect(() => { load(); }, []);

  const handleCreate = async (e) => {
    e.preventDefault();
    setError('');
    if (!form.name.trim()) { setError('Nama komponen wajib diisi'); return; }
    setSubmitting(true);
    try {
      await createWageComponent({
        name: form.name.trim(),
        type: form.type,
        is_fixed: form.is_fixed,
        is_active: form.is_active,
      });
      setForm(emptyForm);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menambah komponen');
    } finally {
      setSubmitting(false);
    }
  };

  const openEdit = (c) => {
    setEditId(c.id);
    setEditForm({ name: c.name, type: c.type, is_fixed: c.is_fixed, is_active: c.is_active });
    setError('');
  };

  const handleUpdate = async (e) => {
    e.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      await updateWageComponent(editId, {
        name: editForm.name.trim(),
        type: editForm.type,
        is_fixed: editForm.is_fixed,
        is_active: editForm.is_active,
      });
      setEditId(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal memperbarui komponen');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (c) => {
    if (!confirm(`Hapus komponen "${c.name}"? Jika masih dipakai struktur gaji, komponen hanya akan dinonaktifkan.`)) return;
    try {
      const res = await deleteWageComponent(c.id);
      if (res.data?.deactivated) {
        alert(res.data.message || 'Komponen masih dipakai, dinonaktifkan.');
      }
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus komponen');
    }
  };

  const toggleActive = async (c) => {
    try {
      await updateWageComponent(c.id, { name: c.name, type: c.type, is_fixed: c.is_fixed, is_active: !c.is_active });
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal mengubah status komponen');
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Komponen Gaji</h1>
      </div>

      <p style={{ color: '#667', fontSize: '0.9rem', marginBottom: '1rem' }}>
        Katalog komponen yang dapat dipakai pada struktur gaji karyawan: Tunjangan, Bonus, dan Potongan.
        Komponen <strong>tetap</strong> (fixed) dihitung pada proyeksi gaji bulanan; komponen <strong>variabel</strong> diisi per periode.
      </p>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      <div className="card" style={{ maxWidth: 760, marginBottom: '1rem' }}>
        <form onSubmit={handleCreate} style={{ display: 'flex', gap: '0.5rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
          <div className="form-group" style={{ margin: 0, flex: 2, minWidth: 180 }}>
            <label>Nama Komponen Baru</label>
            <input value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} placeholder="mis. Tunjangan Transport" />
          </div>
          <div className="form-group" style={{ margin: 0, flex: 1, minWidth: 130 }}>
            <label>Tipe</label>
            <select value={form.type} onChange={e => setForm(f => ({ ...f, type: e.target.value }))}>
              <option value="allowance">Tunjangan</option>
              <option value="bonus">Bonus</option>
              <option value="deduction">Potongan</option>
            </select>
          </div>
          <div className="form-group" style={{ margin: 0, flex: 1, minWidth: 130 }}>
            <label>Sifat</label>
            <select value={form.is_fixed ? 'fixed' : 'variable'} onChange={e => setForm(f => ({ ...f, is_fixed: e.target.value === 'fixed' }))}>
              <option value="fixed">Tetap</option>
              <option value="variable">Variabel</option>
            </select>
          </div>
          <button type="submit" className="btn btn-primary" disabled={submitting}>+ Tambah</button>
        </form>
      </div>

      <div className="card" style={{ maxWidth: 760 }}>
        <table>
          <thead>
            <tr><th>Nama</th><th>Tipe</th><th>Sifat</th><th>Status</th><th></th></tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr><td colSpan={5} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada komponen gaji</td></tr>
            ) : rows.map(c => (
              <tr key={c.id}>
                <td style={{ fontWeight: 500 }}>{c.name}</td>
                <td>{TYPE_LABELS[c.type] || c.type}</td>
                <td>{c.is_fixed ? 'Tetap' : 'Variabel'}</td>
                <td>
                  <button
                    onClick={() => toggleActive(c)}
                    className="badge"
                    style={{ border: 'none', cursor: 'pointer', background: c.is_active ? '#e6f4ea' : '#fce8e6', color: c.is_active ? '#1e7e34' : '#c5221f' }}
                    title="Klik untuk mengubah status"
                  >
                    {c.is_active ? 'Aktif' : 'Nonaktif'}
                  </button>
                </td>
                <td>
                  <div className="actions">
                    <button onClick={() => openEdit(c)} className="btn btn-secondary btn-sm">Edit</button>
                    <button onClick={() => handleDelete(c)} className="btn btn-danger btn-sm">Hapus</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {editId && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '100%', maxWidth: 420, padding: '2rem', margin: '1rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1.5rem' }}>
              <h2 style={{ margin: 0, fontSize: '1.05rem' }}>Edit Komponen Gaji</h2>
              <button onClick={() => setEditId(null)} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
            </div>
            <form onSubmit={handleUpdate}>
              <div className="form-group">
                <label>Nama</label>
                <input value={editForm.name} onChange={e => setEditForm(f => ({ ...f, name: e.target.value }))} required autoFocus />
              </div>
              <div className="form-group">
                <label>Tipe</label>
                <select value={editForm.type} onChange={e => setEditForm(f => ({ ...f, type: e.target.value }))}>
                  <option value="allowance">Tunjangan</option>
                  <option value="bonus">Bonus</option>
                  <option value="deduction">Potongan</option>
                </select>
              </div>
              <div className="form-group">
                <label>Sifat</label>
                <select value={editForm.is_fixed ? 'fixed' : 'variable'} onChange={e => setEditForm(f => ({ ...f, is_fixed: e.target.value === 'fixed' }))}>
                  <option value="fixed">Tetap</option>
                  <option value="variable">Variabel</option>
                </select>
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
