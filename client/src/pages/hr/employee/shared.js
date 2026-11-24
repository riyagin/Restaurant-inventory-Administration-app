import { canUseHR } from '../../../roles';
import { fmtDateTime } from '../../../time';

export const TYPE_LABELS = { allowance: 'Tunjangan', bonus: 'Bonus', deduction: 'Potongan', daily_allowance: 'Harian (Tunai)', welfare_fund: 'Dana Kesejahteraan' };

export const canEdit = canUseHR;

export const DOC_TYPE_LABELS = {
  ktp: 'KTP', kk: 'Kartu Keluarga', ijazah: 'Ijazah', npwp: 'NPWP',
  bpjs_kesehatan: 'BPJS Kesehatan', bpjs_ketenagakerjaan: 'BPJS Ketenagakerjaan',
  pkwt: 'Kontrak PKWT', pkwtt: 'Kontrak PKWTT', surat_peringatan: 'Surat Peringatan',
  paklaring: 'Paklaring', foto: 'Pas Foto', surat_lamaran: 'Surat Lamaran / CV', other: 'Lainnya',
};

export const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '-';

const STATUS_LABELS = { active: 'Aktif', inactive: 'Nonaktif', resigned: 'Resign' };

export const statusLabel = (s) => STATUS_LABELS[s] || 'Nonaktif';

// Whole-day difference between a contract end date and today (negative = overdue).
const DAY_MS = 86400000;

export function contractDaysLeft(dateStr) {
  if (!dateStr) return null;
  const end = new Date(dateStr); end.setHours(0, 0, 0, 0);
  const today = new Date(); today.setHours(0, 0, 0, 0);
  return Math.round((end - today) / DAY_MS);
}

export function perfScoreColor(score) {
  if (score >= 90) return { bg: '#e6f4ea', color: '#1e7e34' };
  if (score >= 70) return { bg: '#fff8e1', color: '#a06800' };
  return { bg: '#fce8e6', color: '#c5221f' };
}

export function fmtDateShort(d) { return d ? new Date(d).toLocaleDateString('id-ID') : '-'; }

export const fmtDateTimeOrDash = (d) => fmtDateTime(d) || '-';
