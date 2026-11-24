import { useEffect, useState } from 'react';
import { getLeaveBalance } from '../../api';
import { estimateCostByMonth, fmtMonth, fmtPeriodRange, isBackdated, WEEKEND_COST } from './leaveRules';

// Widgets for the leave rules in leaveRules.js.

// QuotaPreview: remaining allowance per period the request touches (quota types only).
export function QuotaPreview({ employeeId, startDate, endDate }) {
  // Balances keyed by the employee they were fetched for, so a stale answer for
  // a previously selected employee is never shown.
  const [loaded, setLoaded] = useState({ employeeId: null, months: {}, cutoff: null }); // months: 'YYYY-MM' → balance
  const startYear = startDate?.slice(0, 4);
  const endDay = endDate || startDate;
  const endYear = endDay?.slice(0, 4);
  // A late-December day belongs to next January's period.
  const spillYear = endDay?.slice(5, 7) === '12' ? String(Number(endYear) + 1) : endYear;

  useEffect(() => {
    if (!employeeId || !startYear) return;
    const years = [...new Set([startYear, endYear, spillYear])];
    Promise.all(years.map(y => getLeaveBalance(employeeId, y).then(r => r.data || {}).catch(() => ({}))))
      .then(list => setLoaded({
        employeeId,
        months: Object.fromEntries(list.flatMap(r => r.months || []).map(m => [m.month, m])),
        cutoff: list.find(r => r.cutoff_day != null)?.cutoff_day ?? 0,
      }));
  }, [employeeId, startYear, endYear, spillYear]);

  const ready = loaded.employeeId === employeeId && loaded.cutoff !== null;
  const months = ready ? loaded.months : {};

  const cost = ready ? estimateCostByMonth(startDate, endDate || startDate, loaded.cutoff) : {};
  const entries = Object.entries(cost);
  if (!employeeId || entries.length === 0) return null;

  return (
    <div style={{ fontSize: '0.82rem', color: '#445', margin: '0.4rem 0' }}>
      {entries.map(([ym, need]) => {
        const b = months[ym];
        const left = b ? b.quota_days - b.used_days : null;
        const short = left !== null && need > left;
        return (
          <div key={ym} style={{ color: short ? '#c5221f' : undefined }}>
            Jatah libur {fmtMonth(ym)}{b && fmtPeriodRange(b) ? ` (${fmtPeriodRange(b)})` : ''}: sisa <strong>{left ?? '…'}</strong>{b ? ` / ${b.quota_days}` : ''} hari
            {' · '}pengajuan ini ±<strong>{need}</strong> hari
            {short && ' — melebihi jatah, tidak dapat disimpan'}
          </div>
        );
      })}
      <div style={{ color: '#888', fontSize: '0.76rem' }}>Libur dari jatah langsung disetujui tanpa persetujuan manajer. Sabtu/Minggu dihitung {WEEKEND_COST} hari. Sisa jatah tidak terbawa ke periode berikutnya.</div>
    </div>
  );
}

// BackdatedNotice is the inline warning shown as soon as a past start date is
// picked, before the confirmation prompt on submit.
export function BackdatedNotice({ startDate, autoApproved = false }) {
  if (!isBackdated(startDate)) return null;
  return (
    <div style={{ background: '#fff4e5', color: '#9a3412', border: '1px solid #fdba74', padding: '0.45rem 0.7rem', borderRadius: 6, fontSize: '0.82rem', margin: '0.4rem 0' }}>
      {autoApproved
        ? 'Tanggal mulai sudah lewat. Libur ini akan ditandai mundur tanggal dan langsung berlaku.'
        : 'Tanggal mulai sudah lewat. Pengajuan mundur tanggal memerlukan persetujuan manajer/pemilik.'}
    </div>
  );
}

export function BackdatedBadge({ request }) {
  if (!request?.is_backdated) return null;
  return (
    <span title={request.status === 'pending' ? 'Diajukan setelah tanggal mulai lewat — perlu ditinjau manajer/pemilik satu per satu' : 'Diajukan setelah tanggal mulai lewat'}
      style={{ background: '#fed7aa', color: '#9a3412', padding: '0.1rem 0.4rem', borderRadius: 4, fontSize: '0.7rem', fontWeight: 700, marginLeft: 6, whiteSpace: 'nowrap' }}>
      MUNDUR TANGGAL
    </span>
  );
}

// Sick leave without a doctor's note costs 8 points and a day's pay.
export function DoctorNoteField({ type, checked, onChange }) {
  if (!type?.requires_doctor_note) return null;
  return (
    <div style={{ margin: '0.25rem 0 0.75rem' }}>
      <label style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', fontSize: '0.9rem' }}>
        <input type="checkbox" style={{ width: 'auto' }} checked={!!checked} onChange={(e) => onChange(e.target.checked)} />
        Ada surat keterangan dokter
      </label>
      {!checked && (
        <div style={{ fontSize: '0.78rem', color: '#8a5300', marginTop: '0.25rem' }}>
          Tanpa surat dokter: −8 poin dan potongan gaji 1 hari untuk setiap hari sakit.
        </div>
      )}
    </div>
  );
}
