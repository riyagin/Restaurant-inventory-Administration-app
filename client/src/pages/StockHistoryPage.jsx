import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getItem, getWarehouses, getStockHistory } from '../api';

const SOURCE_PATH = {
  invoice:  (id) => `/invoices/view/${id}`,
  transfer: (id) => `/transfers/group/${id}`,
  dispatch: (id) => `/dispatches/${id}`,
  opname:   (id) => `/stock-opname/detail/${id}`,
};

const TYPE_LABEL = {
  invoice:          'Invoice',
  manual_in:        'Manual In',
  manual_out:       'Manual Out',
  manual_adjustment:'Adjustment',
  pemakaian:        'Pemakaian',
  SO:               'SO',
};

const TYPE_STYLE = {
  invoice:           { background: '#e8f0fe', color: '#4f8ef7' },
  manual_in:         { background: '#e6f9f0', color: '#27ae60' },
  manual_out:        { background: '#fdecea', color: '#e74c3c' },
  manual_adjustment: { background: '#fef9e7', color: '#e67e22' },
  pemakaian:         { background: '#f3e8ff', color: '#8b5cf6' },
  SO:                { background: '#fff3e0', color: '#f57c00' },
};

const idr     = (v) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);
const fmt     = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';
const fmtTime = (d) => d ? new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' }) : '—';

export default function StockHistoryPage() {
  const { itemId } = useParams();
  const [item, setItem]           = useState(null);
  const [warehouses, setWarehouses] = useState([]);
  const [rows, setRows]           = useState([]);
  const [loading, setLoading]     = useState(true);
  const [filters, setFilters]     = useState({ warehouse_id: '', type: '', date_from: '', date_to: '' });

  useEffect(() => {
    Promise.all([getItem(itemId), getWarehouses()]).then(([i, w]) => {
      setItem(i.data);
      setWarehouses(w.data);
    });
  }, [itemId]);

  useEffect(() => {
    setLoading(true);
    const params = { all: 'true' };
    if (filters.warehouse_id) params.warehouse_id = filters.warehouse_id;
    if (filters.type)         params.type         = filters.type;
    if (filters.date_from)    params.date_from     = filters.date_from;
    if (filters.date_to)      params.date_to       = filters.date_to;
    getStockHistory(itemId, params).then(r => { setRows(r.data); setLoading(false); });
  }, [itemId, filters]);

  const set = (field) => (e) => setFilters(f => ({ ...f, [field]: e.target.value }));
  const clearDates = () => setFilters(f => ({ ...f, date_from: '', date_to: '' }));

  const netChange = rows.reduce((s, r) => s + Number(r.quantity_change), 0);

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.2rem' }}>
            {item ? `${item.name}` : 'Riwayat Stok'}
          </h1>
          {item && (
            <div style={{ fontSize: '0.85rem', color: '#888' }}>
              Kode: {item.code} &nbsp;·&nbsp; Satuan: {item.units.map(u => u.name).join(' → ')}
            </div>
          )}
        </div>
        <Link to="/inventory" className="btn btn-secondary">← Kembali ke Inventaris</Link>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>
            {loading ? 'Memuat…' : `${rows.length} pergerakan`}
            {!loading && rows.length > 0 && (
              <span style={{ marginLeft: '0.75rem', fontSize: '0.85rem', fontWeight: 400, color: netChange >= 0 ? '#27ae60' : '#e74c3c' }}>
                Netto: {netChange > 0 ? '+' : ''}{netChange.toLocaleString('id-ID')}
              </span>
            )}
          </h2>
          <div className="filters">
            <select value={filters.warehouse_id} onChange={set('warehouse_id')}>
              <option value="">Semua Gudang</option>
              {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
            </select>
            <select value={filters.type} onChange={set('type')}>
              <option value="">Semua Tipe</option>
              {Object.entries(TYPE_LABEL).map(([v, l]) => <option key={v} value={v}>{l}</option>)}
            </select>
            <input type="date" value={filters.date_from} onChange={set('date_from')} title="Dari tanggal" />
            <input type="date" value={filters.date_to}   onChange={set('date_to')}   title="Sampai tanggal" />
            {(filters.date_from || filters.date_to) && (
              <button type="button" onClick={clearDates} className="btn btn-secondary btn-sm">Hapus filter tanggal</button>
            )}
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>Waktu</th>
              <th>Tipe</th>
              <th>Perubahan</th>
              <th>Satuan</th>
              <th style={{ textAlign: 'right' }}>Nilai</th>
              <th>Gudang</th>
              <th>Vendor</th>
              <th>Referensi</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Memuat…</td></tr>
            ) : rows.length === 0 ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Tidak ada pergerakan ditemukan</td></tr>
            ) : rows.map(r => {
              const isPos = Number(r.quantity_change) > 0;
              const style = TYPE_STYLE[r.type] ?? { background: '#eee', color: '#555' };
              const refPath = r.source_type && r.source_id ? SOURCE_PATH[r.source_type]?.(r.source_id) : null;
              const val = r.value != null ? Number(r.value) : null;
              return (
                <tr key={r.id}>
                  <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmtTime(r.created_at)}</td>
                  <td>
                    <span style={{ display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px', fontSize: '0.75rem', fontWeight: 600, ...style }}>
                      {TYPE_LABEL[r.type] ?? r.type}
                    </span>
                  </td>
                  <td style={{ fontWeight: 700, color: isPos ? '#27ae60' : '#e74c3c', whiteSpace: 'nowrap' }}>
                    {isPos ? '+' : ''}{Number(r.quantity_change).toLocaleString('id-ID')}
                  </td>
                  <td style={{ color: '#555' }}>{r.unit_name}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600, whiteSpace: 'nowrap', color: val == null ? '#ccc' : val >= 0 ? '#27ae60' : '#e74c3c' }}>
                    {val == null ? '—' : (val >= 0 ? '+' : '') + idr(val)}
                  </td>
                  <td><span className="badge">{r.warehouse_name}</span></td>
                  <td style={{ color: '#555' }}>{r.vendor ?? <span style={{ color: '#ccc' }}>—</span>}</td>
                  <td style={{ fontSize: '0.85rem' }}>
                    {refPath ? (
                      <Link to={refPath} style={{ color: '#4f8ef7', textDecoration: 'none', fontWeight: 500 }}>
                        {r.reference ?? 'Lihat'}
                      </Link>
                    ) : (
                      <span style={{ color: r.reference ? '#888' : '#ccc', fontStyle: r.reference ? 'normal' : 'italic' }}>
                        {r.reference ?? '—'}
                      </span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </>
  );
}
