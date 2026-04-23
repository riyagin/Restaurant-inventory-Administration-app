import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getItem, getItemHistory } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);
const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID', { dateStyle: 'medium' }) : '—';

export default function NonStockItemDetail() {
  const { id } = useParams();
  const [item, setItem]       = useState(null);
  const [history, setHistory] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([getItem(id), getItemHistory(id)]).then(([ir, hr]) => {
      setItem(ir.data);
      setHistory(hr.data);
      setLoading(false);
    });
  }, [id]);

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Loading…</div>;
  if (!item)   return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>Item not found.</div>;

  const totalSpend    = history.reduce((s, r) => s + Number(r.line_total), 0);
  const totalQtyMap   = {};
  for (const r of history) {
    const key = r.unit_name ?? '—';
    totalQtyMap[key] = (totalQtyMap[key] ?? 0) + Number(r.quantity);
  }

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.2rem' }}>{item.name}</h1>
          <div style={{ fontSize: '0.85rem', color: '#888' }}>
            {item.code} &nbsp;·&nbsp;
            <span style={{ background: '#fff3e0', color: '#f57c00', fontWeight: 600, fontSize: '0.78rem', padding: '0.1rem 0.45rem', borderRadius: '4px' }}>
              Non-Stock Item
            </span>
          </div>
        </div>
        <Link to="/items" className="btn btn-secondary">← Back to Items</Link>
      </div>

      {/* Summary cards */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '1rem', marginBottom: '1.5rem' }}>
        <div className="card" style={{ padding: '1.25rem' }}>
          <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Total Spend</div>
          <div style={{ fontSize: '1.3rem', fontWeight: 700, color: '#e74c3c' }}>{idr(totalSpend)}</div>
        </div>
        <div className="card" style={{ padding: '1.25rem' }}>
          <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Transactions</div>
          <div style={{ fontSize: '1.3rem', fontWeight: 700 }}>{history.length}</div>
        </div>
        {Object.entries(totalQtyMap).map(([unit, qty]) => (
          <div key={unit} className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Total Qty ({unit})</div>
            <div style={{ fontSize: '1.3rem', fontWeight: 700 }}>{Number(qty).toLocaleString('id-ID')}</div>
          </div>
        ))}
      </div>

      <div className="card">
        <div className="card-header"><h2>Purchase &amp; Usage History</h2></div>
        {history.length === 0 ? (
          <p style={{ padding: '1.5rem', color: '#999', textAlign: 'center' }}>No transactions recorded yet.</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Date</th>
                <th>Invoice</th>
                <th>Branch</th>
                <th>Division</th>
                <th style={{ textAlign: 'right' }}>Qty</th>
                <th>Unit</th>
                <th style={{ textAlign: 'right' }}>Price / Unit</th>
                <th style={{ textAlign: 'right' }}>Total</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {history.map((row, idx) => (
                <tr key={row.id ?? idx}>
                  <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(row.date)}</td>
                  <td style={{ fontWeight: 600 }}>{row.invoice_number}</td>
                  <td style={{ color: '#555' }}>{row.branch_name ?? <span style={{ color: '#bbb', fontStyle: 'italic' }}>—</span>}</td>
                  <td style={{ color: '#555' }}>{row.division_name ?? <span style={{ color: '#bbb', fontStyle: 'italic' }}>—</span>}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{Number(row.quantity).toLocaleString('id-ID')}</td>
                  <td style={{ color: '#666' }}>{row.unit_name ?? '—'}</td>
                  <td style={{ textAlign: 'right', color: '#555' }}>{idr(row.price)}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(row.line_total)}</td>
                  <td>
                    <Link to={`/invoices/view/${row.invoice_id}`} className="btn btn-secondary btn-sm">View</Link>
                  </td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr>
                <td colSpan={7} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>Total Spend:</td>
                <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#e74c3c' }}>{idr(totalSpend)}</td>
                <td></td>
              </tr>
            </tfoot>
          </table>
        )}
      </div>
    </>
  );
}
