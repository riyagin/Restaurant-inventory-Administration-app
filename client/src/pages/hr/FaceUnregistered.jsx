import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { getUnregisteredFaces, getBranches } from '../../api';

// Employees with no face embedding stored on the server, grouped so an operator
// can walk a branch's kiosk and enrol everyone in one pass. Backed by the
// `face=not` filter on GET /api/hr/employees.

const fmtDate = (d) => (d ? new Date(d).toLocaleDateString('id-ID') : '-');

export default function FaceUnregistered() {
  const navigate = useNavigate();
  const [rows, setRows]         = useState([]);
  const [total, setTotal]       = useState(0);
  const [branches, setBranches] = useState([]);
  const [loading, setLoading]   = useState(true);

  const [q, setQ]               = useState('');
  const [branchId, setBranchId] = useState('');
  const [page, setPage]         = useState(1);
  const limit = 50;

  useEffect(() => {
    getBranches().then(r => setBranches(r.data || [])).catch(() => {});
  }, []);

  useEffect(() => {
    setLoading(true);
    // status 'active' only: resigned/inactive staff will never enrol and would
    // otherwise inflate the backlog.
    getUnregisteredFaces({ q, branch_id: branchId, status: 'active', sort: 'name', dir: 'asc', page, limit })
      .then(r => {
        setRows(r.data?.data || []);
        setTotal(r.data?.total || 0);
      })
      .catch(() => { setRows([]); setTotal(0); })
      .finally(() => setLoading(false));
  }, [q, branchId, page]);

  const totalPages = Math.max(1, Math.ceil(total / limit));

  // Count per branch across the current page, to hint where the backlog sits.
  const perBranch = rows.reduce((acc, r) => {
    acc[r.branch_name] = (acc[r.branch_name] || 0) + 1;
    return acc;
  }, {});

  return (
    <>
      <div className="page-header">
        <h1>Wajah Belum Terdaftar</h1>
        <Link to="/hr/face" className="btn btn-secondary">← Ringkasan Wajah &amp; Perangkat</Link>
      </div>

      <div className="card" style={{ marginBottom: '1rem' }}>
        <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap', alignItems: 'flex-end' }}>
          <div className="form-group" style={{ margin: 0, flex: '1 1 240px' }}>
            <label>Cari</label>
            <input
              value={q}
              onChange={e => { setPage(1); setQ(e.target.value); }}
              placeholder="Nama atau kode karyawan"
            />
          </div>
          <div className="form-group" style={{ margin: 0, flex: '1 1 200px' }}>
            <label>Cabang</label>
            <select value={branchId} onChange={e => { setPage(1); setBranchId(e.target.value); }}>
              <option value="">Semua Cabang</option>
              {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
            </select>
          </div>
        </div>

        {!loading && Object.keys(perBranch).length > 1 && (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem', marginTop: '0.75rem' }}>
            {Object.entries(perBranch).sort((a, b) => b[1] - a[1]).map(([name, count]) => (
              <span key={name} className="badge" style={{ background: '#eef1f6', color: '#445' }}>
                {name} · {count}
              </span>
            ))}
          </div>
        )}
      </div>

      <div className="card">
        {loading ? (
          <div style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Memuat…</div>
        ) : rows.length === 0 ? (
          <div style={{ textAlign: 'center', color: '#1e7e34', padding: '2rem' }}>
            ✓ Semua karyawan aktif pada filter ini sudah memiliki data wajah.
          </div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Kode</th>
                <th>Nama</th>
                <th>Cabang</th>
                <th>Jabatan</th>
                <th>Tgl Bergabung</th>
                <th>Data Wajah</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(e => (
                <tr key={e.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/hr/employees/${e.id}`)}>
                  <td style={{ fontFamily: 'monospace' }}>{e.employee_code}</td>
                  <td style={{ fontWeight: 500 }}>{e.full_name}</td>
                  <td>{e.branch_name}</td>
                  <td>{e.position_name}</td>
                  <td style={{ color: '#888', fontSize: '0.85rem' }}>{fmtDate(e.join_date)}</td>
                  <td>
                    <span className="badge" style={{ background: '#fce8e6', color: '#c5221f' }}>Belum terdaftar</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1rem' }}>
        <span style={{ color: '#888', fontSize: '0.85rem' }}>
          Total {total} karyawan aktif belum memiliki data wajah
        </span>
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <button className="btn btn-secondary btn-sm" disabled={page <= 1} onClick={() => setPage(p => Math.max(1, p - 1))}>Sebelumnya</button>
          <span style={{ fontSize: '0.85rem' }}>Halaman {page} / {totalPages}</span>
          <button className="btn btn-secondary btn-sm" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>Berikutnya</button>
        </div>
      </div>

      <p style={{ marginTop: '1rem', fontSize: '0.82rem', color: '#8a93a6' }}>
        Pendaftaran wajah dilakukan dari aplikasi absensi Android di cabang terkait; data tersimpan di server sehingga
        semua perangkat dapat menggunakannya.
      </p>
    </>
  );
}
