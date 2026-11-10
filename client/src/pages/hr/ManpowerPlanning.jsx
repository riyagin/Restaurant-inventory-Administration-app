import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getManpowerPlanning, getLeaveTypes, getBranches } from '../../api';
import { canApprove } from '../../roles';
import { todayLocal, fmtMonth, submitLeaveRequest, WEEKEND_COST } from './leaveRules';
import { QuotaPreview, BackdatedNotice } from './LeaveRuleWidgets';
import { ShiftCellModal, ShiftProfileModal, ShiftSettingsModal, ShiftCoverageView } from './ShiftPlanning';
import { groupByHomeDivision, shiftTimeLabel, shiftHoursLabel } from './shiftRules';

import { fmtDateTime } from '../../time';
const DAY_NAMES = ['Min', 'Sen', 'Sel', 'Rab', 'Kam', 'Jum', 'Sab'];

const shortMonth = (ym) => fmtMonth(ym).split(' ')[0];

// "Sisa libur Okt: 2/4" for each month the view touches.
function leaveBalanceLabel(emp) {
  return (emp.leave_balance || [])
    .map(b => `Sisa libur ${shortMonth(b.month)}: ${b.quota_days - b.used_days}/${b.quota_days}`)
    .join(' · ');
}

function formatDateHeader(dateStr) {
  const d = new Date(dateStr + 'T00:00:00');
  return `${DAY_NAMES[d.getDay()]}, ${d.getDate()}/${d.getMonth() + 1}`;
}

function formatDateHeaderCompact(dateStr) {
  const d = new Date(dateStr + 'T00:00:00');
  return `${d.getDate()}/${d.getMonth() + 1}`;
}

function formatDateLabel(dateStr) {
  const d = new Date(dateStr + 'T00:00:00');
  return d.toLocaleDateString('id-ID', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
}

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
}

