import { useEffect, useState } from 'react';
import {
  getBranches, createBranch, updateBranch, deleteBranch,
  getDivisions, createDivision, updateDivision, deleteDivision,
} from '../api';

export default function Branches() {
  const [branches, setBranches] = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [selectedBranch, setSelectedBranch] = useState(null);
  const [newBranchName, setNewBranchName] = useState('');
  const [newDivisionName, setNewDivisionName] = useState('');
  const [editBranch, setEditBranch] = useState(null);
  const [editDivision, setEditDivision] = useState(null);
  const [error, setError] = useState('');

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
    } catch (err) { setError(err.response?.data?.error || 'Error'); }
  };

  const handleSaveBranch = async (id) => {
    setError('');
    try {
      await updateBranch(id, { name: editBranch.name });
      setEditBranch(null);
      loadBranches();
    } catch (err) { setError(err.response?.data?.error || 'Error'); }
  };

  const handleDeleteBranch = async (id) => {
    if (!confirm('Delete this branch and all its divisions?')) return;
    setError('');
    try {
      await deleteBranch(id);
      if (selectedBranch?.id === id) { setSelectedBranch(null); setDivisions([]); }
      loadBranches();
    } catch (err) { setError(err.response?.data?.error || 'Cannot delete'); }
  };

  // ── Divisions CRUD ──
  const handleAddDivision = async (e) => {
    e.preventDefault();
    setError('');
    try {
      await createDivision({ branch_id: selectedBranch.id, name: newDivisionName });
      setNewDivisionName('');
      loadDivisions(selectedBranch.id);
    } catch (err) { setError(err.response?.data?.error || 'Error'); }
  };

  const handleSaveDivision = async (id) => {
    setError('');
    try {
      await updateDivision(id, { name: editDivision.name });
      setEditDivision(null);
      loadDivisions(selectedBranch.id);
    } catch (err) { setError(err.response?.data?.error || 'Error'); }
  };

  const handleDeleteDivision = async (id) => {
    if (!confirm('Delete this division?')) return;
    setError('');
    try {
      await deleteDivision(id);
      loadDivisions(selectedBranch.id);
    } catch (err) { setError(err.response?.data?.error || 'Cannot delete'); }
  };

  return (
    <>
      <div className="page-header">
        <h1>Branches &amp; Divisions</h1>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem' }}>

        {/* Branches panel */}
        <div className="card">
          <div className="card-header">
            <h2>Branches</h2>
          </div>

          <form onSubmit={handleAddBranch} style={{ display: 'flex', gap: '0.5rem', marginBottom: '1.25rem' }}>
            <input
              style={{ flex: 1, padding: '0.5rem 0.75rem', border: '1px solid #ddd', borderRadius: '6px', fontSize: '0.9rem' }}
              placeholder="New branch name..."
              value={newBranchName}
              onChange={e => setNewBranchName(e.target.value)}
              required
            />
            <button type="submit" className="btn btn-primary">Add</button>
          </form>

          <table>
            <thead>
              <tr>
                <th>Branch</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {branches.length === 0 ? (
                <tr><td colSpan={2} style={{ textAlign: 'center', color: '#999', padding: '1.5rem' }}>No branches yet</td></tr>
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
                      <span style={{ fontWeight: selectedBranch?.id === b.id ? 600 : 400 }}>{b.name}</span>
                    )}
                  </td>
                  <td onClick={e => e.stopPropagation()}>
                    <div className="actions">
                      {editBranch?.id === b.id ? (
                        <>
                          <button onClick={() => handleSaveBranch(b.id)} className="btn btn-primary btn-sm">Save</button>
                          <button onClick={() => setEditBranch(null)} className="btn btn-secondary btn-sm">Cancel</button>
                        </>
                      ) : (
                        <>
                          <button onClick={() => setEditBranch(b)} className="btn btn-secondary btn-sm">Edit</button>
                          <button onClick={() => handleDeleteBranch(b.id)} className="btn btn-danger btn-sm">Delete</button>
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
                ? <>Divisions — <span style={{ color: '#4f8ef7' }}>{selectedBranch.name}</span></>
                : 'Divisions'}
            </h2>
          </div>

          {!selectedBranch ? (
            <p style={{ color: '#999', fontSize: '0.9rem', padding: '1rem 0' }}>Select a branch to manage its divisions.</p>
          ) : (
            <>
              <form onSubmit={handleAddDivision} style={{ display: 'flex', gap: '0.5rem', marginBottom: '1.25rem' }}>
                <input
                  style={{ flex: 1, padding: '0.5rem 0.75rem', border: '1px solid #ddd', borderRadius: '6px', fontSize: '0.9rem' }}
                  placeholder={`New division in ${selectedBranch.name}...`}
                  value={newDivisionName}
                  onChange={e => setNewDivisionName(e.target.value)}
                  required
                />
                <button type="submit" className="btn btn-primary">Add</button>
              </form>

              <table>
                <thead>
                  <tr>
                    <th>Division</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {divisions.length === 0 ? (
                    <tr><td colSpan={2} style={{ textAlign: 'center', color: '#999', padding: '1.5rem' }}>No divisions yet</td></tr>
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
                        ) : d.name}
                      </td>
                      <td>
                        <div className="actions">
                          {editDivision?.id === d.id ? (
                            <>
                              <button onClick={() => handleSaveDivision(d.id)} className="btn btn-primary btn-sm">Save</button>
                              <button onClick={() => setEditDivision(null)} className="btn btn-secondary btn-sm">Cancel</button>
                            </>
                          ) : (
                            <>
                              <button onClick={() => setEditDivision(d)} className="btn btn-secondary btn-sm">Edit</button>
                              <button onClick={() => handleDeleteDivision(d.id)} className="btn btn-danger btn-sm">Delete</button>
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
