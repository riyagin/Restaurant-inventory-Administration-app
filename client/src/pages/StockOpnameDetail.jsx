import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getStockOpnameById } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);
const fmt = (d) => d ? new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' }) : '—';

export default function StockOpnameDetail() {
  const { id } = useParams();
  const [opname, setOpname] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getStockOpnameById(id).then(r => { setOpname(r.data); setLoading(false); });
  }, [id]);

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Memuat…</div>;
  if (!opname) return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>Opname tidak ditemukan.</div>;

  const totalWaste = opname.items.reduce((s, it) => s + Number(it.waste_value), 0);

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.2rem' }}>Stock Opname</h1>
          <div style={{ fontSize: '0.85rem', color: '#888' }}>
            {opname.warehouse_name} &nbsp;·&nbsp; {fmt(opname.performed_at)}
          </div>
        </div>
        <Link to="/stock-opname" className="btn btn-secondary">← Kembali</Link>
      </div>

      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '1.5rem', padding: '0.5rem 0 1rem' }}>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Gudang</div>
            <span className="badge">{opname.warehouse_name}</span>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Penanggung Jawab</div>
            <div style={{ fontWeight: 500 }}>{opname.pic_name ?? <span style={{color:'#aaa',fontStyle:'italic'}}>—</span>}</div>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Pelaksana</div>
            <div style={{ fontWeight: 500 }}>{opname.operator_name ?? <span style={{color:'#aaa',fontStyle:'italic'}}>—</span>}</div>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Dicatat Oleh</div>
            <div style={{ fontWeight: 500 }}>{opname.performed_by_name ?? '—'}</div>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Barang Disesuaikan</div>
            <span className="badge">{opname.items.length}</span>
          </div>
          {totalWaste > 0 && (
            <div>
              <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Total Susut</div>
              <div style={{ fontWeight: 700, color: '#e74c3c' }}>{idr(totalWaste)}</div>
            </div>
          )}
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Catatan</div>
            <div style={{ color: opname.notes ? '#333' : '#aaa', fontStyle: opname.notes ? 'normal' : 'italic' }}>{opname.notes ?? '—'}</div>
          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-header"><h2>{opname.items.length} barang disesuaikan</h2></div>
        <table>
          <thead>
            <tr>
              <th>Barang</th>
              <th>Kode</th>
              <th>Satuan</th>
              <th style={{ textAlign: 'right' }}>Tercatat</th>
              <th style={{ textAlign: 'right' }}>Aktual</th>
              <th style={{ textAlign: 'right' }}>Selisih</th>
              <th style={{ textAlign: 'right' }}>Nilai Susut</th>
            </tr>
          </thead>
          <tbody>
            {opname.items.map(it => {
              const diff = Number(it.difference);
              return (
                <tr key={it.id}>
                  <td style={{ fontWeight: 500 }}>{it.item_name}</td>
                  <td style={{ color: '#888', fontSize: '0.85rem' }}>{it.item_code}</td>
                  <td style={{ color: '#555' }}>{it.unit_name}</td>
                  <td style={{ textAlign: 'right' }}>{Number(it.recorded_quantity).toLocaleString('id-ID')}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{Number(it.actual_quantity).toLocaleString('id-ID')}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600, color: diff >= 0 ? '#27ae60' : '#e74c3c' }}>
                    {diff > 0 ? '+' : ''}{diff.toLocaleString('id-ID')}
                  </td>
                  <td style={{ textAlign: 'right', color: Number(it.waste_value) > 0 ? '#e74c3c' : '#aaa' }}>
                    {Number(it.waste_value) > 0 ? idr(it.waste_value) : '—'}
                  </td>
                </tr>
              );
            })}
          </tbody>
          {totalWaste > 0 && (
            <tfoot>
              <tr>
                <td colSpan={6} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>Total Susut:</td>
                <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#e74c3c' }}>{idr(totalWaste)}</td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>
    </>
  );
}
