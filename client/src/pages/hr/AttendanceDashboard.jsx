import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getAttendance, getBranches, reconcileAttendance } from '../../api';

// Shared attendance indicators (also reused by EmployeeDetail "Absensi" tab).

const STATUS_CHIP = {
  present: { label: 'Hadir', bg: '#e8f5e9', color: '#2e7d32' },
  absent:  { label: 'Absen', bg: '#fdecea', color: '#c62828' },
  leave:   { label: 'Cuti',  bg: '#e3f2fd', color: '#1565c0' },
  holiday: { label: 'Libur', bg: '#f3e5f5', color: '#6a1b9a' },
};

export function StatusChip({ status }) {
  const s = STATUS_CHIP[status] || { label: status || '-', bg: '#eee', color: '#555' };
  return (
    <span style={{ background: s.bg, color: s.color, padding: '0.15rem 0.5rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.78rem' }}>
      {s.label}
    </span>
  );
}

const SOURCE_BADGE = {
  face:        { label: 'Wajah',     icon: '😊', bg: '#e8f0fe', color: '#1a56b0' },
  fingerprint: { label: 'Sidik Jari', icon: '☝', bg: '#fff3e0', color: '#b45309' },
  manual:      { label: 'Manual',    icon: '✎', bg: '#eeeeee', color: '#555' },
};

export function SourceBadge({ source }) {
  if (!source) return <span style={{ color: '#ccc' }}>—</span>;
  const s = SOURCE_BADGE[source] || SOURCE_BADGE.manual;
  return (
    <span title={s.label} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.2rem', background: s.bg, color: s.color, padding: '0.1rem 0.4rem', borderRadius: '4px', fontSize: '0.72rem', fontWeight: 600 }}>
      <span>{s.icon}</span>{s.label}
    </span>
  );
}

export function AnomalyChips({ record }) {
  const chips = [];
  if (record.is_late) chips.push(`Terlambat ${record.late_minutes} mnt`);
  if (record.is_early_leave) chips.push('Pulang Awal');
  if (record.is_missing_checkout) chips.push('Tidak Absen Pulang');
  if (chips.length === 0) return <span style={{ color: '#ccc' }}>—</span>;
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.25rem' }}>
      {chips.map((c, i) => (
        <span key={i} style={{ background: '#fff4e5', color: '#c05621', padding: '0.1rem 0.4rem', borderRadius: '4px', fontSize: '0.72rem', fontWeight: 600 }}>
          {c}
        </span>
      ))}
    </div>
  );
}

export const fmtTime = (ts) => ts ? new Date(ts).toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit' }) : '—';

const todayStr = () => new Date().toISOString().slice(0, 10);

export default function AttendanceDashboard() {
  const [date, setDate] = useState(todayStr());
  const [branchId, setBranchId] = useState('');
  const [status, setStatus] = useState('');
  const [anomalyOnly, setAnomalyOnly] = useState(false);
  const [search, setSearch] = useState('');
  const [branches, setBranches] = useState([]);
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(false);
  const [reconciling, setReconciling] = useState(false);
  const [msg, setMsg] = useState('');

  useEffect(() => {
    getBranches().then(r => setBranches(r.data || [])).catch(() => {});
  }, []);

  const load = () => {
    setLoading(true);
    setMsg('');
    const params = { date };
    if (branchId) params.branch_id = branchId;
    if (status) params.status = status;
    if (anomalyOnly) params.anomaly_only = 'true';
    if (search.trim()) params.search = search.trim();
    getAttendance(params)
      .then(r => setRows(r.data?.data || []))
      .catch(() => setMsg('Gagal memuat data kehadiran'))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); /* eslint-disable-next-line */ }, [date, branchId, status, anomalyOnly]);

  const handleReconcile = async () => {
    setReconciling(true);
    setMsg('');
    try {
      const r = await reconcileAttendance(date);
      setMsg(`Rekonsiliasi selesai: ${r.data.absent_created} karyawan ditandai absen untuk ${r.data.date}.`);
      load();
    } catch {
      setMsg('Gagal menjalankan rekonsiliasi');
    } finally {
      setReconciling(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Absensi Karyawan</h1>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <Link to="/hr/attendance/import" className="btn btn-secondary">Impor Sidik Jari</Link>
          <Link to="/hr/attendance/settings" className="btn btn-secondary">Pengaturan</Link>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '1.25rem' }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.85rem', alignItems: 'flex-end' }}>
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Tanggal</label>
            <input type="date" value={date} onChange={e => setDate(e.target.value)} />
          </div>
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Cabang</label>
            <select value={branchId} onChange={e => setBranchId(e.target.value)}>
              <option value="">Semua Cabang</option>
              {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
            </select>
          </div>
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Status</label>
            <select value={status} onChange={e => setStatus(e.target.value)}>
              <option value="">Semua</option>
              <option value="present">Hadir</option>
              <option value="absent">Absen</option>
              <option value="leave">Cuti</option>
              <option value="holiday">Libur</option>
            </select>
          </div>
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Cari Nama / Kode</label>
            <input value={search} onChange={e => setSearch(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') load(); }} placeholder="cari…" />
          </div>
          <label style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', fontSize: '0.85rem' }}>
            <input type="checkbox" checked={anomalyOnly} onChange={e => setAnomalyOnly(e.target.checked)} />
            Hanya anomali
          </label>
          <button onClick={load} className="btn btn-primary btn-sm">Terapkan</button>
          <button onClick={handleReconcile} disabled={reconciling} className="btn btn-secondary btn-sm">
            {reconciling ? 'Memproses…' : 'Rekonsiliasi Absen'}
          </button>
        </div>
        {msg && <div style={{ marginTop: '0.75rem', color: '#1b5e45', fontSize: '0.85rem' }}>{msg}</div>}
      </div>

      <div className="card" style={{ overflowX: 'auto' }}>
        {loading ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>
        ) : rows.length === 0 ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Tidak ada data kehadiran untuk filter ini.</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Karyawan</th>
                <th>Cabang</th>
                <th>Masuk</th>
                <th>Sumber</th>
                <th>Pulang</th>
                <th>Sumber</th>
                <th>Status</th>
                <th>Anomali</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(r => (
                <tr key={r.id}>
                  <td>
                    <Link to={`/hr/employees/${r.employee_id}`} style={{ fontWeight: 500 }}>{r.full_name}</Link>
                    <div style={{ fontSize: '0.75rem', color: '#999', fontFamily: 'monospace' }}>{r.employee_code}</div>
                  </td>
                  <td style={{ fontSize: '0.85rem' }}>{r.branch_name}</td>
                  <td>{fmtTime(r.check_in)}</td>
                  <td><SourceBadge source={r.check_in_source} /></td>
                  <td>{fmtTime(r.check_out)}</td>
                  <td><SourceBadge source={r.check_out_source} /></td>
                  <td><StatusChip status={r.status} /></td>
                  <td><AnomalyChips record={r} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
