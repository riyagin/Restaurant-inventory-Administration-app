import { fmtTime as fmtWIBTime } from '../../../time';

// Classify an employee's saved bank into an export bucket.
export function classifyBank(name) {
  const s = (name || '').toLowerCase();
  if (s.includes('bca') || s.includes('central asia')) return 'BCA';
  if (s.includes('mandiri')) return 'Mandiri';
  return 'Lainnya';
}

// Trigger a browser download from an Axios blob response.
export function saveBlob(data, filename) {
  const url = window.URL.createObjectURL(new Blob([data]));
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.URL.revokeObjectURL(url);
}

export const STATUS_LABELS = { open: 'Terbuka', closed: 'Ditutup', paid: 'Dibayar' };

export const fmtMonth = (d) => d ? new Date(d).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' }) : '-';

export const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';

export const numVal = (n) => Number(n ?? 0);

// Sortable columns of the payroll line table. `get` extracts the comparable value
// (null = "no value", always sorted last); `numeric` picks the comparison and the
// direction a first click applies — money/score columns open highest-first, text
// columns A–Z. The whole line set is fetched in one request, so sorting happens
// here rather than round-tripping to the server.
export const SORT_COLUMNS = {
  name:                   { numeric: false, get: (l) => l.employee_name || '' },
  position:               { numeric: false, get: (l) => l.position_name || '' },
  branch:                 { numeric: false, get: (l) => l.branch_name || '' },
  base_salary:            { numeric: true,  get: (l) => numVal(l.base_salary) },
  allowance_total:        { numeric: true,  get: (l) => numVal(l.allowance_total) },
  bonus_total:            { numeric: true,  get: (l) => numVal(l.bonus_total) },
  overtime:               { numeric: true,  get: (l) => numVal(l.overtime_amount) + numVal(l.overtime_hourly_amount) + numVal(l.public_holiday_amount) },
  kasbon_deduction:       { numeric: true,  get: (l) => numVal(l.kasbon_deduction) },
  unpaid_leave_deduction: { numeric: true,  get: (l) => numVal(l.unpaid_leave_deduction) },
  attendance_cut_deduction: { numeric: true, get: (l) => numVal(l.attendance_cut_deduction) },
  net_pay:                { numeric: true,  get: (l) => numVal(l.net_pay) },
  performance_score:      { numeric: true,  get: (l) => (l.performance_score === null || l.performance_score === undefined ? null : Number(l.performance_score)) },
  reviewed:               { numeric: true,  get: (l) => (l.reviewed ? 1 : 0) },
};

// sortLines returns a sorted copy. Rows with no value for the active column sink
// to the bottom in both directions, and employee name breaks every tie so the
// order stays stable across re-sorts.
export function sortLines(lines, sort, order) {
  const col = SORT_COLUMNS[sort] || SORT_COLUMNS.name;
  const dir = order === 'desc' ? -1 : 1;
  return [...lines].sort((a, b) => {
    const va = col.get(a);
    const vb = col.get(b);
    const emptyA = va === null || va === '';
    const emptyB = vb === null || vb === '';
    if (emptyA || emptyB) {
      if (emptyA && emptyB) return 0;
      return emptyA ? 1 : -1;
    }
    const cmp = col.numeric ? va - vb : String(va).localeCompare(String(vb), 'id');
    if (cmp !== 0) return cmp * dir;
    return String(a.employee_name || '').localeCompare(String(b.employee_name || ''), 'id');
  });
}

export const fmtTime = (iso) => fmtWIBTime(iso) || '—';

export const fmtMin = (m) => (m ? `${m} mnt` : '—');

export function calcAmounts(line, mult, overtimeDays, overtimeHours, holidayDays, components) {
  if (!line || !mult) return null;
  const overtimeAmt = Math.round(Number(overtimeDays) * Number(line.daily_rate) * mult.overtime);
  const overtimeHourlyAmt = Math.round(Number(overtimeHours) * Number(line.overtime_hourly_rate) * mult.overtime);
  const holidayAmt = Math.round(Number(holidayDays) * Number(line.daily_rate) * mult.holiday);
  const allowTotal = components.filter(c => c.type === 'allowance').reduce((s, c) => s + (Number(c.amount) || 0), 0);
  const bonusTotal = components.filter(c => c.type === 'bonus').reduce((s, c) => s + (Number(c.amount) || 0), 0);
  // daily_allowance is handed out in cash during the month, so it stays out of gross/net
  // and is only surfaced as a separate informational total.
  const dailyPaidTotal = components.filter(c => c.type === 'daily_allowance').reduce((s, c) => s + (Number(c.amount) || 0), 0);
  const gross = Number(line.base_salary) + allowTotal + bonusTotal + overtimeAmt + overtimeHourlyAmt + holidayAmt;
  const net = gross - Number(line.component_deduction_total) - Number(line.kasbon_deduction) - Number(line.unpaid_leave_deduction) - Number(line.half_day_deduction || 0) - Number(line.attendance_cut_deduction || 0);
  return { overtimeAmt, overtimeHourlyAmt, holidayAmt, gross, net, dailyPaidTotal };
}
