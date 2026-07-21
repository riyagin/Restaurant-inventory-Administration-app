import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getAttendance, getBranches, reconcileAttendance } from '../../api';
import AttendanceGrid from './AttendanceGrid';

// ── branch colour palette ─────────────────────────────────────────────────────

const BRANCH_PALETTE = [
  { border: '#4f80e1', bg: '#eef2fd', text: '#2d5ab3' },
  { border: '#e07b3a', bg: '#fdf0e8', text: '#b85e1f' },
  { border: '#4caf7d', bg: '#e9f7ef', text: '#2e7d52' },
  { border: '#9c5de8', bg: '#f3ecfd', text: '#6a2db5' },
  { border: '#e84c7d', bg: '#fdeef4', text: '#b52d57' },
  { border: '#00aacc', bg: '#e5f7fb', text: '#007a96' },
  { border: '#c9a227', bg: '#fdf6e3', text: '#9a7a1a' },
  { border: '#607d8b', bg: '#eceff1', text: '#37474f' },
];

// Builds a stable { branchName → palette entry } map from the rows in view.
function buildBranchColorMap(rows) {
  const seen = new Map();
  rows.forEach(r => {
    if (!seen.has(r.branch_name)) {
      seen.set(r.branch_name, BRANCH_PALETTE[seen.size % BRANCH_PALETTE.length]);
    }
  });
  return seen;
}

// ── shared helpers ────────────────────────────────────────────────────────────

const STATUS_META = {
  present: { label: 'Hadir',  bg: '#e8f5e9', color: '#2e7d32', dot: '#43a047' },
  absent:  { label: 'Absen',  bg: '#fdecea', color: '#c62828', dot: '#e53935' },
  leave:   { label: 'Cuti',   bg: '#e3f2fd', color: '#1565c0', dot: '#1e88e5' },
  holiday: { label: 'Libur',  bg: '#f3e5f5', color: '#6a1b9a', dot: '#8e24aa' },
};

const SOURCE_META = {
  face:        { label: 'Wajah',      icon: '😊', bg: '#e8f0fe', color: '#1a56b0' },
  fingerprint: { label: 'Sidik Jari', icon: '☝',  bg: '#fff3e0', color: '#b45309' },
  manual:      { label: 'Manual',     icon: '✎',  bg: '#eeeeee', color: '#555'    },
};

export function StatusChip({ status }) {
  const s = STATUS_META[status] || { label: status || '-', bg: '#eee', color: '#555' };
  return (
    <span style={{ background: s.bg, color: s.color, padding: '0.15rem 0.5rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.78rem' }}>
      {s.label}
    </span>
  );
}

export function SourceBadge({ source }) {
  if (!source) return <span style={{ color: '#ccc' }}>—</span>;
  const s = SOURCE_META[source] || SOURCE_META.manual;
  return (
    <span title={s.label} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.2rem', background: s.bg, color: s.color, padding: '0.1rem 0.4rem', borderRadius: '4px', fontSize: '0.72rem', fontWeight: 600 }}>
      <span>{s.icon}</span>{s.label}
    </span>
  );
}

export function AnomalyChips({ record }) {
  const chips = [];
  if (record.is_late)            chips.push(`Terlambat ${record.late_minutes} mnt`);
  if (record.is_early_leave)     chips.push('Pulang Awal');
  if (record.is_missing_checkout) chips.push('Tidak Absen Pulang');
  if (record.is_missing_checkin)  chips.push('Tidak Absen Masuk');
  if (record.is_no_punch)         chips.push('Tidak Absen Masuk & Pulang');
  if (chips.length === 0) return null;
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.25rem', marginTop: '0.4rem' }}>
      {chips.map((c, i) => (
        <span key={i} style={{ background: '#fff4e5', color: '#c05621', padding: '0.1rem 0.45rem', borderRadius: '4px', fontSize: '0.7rem', fontWeight: 600 }}>
          {c}
        </span>
      ))}
    </div>
  );
}

export const fmtTime = (ts) =>
  ts ? new Date(ts).toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit' }) : null;