// Builds a self-contained printable HTML document for the manpower plan, opened
// in a new window so it renders free of the app's nav/layout. One table per
// branch, employees sectioned by division, one column per day; a cell shows the
// rostered shift code in the shift's colour, or the leave status (colors print
// via -webkit-print-color-adjust). Each branch ends with a headcount per shift.
function buildManpowerPrintHtml({ data, days }) {
  const printedAt = fmtDateTime(new Date(), { dateStyle: 'long', timeStyle: 'short' });
  const periodLabel = `${formatDateLabel(data.dates[0])} s/d ${formatDateLabel(data.dates[data.dates.length - 1])}`;
  const totalEmployees = data.branches.reduce((s, b) => s + b.employees.length, 0);
  const colspan = data.dates.length + 1;

  const headCols = data.dates.map(d => {
    const dd = new Date(d + 'T00:00:00');
    const weekend = dd.getDay() === 0 || dd.getDay() === 6;
    return `<th class="day${weekend ? ' we' : ''}">${dd.getDate()}/${dd.getMonth() + 1}</th>`;
  }).join('');

  // Full month: give the name column more room; shorter ranges: keep it tight.
  const nameWidth = days >= 31 ? 130 : 90;

  const branchTables = data.branches.map(branch => {
    const shiftsById = Object.fromEntries(branch.shifts.map(s => [s.id, s]));
    const onLeave = branch.employees.reduce((s, e) => s + e.days.filter(d => d.status === 'cuti').length, 0);
    const pending = branch.employees.reduce((s, e) => s + e.days.filter(d => d.status === 'pending').length, 0);
    const empRow = (emp) => {
      const cells = emp.days.map(day => {
        if (day.status === 'cuti') {
          return `<td class="c cuti" title="${escapeHtml(day.leave_type || 'Cuti')}">${escapeHtml((day.leave_type || 'Cuti').slice(0, 8))}</td>`;
        }
        const s = shiftsById[day.shift_id];
        if (s) {
          return `<td class="c" style="background:${escapeHtml(s.color)}">${escapeHtml(s.code)}${day.status === 'pending' ? '<sup>M</sup>' : ''}</td>`;
        }
        if (day.status === 'pending') {
          return `<td class="c pending" title="${escapeHtml(day.leave_type || 'Cuti')} (menunggu)">M</td>`;
        }
        return `<td class="c none">–</td>`;
      }).join('');
      return `<tr>
        <td class="emp"><div class="en">${escapeHtml(emp.name)}${emp.is_delivery_staff ? ' <span class="do">DO</span>' : ''}</div>${emp.position ? `<div class="ep">${escapeHtml(emp.position)}</div>` : ''}<div class="ep">${escapeHtml(leaveBalanceLabel(emp))}</div></td>
        ${cells}
      </tr>`;
    };
    const rows = groupByHomeDivision(branch).map(sec =>
      `<tr class="sec"><td colspan="${colspan}">${escapeHtml(sec.name)} (${sec.employees.length})</td></tr>` +
      sec.employees.map(empRow).join('')
    ).join('');
    const countRows = branch.shifts.filter(s => s.is_active).map(s => {
      const cells = data.dates.map((_, i) =>
        `<td class="c">${branch.employees.filter(e => e.days[i].shift_id === s.id && e.days[i].status !== 'cuti').length || ''}</td>`).join('');
      return `<tr class="cnt"><td class="emp" style="background:${escapeHtml(s.color)}">${escapeHtml(s.code)} · ${escapeHtml(s.name)}</td>${cells}</tr>`;
    }).join('');
    const shiftLegend = branch.shifts.filter(s => s.is_active)
      .map(s => `<span><i style="background:${escapeHtml(s.color)}"></i> ${escapeHtml(s.code)} ${escapeHtml(s.name)} ${escapeHtml(shiftTimeLabel(s))} (${escapeHtml(shiftHoursLabel(s))})</span>`).join('');
    const meta = [`${branch.employees.length} karyawan`];
    if (onLeave > 0) meta.push(`${onLeave} hari cuti`);
    if (pending > 0) meta.push(`${pending} hari menunggu`);
    return `<div class="branch">
      <div class="bh"><b>${escapeHtml(branch.name)}</b> <span>${escapeHtml(meta.join(' · '))}</span></div>
      ${shiftLegend ? `<div class="legend" style="margin:0 0 4px">${shiftLegend}</div>` : ''}
      <table>
        <thead><tr><th class="emp">Karyawan</th>${headCols}</tr></thead>
        <tbody>${rows || `<tr><td class="c" colspan="${colspan}" style="color:#888;padding:12px">Tidak ada karyawan.</td></tr>`}${countRows}</tbody>
      </table>
    </div>`;
  }).join('');

  return `<!doctype html><html lang="id"><head><meta charset="utf-8">
<title>Rencana Tenaga Kerja — ${escapeHtml(periodLabel)}</title>
<style>
  @page { size: A4 landscape; margin: 12mm; }
  * { box-sizing: border-box; -webkit-print-color-adjust: exact; print-color-adjust: exact; }
  body { font-family: Arial, Helvetica, sans-serif; color: #1a1a2e; font-size: 11px; margin: 0; }
  h1 { font-size: 17px; margin: 0 0 2px; }
  .sub { color: #667; font-size: 11px; margin-bottom: 4px; }
  .branch { margin-top: 14px; }
  .bh { font-size: 12px; margin-bottom: 4px; page-break-after: avoid; }
  .bh span { color: #667; font-weight: normal; margin-left: 6px; }
  table { width: 100%; border-collapse: collapse; }
  thead { display: table-header-group; }
  tr { page-break-inside: avoid; }
  th, td { border: 1px solid #cfd5df; padding: 3px 4px; font-size: 10px; }
  th { background: #eef1f6; text-align: center; }
  th.day, td.c { text-align: center; width: 26px; font-weight: 600; }
  th.we { background: #e2e6ee; }
  th.emp, td.emp { text-align: left; width: ${nameWidth}px; min-width: ${nameWidth}px; }
  td.emp .en { font-weight: 600; }
  td.emp .ep { color: #888; font-size: 9px; }
  .do { color: #7c3aed; font-size: 8px; }
  tr.sec td { background: #f3f4f6; font-weight: 700; font-size: 10px; }
  tr.cnt td { font-size: 9px; }
  td.none { color: #bbb; font-weight: 400; }
  td.pending { background: #fed7aa; color: #9a3412; font-weight: 700; }
  td.cuti { background: #1f2937; color: #f9fafb; font-weight: 600; font-size: 9px; }
  sup { color: #c2410c; font-size: 7px; }
  .legend { margin-top: 14px; font-size: 10px; color: #444; display: flex; gap: 14px; flex-wrap: wrap; }
  .legend span { display: inline-flex; align-items: center; gap: 5px; }
  .legend i { width: 12px; height: 12px; border: 1px solid rgba(0,0,0,0.15); display: inline-block; }
  .foot { margin-top: 16px; color: #999; font-size: 9px; }
  @media print { .noprint { display: none; } }
</style></head>
<body>
  <button class="noprint" onclick="window.print()" style="float:right;padding:8px 16px;cursor:pointer">Cetak</button>
  <h1>Rencana Tenaga Kerja</h1>
  <div class="sub">Periode: <b>${escapeHtml(periodLabel)}</b> (${days} hari) — ${totalEmployees} karyawan aktif</div>
  ${branchTables || '<p style="color:#888">Tidak ada karyawan aktif.</p>'}
  <div class="legend">
    <span><i style="background:#fff"></i> – Belum ada shift</span>
    <span><i style="background:#fed7aa"></i> Cuti menunggu persetujuan (M)</span>
    <span><i style="background:#1f2937"></i> Cuti Disetujui</span>
  </div>
  <div class="foot">Dicetak pada ${escapeHtml(printedAt)}</div>
</body></html>`;
}

