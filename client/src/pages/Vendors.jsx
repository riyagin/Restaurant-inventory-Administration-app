import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getVendors, createVendor, updateVendor, deleteVendor } from '../api';

export default function Vendors() {
  const [vendors, setVendors] = useState([]);
  const [newName, setNewName] = useState('');
  const [editId, setEditId] = useState(null);
  const [editName, setEditName] = useState('');
  const [error, setError] = useState('');

  const load = () => getVendors().then(r => setVendors(r.data));
  useEffect(() => { load(); }, []);

  const handleAdd = async (e) => {
    e.preventDefault();
    setError('');
    try {
      await createVendor({ name: newName });
      setNewName('');
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    }
  };

  const handleEdit = async (id) => {
    setError('');
    try {
      await updateVendor(id, { name: editName });
      setEditId(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus vendor ini?')) return;
    setError('');
    try {
      await deleteVendor(id);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Tidak bisa dihapus: vendor sedang digunakan');
    }
  };

  const startEdit = (v) => { setEditId(v.id); setEditName(v.name); };

  return (
    <>
      <div className="page-header">
        <h1>Vendor</h1>
      </div>

      <div className="card" style={{maxWidth:'560px'}}>
        {error && <div className="error-msg" style={{marginBottom:'1rem'}}>{error}</div>}

        <form onSubmit={handleAdd} style={{display:'flex',gap:'0.75rem',marginBottom:'1.5rem'}}>
          <input
            style={{flex:1,padding:'0.55rem 0.75rem',border:'1px solid #ddd',borderRadius:'6px',fontSize:'0.95rem'}}
            placeholder="Nama vendor baru..."
            value={newName}
            onChange={e => setNewName(e.target.value)}
            required
          />
          <button type="submit" className="btn btn-primary">Tambah</button>
        </form>

        <table>
          <thead>
            <tr><th>Nama Vendor</th><th></th></tr>
          </thead>
          <tbody>
            {vendors.length === 0 ? (
              <tr><td colSpan={2} style={{textAlign:'center',color:'#999',padding:'2rem'}}>Belum ada vendor</td></tr>
            ) : vendors.map(v => (
              <tr key={v.id}>
                <td>
                  {editId === v.id ? (
                    <input
                      value={editName}
                      onChange={e => setEditName(e.target.value)}
                      style={{padding:'0.35rem 0.6rem',border:'1px solid #4f8ef7',borderRadius:'6px',fontSize:'0.9rem',width:'100%'}}
                      autoFocus
                    />
                  ) : v.name}
                </td>
                <td>
                  <div className="actions">
                    {editId === v.id ? (
                      <>
                        <button onClick={() => handleEdit(v.id)} className="btn btn-primary btn-sm">Simpan</button>
                        <button onClick={() => setEditId(null)} className="btn btn-secondary btn-sm">Batal</button>
                      </>
                    ) : (
                      <>
                        <Link to={`/vendors/${v.id}/history`} className="btn btn-secondary btn-sm">Riwayat</Link>
                        <button onClick={() => startEdit(v)} className="btn btn-secondary btn-sm">Edit</button>
                        <button onClick={() => handleDelete(v.id)} className="btn btn-danger btn-sm">Hapus</button>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
