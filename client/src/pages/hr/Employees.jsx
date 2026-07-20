import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { getEmployees, getBranches, getPositions, getContractAlerts } from '../../api';

const SERVER = 'http://localhost:5000';

function getUser() {
  try { return JSON.parse(localStorage.getItem('user') || '{}'); } catch { return {}; }
}

const canEdit = () => {
  const role = getUser()?.role;
  return role === 'admin' || role === 'manager';
};

// Whole-day difference between a contract end date and today (negative = overdue).
const DAY_MS = 86400000;
function contractDaysLeft(dateStr) {
  if (!dateStr) return null;
  const end = new Date(dateStr); end.setHours(0, 0, 0, 0);
  const today = new Date(); today.setHours(0, 0, 0, 0);
  return Math.round((end - today) / DAY_MS);
}

// Label for the "days remaining" state of a contract that is in its final month.
function expiryLabel(days) {
  if (days == null) return '';
  if (days < 0) return `Berakhir ${Math.abs(days)} hari lalu`;
  if (days === 0) return 'Berakhir hari ini';
  return `Berakhir dalam ${days} hari`;
}

function EmploymentBadge({ type, contractEnd }) {
  if (type !== 'contract') {
    return <span className="badge" style={{ background: '#eef1f6', color: '#445' }}>Tetap</span>;
  }
  const days = contractDaysLeft(contractEnd);
  const expiring = days != null && days <= 30;
  return (
    <span style={{ display: 'inline-flex', flexDirection: 'column', gap: 2 }}>
      <span className="badge" style={{ background: expiring ? '#fdece9' : '#e8f0fe', color: expiring ? '#c5221f' : '#1967d2' }}>
        Kontrak{expiring ? ' ⚠' : ''}
      </span>
      {contractEnd && (
        <span style={{ fontSize: '0.72rem', color: expiring ? '#c5221f' : '#8a93a6' }}>
          {new Date(contractEnd).toLocaleDateString('id-ID')}
        </span>
      )}
    </span>
  );
}

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
  const [employmentType, setEmploymentType] = useState('');
  const [page, setPage]           = useState(1);
  const [sort, setSort]           = useState('name'); // name | join_date | code
  const [dir, setDir]             = useState('asc');   // asc | desc
  const limit = 25;

  const [alerts, setAlerts] = useState([]);

  const editable = canEdit();

  const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '-';

  const load = () => {
    setLoading(true);
    getEmployees({ q, branch_id: branchId, position_id: positionId, status, employment_type: employmentType, sort, dir, page, limit })
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
    getContractAlerts().then(r => setAlerts(r.data?.data || [])).catch(() => setAlerts([]));
  }, []);

  useEffect(() => { load(); /* eslint-disable-next-line */ }, [q, branchId, positionId, status, employmentType, sort, dir, page]);

  const totalPages = Math.max(1, Math.ceil(total / limit));

  // Toggle sort: click a column to sort by it; click again to flip direction.
  const toggleSort = (col) => {
    setPage(1);
    if (sort === col) {
      setDir(d => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSort(col);
      setDir('asc');
    }
  };

  const SortHeader = ({ col, children }) => (
    <th
      onClick={() => toggleSort(col)}
      style={{ cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap' }}
      title="Klik untuk mengurutkan"
    >
      {children}
      <span style={{ color: sort === col ? '#1967d2' : '#c3c8d2', marginLeft: 4 }}>
        {sort === col ? (dir === 'asc' ? '▲' : '▼') : '↕'}
      </span>
    </th>
  );

  return (
    <>
      <div className="page-header">
        <h1>Karyawan</h1>
        {editable && (
          <Link to="/hr/employees/new" className="btn btn-primary">+ Tambah Karyawan</Link>
        )}
      </div>

      {alerts.length > 0 && (
        <div
          className="card"
          style={{ marginBottom: '1rem', borderLeft: '4px solid #f0a020', background: '#fffaf0' }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
            <span style={{ fontSize: '1.1rem' }}>⚠️</span>
            <strong style={{ color: '#a06800' }}>
              {alerts.length} kontrak karyawan memasuki bulan terakhir
            </strong>
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.5rem' }}>
            {alerts.map(a => (
              <Link
                key={a.id}
                to={`/hr/employees/${a.id}`}
                style={{
                  display: 'inline-flex', alignItems: 'center', gap: '0.4rem',
                  padding: '0.3rem 0.6rem', borderRadius: '6px', background: '#fff',
                  border: '1px solid #f0d090', fontSize: '0.85rem', textDecoration: 'none', color: '#333',
                }}
              >
                <strong>{a.full_name}</strong>
                <span style={{ color: a.days_remaining < 0 ? '#c5221f' : '#a06800' }}>
                  · {expiryLabel(a.days_remaining)}
                </span>
              </Link>
            ))}
          </div>
        </div>
      )}

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
          <div className="form-group" style={{ margin: 0, flex: '1 1 140px' }}>
            <label>Tipe</label>
            <select value={employmentType} onChange={e => { setPage(1); setEmploymentType(e.target.value); }}>
              <option value="">Semua Tipe</option>
              <option value="permanent">Tetap</option>
              <option value="contract">Kontrak</option>
            </select>
          </div>
        </div>
      </div>

      <div className="card">
        <table>
          <thead>
            <tr>
              <th>Foto</th>
              <SortHeader col="code">Kode</SortHeader>
              <SortHeader col="name">Nama</SortHeader>
              <th>Jabatan</th>
              <th>Cabang</th>
              <th>Tipe</th>
              <SortHeader col="join_date">Tgl Bergabung</SortHeader>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Memuat...</td></tr>
            ) : rows.length === 0 ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada karyawan</td></tr>
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
                <td><EmploymentBadge type={e.employment_type} contractEnd={e.contract_end_date} /></td>
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
