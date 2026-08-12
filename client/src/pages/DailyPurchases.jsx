import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getDailyPurchases, cancelDailyPurchase, getBranches } from '../api';
import { getRole } from '../roles';

// The record of what each branch spent out of its cash box.
//
// List only — entry lives on its own page. Keeping the form here meant the list
// re-rendered under every keystroke and a half-filled run was lost the moment
// anyone touched a filter.

const rp = (n) => 'Rp ' + Number(n || 0).toLocaleString('id-ID');
const today = () => new Date().toISOString().slice(0, 10);

export default function DailyPurchases() {
  const isAdmin = getRole() === 'admin';

  const [rows, setRows] = useState([]);
  const [branches, setBranches] = useState([]);
  const [filterBranch, setFilterBranch] = useState('');
  const [from, setFrom] = useState(today());
  const [to, setTo] = useState(today());
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  const load = useCallback(() =>
    getDailyPurchases({ branch_id: filterBranch || undefined, from, to })
      .then(r => setRows(r.data || []))
      .catch(() => setError('Gagal memuat pembelanjaan'))
      .finally(() => setLoading(false)),
  [filterBranch, from, to]);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    getBranches().then(r => setBranches(r.data || [])).catch(() => {});
  }, []);

  const cancel = async (row) => {
    const reason = prompt(`Alasan pembatalan ${row.number}?`);
    if (!reason?.trim()) return;
    try {
      await cancelDailyPurchase(row.id, reason.trim());
      await load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal membatalkan');
    }
  };

  const grandTotal = rows
    .filter(r => r.status === 'posted')
    .reduce((s, r) => s + r.total_amount, 0);

  return (
    <>
      <div className="page-header">
        <h1>Pembelanjaan Harian</h1>
        <div style={{display:'flex',gap:'0.6rem',alignItems:'center',flexWrap:'wrap'}}>
          <Link to="/petty-cash" className="btn btn-secondary btn-sm">Kas Kecil</Link>
          <Link to="/templates?tab=pembelanjaan" className="btn btn-secondary btn-sm">Template</Link>
          <Link to="/daily-purchases/new" className="btn btn-primary">+ Catat Pembelanjaan</Link>
        </div>
      </div>

      {error && <div className="error-msg" style={{marginBottom:'1rem'}}>{error}</div>}

      <div className="card">
        <div style={{display:'flex',gap:'0.75rem',flexWrap:'wrap',alignItems:'center',marginBottom:'1rem'}}>
          <select value={filterBranch} onChange={e => setFilterBranch(e.target.value)} style={fieldStyle}>
            <option value="">Semua cabang</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
          <input type="date" value={from} onChange={e => setFrom(e.target.value)} style={fieldStyle} />
          <span style={{color:'#999'}}>s/d</span>
          <input type="date" value={to} onChange={e => setTo(e.target.value)} style={fieldStyle} />
          <span style={{marginLeft:'auto',fontWeight:600}}>Total: {rp(grandTotal)}</span>
        </div>

        <table>
          <thead>
            <tr>
              <th>Nomor</th><th>Tanggal</th><th>Cabang</th><th>Keterangan</th>
              <th>Baris</th><th style={{textAlign:'right'}}>Total</th><th>Dicatat</th><th></th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={8} style={{textAlign:'center',color:'#999',padding:'2rem'}}>Memuat…</td></tr>
            ) : rows.length === 0 ? (
              <tr><td colSpan={8} style={{textAlign:'center',color:'#999',padding:'2rem'}}>
                Belum ada pembelanjaan pada rentang ini
              </td></tr>
            ) : rows.map(r => (
              <tr key={r.id} style={r.status === 'cancelled'
                    ? {opacity:0.55, textDecoration:'line-through'} : undefined}>
                <td style={{fontFamily:'monospace'}}>{r.number}</td>
                <td>{r.date?.slice(0, 10)}</td>
                <td>{r.branch_name}{r.division_name && <span style={{color:'#888'}}> / {r.division_name}</span>}</td>
                <td style={{color:'#777',fontSize:'0.88rem'}}>{r.notes || (r.vendor_name ?? '—')}</td>
                <td>{r.line_count}</td>
                <td style={{textAlign:'right',fontWeight:600}}>{rp(r.total_amount)}</td>
                <td style={{color:'#888',fontSize:'0.85rem'}}>{r.created_by_name || '—'}</td>
                <td>
                  {isAdmin && r.status === 'posted' && (
                    <button className="btn btn-danger btn-sm" onClick={() => cancel(r)}>Batalkan</button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}

const fieldStyle = {
  padding: '0.45rem 0.6rem', border: '1px solid #ddd',
  borderRadius: '6px', fontSize: '0.9rem',
};
