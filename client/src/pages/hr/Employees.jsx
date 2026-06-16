import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { getEmployees, getBranches, getPositions } from '../../api';

const SERVER = 'http://localhost:5000';

function getUser() {
  try { return JSON.parse(localStorage.getItem('user') || '{}'); } catch { return {}; }
}

const canEdit = () => {
  const role = getUser()?.role;
  return role === 'admin' || role === 'manager';
};

export default function Employees() {
  const navigate = useNavigate();
  const [rows, setRows]         = useState([]);
  const [total, setTotal]       = useState(0);
  const [branches, setBranches] = useState([]);
  const [positions, setPositions] = useState([]);
  const [loading, setLoading]   = useState(true);

  const [q, setQ]                 = useState('');
  const [branchId, setBranchId]   = useState('');
  const [positionId, setPositionId] = useState('');
  const [status, setStatus]       = useState('');
  const [page, setPage]           = useState(1);
  const limit = 25;

  const editable = canEdit();

  const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '-';

  const load = () => {
    setLoading(true);
    getEmployees({ q, branch_id: branchId, position_id: positionId, status, page, limit })
      .then(r => {
        setRows(r.data?.data || []);
        setTotal(r.data?.total || 0);
      })
      .catch(() => { setRows([]); setTotal(0); })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    getBranches().then(r => setBranches(r.data)).catch(() => {});
    getPositions().then(r => setPositions(r.data)).catch(() => {});
  }, []);

  useEffect(() => { load(); /* eslint-disable-next-line */ }, [q, branchId, positionId, status, page]);

  const totalPages = Math.max(1, Math.ceil(total / limit));

  return (
    <>
      <div className="page-header">
        <h1>Karyawan</h1>
        {editable && (
          <Link to="/hr/employees/new" className="btn btn-primary">+ Tambah Karyawan</Link>
        )}
      </div>

      <div className="card" style={{ marginBottom: '1rem' }}>
        <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap', alignItems: 'flex-end' }}>
          <div className="form-group" style={{ margin: 0, flex: '1 1 220px' }}>
            <label>Cari</label>
            <input
              value={q}
              onChange={e => { setPage(1); setQ(e.target.value); }}
              placeholder="Nama atau kode karyawan"
            />
          </div>
          <div className="form-group" style={{ margin: 0, flex: '1 1 160px' }}>
            <label>Cabang</label>
            <select value={branchId} onChange={e => { setPage(1); setBranchId(e.target.value); }}>
              <option value="">Semua Cabang</option>
              {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
            </select>
          </div>
          <div className="form-group" style={{ margin: 0, flex: '1 1 160px' }}>
            <label>Jabatan</label>
            <select value={positionId} onChange={e => { setPage(1); setPositionId(e.target.value); }}>
              <option value="">Semua Jabatan</option>
              {positions.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
          </div>
          <div className="form-group" style={{ margin: 0, flex: '1 1 140px' }}>
            <label>Status</label>
            <select value={status} onChange={e => { setPage(1); setStatus(e.target.value); }}>
              <option value="">Semua Status</option>
              <option value="active">Aktif</option>
              <option value="inactive">Nonaktif</option>
            </select>
          </div>
        </div>
      </div>

      <div className="card">
        <table>
          <thead>
            <tr>
              <th>Foto</th>
              <th>Kode</th>
              <th>Nama</th>
              <th>Jabatan</th>
              <th>Cabang</th>
              <th>Tgl Bergabung</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={7} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Memuat...</td></tr>
            ) : rows.length === 0 ? (
              <tr><td colSpan={7} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada karyawan</td></tr>
            ) : rows.map(e => (
              <tr key={e.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/hr/employees/${e.id}`)}>
                <td>
                  {e.photo_path ? (
                    <img
                      src={`${SERVER}/uploads/${e.photo_path}`}
                      alt={e.full_name}
                      style={{ width: 36, height: 36, borderRadius: '50%', objectFit: 'cover', border: '1px solid #e8e8e8' }}
                    />
                  ) : (
                    <div style={{ width: 36, height: 36, borderRadius: '50%', background: '#eef1f6', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#8a93a6', fontSize: '0.8rem' }}>
                      {(e.full_name || '?').charAt(0).toUpperCase()}
                    </div>
                  )}
                </td>
                <td style={{ fontFamily: 'monospace' }}>{e.employee_code}</td>
                <td style={{ fontWeight: 500 }}>{e.full_name}</td>
                <td>{e.position_name}</td>
                <td>{e.branch_name}</td>
                <td style={{ color: '#888', fontSize: '0.85rem' }}>{fmtDate(e.join_date)}</td>
                <td>
                  <span className="badge" style={{ background: e.status === 'active' ? '#e6f4ea' : '#fce8e6', color: e.status === 'active' ? '#1e7e34' : '#c5221f' }}>
                    {e.status === 'active' ? 'Aktif' : 'Nonaktif'}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1rem' }}>
        <span style={{ color: '#888', fontSize: '0.85rem' }}>Total {total} karyawan</span>
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <button className="btn btn-secondary btn-sm" disabled={page <= 1} onClick={() => setPage(p => Math.max(1, p - 1))}>Sebelumnya</button>
          <span style={{ fontSize: '0.85rem' }}>Halaman {page} / {totalPages}</span>
          <button className="btn btn-secondary btn-sm" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>Berikutnya</button>
        </div>
      </div>
    </>
  );
}
