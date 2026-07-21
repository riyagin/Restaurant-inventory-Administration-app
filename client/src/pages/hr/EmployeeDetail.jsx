import { useEffect, useState, useCallback, Fragment } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import {
  getEmployee, deleteEmployee, transitionEmployeeToPermanent, resignEmployee,
  getEmployeeWage, getEmployeeWageHistory, createEmployeeWage, getWageComponents,
  getAttendance, getEmployeePerformance,
  getLeaveBalance, getEmployeeLeaveRequests,
  getEmployeeKasbons,
} from '../../api';
import CurrencyInput from '../../components/CurrencyInput';
import KasbonFormModal from './KasbonFormModal';
import { StatusChip, SourceBadge, AnomalyChips, fmtTime } from './AttendanceDashboard';

const SERVER = 'http://localhost:5000';

const TYPE_LABELS = { allowance: 'Tunjangan', bonus: 'Bonus', deduction: 'Potongan' };
const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));

// Monthly projection: base + fixed allowances − fixed deductions (bonuses excluded — variable per period).
// Per-hari-hadir components carry a daily rate, so project them across the structure's expected working days.
function monthlyProjection(structure) {
  if (!structure) return 0;
  const workDays = Number(structure.working_days_per_month || 0);
  let total = Number(structure.base_salary || 0);
  for (const c of structure.components || []) {
    if (!c.component_is_fixed) continue;
    const perDay = c.component_calc_method === 'per_present_day';
    const value = Number(c.amount || 0) * (perDay ? workDays : 1);
    if (c.component_type === 'allowance') total += value;
    else if (c.component_type === 'deduction') total -= value;
  }
  return total;
}

function getUser() {
  try { return JSON.parse(localStorage.getItem('user') || '{}'); } catch { return {}; }
}
const canEdit = () => {
  const role = getUser()?.role;
  return role === 'admin' || role === 'manager';
};

const TABS = [
  { key: 'profil', label: 'Profil' },
  { key: 'gaji', label: 'Gaji' },
  { key: 'absensi', label: 'Absensi' },
  { key: 'kasbon', label: 'Kasbon' },
  { key: 'cuti', label: 'Cuti' },
];

const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '-';

const STATUS_LABELS = { active: 'Aktif', inactive: 'Nonaktif', resigned: 'Resign' };
const statusLabel = (s) => STATUS_LABELS[s] || 'Nonaktif';

// Whole-day difference between a contract end date and today (negative = overdue).
const DAY_MS = 86400000;
function contractDaysLeft(dateStr) {
  if (!dateStr) return null;
  const end = new Date(dateStr); end.setHours(0, 0, 0, 0);
  const today = new Date(); today.setHours(0, 0, 0, 0);
  return Math.round((end - today) / DAY_MS);
}

function Field({ label, value }) {
  return (
    <div style={{ marginBottom: '0.85rem' }}>
      <div style={{ fontSize: '0.75rem', color: '#8a93a6', textTransform: 'uppercase', letterSpacing: '0.03em' }}>{label}</div>
      <div style={{ fontWeight: 500 }}>{value || '-'}</div>
    </div>
  );
}

function perfScoreColor(score) {
  if (score >= 90) return { bg: '#e6f4ea', color: '#1e7e34' };
  if (score >= 70) return { bg: '#fff8e1', color: '#a06800' };
  return { bg: '#fce8e6', color: '#c5221f' };
}

const STATUS_PRINT_LABELS = { present: 'Hadir', absent: 'Absen', leave: 'Cuti', holiday: 'Libur' };

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
}

function anomalyText(r) {
  const parts = [];
  if (r.is_late) parts.push(`Terlambat ${r.late_minutes} mnt`);
  if (r.is_early_leave) parts.push('Pulang Awal');
  if (r.is_missing_checkout) parts.push('Tidak Absen Pulang');
  return parts.join(', ');
}

