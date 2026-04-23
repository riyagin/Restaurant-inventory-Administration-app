import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { getInventory, getWarehouses, deleteInventoryRecord, getStockHistory } from '../api';

const SOURCE_PATH = {
  invoice:  (id) => `/invoices/view/${id}`,
  transfer: (id) => `/transfers/group/${id}`,
  dispatch: (id) => `/dispatches/${id}`,
  opname:   (id) => `/stock-opname/detail/${id}`,
};

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';

const TYPE_LABEL = {
  invoice: 'Invoice',
  manual_in: 'Manual In',
  manual_out: 'Manual Out',
  manual_adjustment: 'Adjustment',
  pemakaian: 'Pemakaian',
  SO: 'SO',
};

const TYPE_STYLE = {
  invoice:           { background: '#e8f0fe', color: '#4f8ef7' },
  manual_in:         { background: '#e6f9f0', color: '#27ae60' },
  manual_out:        { background: '#fdecea', color: '#e74c3c' },
  manual_adjustment: { background: '#fef9e7', color: '#e67e22' },
  pemakaian:         { background: '#f3e8ff', color: '#8b5cf6' },
  SO:                { background: '#fff3e0', color: '#f57c00' },
};

function HistoryPanel({ itemId, warehouseId }) {
  const [rows, setRows] = useState(null);

  useEffect(() => {
    getStockHistory(itemId, { warehouse_id: warehouseId }).then(r => setRows(r.data));
  }, [itemId, warehouseId]);

  if (!rows) return (
    <td colSpan={8} style={{padding:'1rem 1.5rem',background:'#f8f9ff'}}>
      <span style={{color:'#999',fontSize:'0.85rem'}}>Loading history...</span>
    </td>
  );

  if (!rows.length) return (
    <td colSpan={8} style={{padding:'1rem 1.5rem',background:'#f8f9ff'}}>
      <span style={{color:'#999',fontSize:'0.85rem'}}>No history yet</span>
    </td>
  );

  return (
    <td colSpan={8} style={{padding:'0.75rem 1.5rem 1rem',background:'#f8f9ff',borderTop:'none'}}>
      <div style={{fontSize:'0.78rem',fontWeight:600,color:'#666',marginBottom:'0.5rem',textTransform:'uppercase',letterSpacing:'0.4px'}}>
        Last {rows.length} interactions
      </div>
      <table style={{width:'100%',borderCollapse:'collapse',fontSize:'0.82rem'}}>
        <thead>
          <tr>
            {['Date','Type','Change','Unit','Value','Vendor','Warehouse','Reference'].map(h => (
              <th key={h} style={{textAlign: h === 'Value' ? 'right' : 'left',padding:'0.3rem 0.6rem',color:'#888',fontWeight:600,borderBottom:'1px solid #e8e8e8',whiteSpace:'nowrap'}}>
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map(r => {
            const isPositive = Number(r.quantity_change) > 0;
            const refPath = r.source_type && r.source_id ? SOURCE_PATH[r.source_type]?.(r.source_id) : null;
            const val = r.value != null ? Number(r.value) : null;
            return (
              <tr key={r.id}>
                <td style={{padding:'0.3rem 0.6rem',color:'#555'}}>{fmt(r.date)}</td>
                <td style={{padding:'0.3rem 0.6rem'}}>
                  <span style={{
                    display:'inline-block',padding:'0.1rem 0.45rem',borderRadius:'4px',fontSize:'0.75rem',fontWeight:600,
                    ...(TYPE_STYLE[r.type] ?? { background: '#eee', color: '#555' }),
                  }}>
                    {TYPE_LABEL[r.type] ?? r.type}
                  </span>
                </td>
                <td style={{padding:'0.3rem 0.6rem',fontWeight:600,color: isPositive ? '#27ae60' : '#e74c3c'}}>
                  {isPositive ? '+' : ''}{Number(r.quantity_change).toLocaleString('id-ID')}
                </td>
                <td style={{padding:'0.3rem 0.6rem',color:'#555'}}>{r.unit_name}</td>
                <td style={{padding:'0.3rem 0.6rem',textAlign:'right',fontWeight:600,whiteSpace:'nowrap',color: val == null ? '#ccc' : val >= 0 ? '#27ae60' : '#e74c3c'}}>
                  {val == null ? '—' : (val >= 0 ? '+' : '') + idr(val)}
                </td>
                <td style={{padding:'0.3rem 0.6rem',color:'#555'}}>{r.vendor ?? '—'}</td>
                <td style={{padding:'0.3rem 0.6rem',color:'#555'}}>{r.warehouse_name}</td>
                <td style={{padding:'0.3rem 0.6rem'}}>
                  {refPath ? (
                    <Link to={refPath} style={{color:'#4f8ef7',textDecoration:'none',fontWeight:500,fontSize:'0.82rem'}}>
                      {r.reference ?? 'View'}
                    </Link>
                  ) : (
                    <span style={{color: r.reference ? '#888' : '#ccc', fontStyle: r.reference ? 'normal' : 'italic'}}>
                      {r.reference ?? '—'}
                    </span>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </td>
  );
}

export default function Inventory() {
  const [records, setRecords] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [search, setSearch] = useState('');
  const [warehouseId, setWarehouseId] = useState('all');
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo, setDateTo] = useState('');
  const [expandedId, setExpandedId] = useState(null);

  const load = useCallback(() => {
    getInventory({ search, warehouse_id: warehouseId, date_from: dateFrom || undefined, date_to: dateTo || undefined })
      .then(r => setRecords(r.data));
  }, [search, warehouseId, dateFrom, dateTo]);

  useEffect(() => {
    getWarehouses().then(r => setWarehouses(r.data));
  }, []);

  useEffect(() => { load(); }, [load]);

  const clearDates = () => { setDateFrom(''); setDateTo(''); };

  const handleDelete = async (e, id) => {
    e.stopPropagation();
    if (!confirm('Delete this inventory record?')) return;
    await deleteInventoryRecord(id);
    setExpandedId(null);
    load();
  };

  const toggleRow = (id) => setExpandedId(prev => prev === id ? null : id);

  return (
    <>
      <div className="page-header">
        <h1>Inventory</h1>
        <Link to="/inventory/new" className="btn btn-primary">+ Add Record</Link>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>{records.length} record{records.length !== 1 ? 's' : ''}</h2>
          <div className="filters">
            <input
              placeholder="Search item..."
              value={search}
              onChange={e => setSearch(e.target.value)}
            />
            <select value={warehouseId} onChange={e => setWarehouseId(e.target.value)}>
              <option value="all">All Warehouses</option>
              {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
            </select>
            <input type="date" value={dateFrom} onChange={e => setDateFrom(e.target.value)} title="Date from" />
            <input type="date" value={dateTo}   onChange={e => setDateTo(e.target.value)}   title="Date to" />
            {(dateFrom || dateTo) && (
              <button type="button" onClick={clearDates} className="btn btn-secondary btn-sm">Clear dates</button>
            )}
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th></th>
              <th>Item</th>
              <th>Code</th>
              <th>Quantity</th>
              <th>Warehouse</th>
              <th>Value (IDR)</th>
              <th>Date</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {records.length === 0 ? (
              <tr><td colSpan={8} style={{textAlign:'center',color:'#999',padding:'2rem'}}>No records found</td></tr>
            ) : records.map(rec => (
              <>
                <tr
                  key={rec.id}
                  onClick={() => toggleRow(rec.id)}
                  style={{cursor:'pointer'}}
                  className={expandedId === rec.id ? 'row-expanded' : ''}
                >
                  <td style={{width:'28px',color:'#aaa',fontSize:'0.75rem',userSelect:'none'}}>
                    {expandedId === rec.id ? '▼' : '▶'}
                  </td>
                  <td>{rec.item_name}</td>
                  <td style={{color:'#888',fontSize:'0.85rem'}}>{rec.item_code}</td>
                  <td><span className="badge">{Number(rec.quantity).toLocaleString('id-ID')} {rec.unit_name}</span></td>
                  <td><span className="badge">{rec.warehouse_name}</span></td>
                  <td style={{fontWeight:600}}>{idr(rec.value)}</td>
                  <td style={{color:'#888',fontSize:'0.85rem'}}>{fmt(rec.date)}</td>
                  <td onClick={e => e.stopPropagation()}>
                    <div className="actions">
                      <Link to={`/inventory/history/${rec.item_id}`} className="btn btn-secondary btn-sm">History</Link>
                      <Link to={`/inventory/edit/${rec.id}`} className="btn btn-secondary btn-sm">Edit</Link>
                      <button onClick={(e) => handleDelete(e, rec.id)} className="btn btn-danger btn-sm">Delete</button>
                    </div>
                  </td>
                </tr>
                {expandedId === rec.id && (
                  <tr key={`${rec.id}-history`} style={{background:'#f8f9ff'}}>
                    <HistoryPanel itemId={rec.item_id} warehouseId={rec.warehouse_id} />
                  </tr>
                )}
              </>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
