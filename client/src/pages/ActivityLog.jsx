import { useEffect, useState } from 'react';
import { getActivityLog } from '../api';

const ACTION_STYLE = {
  create:   { background: '#e6f9f0', color: '#27ae60' },
  update:   { background: '#e8f0fe', color: '#4f8ef7' },
  delete:   { background: '#fdecea', color: '#e74c3c' },
  transfer: { background: '#fef9e7', color: '#e67e22' },
};

const ENTITY_LABEL = {
  invoice:   'Invoice',
  inventory: 'Inventory',
  item:      'Item',
  transfer:  'Transfer',
};

export default function ActivityLog() {
  const [logs, setLogs] = useState([]);
  const [filter, setFilter] = useState({ entity_type: 'all', action: 'all', search: '' });

  useEffect(() => { getActivityLog().then(r => setLogs(r.data)); }, []);

  const fmt = (d) => new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' });

  const visible = logs.filter(l => {
    if (filter.entity_type !== 'all' && l.entity_type !== filter.entity_type) return false;
    if (filter.action !== 'all' && l.action !== filter.action) return false;
    if (filter.search && !l.description.toLowerCase().includes(filter.search.toLowerCase()) && !l.username.toLowerCase().includes(filter.search.toLowerCase())) return false;
    return true;
  });

  const set = (field) => (e) => setFilter(f => ({ ...f, [field]: e.target.value }));

  return (
    <>
      <div className="page-header">
        <h1>Activity Log</h1>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>{visible.length} entr{visible.length !== 1 ? 'ies' : 'y'}</h2>
          <div className="filters">
            <input placeholder="Search..." value={filter.search} onChange={set('search')} />
            <select value={filter.entity_type} onChange={set('entity_type')}>
              <option value="all">All Types</option>
              <option value="invoice">Invoice</option>
              <option value="inventory">Inventory</option>
              <option value="item">Item</option>
              <option value="transfer">Transfer</option>
            </select>
            <select value={filter.action} onChange={set('action')}>
              <option value="all">All Actions</option>
              <option value="create">Create</option>
              <option value="update">Update</option>
              <option value="delete">Delete</option>
              <option value="transfer">Transfer</option>
            </select>
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>User</th>
              <th>Action</th>
              <th>Type</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody>
            {visible.length === 0 ? (
              <tr><td colSpan={5} style={{textAlign:'center',color:'#999',padding:'2rem'}}>No activity yet</td></tr>
            ) : visible.map(log => (
              <tr key={log.id}>
                <td style={{color:'#888',fontSize:'0.82rem',whiteSpace:'nowrap'}}>{fmt(log.created_at)}</td>
                <td style={{fontWeight:500,fontSize:'0.88rem'}}>{log.username}</td>
                <td>
                  <span style={{
                    display:'inline-block',padding:'0.15rem 0.5rem',borderRadius:'4px',fontSize:'0.75rem',fontWeight:600,
                    ...(ACTION_STYLE[log.action] ?? { background:'#eee', color:'#555' })
                  }}>
                    {log.action}
                  </span>
                </td>
                <td>
                  <span className="badge">{ENTITY_LABEL[log.entity_type] ?? log.entity_type}</span>
                </td>
                <td style={{fontSize:'0.88rem',color:'#444'}}>{log.description}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
