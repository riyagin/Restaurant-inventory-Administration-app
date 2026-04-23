import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { getInvoices, deleteInvoice } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const STATUS_LABEL = { unpaid: 'Belum Dibayar', paid: 'Lunas', partial: 'Sebagian' };
const STATUS_CLASS = { unpaid: 'status-unpaid', paid: 'status-paid', partial: 'status-partial' };

export default function Invoices() {
  const [invoices, setInvoices] = useState([]);
  const [status, setStatus] = useState('all');

  const load = useCallback(() => {
    getInvoices({ status }).then(r => setInvoices(r.data));
  }, [status]);

  useEffect(() => { load(); }, [load]);

  const handleDelete = async (id) => {
    if (!confirm('Delete this invoice and all its line items?')) return;
    await deleteInvoice(id);
    load();
  };

  return (
    <>
      <div className="page-header">
        <h1>Invoices</h1>
        <Link to="/invoices/new" className="btn btn-primary">+ New Invoice</Link>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>{invoices.length} invoice{invoices.length !== 1 ? 's' : ''}</h2>
          <div className="filters">
            <select value={status} onChange={e => setStatus(e.target.value)}>
              <option value="all">All Statuses</option>
              <option value="unpaid">Belum Dibayar</option>
              <option value="partial">Sebagian</option>
              <option value="paid">Lunas</option>
            </select>
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>No. Invoice</th>
              <th>Ref. No.</th>
              <th>Type</th>
              <th>Tanggal</th>
              <th>Gudang / Branch</th>
              <th>Vendor</th>
              <th>Akun</th>
              <th>Total</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {invoices.length === 0 ? (
              <tr><td colSpan={10} style={{textAlign:'center',color:'#999',padding:'2rem'}}>No invoices found</td></tr>
            ) : invoices.map(inv => (
              <tr key={inv.id}>
                <td style={{fontWeight:600}}>{inv.invoice_number}</td>
                <td style={{color:'#888',fontSize:'0.85rem'}}>{inv.reference_number ?? '—'}</td>
                <td>
                  <span style={{
                    display:'inline-block',padding:'0.1rem 0.45rem',borderRadius:'4px',fontSize:'0.75rem',fontWeight:600,
                    background: inv.invoice_type === 'expense' ? '#fff3e0' : '#e6f9f0',
                    color: inv.invoice_type === 'expense' ? '#f57c00' : '#27ae60',
                  }}>
                    {inv.invoice_type === 'expense' ? 'Expense' : 'Purchase'}
                  </span>
                </td>
                <td>{new Date(inv.date).toLocaleDateString('id-ID')}</td>
                <td>
                  {inv.invoice_type === 'expense'
                    ? <span style={{color:'#555'}}>{[inv.branch_name, inv.division_name].filter(Boolean).join(' / ') || '—'}</span>
                    : <span style={{color:'#555'}}>{inv.warehouse_name ?? '—'}</span>
                  }
                </td>
                <td style={{color:'#888',fontSize:'0.85rem'}}>{inv.vendor_name ?? '—'}</td>
                <td style={{color:'#888'}}>{inv.account_name ?? '—'}</td>
                <td style={{fontWeight:600}}>{idr(inv.total)}</td>
                <td>
                  <span className={`badge ${STATUS_CLASS[inv.payment_status]}`}>
                    {STATUS_LABEL[inv.payment_status]}
                  </span>
                </td>
                <td>
                  <div className="actions">
                    <Link to={`/invoices/view/${inv.id}`} className="btn btn-secondary btn-sm">View</Link>
                    <Link to={`/invoices/edit/${inv.id}`} className="btn btn-secondary btn-sm">Edit</Link>
                    <button onClick={() => handleDelete(inv.id)} className="btn btn-danger btn-sm">Delete</button>
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
