import { useEffect, useState } from 'react';
import { getWarehouses, createWarehouse, updateWarehouse, deleteWarehouse } from '../api';

export default function Warehouses() {
  const [warehouses, setWarehouses] = useState([]);
  const [newName, setNewName] = useState('');
  const [editId, setEditId] = useState(null);
  const [editName, setEditName] = useState('');
  const [error, setError] = useState('');

  const load = () => getWarehouses().then(r => setWarehouses(r.data));
  useEffect(() => { load(); }, []);

  const handleAdd = async (e) => {
    e.preventDefault();
    setError('');
    try {
      await createWarehouse({ name: newName });
      setNewName('');
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Something went wrong');
    }
  };

  const handleEdit = async (id) => {
    setError('');
    try {
      await updateWarehouse(id, { name: editName });
      setEditId(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Something went wrong');
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Delete this warehouse?')) return;
    setError('');
    try {
      await deleteWarehouse(id);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Cannot delete: warehouse is in use');
    }
  };

  const startEdit = (w) => { setEditId(w.id); setEditName(w.name); };

  return (
    <>
      <div className="page-header">
        <h1>Warehouses</h1>
      </div>

      <div className="card" style={{maxWidth:'560px'}}>
        {error && <div className="error-msg" style={{marginBottom:'1rem'}}>{error}</div>}

        <form onSubmit={handleAdd} style={{display:'flex',gap:'0.75rem',marginBottom:'1.5rem'}}>
          <input
            style={{flex:1,padding:'0.55rem 0.75rem',border:'1px solid #ddd',borderRadius:'6px',fontSize:'0.95rem'}}
            placeholder="New warehouse name..."
            value={newName}
            onChange={e => setNewName(e.target.value)}
            required
          />
          <button type="submit" className="btn btn-primary">Add</button>
        </form>

        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {warehouses.length === 0 ? (
              <tr><td colSpan={2} style={{textAlign:'center',color:'#999',padding:'2rem'}}>No warehouses yet</td></tr>
            ) : warehouses.map(w => (
              <tr key={w.id}>
                <td>
                  {editId === w.id ? (
                    <input
                      value={editName}
                      onChange={e => setEditName(e.target.value)}
                      style={{padding:'0.35rem 0.6rem',border:'1px solid #4f8ef7',borderRadius:'6px',fontSize:'0.9rem',width:'100%'}}
                      autoFocus
                    />
                  ) : w.name}
                </td>
                <td>
                  <div className="actions">
                    {editId === w.id ? (
                      <>
                        <button onClick={() => handleEdit(w.id)} className="btn btn-primary btn-sm">Save</button>
                        <button onClick={() => setEditId(null)} className="btn btn-secondary btn-sm">Cancel</button>
                      </>
                    ) : (
                      <>
                        <button onClick={() => startEdit(w)} className="btn btn-secondary btn-sm">Edit</button>
                        <button onClick={() => handleDelete(w.id)} className="btn btn-danger btn-sm">Delete</button>
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
