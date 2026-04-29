import { useEffect, useState } from 'react';
import {
  getBranches, createBranch, updateBranch, deleteBranch,
  getDivisions, createDivision, updateDivision, deleteDivision,
  getDivisionCategories, createDivisionCategory, deleteDivisionCategory,
} from '../api';

function AccountBadge({ number, name }) {
  if (!number) return <span style={{ color: '#ccc', fontSize: '0.8rem' }}>—</span>;
  return (
    <span style={{ fontSize: '0.8rem', color: '#555' }}>
      <span style={{ fontFamily: 'monospace', color: '#4f8ef7', fontWeight: 600 }}>{number}</span>
      {name && <span style={{ color: '#888' }}> · {name}</span>}
    </span>
  );
}

function DivisionCategories({ division }) {
  const [cats, setCats] = useState([]);
  const [open, setOpen] = useState(false);
  const [newName, setNewName] = useState('');
  const [err, setErr] = useState('');

  const load = () => getDivisionCategories({ division_id: division.id }).then(r => setCats(r.data));

  useEffect(() => { if (open) load(); }, [open]);

  const handleAdd = async (e) => {
    e.preventDefault();
    setErr('');
    try {
      await createDivisionCategory({ division_id: division.id, name: newName });
      setNewName('');
      load();
    } catch (err) { setErr(err.response?.data?.error || 'Terjadi kesalahan'); }
  };

  const handleDelete = async (id) => {
    await deleteDivisionCategory(id);
    load();
  };

  return (
    <div style={{ marginTop: '0.5rem' }}>
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        style={{ background: 'none', border: 'none', color: '#4f8ef7', fontSize: '0.78rem', cursor: 'pointer', padding: '0.1rem 0', display: 'flex', alignItems: 'center', gap: '0.3rem' }}
      >
        <span style={{ fontSize: '0.7rem' }}>{open ? '▼' : '▶'}</span> Kategori POS ({cats.length || '…'})
      </button>
      {open && (
        <div style={{ marginTop: '0.4rem', paddingLeft: '0.75rem', borderLeft: '2px solid #e8eeff' }}>
          {err && <div style={{ color: '#e74c3c', fontSize: '0.78rem', marginBottom: '0.3rem' }}>{err}</div>}
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.3rem', marginBottom: '0.4rem' }}>
            {cats.map(c => (
              <span key={c.id} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem', background: '#f0f4ff', color: '#2c5cc5', borderRadius: '4px', padding: '0.15rem 0.5rem', fontSize: '0.78rem', fontWeight: 500 }}>
                {c.name}
                <button
                  onClick={() => handleDelete(c.id)}
                  style={{ background: 'none', border: 'none', color: '#999', cursor: 'pointer', fontSize: '0.7rem', padding: 0, lineHeight: 1 }}
                  title="Hapus"
                >✕</button>
              </span>
            ))}
            {cats.length === 0 && <span style={{ color: '#bbb', fontSize: '0.78rem' }}>Belum ada kategori</span>}
          </div>
          <form onSubmit={handleAdd} style={{ display: 'flex', gap: '0.3rem' }}>
            <input
              value={newName}
              onChange={e => setNewName(e.target.value)}
              placeholder="Nama kategori..."
              required
              style={{ flex: 1, padding: '0.25rem 0.5rem', border: '1px solid #ddd', borderRadius: '4px', fontSize: '0.8rem' }}
            />
            <button type="submit" className="btn btn-primary btn-sm" style={{ fontSize: '0.78rem' }}>+ Tambah</button>
          </form>
        </div>
      )}
    </div>
  );
}

