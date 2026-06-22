import { useEffect, useState, useMemo } from 'react';
import { Link } from 'react-router-dom';
import { getAttendance, getBranches } from '../../api';

// ── date helpers ──────────────────────────────────────────────────────────────

function toLocalISO(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function addDays(iso, n) {
  const d = new Date(iso + 'T00:00:00');
  d.setDate(d.getDate() + n);
  return toLocalISO(d);
}

function daysBetween(from, to) {
  return Math.round((new Date(to + 'T00:00:00') - new Date(from + 'T00:00:00')) / 86400000);
}

function genDateRange(from, to) {
  const dates = [];
  let cur = from;
  while (cur <= to) {
    dates.push(cur);
    cur = addDays(cur, 1);
  }
  return dates;
}

function getDayOfWeek(iso) {
  return new Date(iso + 'T00:00:00').getDay(); // 0=Sun
}

function fmtMonthHeader(iso) {
  // "Jan 2024"
  return new Date(iso + 'T00:00:00').toLocaleDateString('id-ID', { month: 'short', year: 'numeric' });
}

const DAY_ABBR = ['Min', 'Sen', 'Sel', 'Rab', 'Kam', 'Jum', 'Sab'];

// Groups consecutive dates by month, returns [{ label, dates }]
function groupByMonth(dates) {
  const groups = [];
  for (const d of dates) {
    const key = d.slice(0, 7); // YYYY-MM
    if (!groups.length || groups[groups.length - 1].key !== key) {
      groups.push({ key, label: fmtMonthHeader(d), dates: [] });
    }
    groups[groups.length - 1].dates.push(d);
  }
  return groups;
}

// ── period presets ────────────────────────────────────────────────────────────

function getPeriodDates(preset, customFrom, customTo) {
  const today = toLocalISO(new Date());

  if (preset === 'week') {
    const d = new Date();
    const dow = d.getDay();
    const monday = new Date(d);
    monday.setDate(d.getDate() - (dow === 0 ? 6 : dow - 1));
    return { from: toLocalISO(monday), to: today };
  }

  if (preset === 'month') {
    const d = new Date();
    const from = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`;
    return { from, to: today };
  }

  return { from: customFrom || today, to: customTo || today };
}

// ── cell appearance ───────────────────────────────────────────────────────────

function getCellInfo(rec) {
  if (!rec) return { bg: '#f5f5f5', border: '#e0e0e0', color: '#ccc', label: '' };

  const { status, is_late, is_early_leave, is_missing_checkout } = rec;
  const parts = [];
  if (is_late) parts.push('L');
  if (is_early_leave) parts.push('P');
  if (is_missing_checkout) parts.push('T');
  const label = parts.join('');
  const multi = parts.length > 1;

  if (status === 'absent')  return { bg: '#ffebee', border: '#ef5350', color: '#c62828', label: '' };
  if (status === 'leave')   return { bg: '#e3f2fd', border: '#42a5f5', color: '#1565c0', label: '' };
  if (status === 'holiday') return { bg: '#f3e5f5', border: '#ab47bc', color: '#6a1b9a', label: '' };

  // present
  if (!label)                          return { bg: '#e8f5e9', border: '#66bb6a', color: '#2e7d32', label: '' };
  if (multi)                           return { bg: '#fff0e0', border: '#ff7043', color: '#bf360c', label };
  if (is_late)                         return { bg: '#fff3e0', border: '#ffa726', color: '#e65100', label };
  if (is_early_leave)                  return { bg: '#fffde7', border: '#ffca28', color: '#f57f17', label };
  /* is_missing_checkout */            return { bg: '#fce4ec', border: '#f06292', color: '#880e4f', label };
}

// ── legend definition ─────────────────────────────────────────────────────────

const LEGEND = [
  { bg: '#e8f5e9', border: '#66bb6a', label: 'Hadir' },
  { bg: '#fff3e0', border: '#ffa726', label: 'Terlambat (L)' },
  { bg: '#fffde7', border: '#ffca28', label: 'Pulang Awal (P)' },
  { bg: '#fce4ec', border: '#f06292', label: 'Tdk Absen Pulang (T)' },
  { bg: '#fff0e0', border: '#ff7043', label: 'Anomali Ganda' },
  { bg: '#ffebee', border: '#ef5350', label: 'Absen' },
  { bg: '#e3f2fd', border: '#42a5f5', label: 'Cuti' },
  { bg: '#f3e5f5', border: '#ab47bc', label: 'Libur' },
  { bg: '#f5f5f5', border: '#e0e0e0', label: 'Tidak Ada Data' },
];

// ── branch palette ────────────────────────────────────────────────────────────

const BRANCH_COLORS = [
  '#4f80e1','#e07b3a','#4caf7d','#9c5de8','#e84c7d','#00aacc','#c9a227','#607d8b',
];

// ── tooltip ───────────────────────────────────────────────────────────────────

function cellTooltip(iso, rec) {
  const dateLabel = new Date(iso + 'T00:00:00').toLocaleDateString('id-ID', { weekday: 'long', day: 'numeric', month: 'long' });
  if (!rec) return `${dateLabel}\nTidak ada data`;

  const STATUS_LABEL = { present: 'Hadir', absent: 'Absen', leave: 'Cuti', holiday: 'Libur' };
  const lines = [`${dateLabel}`, `Status: ${STATUS_LABEL[rec.status] ?? rec.status}`];

  const fmtT = (ts) => ts ? new Date(ts).toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit' }) : null;
  const ci = fmtT(rec.check_in);
  const co = fmtT(rec.check_out);
  if (ci) lines.push(`Masuk: ${ci}`);
  if (co) lines.push(`Pulang: ${co}`);
  if (rec.is_late)             lines.push(`Terlambat ${rec.late_minutes} mnt`);
  if (rec.is_early_leave)      lines.push(`Pulang awal ${rec.early_leave_minutes} mnt`);
  if (rec.is_missing_checkout) lines.push('Tidak absen pulang');
  return lines.join('\n');
}

// ── main component ────────────────────────────────────────────────────────────

export default function AttendanceGrid() {
  const today = toLocalISO(new Date());

  const [preset, setPreset]         = useState('month');
  const [customFrom, setCustomFrom] = useState(() => {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`;
  });
  const [customTo, setCustomTo]     = useState(today);
  const [branchId, setBranchId]     = useState('');
  const [branches, setBranches]     = useState([]);
  const [records, setRecords]       = useState([]);
  const [loading, setLoading]       = useState(false);
  const [error, setError]           = useState('');

  useEffect(() => {
    getBranches().then(r => setBranches(r.data || [])).catch(() => {});
  }, []);

  const { from, to } = useMemo(
    () => getPeriodDates(preset, customFrom, customTo),
    [preset, customFrom, customTo],
  );

  const dates = useMemo(
    () => (!from || !to || from > to) ? [] : genDateRange(from, to),
    [from, to],
  );

  const monthGroups = useMemo(() => groupByMonth(dates), [dates]);

  const customRangeError = useMemo(() => {
    if (preset !== 'custom' || !customFrom || !customTo) return '';
    if (customTo < customFrom) return 'Tanggal akhir harus setelah tanggal awal';
    if (daysBetween(customFrom, customTo) > 30) return 'Maksimal rentang 31 hari';
    return '';
  }, [preset, customFrom, customTo]);

  const load = () => {
    if (!from || !to || from > to || customRangeError) return;
    setLoading(true);
    setError('');
    const params = { date_from: from, date_to: to };
    if (branchId) params.branch_id = branchId;
    getAttendance(params)
      .then(r => setRecords(r.data?.data || []))
      .catch(() => setError('Gagal memuat data absensi'))
      .finally(() => setLoading(false));
  };

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load(); }, [from, to, branchId]);

  // ── build grid ──────────────────────────────────────────────────────────────

  const { employees, grid } = useMemo(() => {
    const empMap  = new Map(); // id → employee meta
    const gridMap = new Map(); // id → { date → record }

    for (const rec of records) {
      const eid = rec.employee_id;
      if (!empMap.has(eid)) {
        empMap.set(eid, {
          id:            eid,
          full_name:     rec.full_name,
          employee_code: rec.employee_code,
          branch_name:   rec.branch_name,
        });
      }
      if (!gridMap.has(eid)) gridMap.set(eid, {});
      // rec.date comes as "YYYY-MM-DD" from the API
      const dateKey = String(rec.date).slice(0, 10);
      gridMap.get(eid)[dateKey] = rec;
    }

    const emps = [...empMap.values()].sort((a, b) => {
      const bc = a.branch_name.localeCompare(b.branch_name);
      return bc !== 0 ? bc : a.full_name.localeCompare(b.full_name);
    });

    return { employees: emps, grid: gridMap };
  }, [records]);

  const branchColorMap = useMemo(() => {
    const seen = new Map();
    employees.forEach(e => {
      if (!seen.has(e.branch_name))
        seen.set(e.branch_name, BRANCH_COLORS[seen.size % BRANCH_COLORS.length]);
    });
    return seen;
  }, [employees]);

  // per-employee summary
  function summary(empId) {
    const dayMap = grid.get(empId) || {};
    let present = 0, absent = 0, leave = 0, late = 0;
    for (const d of dates) {
      const r = dayMap[d];
      if (!r) continue;
      if (r.status === 'present') present++;
      else if (r.status === 'absent') absent++;
      else if (r.status === 'leave') leave++;
      if (r.is_late) late++;
    }
    return { present, absent, leave, late };
  }

  // ── shared cell/header styles ───────────────────────────────────────────────

  const stickyNameTh = {
    position: 'sticky', left: 0, zIndex: 3,
    background: '#f8f9fc',
    borderBottom: '2px solid #e0e0e0',
    borderRight: '1px solid #e0e0e0',
    padding: '0.5rem 0.75rem',
    textAlign: 'left', fontWeight: 700, fontSize: '0.78rem',
    color: '#555', letterSpacing: '0.03em',
    minWidth: '190px', whiteSpace: 'nowrap',
    boxShadow: '3px 0 6px rgba(0,0,0,0.06)',
  };

  const summaryTh = (color) => ({
    background: '#f8f9fc',
    borderBottom: '2px solid #e0e0e0',
    padding: '0.5rem 0.4rem',
    textAlign: 'center', fontWeight: 700,
    fontSize: '0.7rem', color,
    minWidth: '28px', whiteSpace: 'nowrap',
  });

  // ── render ──────────────────────────────────────────────────────────────────

  return (
    <div>
      {/* ── controls ── */}
      <div className="card" style={{ marginBottom: '1rem' }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.75rem', alignItems: 'center' }}>

          {/* preset pills */}
          <div style={{ display: 'flex', gap: '0.35rem' }}>
            {[
              { key: 'week',   label: 'Minggu Ini' },
              { key: 'month',  label: 'Bulan Ini'  },
              { key: 'custom', label: 'Kustom'     },
            ].map(p => (
              <button
                key={p.key}
                onClick={() => setPreset(p.key)}
                className={`btn btn-sm ${preset === p.key ? 'btn-primary' : 'btn-secondary'}`}
              >
                {p.label}
              </button>
            ))}
          </div>

          {/* custom date range */}
          {preset === 'custom' && (
            <div style={{ display: 'flex', gap: '0.4rem', alignItems: 'center' }}>
              <input
                type="date" value={customFrom} max={today}
                onChange={e => setCustomFrom(e.target.value)}
                style={{ fontSize: '0.85rem' }}
              />
              <span style={{ color: '#aaa', fontSize: '0.85rem' }}>s/d</span>
              <input
                type="date" value={customTo} max={today} min={customFrom}
                onChange={e => setCustomTo(e.target.value)}
                style={{ fontSize: '0.85rem' }}
              />
              <button onClick={load} className="btn btn-primary btn-sm">Terapkan</button>
            </div>
          )}

          {/* branch filter */}
          <select value={branchId} onChange={e => setBranchId(e.target.value)} style={{ fontSize: '0.85rem' }}>
            <option value="">Semua Cabang</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>

          {/* period label */}
          {from && to && !customRangeError && (
            <span style={{ color: '#888', fontSize: '0.8rem' }}>
              {new Date(from + 'T00:00:00').toLocaleDateString('id-ID', { day: 'numeric', month: 'short', year: 'numeric' })}
              {' — '}
              {new Date(to + 'T00:00:00').toLocaleDateString('id-ID', { day: 'numeric', month: 'short', year: 'numeric' })}
              {' · '}
              <strong>{dates.length}</strong> hari
            </span>
          )}
        </div>

        {customRangeError && (
          <div style={{ marginTop: '0.5rem', color: '#c62828', fontSize: '0.82rem' }}>
            {customRangeError}
          </div>
        )}
        {error && (
          <div style={{ marginTop: '0.5rem', color: '#c62828', fontSize: '0.82rem' }}>{error}</div>
        )}
      </div>

      {/* ── legend ── */}
      <div style={{
        marginBottom: '1rem',
        display: 'flex', flexWrap: 'wrap', gap: '0.6rem', alignItems: 'center',
        padding: '0.6rem 0.9rem',
        background: '#fff', border: '1px solid #eee', borderRadius: '8px',
      }}>
        <span style={{ fontSize: '0.72rem', fontWeight: 700, color: '#888', marginRight: '0.2rem', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
          Keterangan:
        </span>
        {LEGEND.map(l => (
          <span key={l.label} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.75rem', color: '#555' }}>
            <span style={{
              display: 'inline-block', width: '14px', height: '14px',
              background: l.bg, border: `2px solid ${l.border}`, borderRadius: '3px',
              flexShrink: 0,
            }} />
            {l.label}
          </span>
        ))}
        <span style={{ marginLeft: 'auto', fontSize: '0.72rem', color: '#aaa' }}>
          Hover sel untuk detail · L=Terlambat P=Pulang Awal T=Tdk Absen Pulang
        </span>
      </div>

      {/* ── grid ── */}
      {loading ? (
        <div className="card" style={{ textAlign: 'center', padding: '2.5rem', color: '#aaa' }}>Memuat…</div>
      ) : employees.length === 0 ? (
        <div className="card" style={{ textAlign: 'center', padding: '2.5rem', color: '#aaa' }}>
          Tidak ada data absensi untuk periode ini.
        </div>
      ) : (
        <div style={{ overflowX: 'auto', borderRadius: '8px', border: '1px solid #e8e8e8', background: '#fff' }}>
          <table style={{ borderCollapse: 'separate', borderSpacing: 0, fontSize: '0.8rem', tableLayout: 'fixed' }}>
            <thead>
              {/* ── month group header (only shown when range spans >1 month) ── */}
              {monthGroups.length > 1 && (
                <tr>
                  <th style={{ ...stickyNameTh, borderBottom: '1px solid #e0e0e0' }} />
                  {monthGroups.map(mg => (
                    <th
                      key={mg.key}
                      colSpan={mg.dates.length}
                      style={{
                        background: '#f0f4ff',
                        borderBottom: '1px solid #dde4f5',
                        borderLeft: '2px solid #c5d0f0',
                        padding: '0.3rem 0.4rem',
                        textAlign: 'center', fontWeight: 700,
                        fontSize: '0.72rem', color: '#4056a1',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {mg.label}
                    </th>
                  ))}
                  {/* summary col group header */}
                  <th colSpan={4} style={{ background: '#f8f9fc', borderBottom: '1px solid #e0e0e0', borderLeft: '1px solid #e0e0e0', padding: '0.3rem', textAlign: 'center', fontSize: '0.68rem', color: '#888' }}>
                    Rekap
                  </th>
                </tr>
              )}

              {/* ── day header ── */}
              <tr>
                <th style={stickyNameTh}>Karyawan</th>

                {dates.map(d => {
                  const dow     = getDayOfWeek(d);
                  const isWknd  = dow === 0 || dow === 6;
                  const dayNum  = d.slice(8);
                  return (
                    <th key={d} style={{
                      background: isWknd ? '#f9f0ff' : '#f8f9fc',
                      borderBottom: '2px solid #e0e0e0',
                      borderLeft: d.slice(8) === '01' ? '2px solid #c5d0f0' : undefined,
                      padding: '0.2rem 0',
                      textAlign: 'center',
                      width: '36px', minWidth: '36px',
                      fontWeight: 600,
                      color: isWknd ? '#9c27b0' : '#555',
                      lineHeight: 1.3,
                    }}>
                      <div style={{ fontSize: '0.62rem' }}>{DAY_ABBR[dow]}</div>
                      <div style={{ fontSize: '0.82rem' }}>{dayNum}</div>
                    </th>
                  );
                })}

                {/* summary headers */}
                <th style={{ ...summaryTh('#2e7d32'), borderLeft: '2px solid #e0e0e0' }} title="Hadir">H</th>
                <th style={summaryTh('#c62828')} title="Absen">A</th>
                <th style={summaryTh('#1565c0')} title="Cuti">C</th>
                <th style={summaryTh('#e65100')} title="Terlambat">L</th>
              </tr>
            </thead>

            <tbody>
              {employees.map((emp, rowIdx) => {
                const dayMap     = grid.get(emp.id) || {};
                const s          = summary(emp.id);
                const branchClr  = branchColorMap.get(emp.branch_name) || BRANCH_COLORS[0];
                const rowBg      = rowIdx % 2 === 0 ? '#fff' : '#fafbfc';

                return (
                  <tr key={emp.id}>
                    {/* employee name — sticky */}
                    <td style={{
                      position: 'sticky', left: 0, zIndex: 1,
                      background: rowBg,
                      borderBottom: '1px solid #f0f0f0',
                      borderRight: '1px solid #e8e8e8',
                      borderLeft: `3px solid ${branchClr}`,
                      padding: '0.35rem 0.65rem 0.35rem 0.6rem',
                      boxShadow: '3px 0 6px rgba(0,0,0,0.04)',
                    }}>
                      <Link
                        to={`/hr/employees/${emp.id}`}
                        style={{
                          fontWeight: 600, color: '#1a1a2e', textDecoration: 'none',
                          fontSize: '0.82rem', display: 'block',
                          whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
                          maxWidth: '155px',
                        }}
                      >
                        {emp.full_name}
                      </Link>
                      <div style={{ fontSize: '0.66rem', color: '#aaa', fontFamily: 'monospace' }}>
                        {emp.employee_code}
                      </div>
                    </td>

                    {/* attendance cells */}
                    {dates.map(d => {
                      const rec                          = dayMap[d];
                      const { bg, border, color, label } = getCellInfo(rec);
                      const isWknd                       = getDayOfWeek(d) === 0 || getDayOfWeek(d) === 6;

                      return (
                        <td
                          key={d}
                          title={cellTooltip(d, rec)}
                          style={{
                            padding: '3px 2px',
                            borderBottom: '1px solid #f0f0f0',
                            background: isWknd ? `${rowBg}` : rowBg,
                            borderLeft: d.slice(8) === '01' ? '2px solid #dde4f5' : undefined,
                          }}
                        >
                          <div style={{
                            width: '32px', height: '26px', margin: '0 auto',
                            background: isWknd && !rec ? '#f3e8ff44' : bg,
                            border: `1.5px solid ${isWknd && !rec ? '#d4aaee' : border}`,
                            borderRadius: '4px',
                            display: 'flex', alignItems: 'center', justifyContent: 'center',
                            fontSize: '0.58rem', fontWeight: 800, color,
                            letterSpacing: '-0.02em',
                          }}>
                            {label}
                          </div>
                        </td>
                      );
                    })}

                    {/* summary cells */}
                    <td style={{ textAlign: 'center', borderBottom: '1px solid #f0f0f0', borderLeft: '2px solid #e0e0e0', padding: '0 0.2rem', fontWeight: 700, fontSize: '0.78rem', color: '#2e7d32', background: rowBg }}>
                      {s.present || ''}
                    </td>
                    <td style={{ textAlign: 'center', borderBottom: '1px solid #f0f0f0', padding: '0 0.2rem', fontWeight: 700, fontSize: '0.78rem', color: s.absent ? '#c62828' : '#ccc', background: rowBg }}>
                      {s.absent || ''}
                    </td>
                    <td style={{ textAlign: 'center', borderBottom: '1px solid #f0f0f0', padding: '0 0.2rem', fontWeight: 700, fontSize: '0.78rem', color: s.leave ? '#1565c0' : '#ccc', background: rowBg }}>
                      {s.leave || ''}
                    </td>
                    <td style={{ textAlign: 'center', borderBottom: '1px solid #f0f0f0', padding: '0 0.2rem', fontWeight: 700, fontSize: '0.78rem', color: s.late ? '#e65100' : '#ccc', background: rowBg }}>
                      {s.late || ''}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* ── footer count ── */}
      {!loading && employees.length > 0 && (
        <div style={{ marginTop: '0.6rem', fontSize: '0.75rem', color: '#aaa', textAlign: 'right' }}>
          {employees.length} karyawan · {dates.length} hari
        </div>
      )}
    </div>
  );
}