function fmtDateLabel(iso) {
  return new Date(iso + 'T00:00:00').toLocaleDateString('id-ID', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
}

function toLocalISO(d) {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

function shiftDate(iso, days) {
  const d = new Date(iso + 'T00:00:00');
  d.setDate(d.getDate() + days);
  return toLocalISO(d);
}

const todayStr = () => toLocalISO(new Date());

// ── card ──────────────────────────────────────────────────────────────────────

function EmployeeCard({ r, branchColor }) {
  const sm = STATUS_META[r.status] || STATUS_META.absent;
  const bc = branchColor || BRANCH_PALETTE[0];
  const checkIn  = fmtTime(r.check_in);
  const checkOut = fmtTime(r.check_out);

  return (
    <div style={{
      border: '1px solid #e8e8e8',
      borderLeft: `4px solid ${sm.dot}`,
      borderRadius: '8px',
      overflow: 'hidden',
      background: '#fff',
      display: 'flex',
      flexDirection: 'column',
      minWidth: 0,
    }}>
      {/* branch colour strip */}
      <div style={{ height: '4px', background: bc.border }} />

      <div style={{ padding: '0.85rem 1rem', display: 'flex', flexDirection: 'column', gap: '0.35rem', flex: 1 }}>
        {/* header row */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '0.5rem' }}>
          <div style={{ minWidth: 0 }}>
            <Link
              to={`/hr/employees/${r.employee_id}`}
              style={{ fontWeight: 600, fontSize: '0.9rem', color: '#1a1a2e', textDecoration: 'none', display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
            >
              {r.full_name}
            </Link>
            <div style={{ fontSize: '0.72rem', color: '#999', fontFamily: 'monospace' }}>{r.employee_code}</div>
          </div>
          <StatusChip status={r.status} />
        </div>

        {/* branch badge */}
        <div style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', alignSelf: 'flex-start' }}>
          <span style={{
            background: bc.bg, color: bc.text,
            fontSize: '0.7rem', fontWeight: 600,
            padding: '0.1rem 0.45rem', borderRadius: '4px',
            border: `1px solid ${bc.border}33`,
          }}>
            {r.branch_name}
          </span>
        </div>

        {/* times */}
        <div style={{ borderTop: '1px solid #f0f0f0', marginTop: '0.25rem', paddingTop: '0.5rem', display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.4rem' }}>
          <div>
            <div style={{ fontSize: '0.68rem', color: '#aaa', textTransform: 'uppercase', letterSpacing: '0.04em' }}>Masuk</div>
            {checkIn
              ? <div style={{ display: 'flex', flexDirection: 'column', gap: '0.15rem' }}>
                  <span style={{ fontSize: '0.95rem', fontWeight: 700, color: '#222' }}>{checkIn}</span>
                  <SourceBadge source={r.check_in_source} />
                </div>
              : <span style={{ fontSize: '0.85rem', color: '#ccc' }}>—</span>
            }
          </div>
          <div>
            <div style={{ fontSize: '0.68rem', color: '#aaa', textTransform: 'uppercase', letterSpacing: '0.04em' }}>Pulang</div>
            {checkOut
              ? <div style={{ display: 'flex', flexDirection: 'column', gap: '0.15rem' }}>
                  <span style={{ fontSize: '0.95rem', fontWeight: 700, color: '#222' }}>{checkOut}</span>
                  <SourceBadge source={r.check_out_source} />
                </div>
              : <span style={{ fontSize: '0.85rem', color: '#ccc' }}>—</span>
            }
          </div>
        </div>

        <AnomalyChips record={r} />
      </div>
    </div>
  );
}

// ── summary bar ───────────────────────────────────────────────────────────────

function SummaryBar({ rows, activeStatus, onStatusChange }) {
  const counts = { '': rows.length };
  for (const s of Object.keys(STATUS_META)) {
    counts[s] = rows.filter(r => r.status === s).length;
  }

  const pills = [
    { key: '',        label: 'Semua'  },
    { key: 'present', label: 'Hadir'  },
    { key: 'absent',  label: 'Absen'  },
    { key: 'leave',   label: 'Cuti'   },
    { key: 'holiday', label: 'Libur'  },
  ];

  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem', alignItems: 'center' }}>
      {pills.map(p => {
        const meta = STATUS_META[p.key];
        const active = activeStatus === p.key;
        return (
          <button
            key={p.key}
            onClick={() => onStatusChange(p.key)}
            style={{
              padding: '0.3rem 0.75rem',
              borderRadius: '20px',
              border: active ? `2px solid ${meta?.dot || '#333'}` : '2px solid #e0e0e0',
              background: active ? (meta?.bg || '#f5f5f5') : '#fff',
              color: active ? (meta?.color || '#333') : '#555',
              fontWeight: active ? 700 : 400,
              fontSize: '0.82rem',
              cursor: 'pointer',
              display: 'inline-flex',
              alignItems: 'center',
              gap: '0.35rem',
              transition: 'all 0.15s',
            }}
          >
            {p.label}
            <span style={{
              background: active ? (meta?.dot || '#999') : '#e0e0e0',
              color: '#fff',
              borderRadius: '10px',
              padding: '0 0.4rem',
              fontSize: '0.72rem',
              fontWeight: 700,
              minWidth: '1.3rem',
              textAlign: 'center',
            }}>
              {counts[p.key] ?? 0}
            </span>
          </button>
        );
      })}
    </div>
  );
}

// ── main page ─────────────────────────────────────────────────────────────────

export default function AttendanceDashboard() {
  const [view, setView]           = useState('daily'); // 'daily' | 'grid'
  const [date, setDate]           = useState(todayStr());
  const [branchId, setBranchId]   = useState('');
  const [status, setStatus]       = useState('');
  const [anomalyOnly, setAnomalyOnly] = useState(false);
  const [search, setSearch]       = useState('');
  const [branches, setBranches]   = useState([]);
  const [allRows, setAllRows]     = useState([]);   // unfiltered by status (for counts)
  const [loading, setLoading]     = useState(false);
  const [reconciling, setReconciling] = useState(false);
  const [msg, setMsg]             = useState('');

  useEffect(() => {
    getBranches().then(r => setBranches(r.data || [])).catch(() => {});
  }, []);

  const load = (overrideDate) => {
    setLoading(true);
    setMsg('');
    const params = { date: overrideDate ?? date };
    if (branchId)          params.branch_id   = branchId;
    if (anomalyOnly)       params.anomaly_only = 'true';
    if (search.trim())     params.search       = search.trim();
    getAttendance(params)
      .then(r => setAllRows(r.data?.data || []))
      .catch(() => setMsg('Gagal memuat data kehadiran'))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); /* eslint-disable-next-line */ }, [date, branchId, anomalyOnly]);

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

  const navigate = (days) => {
    const next = shiftDate(date, days);
    setDate(next);
  };

  // client-side status filter (so summary bar counts stay accurate)
  const visibleRows = status ? allRows.filter(r => r.status === status) : allRows;

  // stable branch → color map derived from the full unfiltered result
  const branchColorMap = buildBranchColorMap(allRows);

  return (
    <>
      {/* ── page header ── */}
      <div className="page-header">
        <h1>Absensi Karyawan</h1>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <Link to="/hr/attendance/import" className="btn btn-secondary">Impor Sidik Jari</Link>
          <Link to="/hr/attendance/settings" className="btn btn-secondary">Pengaturan</Link>
        </div>
      </div>

      {/* ── view toggle ── */}
      <div style={{ display: 'flex', gap: '0.35rem', marginBottom: '1rem' }}>
        {[
          { key: 'daily', label: '📅 Harian' },
          { key: 'grid',  label: '📊 Rekap Grid' },
        ].map(v => (
          <button
            key={v.key}
            onClick={() => setView(v.key)}
            className={`btn btn-sm ${view === v.key ? 'btn-primary' : 'btn-secondary'}`}
          >
            {v.label}
          </button>
        ))}
      </div>

      {/* ── grid view ── */}
      {view === 'grid' && <AttendanceGrid />}

      {/* ── daily view ── */}
      {view !== 'grid' && <>

      {/* ── date navigation ── */}
      <div className="card" style={{ marginBottom: '1rem' }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '1rem', alignItems: 'center', justifyContent: 'space-between' }}>

          {/* date nav */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <button onClick={() => navigate(-1)} className="btn btn-secondary btn-sm" style={{ padding: '0.3rem 0.7rem', fontSize: '1rem' }}>‹</button>
            <div style={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
              <input
                type="date"
                value={date}
                onChange={e => setDate(e.target.value)}
                style={{ paddingRight: '2rem', fontWeight: 600, fontSize: '0.95rem', cursor: 'pointer' }}
              />
            </div>
            <button
              onClick={() => navigate(1)}
              disabled={date >= todayStr()}
              className="btn btn-secondary btn-sm"
              style={{ padding: '0.3rem 0.7rem', fontSize: '1rem', opacity: date >= todayStr() ? 0.4 : 1 }}
            >›</button>
            <span style={{ color: '#888', fontSize: '0.82rem', marginLeft: '0.25rem' }}>
              {fmtDateLabel(date)}
            </span>
            {date !== todayStr() && (
              <button onClick={() => setDate(todayStr())} className="btn btn-secondary btn-sm">Hari Ini</button>
            )}
          </div>

          {/* filters */}
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.6rem', alignItems: 'center' }}>
            <select value={branchId} onChange={e => setBranchId(e.target.value)} style={{ fontSize: '0.85rem' }}>
              <option value="">Semua Cabang</option>
              {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
            </select>

            <input
              value={search}
              onChange={e => setSearch(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') load(); }}
              placeholder="Cari nama / kode…"
              style={{ fontSize: '0.85rem', minWidth: '160px' }}
            />

            <label style={{ display: 'flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>
              <input type="checkbox" checked={anomalyOnly} onChange={e => setAnomalyOnly(e.target.checked)} />
              Hanya anomali
            </label>

            <button onClick={() => load()} className="btn btn-primary btn-sm">Terapkan</button>
            <button onClick={handleReconcile} disabled={reconciling} className="btn btn-secondary btn-sm">
              {reconciling ? 'Memproses…' : 'Rekonsiliasi Absen'}
            </button>
          </div>
        </div>

        {msg && (
          <div style={{ marginTop: '0.75rem', background: '#e8f5e9', color: '#1b5e20', padding: '0.5rem 0.75rem', borderRadius: '6px', fontSize: '0.85rem' }}>
            {msg}
          </div>
        )}
      </div>

      {/* ── status pill bar + branch legend ── */}
      {!loading && allRows.length > 0 && (
        <div style={{ marginBottom: '1rem', display: 'flex', flexWrap: 'wrap', gap: '0.75rem', alignItems: 'center', justifyContent: 'space-between' }}>
          <SummaryBar rows={allRows} activeStatus={status} onStatusChange={setStatus} />
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem', alignItems: 'center' }}>
            {[...branchColorMap.entries()].map(([name, bc]) => (
              <span key={name} style={{
                display: 'inline-flex', alignItems: 'center', gap: '0.35rem',
                background: bc.bg, color: bc.text,
                border: `1px solid ${bc.border}55`,
                borderLeft: `3px solid ${bc.border}`,
                fontSize: '0.75rem', fontWeight: 600,
                padding: '0.2rem 0.55rem', borderRadius: '4px',
              }}>
                {name}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* ── card grid ── */}
      {loading ? (
        <div className="card" style={{ textAlign: 'center', padding: '2.5rem', color: '#aaa' }}>Memuat…</div>
      ) : visibleRows.length === 0 ? (
        <div className="card" style={{ textAlign: 'center', padding: '2.5rem', color: '#aaa' }}>
          {allRows.length === 0
            ? 'Tidak ada data kehadiran untuk filter ini.'
            : 'Tidak ada karyawan dengan status ini.'}
        </div>
      ) : (
        <div style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))',
          gap: '0.75rem',
        }}>
          {visibleRows.map(r => (
            <EmployeeCard key={r.id} r={r} branchColor={branchColorMap.get(r.branch_name)} />
          ))}
        </div>
      )}

      </> /* end daily view */}
    </>
  );
}
