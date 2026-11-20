import { useEffect, useState } from 'react';
import { getEmployeeAttendanceHistory } from '../../../api';
import { StatusChip, SourceBadge, fmtTime } from '../AttendanceDashboard';

const PERMIT_LABELS = { late_arrival: 'Izin masuk siang', early_departure: 'Izin pulang awal', off_site: 'Dinas luar' };

function historyNotes(d) {
  const notes = [];
  if (d.is_late) notes.push({ text: `Terlambat ${d.late_minutes} mnt`, warn: true });
  if (d.is_early_leave) notes.push({ text: `Pulang awal ${d.early_leave_minutes} mnt`, warn: true });
  if (d.is_missing_checkin) notes.push({ text: 'Tidak absen masuk', warn: true });
  if (d.is_missing_checkout) notes.push({ text: 'Tidak absen pulang', warn: true });
  if (d.is_half_day) notes.push({ text: 'Setengah hari', warn: true });
  if (d.alpha === 'quota') notes.push({ text: 'Alpha — memakai jatah libur', warn: true });
  if (d.alpha === 'unpaid') notes.push({ text: 'Alpha — tidak dibayar', warn: true });
  if (d.leave_type) notes.push({ text: d.leave_type });
  if (d.excused_by) notes.push({ text: PERMIT_LABELS[d.excused_by] || d.excused_by });
  if ((d.check_in && !d.check_in_verified) || (d.check_out && !d.check_out_verified)) {
    notes.push({ text: 'Jam dari perangkat (belum terverifikasi)', warn: true });
  }
  return notes;
}

function HistoryStat({ label, value, sub, color }) {
  return (
    <div style={{ padding: '0.6rem 0.8rem', borderRadius: 6, background: '#f6f8fb' }}>
      <div style={{ fontSize: '0.72rem', color: '#667', textTransform: 'uppercase', letterSpacing: '0.03em' }}>{label}</div>
      <div style={{ fontSize: '1.35rem', fontWeight: 700, color: color || '#223' }}>{value}</div>
      {sub && <div style={{ fontSize: '0.75rem', color: '#889' }}>{sub}</div>}
    </div>
  );
}

export function HistoryTab({ employeeId }) {
  const [hist, setHist] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let alive = true;
    getEmployeeAttendanceHistory(employeeId)
      .then(r => { if (alive) { setHist(r.data); setError(''); } })
      .catch(err => { if (alive) setError(err.response?.data?.error || 'Gagal memuat riwayat kehadiran'); });
    return () => { alive = false; };
  }, [employeeId]);

  if (error) return <div className="error-msg">{error}</div>;
  if (!hist) return <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>;

  const s = hist.summary;
  const fmtDay = (iso) => new Date(`${iso}T00:00:00`).toLocaleDateString('id-ID', { weekday: 'short', day: '2-digit', month: 'short' });

  return (
    <div className="card">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', flexWrap: 'wrap', gap: '0.5rem', marginBottom: '0.75rem' }}>
        <h3 style={{ margin: 0, fontSize: '1rem' }}>30 Hari Terakhir</h3>
        <span style={{ fontSize: '0.82rem', color: '#889' }}>{fmtDay(hist.from)} – {fmtDay(hist.to)} · sama dengan yang dilihat karyawan di aplikasi</span>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))', gap: '0.6rem', marginBottom: '1rem' }}>
        <HistoryStat label="Hadir" value={s.present} sub={`dari ${s.days} hari`} color="#2e7d32" />
        <HistoryStat label="Terlambat" value={s.late} sub={s.late_minutes ? `total ${s.late_minutes} mnt` : null} color={s.late ? '#a06800' : undefined} />
        <HistoryStat label="Pulang Awal" value={s.early_leave} color={s.early_leave ? '#a06800' : undefined} />
        <HistoryStat label="Tidak Absen Pulang" value={s.missing_checkout} color={s.missing_checkout ? '#a06800' : undefined} />
        <HistoryStat label="Alpha" value={s.alpha} sub={s.alpha_unpaid ? `${s.alpha_unpaid} tidak dibayar` : null} color={s.alpha ? '#c62828' : undefined} />
        <HistoryStat label="Libur" value={s.leave} color="#1565c0" />
      </div>

      <div style={{ overflowX: 'auto' }}>
        <table style={{ width: '100%' }}>
          <thead>
            <tr><th>Tanggal</th><th>Shift</th><th>Masuk</th><th>Pulang</th><th>Status</th><th>Keterangan</th></tr>
          </thead>
          <tbody>
            {hist.days.map(d => {
              const notes = historyNotes(d);
              return (
                <tr key={d.date}>
                  <td style={{ whiteSpace: 'nowrap' }}>{fmtDay(d.date)}</td>
                  <td style={{ fontSize: '0.82rem', color: '#556' }}>{d.shift || '—'}</td>
                  <td style={{ whiteSpace: 'nowrap' }}>{fmtTime(d.check_in)} <SourceBadge source={d.check_in_source} /></td>
                  <td style={{ whiteSpace: 'nowrap' }}>{fmtTime(d.check_out)} <SourceBadge source={d.check_out_source} /></td>
                  <td>{d.status ? <StatusChip status={d.status} /> : <span style={{ color: '#aab', fontSize: '0.82rem' }}>Tidak ada catatan</span>}</td>
                  <td style={{ fontSize: '0.82rem' }}>
                    {notes.length === 0 ? <span style={{ color: '#ccc' }}>—</span> : notes.map((n, i) => (
                      <span key={i} style={{ color: n.warn ? '#b3261e' : '#556' }}>{i > 0 && ' · '}{n.text}</span>
                    ))}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
