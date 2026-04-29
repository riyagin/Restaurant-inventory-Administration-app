import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getStockTransferGroup } from '../api';

const fmt = (d) => d ? new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' }) : '—';

export default function TransferDetail() {
  const { id } = useParams();
  const [items, setItems] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getStockTransferGroup(id).then(r => { setItems(r.data); setLoading(false); });
  }, [id]);

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Memuat…</div>;
  if (!items) return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>Transfer tidak ditemukan.</div>;

  const first = items[0];

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.2rem' }}>Transfer Stok</h1>
          <div style={{ fontSize: '0.85rem', color: '#888' }}>
            {first.from_warehouse_name} → {first.to_warehouse_name} &nbsp;·&nbsp; {fmt(first.transferred_at)}
          </div>
        </div>
        <Link to="/transfers" className="btn btn-secondary">← Kembali</Link>
      </div>

      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '1.5rem', padding: '0.5rem 0 1rem' }}>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Dari</div>
            <span className="badge">{first.from_warehouse_name}</span>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Ke</div>
            <span className="badge">{first.to_warehouse_name}</span>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Ditransfer Oleh</div>
            <div style={{ fontWeight: 500 }}>{first.transferred_by_name ?? '—'}</div>
          </div>
          <div>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Catatan</div>
            <div style={{ color: first.notes ? '#333' : '#aaa', fontStyle: first.notes ? 'normal' : 'italic' }}>{first.notes ?? '—'}</div>
          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-header"><h2>{items.length} barang</h2></div>
        <table>
          <thead>
            <tr>
              <th>Barang</th>
              <th>Kode</th>
              <th style={{ textAlign: 'right' }}>Jumlah</th>
              <th>Satuan</th>
            </tr>
          </thead>
          <tbody>
            {items.map(it => (
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