// Builds a self-contained printable HTML document for one employee's monthly
// attendance, opened in a new window so it renders free of the app's nav/layout.
function buildAttendancePrintHtml({ employee, month, rows, perf }) {
  const [y, m] = month.split('-').map(Number);
  const monthLabel = new Date(y, m - 1, 1).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' });
  const printedAt = new Date().toLocaleString('id-ID', { dateStyle: 'long', timeStyle: 'short' });

  const counts = { present: 0, absent: 0, leave: 0, holiday: 0 };
  rows.forEach(r => { if (counts[r.status] !== undefined) counts[r.status]++; });
  const lateCount = rows.filter(r => r.is_late).length;
  const score = perf?.score ?? 100;
  const violCount = perf?.violations?.length ?? 0;

  const sorted = [...rows].sort((a, b) => new Date(a.date) - new Date(b.date));
  const bodyRows = sorted.length === 0
    ? `<tr><td colspan="6" style="text-align:center;color:#888;padding:14px">Tidak ada catatan kehadiran pada bulan ini.</td></tr>`
    : sorted.map(r => {
        const d = new Date(r.date);
        const tgl = d.toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' });
        const hari = d.toLocaleDateString('id-ID', { weekday: 'long' });
        return `<tr>
          <td>${escapeHtml(tgl)}</td>
          <td>${escapeHtml(hari)}</td>
          <td class="c">${fmtTime(r.check_in) || '—'}</td>
          <td class="c">${fmtTime(r.check_out) || '—'}</td>
          <td class="c">${escapeHtml(STATUS_PRINT_LABELS[r.status] || r.status || '-')}</td>
          <td>${escapeHtml(anomalyText(r)) || ''}</td>
        </tr>`;
      }).join('');

  return `<!doctype html><html lang="id"><head><meta charset="utf-8">
<title>Kehadiran ${escapeHtml(employee?.full_name || '')} — ${escapeHtml(monthLabel)}</title>
<style>
  @page { size: A4; margin: 15mm; }
  * { box-sizing: border-box; }
  body { font-family: Arial, Helvetica, sans-serif; color: #1a1a2e; font-size: 12px; margin: 0; }
  h1 { font-size: 18px; margin: 0 0 2px; }
  .sub { color: #667; font-size: 12px; margin-bottom: 14px; }
  .info { display: grid; grid-template-columns: 1fr 1fr; gap: 2px 24px; margin-bottom: 12px; }
  .info div span { color: #667; display: inline-block; min-width: 120px; }
  .summary { display: flex; flex-wrap: wrap; gap: 8px; margin: 10px 0 14px; }
  .chip { border: 1px solid #d5dae3; border-radius: 4px; padding: 4px 10px; font-size: 11px; }
  .chip b { font-size: 13px; }
  table { width: 100%; border-collapse: collapse; }
  th, td { border: 1px solid #cfd5df; padding: 5px 8px; text-align: left; font-size: 11.5px; }
  th { background: #eef1f6; }
  td.c { text-align: center; }
  .sign { display: flex; justify-content: space-between; margin-top: 40px; }
  .sign div { text-align: center; width: 40%; }
  .sign .line { margin-top: 56px; border-top: 1px solid #333; padding-top: 4px; }
  .foot { margin-top: 18px; color: #999; font-size: 10px; }
  @media print { .noprint { display: none; } }
</style></head>
<body>
  <button class="noprint" onclick="window.print()" style="float:right;padding:8px 16px;cursor:pointer">Cetak</button>
  <h1>Laporan Kehadiran Bulanan</h1>
  <div class="sub">Periode: <b>${escapeHtml(monthLabel)}</b></div>
  <div class="info">
    <div><span>Nama</span> ${escapeHtml(employee?.full_name || '-')}</div>
    <div><span>Cabang</span> ${escapeHtml(employee?.branch_name || '-')}</div>
    <div><span>Kode Karyawan</span> ${escapeHtml(employee?.employee_code || '-')}</div>
    <div><span>Jabatan</span> ${escapeHtml(employee?.position_name || '-')}</div>
  </div>
  <div class="summary">
    <span class="chip">Hadir <b>${counts.present}</b></span>
    <span class="chip">Absen <b>${counts.absent}</b></span>
    <span class="chip">Cuti <b>${counts.leave}</b></span>
    <span class="chip">Libur <b>${counts.holiday}</b></span>
    <span class="chip">Terlambat <b>${lateCount}</b></span>
    <span class="chip">Skor Evaluasi <b>${score}/100</b> (${violCount} pelanggaran)</span>
  </div>
  <table>
    <thead><tr><th>Tanggal</th><th>Hari</th><th>Masuk</th><th>Pulang</th><th>Status</th><th>Keterangan</th></tr></thead>
    <tbody>${bodyRows}</tbody>
  </table>
  <div class="sign">
    <div><div>Karyawan</div><div class="line">${escapeHtml(employee?.full_name || '')}</div></div>
    <div><div>Disetujui oleh</div><div class="line">(&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;)</div></div>
  </div>
  <div class="foot">Dicetak pada ${escapeHtml(printedAt)}</div>
</body></html>`;
}