const overlay = {
  position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)',
  display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
};

const modal = {
  background: '#fff', borderRadius: 8, padding: '1.5rem',
  width: '100%', maxWidth: 420, boxShadow: '0 8px 32px rgba(0,0,0,0.18)',
};

const inputStyle = {
  width: '100%', boxSizing: 'border-box', padding: '7px 10px',
  border: '1px solid #d1d5db', borderRadius: 4, fontSize: 13,
};

// ── Quick-create modal ────────────────────────────────────────────────────────
function QuickLeaveModal({ employee, date, onClose, onSaved }) {
  const [types, setTypes] = useState([]);
  const [form, setForm] = useState({
    employee_id: employee.id,
    leave_type_id: '',
    start_date: date,
    end_date: date,
    reason: '',
  });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getLeaveTypes({ active: 1 }).then(r => setTypes(r.data || [])).catch(() => setTypes([]));
  }, []);

  const usesQuota = !!types.find(t => t.id === form.leave_type_id)?.uses_quota;

  const calendarDays = (() => {
    const s = new Date(form.start_date), e = new Date(form.end_date);
    if (isNaN(s) || isNaN(e) || e < s) return 0;
    return Math.round((e - s) / 86400000) + 1;
  })();

  const submit = async () => {
    setError('');
    if (!form.leave_type_id) { setError('Pilih jenis cuti.'); return; }
    if (new Date(form.end_date) < new Date(form.start_date)) {
      setError('Tanggal selesai harus setelah atau sama dengan tanggal mulai.');
      return;
    }
    setSaving(true);
    try {
      const created = await submitLeaveRequest(form);
      if (created) onSaved(created);
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan pengajuan cuti.');
    } finally {
      setSaving(false);
    }
  };

  const field = (label, children) => (
    <div style={{ marginBottom: 12 }}>
      <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: '#374151', marginBottom: 4 }}>
        {label}
      </label>
      {children}
    </div>
  );

  return (
    <div style={overlay} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={modal}>
        <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700 }}>Ajukan Cuti</h3>
        <p style={{ margin: '0 0 16px', fontSize: 13, color: '#6b7280' }}>
          {employee.name}{employee.position ? ` · ${employee.position}` : ''}
        </p>

        {field('Jenis Cuti',
          <select
            style={inputStyle}
            value={form.leave_type_id}
            onChange={e => setForm(f => ({ ...f, leave_type_id: e.target.value }))}
          >
            <option value="">— Pilih jenis cuti —</option>
            {types.map(t => (
              <option key={t.id} value={t.id}>{t.name}</option>
            ))}
          </select>
        )}


        {field('Tanggal Mulai',
          <input
            type="date"
            style={inputStyle}
            value={form.start_date}
            onChange={e => setForm(f => ({ ...f, start_date: e.target.value }))}
          />
        )}

        {field('Tanggal Selesai',
          <input
            type="date"
            style={inputStyle}
            value={form.end_date}
            min={form.start_date}
            onChange={e => setForm(f => ({ ...f, end_date: e.target.value }))}
          />
        )}

        {calendarDays > 0 && (
          <p style={{ fontSize: 12, color: '#6b7280', margin: '-6px 0 12px' }}>
            {calendarDays} hari kalender
          </p>
        )}
        <BackdatedNotice startDate={form.start_date} />
        {usesQuota && <QuotaPreview employeeId={employee.id} startDate={form.start_date} endDate={form.end_date} />}

        {field('Keterangan (opsional)',
          <textarea
            style={{ ...inputStyle, resize: 'vertical', minHeight: 60 }}
            value={form.reason}
            onChange={e => setForm(f => ({ ...f, reason: e.target.value }))}
            placeholder="Alasan cuti..."
          />
        )}

        {error && <p style={{ color: '#dc2626', fontSize: 13, margin: '0 0 10px' }}>{error}</p>}

        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <button
            onClick={onClose}
            style={{ padding: '7px 16px', borderRadius: 4, border: '1px solid #d1d5db', background: '#fff', cursor: 'pointer', fontSize: 13 }}
          >
            Batal
          </button>
          <button
            onClick={submit}
            disabled={saving}
            style={{ padding: '7px 16px', borderRadius: 4, border: 'none', background: '#2563eb', color: '#fff', cursor: 'pointer', fontSize: 13, fontWeight: 600, opacity: saving ? 0.7 : 1 }}
          >
            {saving ? 'Menyimpan...' : 'Ajukan Cuti'}
          </button>
        </div>
      </div>
    </div>
  );
}

