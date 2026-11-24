import { createLeaveRequest } from '../../api';

import { todayWIB } from '../../time';
// Leave rules shared by every leave form; the server is authoritative.
// Allowance: 4 days per payroll period (Sat/Sun cost 2), no carry-over.
// Cuti is approved on save (refused when short); other types wait for a manager.
// Backdated requests must be confirmed.

export const WEEKEND_COST = 2;

// Today on the WIB calendar, whatever zone the browser is in (toISOString()
// would report yesterday before 07:00 WIB; the browser's own date is wrong off WIB).
export function todayLocal() {
  return todayWIB();
}

export const isBackdated = (startDate) => !!startDate && startDate < todayLocal();

const MONTHS_ID = ['Jan', 'Feb', 'Mar', 'Apr', 'Mei', 'Jun', 'Jul', 'Agu', 'Sep', 'Okt', 'Nov', 'Des'];
export const fmtMonth = (ym) => {
  const [y, m] = ym.split('-').map(Number);
  return `${MONTHS_ID[m - 1]} ${y}`;
};

// periodOf returns the allowance period label for a date; mirrors service.LeavePeriodOf.
export function periodOf(date, cutoff) {
  let [y, m, d] = date.slice(0, 10).split('-').map(Number);
  if (cutoff > 0 && d > cutoff) {
    m += 1;
    if (m > 12) { m = 1; y += 1; }
  }
  return `${y}-${String(m).padStart(2, '0')}`;
}

const fmtDayMonth = (iso) => {
  const [, m, d] = iso.split('-').map(Number);
  return `${d} ${MONTHS_ID[m - 1]}`;
};

// fmtPeriodRange labels a balance's dates, e.g. "26 Okt – 25 Nov". Empty when
// the balance carries no period bounds.
export const fmtPeriodRange = (b) =>
  b?.period_start && b?.period_end ? `${fmtDayMonth(b.period_start)} – ${fmtDayMonth(b.period_end)}` : '';

function backdatedMessage(startDate, autoApproved) {
  const label = new Date(startDate + 'T00:00:00').toLocaleDateString('id-ID', { day: 'numeric', month: 'long', year: 'numeric' });
  return `Tanggal mulai (${label}) sudah lewat.\n\n`
    + (autoApproved
      ? 'Libur ini akan ditandai MUNDUR TANGGAL dan langsung berlaku.'
      : 'Perencanaan tenaga kerja tidak boleh diubah mundur tanggal tanpa izin. '
        + 'Pengajuan ini akan ditandai MUNDUR TANGGAL dan baru berlaku setelah '
        + 'disetujui manajer/pemilik.')
    + '\n\nLanjutkan pengajuan?';
}

// submitLeaveRequest prompts before a backdated request; resolves null when cancelled.
export async function submitLeaveRequest(form, { autoApproved = false } = {}) {
  let confirmed = false;
  if (isBackdated(form.start_date)) {
    if (!window.confirm(backdatedMessage(form.start_date, autoApproved))) return null;
    confirmed = true;
  }
  try {
    const r = await createLeaveRequest({ ...form, confirm_backdated: confirmed });
    return r.data;
  } catch (err) {
    if (!confirmed && err?.response?.data?.code === 'backdated_requires_confirmation') {
      if (!window.confirm(backdatedMessage(form.start_date, autoApproved))) return null;
      const r = await createLeaveRequest({ ...form, confirm_backdated: true });
      return r.data;
    }
    throw err;
  }
}

// estimateCostByMonth ignores holidays and check-ins, so the server's figure can only be lower.
export function estimateCostByMonth(start, end, cutoff = 0) {
  const out = {};
  const s = new Date(start + 'T00:00:00'), e = new Date(end + 'T00:00:00');
  if (isNaN(s) || isNaN(e) || e < s) return out;
  for (const d = new Date(s); d <= e; d.setDate(d.getDate() + 1)) {
    const iso = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
    const ym = periodOf(iso, cutoff);
    const wd = d.getDay();
    out[ym] = (out[ym] || 0) + (wd === 0 || wd === 6 ? WEEKEND_COST : 1);
  }
  return out;
}
