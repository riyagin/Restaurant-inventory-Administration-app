import { useEffect, useState } from 'react';
import { getLeaveBalance, getEmployeeLeaveRequests } from '../../../api';
import { todayLocal, fmtMonth, fmtPeriodRange, periodOf } from '../leaveRules';
import { BackdatedBadge } from '../LeaveRuleWidgets';
import { UnusedChip, UsedDaysCell } from '../LeaveBalancesPanel';
import { fmtDateShort } from './shared';

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

export function CutiTab({ employeeId }) {
  const thisYear = new Date().getFullYear();
  const [year, setYear] = useState(thisYear);
  const [balance, setBalance] = useState(null);
  const [requests, setRequests] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getLeaveBalance(employeeId, year).then(r => setBalance(r.data)).catch(() => setBalance(null));
  }, [employeeId, year]);

  useEffect(() => {
    setLoading(true);
    getEmployeeLeaveRequests(employeeId).then(r => setRequests(r.data || [])).catch(() => setRequests([]))
      .finally(() => setLoading(false));
  }, [employeeId]);

  const months = balance?.months || [];
  // The running allowance period (26th–25th with the default cut-off).
  const thisMonth = periodOf(todayLocal(), balance?.cutoff_day ?? 0);
  const current = months.find(m => m.month === thisMonth);

  return (
    <div>
      {current && (
        <div className="card" style={{ marginBottom: '1rem' }}>
          <h3 style={{ margin: '0 0 0.75rem', fontSize: '1rem' }}>Jatah Libur {fmtMonth(thisMonth)}
            {fmtPeriodRange(current) && <span style={{ fontWeight: 400, fontSize: '0.85rem', color: '#667' }}> · {fmtPeriodRange(current)}</span>}
          </h3>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '1rem' }}>
            <BalanceStat label="Jatah" value={current.quota_days} color="#2563eb" />
            <BalanceStat label="Terpakai" value={current.used_days} color="#a06800" />
            <BalanceStat label="Sisa" value={current.unused_days} color={current.unused_days > 0 ? '#1e7e34' : '#c5221f'} />
          </div>
          <p style={{ fontSize: '0.78rem', color: '#888', margin: '0.6rem 0 0' }}>
            {balance.default_quota} hari libur berbayar per bulan; Sabtu/Minggu dihitung {balance.weekend_cost} hari. Periode mengikuti tanggal tutup gaji. Sisa tidak terbawa ke periode berikutnya.
            {' '}Alpha (tidak masuk tanpa izin) memakai jatah yang sama; bila jatah tidak cukup, hari itu tidak dibayar.
          </p>
          {current.alpha_days > 0 && (
            <p style={{ fontSize: '0.85rem', color: '#c5221f', margin: '0.4rem 0 0' }}>
              Bulan ini: {current.alpha_days} hari alpha — {current.alpha_used_days} hari jatah terpakai
              {current.alpha_unpaid_days > 0 && `, ${current.alpha_unpaid_days} hari tidak dibayar`}.
            </p>
          )}
        </div>
      )}

      <div className="card" style={{ marginBottom: '1rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem', gap: '0.5rem', flexWrap: 'wrap' }}>
          <h3 style={{ margin: 0, fontSize: '1rem' }}>Sisa Jatah Libur per Bulan</h3>
          <select value={year} onChange={e => setYear(Number(e.target.value))} style={{ padding: '0.3rem 0.5rem', border: '1px solid #d4d9e2', borderRadius: 6 }}>
            {[thisYear - 2, thisYear - 1, thisYear, thisYear + 1].map(y => <option key={y} value={y}>{y}</option>)}
          </select>
        </div>
        {balance && (
          <p style={{ fontSize: '0.88rem', margin: '0 0 0.6rem' }}>
            Total sisa bulan yang sudah ditutup: <strong>{balance.unused_closed_total}</strong> hari
            <span style={{ color: '#888', fontSize: '0.8rem' }}> — biasanya dikonversi menjadi uang saat evaluasi</span>
          </p>
        )}
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%' }}>
            <thead>
              <tr><th>Bulan</th><th style={{ textAlign: 'center' }}>Jatah</th><th style={{ textAlign: 'center' }}>Terpakai</th><th style={{ textAlign: 'center' }}>Sisa</th><th>Status</th></tr>
            </thead>
            <tbody>
              {months.map(m => (
                <tr key={m.month}>
                  <td>{fmtMonth(m.month)}{fmtPeriodRange(m) && <div style={{ fontSize: '0.74rem', color: '#889' }}>{fmtPeriodRange(m)}</div>}</td>
                  <td style={{ textAlign: 'center' }}>{m.quota_days}{m.overridden && <span title="Jatah bulan ini diatur khusus" style={{ color: '#889' }}> *</span>}</td>
                  <td style={{ textAlign: 'center' }}><UsedDaysCell balance={m} /></td>
                  <td style={{ textAlign: 'center' }}><UnusedChip days={m.unused_days} closed={m.closed} /></td>
                  <td style={{ fontSize: '0.82rem', color: '#667' }}>{m.closed ? 'Final' : m.month === thisMonth ? 'Berjalan' : 'Akan datang'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="card">
        <h3 style={{ margin: '0 0 0.75rem', fontSize: '1rem' }}>Riwayat Pengajuan Libur</h3>
        {loading ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>
        ) : requests.length === 0 ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Belum ada pengajuan libur.</p>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%' }}>
              <thead>
                <tr><th>Jenis</th><th>Mulai</th><th>Selesai</th><th style={{ textAlign: 'center' }}>Hari</th><th>Status</th><th>Alasan</th></tr>
              </thead>
              <tbody>
                {requests.map(rq => (
                  <tr key={rq.id}>
                    <td>{rq.leave_type_name}{!rq.is_paid && <span style={{ fontSize: '0.72rem', color: '#c5221f' }}> (tanpa gaji)</span>}<BackdatedBadge request={rq} /></td>
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
