import { useEffect, useState } from 'react';
import { getLeaveBalance } from '../../api';
import { estimateCostByMonth, fmtMonth, isBackdated, WEEKEND_COST } from './leaveRules';

// Widgets for the leave rules in leaveRules.js.

// QuotaPreview shows, per month the request touches, the remaining allowance and
// what this request would use. Render only for quota leave types.
export function QuotaPreview({ employeeId, startDate, endDate }) {
  // Balances keyed by the employee they were fetched for, so a stale answer for
  // a previously selected employee is never shown.
  const [loaded, setLoaded] = useState({ employeeId: null, months: {} }); // months: 'YYYY-MM' → balance
  const startYear = startDate?.slice(0, 4);
  const endYear = (endDate || startDate)?.slice(0, 4);

  useEffect(() => {
    if (!employeeId || !startYear) return;
    const years = [...new Set([startYear, endYear])];
    Promise.all(years.map(y => getLeaveBalance(employeeId, y).then(r => r.data?.months || []).catch(() => [])))
      .then(lists => setLoaded({ employeeId, months: Object.fromEntries(lists.flat().map(m => [m.month, m])) }));
  }, [employeeId, startYear, endYear]);

  const months = loaded.employeeId === employeeId ? loaded.months : {};

  const cost = estimateCostByMonth(startDate, endDate || startDate);
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
            Jatah libur {fmtMonth(ym)}: sisa <strong>{left ?? '…'}</strong>{b ? ` / ${b.quota_days}` : ''} hari
            {' · '}pengajuan ini ±<strong>{need}</strong> hari
            {short && ' — melebihi jatah, tidak dapat disetujui'}
          </div>
        );
      })}
      <div style={{ color: '#888', fontSize: '0.76rem' }}>Sabtu/Minggu dihitung {WEEKEND_COST} hari. Sisa jatah tidak terbawa ke bulan berikutnya.</div>
    </div>
  );
}

// BackdatedNotice is the inline warning shown as soon as a past start date is
// picked, before the confirmation prompt on submit.
export function BackdatedNotice({ startDate }) {
  if (!isBackdated(startDate)) return null;
  return (
    <div style={{ background: '#fff4e5', color: '#9a3412', border: '1px solid #fdba74', padding: '0.45rem 0.7rem', borderRadius: 6, fontSize: '0.82rem', margin: '0.4rem 0' }}>
      Tanggal mulai sudah lewat. Pengajuan mundur tanggal memerlukan persetujuan manajer/pemilik.
    </div>
  );
}

export function BackdatedBadge({ request }) {
  if (!request?.is_backdated) return null;
  return (
    <span title="Diajukan setelah tanggal mulai lewat — perlu ditinjau manajer/pemilik satu per satu"
      style={{ background: '#fed7aa', color: '#9a3412', padding: '0.1rem 0.4rem', borderRadius: 4, fontSize: '0.7rem', fontWeight: 700, marginLeft: 6, whiteSpace: 'nowrap' }}>
      MUNDUR TANGGAL
    </span>
  );
}