function LegendItem({ color, label, dashed }) {
  return (
    <span style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: '#374151' }}>
      <span style={{ width: 14, height: 14, borderRadius: 3, background: color, border: dashed ? '1px dashed #c2410c' : '1px solid rgba(0,0,0,0.1)' }} />
      {label}
    </span>
  );
}

// ── Main page ─────────────────────────────────────────────────────────────────
const cellStyle = {
  padding: '8px 6px',
  textAlign: 'center',
  fontSize: 12,
  borderBottom: '1px solid #e5e7eb',
  borderRight: '1px solid #e5e7eb',
  minWidth: 70,
  maxWidth: 110,
  cursor: 'pointer',
  transition: 'filter 0.1s',
  position: 'relative',
};

const headerCellStyle = {
  padding: '8px 6px',
  textAlign: 'center',
  fontSize: 12,
  fontWeight: 600,
  background: '#f9fafb',
  color: '#374151',
  borderBottom: '1px solid #e5e7eb',
  borderRight: '1px solid #e5e7eb',
  minWidth: 70,
  maxWidth: 110,
  position: 'sticky',
  top: 0,
  zIndex: 1,
};

// Days already gone are history: read-only, so they read as faded.
const pastCellStyle = { opacity: 0.45 };

// Compact mode (>10 days in view): narrower cells, shift code only.
const compactCellStyle = {
  padding: '8px 2px',
  minWidth: 32,
  maxWidth: 36,
  fontSize: 10,
};

const compactHeaderCellStyle = {
  padding: '6px 2px',
  minWidth: 32,
  maxWidth: 36,
  fontSize: 10,
};

const nameCellStyle = {
  padding: '8px 10px',
  fontSize: 13,
  borderBottom: '1px solid #e5e7eb',
  borderRight: '1px solid #e5e7eb',
  minWidth: 180,
  position: 'sticky',
  left: 0,
  background: '#fff',
  zIndex: 0,
};

const nameHeaderStyle = {
  ...nameCellStyle,
  fontWeight: 600,
  background: '#f9fafb',
  color: '#374151',
  position: 'sticky',
  top: 0,
  left: 0,
  zIndex: 2,
  fontSize: 12,
};

const sectionRowStyle = {
  padding: '6px 10px',
  fontSize: 12,
  fontWeight: 700,
  color: '#374151',
  background: '#f3f4f6',
  borderBottom: '1px solid #e5e7eb',
  position: 'sticky',
  left: 0,
};

const countCellStyle = {
  padding: '4px 2px',
  textAlign: 'center',
  fontSize: 11,
  fontWeight: 600,
  borderBottom: '1px solid #e5e7eb',
  borderRight: '1px solid #e5e7eb',
  background: '#f9fafb',
};

const smallBtn = {
  fontSize: 12, padding: '3px 10px', borderRadius: 4, cursor: 'pointer',
  border: '1px solid #d1d5db', background: '#fff', color: '#374151',
};

const RANGE_PRESETS = [
  { label: '1 Minggu', days: 7 },
  { label: '2 Minggu', days: 14 },
  { label: '3 Minggu', days: 21 },
  { label: '1 Bulan', days: 31 },
];
const MIN_DAYS = 7;
const MAX_DAYS = 31;
const COMPACT_THRESHOLD = 10; // > this many days: shift codes only, no leave text

// How one roster cell looks: approved leave wins, then the rostered shift,
// then a pending leave request, else "not rostered yet".
function cellLook(day, shift) {
  if (day.status === 'cuti') return { background: '#1f2937', color: '#f9fafb' };
  if (shift) return { background: shift.color, color: '#1f2937' };
  if (day.status === 'pending') return { background: '#fed7aa', color: '#9a3412' };
  return { background: '#fff', color: '#9ca3af' };
}

