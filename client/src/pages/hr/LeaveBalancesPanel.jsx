import { useEffect, useState } from 'react';
import { getLeaveBalances } from '../../api';
import { fmtMonth, todayLocal, WEEKEND_COST } from './leaveRules';

// Unused-leave indicator: every active employee's paid-leave allowance for one
// month. Unused days do not carry over; once the month has closed they are the
// figure evaluation converts to cash.
export default function LeaveBalancesPanel() {
  const [month, setMonth] = useState(todayLocal().slice(0, 7));
  // The answer remembers which month it is for; loading is "not for this month yet".
  const [result, setResult] = useState({ month: null, data: null, error: '' });

  useEffect(() => {
    if (!month) return;
    getLeaveBalances(month)
      .then(r => setResult({ month, data: r.data, error: '' }))
      .catch(() => setResult({ month, data: null, error: 'Gagal memuat sisa jatah libur.' }));
  }, [month]);

  const loading = !!month && result.month !== month;
  const data = loading ? null : result.data;
  const error = loading ? '' : result.error;

  const rows = data?.employees || [];
  const totalUnused = rows.reduce((s, r) => s + r.unused_days, 0);
  const withUnused = rows.filter(r => r.unused_days > 0).length;

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap', marginBottom: '0.75rem' }}>
        <label style={{ fontSize: '0.85rem', color: '#667' }}>Bulan:</label>
        <input type="month" value={month} onChange={e => setMonth(e.target.value)}
          style={{ padding: '0.4rem 0.5rem', border: '1px solid #d4d9e2', borderRadius: 6 }} />
        {data && (
          <span style={{ fontSize: '0.85rem', color: '#445' }}>
            {data.closed ? 'Bulan sudah ditutup — sisa bersifat final.' : 'Bulan berjalan — sisa masih dapat berubah.'}
          </span>
        )}
      </div>

      <p style={{ fontSize: '0.8rem', color: '#888', margin: '0 0 0.75rem' }}>
        Jatah libur berbayar {data?.default_quota ?? 4} hari per bulan; Sabtu/Minggu dihitung {WEEKEND_COST} hari.
        Sisa jatah tidak terbawa ke bulan berikutnya dan biasanya dikonversi menjadi uang saat evaluasi.
      </p>

      {data && rows.length > 0 && (
        <div style={{ display: 'flex', gap: '1.5rem', marginBottom: '0.75rem', fontSize: '0.9rem' }}>
          <span>Total sisa {fmtMonth(data.month)}: <strong>{totalUnused}</strong> hari</span>
          <span><strong>{withUnused}</strong> dari {rows.length} karyawan belum memakai seluruh jatah</span>
        </div>
      )}

      {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: '0.5rem 0.75rem', borderRadius: 6, fontSize: '0.85rem', marginBottom: '0.5rem' }}>{error}</div>}

      <div className="card" style={{ padding: 0, overflowX: 'auto' }}>
        <table style={{ width: '100%' }}>
          <thead>
            <tr>
              <th>Cabang</th><th>Karyawan</th>
              <th style={{ textAlign: 'center', width: 80 }}>Jatah</th>
              <th style={{ textAlign: 'center', width: 90 }}>Terpakai</th>
              <th style={{ textAlign: 'center', width: 110 }}>Sisa</th>
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={5} style={{ textAlign: 'center', color: '#888', padding: '1.5rem' }}>Memuat…</td></tr>}
            {!loading && rows.length === 0 && <tr><td colSpan={5} style={{ textAlign: 'center', color: '#888', padding: '1.5rem' }}>Tidak ada karyawan aktif.</td></tr>}
            {!loading && rows.map(r => (
              <tr key={r.employee_id}>
                <td style={{ fontSize: '0.88rem', color: '#556' }}>{r.branch_name}</td>
                <td>{r.employee_name}<div style={{ fontSize: '0.78rem', color: '#889' }}>{r.employee_code}</div></td>
                <td style={{ textAlign: 'center' }}>
                  {r.quota_days}{r.overridden && <span title="Jatah bulan ini diatur khusus" style={{ color: '#889' }}> *</span>}
                </td>
                <td style={{ textAlign: 'center' }}>{r.used_days}</td>
                <td style={{ textAlign: 'center' }}>
                  <UnusedChip days={r.unused_days} closed={r.closed} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// UnusedChip: green when nothing is left over, amber when days remain — on a
// closed month that amber is what evaluation pays out.
export function UnusedChip({ days, closed }) {
  const left = days > 0;
  const style = left
    ? { background: '#fff4e5', color: '#9a3412' }
    : { background: '#e6f4ea', color: '#1e7e34' };
  return (
    <span title={left ? (closed ? 'Belum terpakai — dikonversi saat evaluasi' : 'Masih tersedia bulan ini') : 'Jatah terpakai seluruhnya'}
      style={{ ...style, padding: '0.15rem 0.55rem', borderRadius: 4, fontWeight: 600, fontSize: '0.82rem' }}>
      {days} hari
    </span>
  );
}
