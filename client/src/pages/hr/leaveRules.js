import { createLeaveRequest } from '../../api';

// Shared leave rules for every form that creates a leave request (Cuti,
// Pengajuan, Perencanaan Tenaga Kerja). The server is authoritative for both
// rules; this module only warns early and previews.
//
//  1. Monthly allowance: 4 days of paid leave per month, a Saturday or Sunday off
//     costs 2. Unused days do not carry over — they are shown as an indicator and
//     converted to cash at evaluation.
//  2. Backdating: a request starting before today needs a manager/owner's
//     approval. The user is warned and must confirm; the request is then saved
//     flagged "mundur tanggal" and can only be approved individually.

export const WEEKEND_COST = 2;

// Local (not UTC) date: before 07:00 WIB, toISOString() still reports yesterday.
export function todayLocal() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

export const isBackdated = (startDate) => !!startDate && startDate < todayLocal();

const MONTHS_ID = ['Jan', 'Feb', 'Mar', 'Apr', 'Mei', 'Jun', 'Jul', 'Agu', 'Sep', 'Okt', 'Nov', 'Des'];
export const fmtMonth = (ym) => {
  const [y, m] = ym.split('-').map(Number);
  return `${MONTHS_ID[m - 1]} ${y}`;
};

function backdatedMessage(startDate) {
  const label = new Date(startDate + 'T00:00:00').toLocaleDateString('id-ID', { day: 'numeric', month: 'long', year: 'numeric' });
  return `Tanggal mulai (${label}) sudah lewat.\n\n`
    + 'Perencanaan tenaga kerja tidak boleh diubah mundur tanggal tanpa izin. '
    + 'Pengajuan ini akan ditandai MUNDUR TANGGAL dan baru berlaku setelah '
    + 'disetujui manajer/pemilik.\n\nLanjutkan pengajuan?';
}

// submitLeaveRequest creates the request, prompting first when it is backdated.
// Resolves to the created request, or null when the user backs out. Also handles
// the server's 409 for the case where the browser's clock disagrees with it.
export async function submitLeaveRequest(form) {
  let confirmed = false;
  if (isBackdated(form.start_date)) {
    if (!window.confirm(backdatedMessage(form.start_date))) return null;
    confirmed = true;
  }
  try {
    const r = await createLeaveRequest({ ...form, confirm_backdated: confirmed });
    return r.data;
  } catch (err) {
    if (!confirmed && err?.response?.data?.code === 'backdated_requires_confirmation') {
      if (!window.confirm(backdatedMessage(form.start_date))) return null;
      const r = await createLeaveRequest({ ...form, confirm_backdated: true });
      return r.data;
    }
    throw err;
  }
}

// estimateCostByMonth previews what [start, end] draws from each month's
// allowance. It cannot see public holidays (which cost nothing) or days the
// employee already checked in, so the server's figure can only be lower.
export function estimateCostByMonth(start, end) {
  const out = {};
  const s = new Date(start + 'T00:00:00'), e = new Date(end + 'T00:00:00');
  if (isNaN(s) || isNaN(e) || e < s) return out;
  for (const d = new Date(s); d <= e; d.setDate(d.getDate() + 1)) {
    const ym = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
    const wd = d.getDay();
    out[ym] = (out[ym] || 0) + (wd === 0 || wd === 6 ? WEEKEND_COST : 1);
  }
  return out;
}