export default function ManpowerPlanning() {
  const [date, setDate] = useState(todayLocal());
  const [days, setDays] = useState(7);
  const [branchId, setBranchId] = useState('');
  const [branchList, setBranchList] = useState([]);
  const [view, setView] = useState('grid'); // grid | coverage
  const [coverageDate, setCoverageDate] = useState(todayLocal());
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  // { type: 'shift'|'leave'|'profile'|'settings', employee?, day?, branch }
  const [modal, setModal] = useState(null);

  const compact = days > COMPACT_THRESHOLD;
  // The server's business date decides what is locked; fall back to the local
  // date only until the first response arrives.
  const today = data?.today || todayLocal();
  const approver = canApprove();

  useEffect(() => {
    getBranches().then(r => setBranchList(r.data || [])).catch(() => setBranchList([]));
  }, []);

  const load = () => {
    setLoading(true);
    setError('');
    getManpowerPlanning({ date, days, branch_id: branchId || undefined })
      .then(r => {
        setData(r.data);
        // Keep the coverage day inside the window being viewed.
        setCoverageDate(cd => (r.data.dates.includes(cd) ? cd : r.data.dates[0]));
      })
      .catch(() => setError('Gagal memuat data perencanaan tenaga kerja.'))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [date, days, branchId]);

  const clampDays = (v) => Math.min(MAX_DAYS, Math.max(MIN_DAYS, v));

  const openCell = (employee, day, branch) => setModal({ type: 'shift', employee, day, branch });

  const handleSaved = (msg) => {
    setModal(null);
    if (msg) setNotice(msg);
    load();
  };

  const handlePrint = () => {
    if (!data) return;
    const html = buildManpowerPrintHtml({ data, days });
    const win = window.open('', '_blank');
    if (!win) return; // popup blocked
    win.document.write(html);
    win.document.close();
    win.focus();
    // Give the new document a tick to lay out before invoking print.
    setTimeout(() => { try { win.print(); } catch { /* user can use the Cetak button */ } }, 300);
  };

  const totalEmployees = data?.branches.reduce((s, b) => s + b.employees.length, 0) ?? 0;
  const countOnLeave = (employees) =>
    employees.reduce((s, e) => s + e.days.filter(d => d.status === 'cuti').length, 0);
  const countPending = (employees) =>
    employees.reduce((s, e) => s + e.days.filter(d => d.status === 'pending').length, 0);
  const pendingShiftChanges = data?.branches.reduce((s, b) =>
    s + b.employees.reduce((n, e) => n + e.days.filter(d => d.pending_request_id).length, 0), 0) ?? 0;

  const tabBtn = (key, label) => (
    <button
      onClick={() => setView(key)}
      style={{
        fontSize: 13, padding: '6px 14px', cursor: 'pointer', border: 'none',
        borderBottom: view === key ? '2px solid #2563eb' : '2px solid transparent',
        background: 'transparent', color: view === key ? '#1d4ed8' : '#6b7280', fontWeight: view === key ? 700 : 500,
      }}
    >{label}</button>
  );

  return (
    <div style={{ padding: '1.5rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem', flexWrap: 'wrap', gap: 8 }}>
        <h2 style={{ margin: 0, fontSize: 20, fontWeight: 700 }}>Perencanaan Tenaga Kerja</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          <select
            value={branchId}
            onChange={e => setBranchId(e.target.value)}
            style={{ fontSize: 13, padding: '4px 8px', border: '1px solid #d1d5db', borderRadius: 4 }}
          >
            <option value="">Semua cabang</option>
            {branchList.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
          <label style={{ fontSize: 13, color: '#6b7280' }}>Mulai tanggal:</label>
          <input
            type="date"
            value={date}
            onChange={e => setDate(e.target.value)}
            style={{ fontSize: 13, padding: '4px 8px', border: '1px solid #d1d5db', borderRadius: 4 }}
          />
          <div style={{ display: 'flex', gap: 4 }}>
            {RANGE_PRESETS.map(p => (
              <button
                key={p.days}
                onClick={() => setDays(p.days)}
                style={{
                  fontSize: 12, padding: '4px 10px', borderRadius: 4, cursor: 'pointer',
                  border: days === p.days ? '1px solid #2563eb' : '1px solid #d1d5db',
                  background: days === p.days ? '#2563eb' : '#fff',
                  color: days === p.days ? '#fff' : '#374151',
                  fontWeight: days === p.days ? 600 : 400,
                }}
              >
                {p.label}
              </button>
            ))}
          </div>
          <label style={{ fontSize: 13, color: '#6b7280' }}>Hari:</label>
          <input
            type="number"
            min={MIN_DAYS}
            max={MAX_DAYS}
            value={days}
            onChange={e => setDays(clampDays(Number(e.target.value) || MIN_DAYS))}
            style={{ width: 56, fontSize: 13, padding: '4px 8px', border: '1px solid #d1d5db', borderRadius: 4 }}
          />
          <button
            onClick={handlePrint}
            disabled={!data || loading}
            title="Cetak rencana tenaga kerja"
            style={{
              fontSize: 13, padding: '4px 12px', borderRadius: 4, cursor: 'pointer',
              border: '1px solid #d1d5db', background: '#fff', color: '#374151',
              opacity: (!data || loading) ? 0.6 : 1,
            }}
          >
            🖨️ Cetak
          </button>
        </div>
      </div>

      <p style={{ fontSize: 12, color: '#9ca3af', margin: '0 0 0.75rem' }}>
        Klik kotak hari untuk mengatur shift atau mengajukan cuti. Hari yang sudah lewat (pudar) tidak dapat diubah;
        perubahan shift <b>hari ini</b> {approver ? 'langsung berlaku karena Anda manajer' : 'memerlukan persetujuan manajer'}.
        {' '}Jatah libur 4 hari/bulan, Sabtu/Minggu dihitung {WEEKEND_COST} hari.
      </p>

      {notice && (
        <div style={{ background: '#ecfdf5', color: '#065f46', border: '1px solid #a7f3d0', borderRadius: 6, padding: '8px 12px', fontSize: 13, marginBottom: 10, display: 'flex', justifyContent: 'space-between', gap: 8 }}>
          <span>{notice}</span>
          <button onClick={() => setNotice('')} style={{ border: 'none', background: 'none', cursor: 'pointer', color: '#065f46' }}>✕</button>
        </div>
      )}
      {pendingShiftChanges > 0 && (
        <div style={{ background: '#fff7ed', color: '#9a3412', border: '1px solid #fdba74', borderRadius: 6, padding: '8px 12px', fontSize: 13, marginBottom: 10 }}>
          {pendingShiftChanges} perubahan shift hari ini menunggu persetujuan manajer (ditandai garis putus-putus).
          {approver && <> <Link to="/hr/approvals">Buka Persetujuan →</Link></>}
        </div>
      )}

      <div style={{ display: 'flex', gap: 4, borderBottom: '1px solid #e5e7eb', marginBottom: '1rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
        {tabBtn('grid', 'Jadwal Karyawan')}
        {tabBtn('coverage', 'Cakupan per Shift & Divisi')}
        {view === 'coverage' && data && (
          <select
            value={coverageDate}
            onChange={e => setCoverageDate(e.target.value)}
            style={{ marginLeft: 'auto', marginBottom: 4, fontSize: 13, padding: '4px 8px', border: '1px solid #d1d5db', borderRadius: 4 }}
          >
            {data.dates.map(d => <option key={d} value={d}>{formatDateLabel(d)}{d === today ? ' (hari ini)' : ''}</option>)}
          </select>
        )}
      </div>

      {error && <p style={{ color: '#dc2626' }}>{error}</p>}
      {loading && <p style={{ color: '#6b7280' }}>Memuat...</p>}

      {!loading && data && view === 'coverage' && (
        <ShiftCoverageView data={data} date={coverageDate} onCellClick={openCell} />
      )}

      {!loading && data && view === 'grid' && (
        <>
          <p style={{ fontSize: 13, color: '#6b7280', marginBottom: '1rem' }}>
            {totalEmployees} karyawan aktif &mdash; {data.dates[0]} s/d {data.dates[data.dates.length - 1]}
          </p>

          {data.branches.length === 0 && (
            <p style={{ color: '#6b7280' }}>Tidak ada karyawan aktif.</p>
          )}

          {data.branches.map(branch => {
            const onLeaveCount = countOnLeave(branch.employees);
            const pendingCount = countPending(branch.employees);
            const shiftsById = Object.fromEntries(branch.shifts.map(s => [s.id, s]));
            const activeShifts = branch.shifts.filter(s => s.is_active);
            const colCount = data.dates.length + 1;
            return (
              <div key={branch.id} style={{ marginBottom: '2rem' }}>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 6, flexWrap: 'wrap' }}>
                  <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700, color: '#111827' }}>
                    {branch.name}
                  </h3>
                  <span style={{ fontSize: 12, color: '#6b7280' }}>
                    {branch.employees.length} karyawan
                    {onLeaveCount > 0 && (
                      <span style={{ color: '#374151', marginLeft: 6 }}>
                        · {onLeaveCount} hari cuti di periode ini
                      </span>
                    )}
                    {pendingCount > 0 && (
                      <span style={{ color: '#c2410c', marginLeft: 6 }}>
                        · {pendingCount} hari menunggu persetujuan
                      </span>
                    )}
                  </span>
                  {branch.id && (
                    <button style={{ ...smallBtn, marginLeft: 'auto' }} onClick={() => setModal({ type: 'settings', branch })}>
                      ⚙ Atur Shift
                    </button>
                  )}
                </div>
                {activeShifts.length > 0 && (
                  <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', fontSize: 11, color: '#4b5563', marginBottom: 6 }}>
                    {activeShifts.map(s => (
                      <span key={s.id} style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                        <span style={{ background: s.color, padding: '0 5px', borderRadius: 3, fontWeight: 700, color: '#1f2937' }}>{s.code}</span>
                        {s.name} {shiftTimeLabel(s)} ({shiftHoursLabel(s)}){s.delivery_only && ' · khusus DO'}
                      </span>
                    ))}
                  </div>
                )}

                <div style={{ overflowX: 'auto', border: '1px solid #e5e7eb', borderRadius: 6 }}>
                  <table style={{ borderCollapse: 'collapse', width: '100%', minWidth: 600 }}>
                    <thead>
                      <tr>
                        <th style={nameHeaderStyle}>Karyawan</th>
                        {data.dates.map(d => (
                          <th key={d} style={{
                            ...headerCellStyle, ...(compact ? compactHeaderCellStyle : null),
                            ...(d === today ? { background: '#dbeafe', color: '#1e3a8a' } : null),
                          }}>
                            {compact ? formatDateHeaderCompact(d) : formatDateHeader(d)}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {groupByHomeDivision(branch).map(section => (
                        <SectionRows
                          key={section.id}
                          section={section}
                          colCount={colCount}
                          shiftsById={shiftsById}
                          today={today}
                          compact={compact}
                          onCell={(emp, day) => openCell(emp, day, branch)}
                          onProfile={(emp) => setModal({ type: 'profile', employee: emp, branch })}
                        />
                      ))}
                      {activeShifts.map(s => (
                        <tr key={`count-${s.id}`}>
                          <td style={{ ...nameCellStyle, ...countCellStyle, textAlign: 'left', background: s.color }}>
                            Σ {s.code} · {s.name}
                          </td>
                          {data.dates.map((d, i) => {
                            const n = branch.employees.filter(e => e.days[i].shift_id === s.id && e.days[i].status !== 'cuti').length;
                            return <td key={d} style={{ ...countCellStyle, color: n ? '#111827' : '#d1d5db' }}>{n}</td>;
                          })}
                        </tr>
                      ))}
                      {activeShifts.length > 0 && (
                        <tr>
                          <td style={{ ...nameCellStyle, ...countCellStyle, textAlign: 'left', color: '#6b7280' }}>Belum dijadwalkan</td>
                          {data.dates.map((d, i) => {
                            const n = branch.employees.filter(e => !e.days[i].shift_id && e.days[i].status !== 'cuti').length;
                            return <td key={d} style={{ ...countCellStyle, color: n ? '#b45309' : '#d1d5db' }}>{n}</td>;
                          })}
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
            );
          })}

          <div style={{ display: 'flex', alignItems: 'center', gap: 16, flexWrap: 'wrap', marginTop: 4, padding: '10px 12px', background: '#f9fafb', border: '1px solid #e5e7eb', borderRadius: 6 }}>
            <span style={{ fontSize: 12, fontWeight: 600, color: '#374151' }}>Keterangan:</span>
            <LegendItem color="#fff" label="– Belum ada shift" />
            <LegendItem color="#dbeafe" label="Kode shift (warna per shift)" />
            <LegendItem color="#fff" dashed label="Perubahan menunggu persetujuan" />
            <LegendItem color="#fed7aa" label="Cuti menunggu persetujuan" />
            <LegendItem color="#1f2937" label="Cuti (Disetujui)" />
            <span style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: '#374151', opacity: 0.6 }}>
              <span style={{ width: 14, height: 14, borderRadius: 3, background: '#dbeafe', border: '1px solid rgba(0,0,0,0.1)' }} />
              Sudah lewat (pudar, tidak dapat diubah)
            </span>
          </div>
        </>
      )}

      {modal?.type === 'shift' && (
        <ShiftCellModal
          employee={modal.employee}
          day={modal.day}
          branch={modal.branch}
          dates={data.dates}
          today={today}
          approver={approver}
          onClose={() => setModal(null)}
          onSaved={handleSaved}
          onRequestLeave={() => setModal({ ...modal, type: 'leave' })}
        />
      )}

      {modal?.type === 'leave' && (
        <QuickLeaveModal
          employee={modal.employee}
          date={modal.day.date}
          onClose={() => setModal(null)}
          onSaved={() => handleSaved('Pengajuan cuti disimpan.')}
        />
      )}

      {modal?.type === 'profile' && (
        <ShiftProfileModal
          employee={modal.employee}
          branch={modal.branch}
          onClose={() => setModal(null)}
          onSaved={() => handleSaved('Profil shift disimpan.')}
        />
      )}

      {modal?.type === 'settings' && (
        <ShiftSettingsModal
          branch={modal.branch}
          onClose={() => { setModal(null); load(); }}
        />
      )}
    </div>
  );
}

// One division section of a branch grid: a heading row, then its employees.
function SectionRows({ section, colCount, shiftsById, today, compact, onCell, onProfile }) {
  return (
    <>
      <tr>
        <td colSpan={colCount} style={sectionRowStyle}>
          {section.name} <span style={{ fontWeight: 400, color: '#6b7280' }}>· {section.employees.length} orang</span>
        </td>
      </tr>
      {section.employees.map(emp => (
        <tr key={emp.id}>
          <td style={nameCellStyle}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <span style={{ fontWeight: 500, color: '#111827', flex: 1 }}>
                {emp.name}
                {emp.is_delivery_staff && (
                  <span style={{ marginLeft: 6, fontSize: 10, fontWeight: 700, color: '#7c3aed', background: '#ede9fe', padding: '0 4px', borderRadius: 3 }}>DO</span>
                )}
              </span>
              <button
                onClick={() => onProfile(emp)}
                title="Atur divisi & status staf DO"
                style={{ border: 'none', background: 'none', cursor: 'pointer', color: '#9ca3af', fontSize: 13, padding: 0 }}
              >✎</button>
            </div>
            {emp.position && (
              <div style={{ fontSize: 11, color: '#9ca3af' }}>{emp.position}</div>
            )}
            {(emp.leave_balance || []).map(b => {
              const left = b.quota_days - b.used_days;
              return (
                <div key={b.month} style={{ fontSize: 11, color: left > 0 ? '#15803d' : '#b91c1c' }}
                  title={`Jatah libur ${fmtMonth(b.month)}: terpakai ${b.used_days} dari ${b.quota_days} hari`}>
                  Sisa libur {shortMonth(b.month)}: {left}/{b.quota_days}
                </div>
              );
            })}
          </td>
          {emp.days.map(day => {
            const shift = shiftsById[day.shift_id];
            const pendingShift = day.pending_request_id ? (shiftsById[day.pending_shift_id] || null) : undefined;
            const past = day.date < today;
            const look = cellLook(day, shift);
            const titleParts = [];
            if (day.status === 'cuti') titleParts.push(`${day.leave_type} (disetujui)`);
            if (shift) titleParts.push(`${shift.name} ${shiftTimeLabel(shift)}`);
            if (day.status === 'pending') titleParts.push(`${day.leave_type} (cuti menunggu persetujuan)`);
            if (pendingShift !== undefined) titleParts.push(`Menunggu persetujuan: ${pendingShift ? pendingShift.name : 'hapus shift'}`);
            if (!titleParts.length) titleParts.push('Belum ada shift');
            titleParts.push(past ? 'sudah lewat — tidak dapat diubah' : 'klik untuk ubah');
            return (
              <td
                key={day.date}
                title={titleParts.join(' · ')}
                onClick={() => onCell(emp, day)}
                style={{
                  ...cellStyle, ...(compact ? compactCellStyle : null), ...look,
                  ...(past ? pastCellStyle : null),
                  ...(pendingShift !== undefined ? { outline: '2px dashed #c2410c', outlineOffset: -3 } : null),
                }}
                onMouseEnter={e => e.currentTarget.style.filter = 'brightness(0.93)'}
                onMouseLeave={e => e.currentTarget.style.filter = ''}
              >
                {day.status === 'cuti' ? (
                  compact ? 'C' : (
                    <span style={{ display: 'block', fontSize: 11, fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {day.leave_type || 'Cuti'}
                    </span>
                  )
                ) : shift ? (
                  <span style={{ fontWeight: 700 }}>{shift.code}</span>
                ) : day.status === 'pending' ? (
                  compact ? 'M' : (
                    <span style={{ display: 'block', fontSize: 11, fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {day.leave_type || 'Cuti'}
                    </span>
                  )
                ) : '–'}
                {shift && day.status === 'pending' && (
                  <span title="Cuti menunggu persetujuan" style={{ position: 'absolute', top: 2, right: 3, fontSize: 9, fontWeight: 700, color: '#c2410c' }}>M</span>
                )}
                {pendingShift !== undefined && !compact && (
                  <div style={{ fontSize: 10, color: '#c2410c', fontWeight: 600 }}>→ {pendingShift ? pendingShift.code : 'hapus'}</div>
                )}
              </td>
            );
          })}
        </tr>
      ))}
    </>
  );
}