export default function Branches() {
  const [branches, setBranches]           = useState([]);
  const [divisions, setDivisions]         = useState([]);
  const [selectedBranch, setSelectedBranch] = useState(null);
  const [newBranchName, setNewBranchName] = useState('');
  const [newDivisionName, setNewDivisionName] = useState('');
  const [editBranch, setEditBranch]       = useState(null);
  const [editDivision, setEditDivision]   = useState(null);
  const [error, setError]                 = useState('');

  const loadBranches = () => getBranches().then(r => setBranches(r.data));

  const loadDivisions = (branchId) => {
    if (!branchId) { setDivisions([]); return; }
    getDivisions({ branch_id: branchId }).then(r => setDivisions(r.data));
  };

  useEffect(() => { loadBranches(); }, []);

  const selectBranch = (b) => {
    setSelectedBranch(b);
    setNewDivisionName('');
    setEditDivision(null);
    loadDivisions(b.id);
  };

  // ── Branches CRUD ──
  const handleAddBranch = async (e) => {
    e.preventDefault();
    setError('');
    try {
      await createBranch({ name: newBranchName });
      setNewBranchName('');
      loadBranches();
    } catch (err) { setError(err.response?.data?.error || 'Terjadi kesalahan'); }
  };

  const handleSaveBranch = async (id) => {
    setError('');
    try {
      await updateBranch(id, { name: editBranch.name });
      setEditBranch(null);
      loadBranches();
    } catch (err) { setError(err.response?.data?.error || 'Terjadi kesalahan'); }
  };

  const handleDeleteBranch = async (id) => {
    if (!confirm('Yakin hapus cabang ini beserta semua divisinya?')) return;
    setError('');
    try {
      await deleteBranch(id);
      if (selectedBranch?.id === id) { setSelectedBranch(null); setDivisions([]); }
      loadBranches();
    } catch (err) { setError(err.response?.data?.error || 'Tidak bisa dihapus'); }
  };

  // ── Divisions CRUD ──
  const handleAddDivision = async (e) => {
    e.preventDefault();
    setError('');
    try {
      await createDivision({ branch_id: selectedBranch.id, name: newDivisionName });
      setNewDivisionName('');
      loadDivisions(selectedBranch.id);
    } catch (err) { setError(err.response?.data?.error || 'Terjadi kesalahan'); }
  };

  const handleSaveDivision = async (id) => {
    setError('');
    try {
      await updateDivision(id, { name: editDivision.name });
      setEditDivision(null);
      loadDivisions(selectedBranch.id);
    } catch (err) { setError(err.response?.data?.error || 'Terjadi kesalahan'); }
  };

  const handleDeleteDivision = async (id) => {
    if (!confirm('Yakin hapus divisi ini?')) return;
    setError('');
    try {
      await deleteDivision(id);
      loadDivisions(selectedBranch.id);
    } catch (err) { setError(err.response?.data?.error || 'Tidak bisa dihapus'); }
  };

  return (
    <>
      <div className="page-header">
        <h1>Cabang &amp; Divisi</h1>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem' }}>

        {/* Branches panel */}
        <div className="card">
          <div className="card-header">
            <h2>Cabang</h2>
          </div>

          <form onSubmit={handleAddBranch} style={{ display: 'flex', gap: '0.5rem', marginBottom: '1.25rem' }}>
            <input
              style={{ flex: 1, padding: '0.5rem 0.75rem', border: '1px solid #ddd', borderRadius: '6px', fontSize: '0.9rem' }}
              placeholder="Nama cabang baru..."
              value={newBranchName}
              onChange={e => setNewBranchName(e.target.value)}
              required
            />
            <button type="submit" className="btn btn-primary">Tambah</button>
          </form>

          <table>
            <thead>
              <tr>
                <th>Cabang</th>
                <th>Akun Pendapatan</th>
                <th>Akun Beban</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {branches.length === 0 ? (
                <tr><td colSpan={4} style={{ textAlign: 'center', color: '#999', padding: '1.5rem' }}>Belum ada cabang</td></tr>
              ) : branches.map(b => (
                <tr
                  key={b.id}
                  style={{ cursor: 'pointer', background: selectedBranch?.id === b.id ? '#f0f4ff' : undefined }}
                  onClick={() => selectBranch(b)}
                >
                  <td>
                    {editBranch?.id === b.id ? (
                      <input
                        value={editBranch.name}
                        onChange={e => setEditBranch(eb => ({ ...eb, name: e.target.value }))}
                        onClick={e => e.stopPropagation()}
                        style={{ padding: '0.3rem 0.5rem', border: '1px solid #4f8ef7', borderRadius: '5px', fontSize: '0.9rem' }}
                        autoFocus
                      />
                    ) : (
                      <span style={{ fontWeight: selectedBranch?.id === b.id ? 600 : 500 }}>{b.name}</span>
                    )}
                  </td>
                  <td onClick={e => e.stopPropagation()}>
                    <AccountBadge number={b.revenue_account_number} name={null} />
                  </td>
                  <td onClick={e => e.stopPropagation()}>
                    <AccountBadge number={b.expense_account_number} name={null} />
                  </td>
                  <td onClick={e => e.stopPropagation()}>
                    <div className="actions">
                      {editBranch?.id === b.id ? (
                        <>
                          <button onClick={() => handleSaveBranch(b.id)} className="btn btn-primary btn-sm">Simpan</button>
                          <button onClick={() => setEditBranch(null)} className="btn btn-secondary btn-sm">Batal</button>
                        </>
                      ) : (
                        <>
                          <button onClick={() => setEditBranch(b)} className="btn btn-secondary btn-sm">Edit</button>
                          <button onClick={() => handleDeleteBranch(b.id)} className="btn btn-danger btn-sm">Hapus</button>
                        </>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {/* Divisions panel */}
        <div className="card">
          <div className="card-header">
            <h2>
              {selectedBranch
                ? <>Divisi — <span style={{ color: '#4f8ef7' }}>{selectedBranch.name}</span></>
                : 'Divisi'}
            </h2>
          </div>

          {!selectedBranch ? (
            <p style={{ color: '#999', fontSize: '0.9rem', padding: '1rem 0' }}>Pilih cabang untuk mengelola divisinya.</p>
          ) : (
            <>
              <form onSubmit={handleAddDivision} style={{ display: 'flex', gap: '0.5rem', marginBottom: '1.25rem' }}>
                <input
                  style={{ flex: 1, padding: '0.5rem 0.75rem', border: '1px solid #ddd', borderRadius: '6px', fontSize: '0.9rem' }}
                  placeholder={`Nama divisi baru di ${selectedBranch.name}...`}
                  value={newDivisionName}
                  onChange={e => setNewDivisionName(e.target.value)}
                  required
                />
                <button type="submit" className="btn btn-primary">Tambah</button>
              </form>

              <table>
                <thead>
                  <tr>
                    <th>Divisi</th>
                    <th>Pendapatan</th>
                    <th>Beban</th>
                    <th>Diskon</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {divisions.length === 0 ? (
                    <tr><td colSpan={5} style={{ textAlign: 'center', color: '#999', padding: '1.5rem' }}>Belum ada divisi</td></tr>
                  ) : divisions.map(d => (
                    <tr key={d.id}>
                      <td>
                        {editDivision?.id === d.id ? (
                          <input
                            value={editDivision.name}
                            onChange={e => setEditDivision(ed => ({ ...ed, name: e.target.value }))}
                            style={{ padding: '0.3rem 0.5rem', border: '1px solid #4f8ef7', borderRadius: '5px', fontSize: '0.9rem' }}
                            autoFocus
                          />
                        ) : (
                          <div>
                            <div style={{ fontWeight: 500 }}>{d.name}</div>
                            <DivisionCategories division={d} />
                          </div>
                        )}
                      </td>
                      <td>
                        <AccountBadge number={d.revenue_account_number} name={null} />
                      </td>
                      <td>
                        <AccountBadge number={d.expense_account_number} name={null} />
                      </td>
                      <td>
                        <AccountBadge number={d.discount_account_number} name={null} />
                      </td>
                      <td>
                        <div className="actions">
                          {editDivision?.id === d.id ? (
                            <>
                              <button onClick={() => handleSaveDivision(d.id)} className="btn btn-primary btn-sm">Simpan</button>
                              <button onClick={() => setEditDivision(null)} className="btn btn-secondary btn-sm">Batal</button>
                            </>
                          ) : (
                            <>
                              <button onClick={() => setEditDivision(d)} className="btn btn-secondary btn-sm">Edit</button>
                              <button onClick={() => handleDeleteDivision(d.id)} className="btn btn-danger btn-sm">Hapus</button>
                            </>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
        </div>
      </div>
    </>
  );
}