function AttendanceTab({ employeeId, employee }) {
  const now = new Date();
  const [month, setMonth] = useState(`${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`);
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(false);
  const [perf, setPerf] = useState(null);

  useEffect(() => {
    const [y, m] = month.split('-').map(Number);
    const from = `${month}-01`;
    const to = new Date(y, m, 0).toISOString().slice(0, 10); // last day of month
    setLoading(true);
    getAttendance({ employee_id: employeeId, date_from: from, date_to: to })
      .then(r => setRows(r.data?.data || []))
      .catch(() => setRows([]))
      .finally(() => setLoading(false));
    getEmployeePerformance(employeeId, { month })
      .then(r => setPerf(r.data))
      .catch(() => setPerf(null));
  }, [employeeId, month]);

  const score = perf?.score ?? 100;
  const violCount = perf?.violations?.length ?? 0;
  const sc = perfScoreColor(score);

  const handlePrint = () => {
    const html = buildAttendancePrintHtml({ employee, month, rows, perf });
    const win = window.open('', '_blank');
    if (!win) return; // popup blocked
    win.document.write(html);
    win.document.close();
    win.focus();
    // Give the new document a tick to lay out before invoking print.
    setTimeout(() => { try { win.print(); } catch { /* user can use the Cetak button */ } }, 300);
  };

  return (
    <div className="card">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem', gap: '0.5rem', flexWrap: 'wrap' }}>
        <h3 style={{ margin: 0, fontSize: '1rem' }}>Kehadiran Bulanan</h3>
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <input type="month" value={month} onChange={e => setMonth(e.target.value)} />
          <button onClick={handlePrint} disabled={loading} className="btn btn-secondary btn-sm" title="Cetak kehadiran bulan ini">🖨️ Cetak</button>
        </div>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', padding: '0.85rem 1rem', borderRadius: '6px', background: sc.bg, marginBottom: '1rem' }}>
        <div>
          <div style={{ fontSize: '0.72rem', color: '#667', textTransform: 'uppercase', letterSpacing: '0.03em' }}>Skor Evaluasi Bulan Ini</div>
          <div style={{ fontSize: '1.6rem', fontWeight: 700, color: sc.color }}>{score}<span style={{ fontSize: '0.85rem', color: '#999' }}> / 100</span></div>
        </div>
        <div style={{ fontSize: '0.85rem', color: '#667' }}>{violCount} pelanggaran</div>
        <Link to="/hr/performance" style={{ marginLeft: 'auto', fontSize: '0.85rem' }}>Lihat rincian evaluasi →</Link>
      </div>
      {loading ? (
        <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>
      ) : rows.length === 0 ? (
        <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Tidak ada catatan kehadiran pada bulan ini.</p>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table>
            <thead>
              <tr><th>Tanggal</th><th>Masuk</th><th>Sumber</th><th>Pulang</th><th>Sumber</th><th>Status</th><th>Anomali</th></tr>
            </thead>
            <tbody>
              {rows.map(r => (
                <tr key={r.id}>
                  <td>{new Date(r.date).toLocaleDateString('id-ID', { weekday: 'short', day: '2-digit', month: 'short' })}</td>
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
        </div>
      )}
    </div>
  );
}

const LEAVE_STATUS_LABELS = { pending: 'Menunggu', approved: 'Disetujui', rejected: 'Ditolak', cancelled: 'Dibatalkan' };
const LEAVE_STATUS_COLORS = {
  pending: { bg: '#fff8e1', color: '#a06800' },
  approved: { bg: '#e6f4ea', color: '#1e7e34' },
  rejected: { bg: '#fce8e6', color: '#c5221f' },
  cancelled: { bg: '#eef1f6', color: '#667' },
};
function LeaveStatusChip({ status }) {
  const c = LEAVE_STATUS_COLORS[status] || LEAVE_STATUS_COLORS.cancelled;
  return <span style={{ background: c.bg, color: c.color, padding: '0.15rem 0.55rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.8rem' }}>{LEAVE_STATUS_LABELS[status] || status}</span>;
}

function CutiTab({ employeeId }) {
  const year = new Date().getFullYear();
  const [balance, setBalance] = useState(null);
  const [requests, setRequests] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    Promise.all([
      getLeaveBalance(employeeId, year).then(r => setBalance(r.data)).catch(() => setBalance(null)),
      getEmployeeLeaveRequests(employeeId).then(r => setRequests(r.data || [])).catch(() => setRequests([])),
    ]).finally(() => setLoading(false));
  }, [employeeId, year]);

  const quota = balance?.quota_days ?? 0;
  const used = balance?.used_days ?? 0;
  const remaining = quota - used;

  return (
    <div>
      <div className="card" style={{ marginBottom: '1rem' }}>
        <h3 style={{ margin: '0 0 0.75rem', fontSize: '1rem' }}>Saldo Cuti Tahunan {year}</h3>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '1rem' }}>
          <BalanceStat label="Kuota" value={quota} color="#2563eb" />
          <BalanceStat label="Terpakai" value={used} color="#a06800" />
          <BalanceStat label="Sisa" value={remaining} color={remaining > 0 ? '#1e7e34' : '#c5221f'} />
        </div>
      </div>

      <div className="card">
        <h3 style={{ margin: '0 0 0.75rem', fontSize: '1rem' }}>Riwayat Pengajuan Cuti</h3>
        {loading ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>
        ) : requests.length === 0 ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Belum ada pengajuan cuti.</p>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%' }}>
              <thead>
                <tr><th>Jenis</th><th>Mulai</th><th>Selesai</th><th style={{ textAlign: 'center' }}>Hari Kerja</th><th>Status</th><th>Alasan</th></tr>
              </thead>
              <tbody>
                {requests.map(rq => (
                  <tr key={rq.id}>
                    <td>{rq.leave_type_name}{!rq.is_paid && <span style={{ fontSize: '0.72rem', color: '#c5221f' }}> (tanpa gaji)</span>}</td>
                    <td>{fmtDateShort(rq.start_date)}</td>
                    <td>{fmtDateShort(rq.end_date)}</td>
                    <td style={{ textAlign: 'center' }}>{rq.day_count}</td>
                    <td><LeaveStatusChip status={rq.status} /></td>
                    <td style={{ fontSize: '0.85rem', color: '#667' }}>{rq.reason || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

function BalanceStat({ label, value, color }) {
  return (
    <div style={{ textAlign: 'center', padding: '0.85rem', background: '#fafbfc', borderRadius: '6px' }}>
      <div style={{ fontSize: '0.72rem', color: '#667', textTransform: 'uppercase', letterSpacing: '0.03em' }}>{label}</div>
      <div style={{ fontSize: '1.8rem', fontWeight: 700, color }}>{value}<span style={{ fontSize: '0.8rem', color: '#999' }}> hari</span></div>
    </div>
  );
}

const KASBON_STATUS_LABELS = {
  pending: 'Menunggu', approved: 'Disetujui', rejected: 'Ditolak',
  processed: 'Diproses', resolved: 'Lunas', cancelled: 'Dibatalkan',
};
const KASBON_STATUS_COLORS = {
  pending: { bg: '#fff8e1', color: '#a06800' },
  approved: { bg: '#e8f0fe', color: '#1967d2' },
  rejected: { bg: '#fce8e6', color: '#c5221f' },
  processed: { bg: '#e6f4ea', color: '#1e7e34' },
  resolved: { bg: '#eef1f6', color: '#445' },
  cancelled: { bg: '#eef1f6', color: '#667' },
};
function KasbonStatusChip({ status }) {
  const c = KASBON_STATUS_COLORS[status] || KASBON_STATUS_COLORS.cancelled;
  return <span style={{ background: c.bg, color: c.color, padding: '0.15rem 0.55rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.8rem' }}>{KASBON_STATUS_LABELS[status] || status}</span>;
}

function KasbonTab({ employeeId }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    getEmployeeKasbons(employeeId)
      .then(r => setRows(r.data || []))
      .catch(() => setRows([]))
      .finally(() => setLoading(false));
  }, [employeeId]);

  useEffect(() => { load(); }, [load]);

  // Outstanding balance = sum of amounts for kasbons that are approved/processed
  // (disbursed but not yet fully resolved).
  const outstanding = rows
    .filter(k => k.status === 'approved' || k.status === 'processed')
    .reduce((s, k) => s + Number(k.amount || 0), 0);

  return (
    <div>
      <div className="card" style={{ marginBottom: '1rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <div style={{ fontSize: '0.72rem', color: '#667', textTransform: 'uppercase', letterSpacing: '0.03em' }}>Saldo Kasbon Berjalan</div>
          <div style={{ fontSize: '1.6rem', fontWeight: 700, color: outstanding > 0 ? '#c5221f' : '#1e7e34' }}>{fmtIDR(outstanding)}</div>
        </div>
        <button onClick={() => setShowModal(true)} className="btn btn-primary btn-sm">+ Pengajuan Kasbon</button>
      </div>

      <div className="card">
        <h3 style={{ margin: '0 0 0.75rem', fontSize: '1rem' }}>Riwayat Kasbon</h3>
        {loading ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>
        ) : rows.length === 0 ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Belum ada kasbon.</p>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%' }}>
              <thead>
                <tr><th>Nomor</th><th style={{ textAlign: 'right' }}>Jumlah</th><th>Penyelesaian</th><th>Status</th></tr>
              </thead>
              <tbody>
                {rows.map(k => (
                  <tr key={k.id}>
                    <td><Link to={`/hr/kasbon/${k.id}`}>{k.kasbon_number}</Link></td>
                    <td style={{ textAlign: 'right' }}>{fmtIDR(k.amount)}</td>
                    <td>{k.resolution_month ? new Date(k.resolution_month).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' }) : '-'}</td>
                    <td><KasbonStatusChip status={k.status} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {showModal && <KasbonFormModal presetEmployeeId={employeeId} onClose={() => setShowModal(false)} onSaved={() => { setShowModal(false); load(); }} />}
    </div>
  );
}

function fmtDateShort(d) { return d ? new Date(d).toLocaleDateString('id-ID') : '-'; }

function ComponentRows({ components }) {
  if (!components || components.length === 0) {
    return <div style={{ color: '#999', fontSize: '0.85rem' }}>Tidak ada komponen.</div>;
  }
  return (
    <table style={{ width: '100%' }}>
      <thead>
        <tr><th>Komponen</th><th>Tipe</th><th>Sifat</th><th style={{ textAlign: 'right' }}>Nominal</th></tr>
      </thead>
      <tbody>
        {components.map(c => (
          <tr key={c.id}>
            <td>
              {c.component_name}
              {c.component_min_score != null && <span style={{ color: '#8a93a6', fontSize: '0.8rem' }}> · syarat skor ≥ {c.component_min_score}</span>}
            </td>
            <td>{TYPE_LABELS[c.component_type] || c.component_type}</td>
            <td>{c.component_is_fixed ? 'Tetap' : 'Variabel'}</td>
            <td style={{ textAlign: 'right' }}>
              {fmtIDR(c.amount)}
              {c.component_calc_method === 'per_present_day' && <span style={{ color: '#8a93a6' }}> /hari</span>}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function WageTab({ employeeId, editable, isContract }) {
  const [current, setCurrent] = useState(null);
  const [history, setHistory] = useState([]);
  const [catalog, setCatalog] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [expanded, setExpanded] = useState({});
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const today = new Date().toISOString().slice(0, 10);
  const [form, setForm] = useState({ base_salary: '', working_days_per_month: '26', effective_date: today });
  const [picked, setPicked] = useState([]); // [{ component_id, amount }]

  const load = () => {
    setLoading(true);
    Promise.all([
      getEmployeeWage(employeeId).then(r => r.data).catch(() => null),
      getEmployeeWageHistory(employeeId).then(r => r.data || []).catch(() => []),
    ]).then(([cur, hist]) => {
      setCurrent(cur);
      setHistory(hist);
    }).finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [employeeId]);

  const openForm = () => {
    setError('');
    getWageComponents({ active: 1 }).then(r => setCatalog(r.data || [])).catch(() => setCatalog([]));
    // Prefill from current open version for convenience.
    if (current) {
      setForm({
        base_salary: String(current.base_salary || ''),
        working_days_per_month: String(current.working_days_per_month || '26'),
        effective_date: today,
      });
      setPicked((current.components || []).map(c => ({ component_id: c.wage_component_id, amount: String(c.amount) })));
    } else {
      setForm({ base_salary: '', working_days_per_month: '26', effective_date: today });
      setPicked([]);
    }
    setShowForm(true);
  };

  const dailyRatePreview = () => {
    const base = Number(form.base_salary || 0);
    const wd = Number(form.working_days_per_month || 0);
    if (!wd || wd < 1) return 0;
    return Math.round(base / wd);
  };

  const addComponent = (id) => {
    if (!id || picked.some(p => p.component_id === id)) return;
    setPicked(p => [...p, { component_id: id, amount: '' }]);
  };
  const removeComponent = (id) => setPicked(p => p.filter(x => x.component_id !== id));
  const setComponentAmount = (id, amount) => setPicked(p => p.map(x => x.component_id === id ? { ...x, amount } : x));

  const componentMeta = (id) => catalog.find(c => c.id === id) || {};

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (form.base_salary === '' || Number(form.base_salary) < 0) { setError('Gaji pokok wajib diisi'); return; }
    const wd = Number(form.working_days_per_month);
    if (!wd || wd < 1 || wd > 31) { setError('Hari kerja per bulan harus antara 1 dan 31'); return; }
    if (!form.effective_date) { setError('Tanggal berlaku wajib diisi'); return; }
    for (const p of picked) {
      if (p.amount === '' || Number(p.amount) < 0) { setError('Nominal komponen tidak boleh kosong/negatif'); return; }
    }
    setSubmitting(true);
    try {
      await createEmployeeWage(employeeId, {
        base_salary: Math.round(Number(form.base_salary)),
        working_days_per_month: wd,
        effective_date: form.effective_date,
        components: picked.map(p => ({ component_id: p.component_id, amount: Math.round(Number(p.amount)) })),
      });
      setShowForm(false);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan struktur gaji');
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) return <div style={{ color: '#999', padding: '2rem' }}>Memuat struktur gaji...</div>;

  const projection = monthlyProjection(current);

  return (
    <div style={{ display: 'grid', gap: '1rem' }}>
      {/* Current structure card */}
      <div className="card">
        <div className="card-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <h2>Struktur Gaji Saat Ini</h2>
          {editable && <button onClick={openForm} className="btn btn-primary btn-sm">Ubah Struktur Gaji</button>}
        </div>
        {!current ? (
          <div style={{ color: '#999', padding: '1rem 0' }}>Belum ada struktur gaji untuk karyawan ini.</div>
        ) : (
          <>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', gap: '0.5rem 1.5rem', marginBottom: '1rem' }}>
              <Field label="Gaji Pokok" value={fmtIDR(current.base_salary)} />
              <Field label="Hari Kerja / Bulan" value={current.working_days_per_month} />
              <Field label="Tarif Harian" value={fmtIDR(current.daily_rate)} />
              <Field label="Berlaku Sejak" value={fmtDateShort(current.effective_date)} />
            </div>
            <h3 style={{ fontSize: '0.95rem', margin: '0 0 0.5rem' }}>Komponen</h3>
            <ComponentRows components={current.components} />
            <div style={{ marginTop: '1rem', paddingTop: '0.75rem', borderTop: '1px solid #e8e8e8', display: 'flex', justifyContent: 'space-between', fontWeight: 600 }}>
              <span>Proyeksi Gaji Bulanan (pokok + tunjangan tetap − potongan tetap)</span>
              <span>{fmtIDR(projection)}</span>
            </div>
            {current.thr ? (
              <div style={{ marginTop: '0.75rem', padding: '0.75rem 1rem', background: '#f5f9ff', border: '1px solid #d9e6fb', borderRadius: 8 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontWeight: 600 }}>
                  <span>THR (Tunjangan Hari Raya)</span>
                  <span style={{ color: '#1967d2' }}>{fmtIDR(current.thr.amount)}</span>
                </div>
                <div style={{ fontSize: '0.78rem', color: '#667', marginTop: 4 }}>
                  Masa kerja {current.thr.months_worked} bulan → {current.thr.months_worked >= 12 ? '1 bulan penuh' : `${current.thr.months_worked}/12`} × gaji pokok
                  {' '}(estimasi per {fmtDateShort(current.thr.as_of)}). Dibayarkan melalui run THR.
                  {current.thr.transitioned && (
                    <span> Dihitung sejak tanggal status tetap ({fmtDateShort(current.thr.tenure_since)}).</span>
                  )}
                </div>
              </div>
            ) : isContract && (
              <div style={{ marginTop: '0.75rem', padding: '0.75rem 1rem', background: '#f6f7fa', border: '1px solid #e6e8ee', borderRadius: 8, fontSize: '0.82rem', color: '#667' }}>
                <strong>THR (Tunjangan Hari Raya):</strong> Karyawan kontrak (PKWT) tidak menerima THR.
              </div>
            )}
          </>
        )}
      </div>

      {/* History */}
      <div className="card">
        <div className="card-header"><h2>Riwayat Struktur Gaji</h2></div>
        {history.length === 0 ? (
          <div style={{ color: '#999', padding: '0.5rem 0' }}>Belum ada riwayat.</div>
        ) : (
          <table style={{ width: '100%' }}>
            <thead>
              <tr><th>Berlaku</th><th>Berakhir</th><th style={{ textAlign: 'right' }}>Gaji Pokok</th><th style={{ textAlign: 'right' }}>Tarif Harian</th><th></th></tr>
            </thead>
            <tbody>
              {history.map(v => (
                <Fragment key={v.id}>
                  <tr>
                    <td>{fmtDateShort(v.effective_date)}</td>
                    <td>{v.end_date ? fmtDateShort(v.end_date) : <span className="badge" style={{ background: '#e6f4ea', color: '#1e7e34' }}>Aktif</span>}</td>
                    <td style={{ textAlign: 'right' }}>{fmtIDR(v.base_salary)}</td>
                    <td style={{ textAlign: 'right' }}>{fmtIDR(v.daily_rate)}</td>
                    <td style={{ textAlign: 'right' }}>
                      <button className="btn btn-secondary btn-sm" onClick={() => setExpanded(e => ({ ...e, [v.id]: !e[v.id] }))}>
                        {expanded[v.id] ? 'Tutup' : 'Detail'}
                      </button>
                    </td>
                  </tr>
                  {expanded[v.id] && (
                    <tr>
                      <td colSpan={5} style={{ background: '#f8f9fb' }}>
                        <div style={{ padding: '0.5rem' }}>
                          <ComponentRows components={v.components} />
                        </div>
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* New version form modal */}
      {showForm && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'flex-start', justifyContent: 'center', zIndex: 1000, overflowY: 'auto', padding: '2rem 1rem' }}>
          <div className="card" style={{ width: '100%', maxWidth: 560, padding: '2rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
              <h2 style={{ margin: 0, fontSize: '1.05rem' }}>Ubah Struktur Gaji</h2>
              <button onClick={() => setShowForm(false)} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
            </div>
            <p style={{ color: '#667', fontSize: '0.85rem', marginTop: 0 }}>
              Menyimpan akan membuat <strong>versi baru</strong>. Versi sebelumnya tetap tersimpan sebagai riwayat (tidak diubah).
            </p>
            {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
            <form onSubmit={handleSubmit}>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
                <div className="form-group">
                  <label>Gaji Pokok (Rp)</label>
                  <CurrencyInput value={form.base_salary} onChange={e => setForm(f => ({ ...f, base_salary: e.target.value }))} />
                </div>
                <div className="form-group">
                  <label>Hari Kerja / Bulan</label>
                  <input type="number" min="1" max="31" value={form.working_days_per_month} onChange={e => setForm(f => ({ ...f, working_days_per_month: e.target.value }))} />
                </div>
                <div className="form-group">
                  <label>Tanggal Berlaku</label>
                  <input type="date" value={form.effective_date} onChange={e => setForm(f => ({ ...f, effective_date: e.target.value }))} />
                </div>
                <div className="form-group">
                  <label>Tarif Harian (otomatis)</label>
                  <input value={fmtIDR(dailyRatePreview())} readOnly style={{ background: '#f3f4f6' }} />
                </div>
              </div>

              <h3 style={{ fontSize: '0.95rem', margin: '0.5rem 0' }}>Komponen</h3>
              <div className="form-group" style={{ marginBottom: '0.75rem' }}>
                <select value="" onChange={e => { addComponent(e.target.value); e.target.value = ''; }}>
                  <option value="">+ Tambah komponen dari katalog aktif...</option>
                  {catalog.filter(c => !picked.some(p => p.component_id === c.id)).map(c => (
                    <option key={c.id} value={c.id}>{c.name} — {TYPE_LABELS[c.type]} ({c.is_fixed ? 'Tetap' : 'Variabel'}){c.calc_method === 'per_present_day' ? ' · per hari hadir' : ''}</option>
                  ))}
                </select>
              </div>

              {picked.length === 0 ? (
                <div style={{ color: '#999', fontSize: '0.85rem', marginBottom: '0.75rem' }}>Belum ada komponen dipilih.</div>
              ) : (
                <table style={{ width: '100%', marginBottom: '0.75rem' }}>
                  <thead>
                    <tr><th>Komponen</th><th>Tipe</th><th style={{ textAlign: 'right' }}>Nominal (Rp)</th><th></th></tr>
                  </thead>
                  <tbody>
                    {picked.map(p => {
                      const meta = componentMeta(p.component_id);
                      const perDay = meta.calc_method === 'per_present_day';
                      return (
                        <tr key={p.component_id}>
                          <td>{meta.name || '-'}{perDay && <span style={{ color: '#8a93a6', fontSize: '0.8rem' }}> · per hari hadir</span>}</td>
                          <td>{TYPE_LABELS[meta.type] || '-'}</td>
                          <td>
                            <CurrencyInput value={p.amount} onChange={e => setComponentAmount(p.component_id, e.target.value)} />
                            {perDay && <div style={{ fontSize: '0.72rem', color: '#8a93a6' }}>tarif per hari hadir</div>}
                          </td>
                          <td style={{ textAlign: 'right' }}>
                            <button type="button" className="btn btn-danger btn-sm" onClick={() => removeComponent(p.component_id)}>Hapus</button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              )}

              <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1rem' }}>
                <button type="submit" className="btn btn-primary" disabled={submitting} style={{ flex: 1, justifyContent: 'center' }}>
                  {submitting ? 'Menyimpan...' : 'Simpan Versi Baru'}
                </button>
                <button type="button" onClick={() => setShowForm(false)} className="btn btn-secondary">Batal</button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

export default function EmployeeDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [emp, setEmp]   = useState(null);
  const [tab, setTab]   = useState('profil');
  const [error, setError] = useState('');
  const [showTransition, setShowTransition] = useState(false);
  const [showResign, setShowResign] = useState(false);

  const editable = canEdit();

  const loadEmployee = useCallback(() => {
    getEmployee(id)
      .then(r => setEmp(r.data))
      .catch(() => setError('Karyawan tidak ditemukan'));
  }, [id]);

  useEffect(() => { loadEmployee(); }, [loadEmployee]);

  const handleDelete = async () => {
    if (!confirm('Yakin hapus karyawan ini? Sebaiknya ubah status menjadi nonaktif jika sudah ada data terkait.')) return;
    try {
      await deleteEmployee(id);
      navigate('/hr/employees');
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus karyawan');
    }
  };

  if (error) return <div className="error-msg">{error}</div>;
  if (!emp) return <div style={{ color: '#999', padding: '2rem' }}>Memuat...</div>;

  return (
    <>
      <div className="page-header">
        <h1>{emp.full_name}</h1>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <button onClick={() => navigate('/hr/employees')} className="btn btn-secondary">Kembali</button>
          {editable && emp.employment_type === 'contract' && (
            <button onClick={() => setShowTransition(true)} className="btn btn-primary" style={{ background: '#1e7e34', borderColor: '#1e7e34' }}>
              Jadikan Karyawan Tetap
            </button>
          )}
          {editable && emp.status !== 'resigned' && (
            <button onClick={() => setShowResign(true)} className="btn btn-primary" style={{ background: '#7b2cbf', borderColor: '#7b2cbf' }}>
              Tandai Resign
            </button>
          )}
          {editable && <Link to={`/hr/employees/${id}/edit`} className="btn btn-primary">Edit</Link>}
          {editable && <button onClick={handleDelete} className="btn btn-danger">Hapus</button>}
        </div>
      </div>

      <div style={{ display: 'flex', gap: '0.5rem', borderBottom: '1px solid #e8e8e8', marginBottom: '1rem', flexWrap: 'wrap' }}>
        {TABS.map(t => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={tab === t.key ? 'btn btn-primary btn-sm' : 'btn btn-secondary btn-sm'}
            style={{ borderRadius: '6px 6px 0 0' }}
          >
            {t.label}
          </button>
        ))}
      </div>

      {emp.status === 'resigned' && (
        <div className="card" style={{ marginBottom: '1rem', borderLeft: '4px solid #7b2cbf', background: '#faf5ff' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span style={{ fontSize: '1.1rem' }}>🚪</span>
            <strong style={{ color: '#7b2cbf' }}>
              Karyawan telah resign{emp.resign_date ? ` per ${fmtDate(emp.resign_date)}` : ''}.
            </strong>
          </div>
        </div>
      )}

      {(() => {
        if (emp.employment_type !== 'contract' || !emp.contract_end_date) return null;
        const days = contractDaysLeft(emp.contract_end_date);
        if (days == null || days > 30) return null;
        const overdue = days < 0;
        const msg = overdue
          ? `Kontrak telah berakhir ${Math.abs(days)} hari lalu (${fmtDate(emp.contract_end_date)}).`
          : days === 0
            ? `Kontrak berakhir hari ini (${fmtDate(emp.contract_end_date)}).`
            : `Kontrak akan berakhir dalam ${days} hari (${fmtDate(emp.contract_end_date)}).`;
        return (
          <div
            className="card"
            style={{ marginBottom: '1rem', borderLeft: `4px solid ${overdue ? '#c5221f' : '#f0a020'}`, background: overdue ? '#fdece9' : '#fffaf0' }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <span style={{ fontSize: '1.1rem' }}>{overdue ? '⛔' : '⚠️'}</span>
              <strong style={{ color: overdue ? '#c5221f' : '#a06800' }}>{msg}</strong>
              {editable && <Link to={`/hr/employees/${id}/edit`} style={{ marginLeft: 'auto', fontSize: '0.85rem' }}>Perpanjang / Ubah →</Link>}
            </div>
          </div>
        );
      })()}

      {tab === 'profil' && (
        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 2fr) minmax(0, 1fr)', gap: '1rem', alignItems: 'start' }}>
          <div>
            <div className="card" style={{ marginBottom: '1rem' }}>
              <div className="card-header"><h2>Identitas</h2></div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.5rem 1.5rem' }}>
                <Field label="Kode Karyawan" value={emp.employee_code} />
                <Field label="Nama Lengkap" value={emp.full_name} />
                <Field label="Jabatan" value={emp.position_name} />
                <Field label="Cabang" value={emp.branch_name} />
                <Field label="Tanggal Lahir" value={fmtDate(emp.dob)} />
                <Field label="Tanggal Bergabung" value={fmtDate(emp.join_date)} />
                <Field label="NIK / KTP" value={emp.national_id} />
                <Field label="Status" value={statusLabel(emp.status)} />
                {emp.status === 'resigned' && emp.resign_date && (
                  <Field label="Tanggal Resign" value={fmtDate(emp.resign_date)} />
                )}
                <Field label="Tipe Kepegawaian" value={emp.employment_type === 'contract' ? 'Kontrak' : 'Tetap'} />
                {emp.employment_type === 'contract' && (
                  <Field label="Berakhir Kontrak" value={fmtDate(emp.contract_end_date)} />
                )}
                {emp.employment_type === 'permanent' && emp.permanent_since && (
                  <Field label="Tetap Sejak" value={fmtDate(emp.permanent_since)} />
                )}
              </div>
            </div>

            <div className="card" style={{ marginBottom: '1rem' }}>
              <div className="card-header"><h2>Kontak</h2></div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.5rem 1.5rem' }}>
                <Field label="Telepon" value={emp.phone} />
                <Field label="Email" value={emp.email} />
                <Field label="Alamat" value={emp.address} />
              </div>
            </div>

            <div className="card">
              <div className="card-header"><h2>Rekening Bank</h2></div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.5rem 1.5rem' }}>
                <Field label="Nama Bank" value={emp.bank_name} />
                <Field label="Nomor Rekening" value={emp.bank_account_number} />
                <Field label="Atas Nama" value={emp.bank_account_holder} />
              </div>
            </div>
          </div>

          <div className="card" style={{ textAlign: 'center' }}>
            {emp.photo_path ? (
              <img src={`${SERVER}/uploads/${emp.photo_path}`} alt={emp.full_name} style={{ width: '100%', maxWidth: 220, borderRadius: '8px', objectFit: 'cover', border: '1px solid #e8e8e8' }} />
            ) : (
              <div style={{ width: '100%', height: 200, borderRadius: '8px', background: '#eef1f6', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#8a93a6', fontSize: '3rem' }}>
                {(emp.full_name || '?').charAt(0).toUpperCase()}
              </div>
            )}
          </div>
        </div>
      )}

      {tab === 'gaji' && <WageTab employeeId={id} editable={editable} isContract={emp.employment_type === 'contract'} />}
      {tab === 'absensi' && <AttendanceTab employeeId={id} employee={emp} />}
      {tab === 'kasbon' && <KasbonTab employeeId={id} />}
      {tab === 'cuti' && <CutiTab employeeId={id} />}

      {showTransition && (
        <TransitionPermanentModal
          employee={emp}
          onClose={() => setShowTransition(false)}
          onDone={() => { setShowTransition(false); loadEmployee(); }}
        />
      )}

      {showResign && (
        <ResignModal
          employee={emp}
          onClose={() => setShowResign(false)}
          onDone={() => { setShowResign(false); loadEmployee(); }}
        />
      )}
    </>
  );
}

function ResignModal({ employee, onClose, onDone }) {
  const today = new Date().toISOString().slice(0, 10);
  const [date, setDate] = useState(today);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e) => {
    e.preventDefault();
    if (!date) { setError('Tanggal wajib diisi'); return; }
    setBusy(true); setError('');
    try {
      await resignEmployee(employee.id, { resign_date: date });
      onDone();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menandai resign');
      setBusy(false);
    }
  };

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'flex-start', justifyContent: 'center', zIndex: 1000, overflowY: 'auto', padding: '3rem 1rem' }}>
      <div className="card" style={{ width: '100%', maxWidth: 460, padding: '2rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
          <h2 style={{ margin: 0, fontSize: '1.1rem' }}>Tandai Resign</h2>
          <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
        </div>
        <p style={{ color: '#667', fontSize: '0.85rem', marginTop: 0 }}>
          Menandai <strong>{employee.full_name}</strong> sebagai karyawan yang mengundurkan diri (resign).
          Data karyawan dan riwayat HR tetap tersimpan; karyawan tidak lagi berstatus aktif.
        </p>
        {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
        <form onSubmit={submit}>
          <div className="form-group">
            <label>Tanggal Resign</label>
            <input type="date" value={date} onChange={e => setDate(e.target.value)} required />
          </div>
          <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', marginTop: '1rem' }}>
            <button type="button" onClick={onClose} className="btn btn-secondary" disabled={busy}>Batal</button>
            <button type="submit" className="btn btn-primary" disabled={busy} style={{ background: '#7b2cbf', borderColor: '#7b2cbf' }}>
              {busy ? 'Menyimpan…' : 'Tandai Resign'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function TransitionPermanentModal({ employee, onClose, onDone }) {
  const today = new Date().toISOString().slice(0, 10);
  const [date, setDate] = useState(today);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e) => {
    e.preventDefault();
    if (!date) { setError('Tanggal wajib diisi'); return; }
    setBusy(true); setError('');
    try {
      await transitionEmployeeToPermanent(employee.id, { effective_date: date });
      onDone();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal mengubah status karyawan');
      setBusy(false);
    }
  };

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'flex-start', justifyContent: 'center', zIndex: 1000, overflowY: 'auto', padding: '3rem 1rem' }}>
      <div className="card" style={{ width: '100%', maxWidth: 460, padding: '2rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
          <h2 style={{ margin: 0, fontSize: '1.1rem' }}>Jadikan Karyawan Tetap</h2>
          <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
        </div>
        <p style={{ color: '#667', fontSize: '0.85rem', marginTop: 0 }}>
          Mengubah <strong>{employee.full_name}</strong> dari karyawan kontrak (PKWT) menjadi karyawan tetap (PKWTT).
          Tanggal status tetap menjadi titik awal (hari ke-0) perhitungan masa kerja THR.
        </p>
        {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
        <form onSubmit={submit}>
          <div className="form-group">
            <label>Tanggal Status Tetap</label>
            <input type="date" value={date} onChange={e => setDate(e.target.value)} required />
          </div>
          <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', marginTop: '1rem' }}>
            <button type="button" onClick={onClose} className="btn btn-secondary" disabled={busy}>Batal</button>
            <button type="submit" className="btn btn-primary" disabled={busy} style={{ background: '#1e7e34', borderColor: '#1e7e34' }}>
              {busy ? 'Menyimpan…' : 'Ubah Status'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
