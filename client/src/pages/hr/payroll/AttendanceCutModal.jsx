import { useEffect, useState } from 'react';
import { getPayrollLineAttendanceCuts } from '../../../api';
import { fmtIDR } from '../../../format';
import { fmtMin, fmtTime } from './shared';

// Per-day breakdown of a line's late-arrival / early-departure cut. The line's
// figure is frozen at generation; the days are recomputed from attendance now,
// so a correction made since shows up here as a mismatch until the line is
// regenerated.
export function AttendanceCutModal({ lineId, onClose }) {
  const [data, setData] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    getPayrollLineAttendanceCuts(lineId)
      .then(({ data }) => setData(data))
      .catch((err) => setError(err.response?.data?.error || 'Gagal memuat rincian'));
  }, [lineId]);

  const days = data?.live?.days || [];
  const th = { padding: '8px 10px', textAlign: 'right', fontWeight: 600, fontSize: 12, color: '#556' };
  const td = { padding: '7px 10px', textAlign: 'right', borderTop: '1px solid #eef0f4' };

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 200 }} onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} style={{ background: '#fff', borderRadius: 12, width: 860, maxWidth: '95vw', maxHeight: '90vh', display: 'flex', flexDirection: 'column', boxShadow: '0 8px 32px rgba(0,0,0,.18)' }}>
        <div style={{ padding: '16px 20px', borderBottom: '1px solid #e6e8ee', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div>
            <h2 style={{ margin: 0, fontSize: 18 }}>Potongan Telat / Pulang Awal</h2>
            {data && <div style={{ fontSize: 13, color: '#667', marginTop: 2 }}>{data.employee_name} · {data.employee_code} · upah harian {fmtIDR(data.daily_rate)}</div>}
          </div>
          <button onClick={onClose} style={{ background: 'none', border: 0, fontSize: 22, cursor: 'pointer', color: '#889' }}>×</button>
        </div>
        <div style={{ padding: 20, overflow: 'auto' }}>
          {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 12, borderRadius: 8 }}>{error}</div>}
          {!data && !error && <div style={{ color: '#889' }}>Memuat…</div>}
          {data && (
            <>
              <p style={{ margin: '0 0 12px', fontSize: 13, color: '#556' }}>
                Menit telat dan menit pulang awal masing-masing dibulatkan ke atas per {data.block_minutes} menit,
                lalu dihargai upah harian × menit ÷ panjang shift hari itu (maks. satu hari).
                Hari tanpa shift memakai jam cabang ({(data.branch_paid_minutes / 60).toLocaleString('id-ID')} jam).
              </p>
              {data.stale && (
                <div style={{ background: '#fff8e1', color: '#a06800', padding: 10, borderRadius: 8, marginBottom: 12, fontSize: 13 }}>
                  Absensi berubah sejak baris dibuat: tersimpan {fmtIDR(data.line_amount)}, dihitung ulang {fmtIDR(data.live.amount)}.
                  Regenerasi baris untuk memperbarui.
                </div>
              )}
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                  <tr style={{ background: '#f1f3f7' }}>
                    <th style={{ ...th, textAlign: 'left' }}>Tanggal</th>
                    <th style={{ ...th, textAlign: 'center' }}>Shift</th>
                    <th style={th}>Masuk</th>
                    <th style={th}>Pulang</th>
                    <th style={th}>Telat</th>
                    <th style={th}>Pulang Awal</th>
                    <th style={th}>Dipotong</th>
                    <th style={th}>Jam Kerja</th>
                    <th style={th}>Potongan</th>
                  </tr>
                </thead>
                <tbody>
                  {days.length === 0 ? (
                    <tr><td colSpan={9} style={{ ...td, textAlign: 'center', color: '#889' }}>Tidak ada hari telat atau pulang awal.</td></tr>
                  ) : days.map((d) => (
                    <tr key={d.date} style={d.half_day ? { color: '#889' } : undefined}>
                      <td style={{ ...td, textAlign: 'left' }}>{new Date(d.date + 'T00:00:00').toLocaleDateString('id-ID', { weekday: 'short', day: 'numeric', month: 'short' })}</td>
                      <td style={{ ...td, textAlign: 'center' }}>{d.shift_code || '—'}</td>
                      <td style={td}>{fmtTime(d.check_in)}</td>
                      <td style={td}>{fmtTime(d.check_out)}</td>
                      <td style={td}>{fmtMin(d.late_minutes)}</td>
                      <td style={td}>{fmtMin(d.early_minutes)}</td>
                      <td style={td}>{d.half_day ? 'setengah hari*' : `${d.cut_minutes} mnt${d.capped ? ' (maks)' : ''}`}</td>
                      <td style={td}>{(d.paid_minutes / 60).toLocaleString('id-ID', { maximumFractionDigits: 2 })} jam</td>
                      <td style={{ ...td, color: d.amount ? '#c5221f' : undefined, fontWeight: 600 }}>{d.half_day ? '—' : fmtIDR(d.amount)}</td>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr style={{ background: '#f8f9fb', fontWeight: 700 }}>
                    <td style={{ ...td, textAlign: 'left' }} colSpan={6}>Total</td>
                    <td style={td}>{data.live.minutes} mnt</td>
                    <td style={td}></td>
                    <td style={{ ...td, color: '#c5221f' }}>{fmtIDR(data.live.amount)}</td>
                  </tr>
                </tfoot>
              </table>
              {days.some((d) => d.half_day) && (
                <div style={{ fontSize: 12, color: '#889', marginTop: 8 }}>
                  * Hari yang dikoreksi menjadi setengah hari dipotong lewat Potongan Setengah Hari, bukan di sini — agar tidak terpotong dua kali.
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}
