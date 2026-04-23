import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getDispatch } from '../api';

const fmt = (d) => d ? new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' }) : '—';

export default function DispatchDetail() {
  const { id } = useParams();
  const [dispatch, setDispatch] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getDispatch(id).then(r => { setDispatch(r.data); setLoading(false); });
  }, [id]);

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Loading…</div>;
  if (!dispatch) return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>Dispatch not found.</div>;

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.2rem' }}>Dispatch to Branch</h1>
          <div style={{ fontSize: '0.85rem', color: '#888' }}>
            {dispatch.warehouse_name} → {dispatch.branch_name} / {dispatch.division_name} &nbsp;·&nbsp; {fmt(dispatch.dispatched_at)}
          </div>
        </div>
        <Link to="/dispatch" className="btn btn-secondary">← Back</Link>
      </div>

      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '1.5rem', padding: '0.5rem 0 1rem' }}>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Warehouse</div>
            <span className="badge">{dispatch.warehouse_name}</span>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Branch</div>
            <div style={{ fontWeight: 500 }}>{dispatch.branch_name}</div>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Division</div>
            <div style={{ fontWeight: 500 }}>{dispatch.division_name}</div>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Dispatched By</div>
            <div style={{ fontWeight: 500 }}>{dispatch.dispatched_by_name ?? '—'}</div>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Notes</div>
            <div style={{ color: dispatch.notes ? '#333' : '#aaa', fontStyle: dispatch.notes ? 'normal' : 'italic' }}>{dispatch.notes ?? '—'}</div>
          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-header"><h2>{dispatch.items.length} item{dispatch.items.length !== 1 ? 's' : ''}</h2></div>
        <table>
          <thead>
            <tr>
              <th>Item</th>
              <th>Code</th>
              <th style={{ textAlign: 'right' }}>Quantity</th>
              <th>Unit</th>
            </tr>
          </thead>
          <tbody>
            {dispatch.items.map(it => (
              <tr key={it.id}>
                <td style={{ fontWeight: 500 }}>{it.item_name}</td>
                <td style={{ color: '#888', fontSize: '0.85rem' }}>{it.item_code}</td>
                <td style={{ textAlign: 'right', fontWeight: 600 }}>{Number(it.quantity).toLocaleString('id-ID')}</td>
                <td style={{ color: '#555' }}>{it.unit_name}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
