import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import CurrencyInput from '../../components/CurrencyInput';
import { getShifts, getWorkSchedules, simulateAttendanceCut } from '../../api';

const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));
const hm = (s) => (s ? String(s).slice(0, 5) : '');
const hours = (m) => (Number(m || 0) / 60).toLocaleString('id-ID', { maximumFractionDigits: 2 });

// Typical days to try against whichever shift is loaded, as offsets in minutes
// from the shift's own start / end so they make sense on any shift.
const QUICK = [
  { label: 'Tepat waktu', in: 0, out: 0 },
  { label: 'Telat 6 mnt', in: 6, out: 0 },
  { label: 'Telat 2 jam', in: 120, out: 0 },
  { label: 'Pulang 2 jam awal', in: 0, out: -120 },
  { label: 'Telat 10 + pulang 25 awal', in: 10, out: -25 },
  { label: 'Scan masuk lalu pulang', in: 0, out: 'in+1' },
];

const toMin = (s) => { const [h, m] = (s || '0:0').split(':').map(Number); return h * 60 + m; };
const toClock = (m) => `${String(Math.floor(((m % 1440) + 1440) % 1440 / 60)).padStart(2, '0')}:${String((((m % 1440) + 1440) % 1440) % 60).padStart(2, '0')}`;

const field = { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13, color: '#556' };
const input = { padding: '8px 10px', border: '1px solid #d5d9e2', borderRadius: 8, fontSize: 14 };
const card = { background: '#fff', border: '1px solid #e6e8ee', borderRadius: 12, padding: 20 };

// A calculator over the real payroll rule: the server runs the hand-entered day
// through the same anomaly detection and pricing payroll uses, so what it shows
// is exactly what a payroll line would deduct for that day.
export default function AttendanceCutSimulator() {
  const [shifts, setShifts] = useState([]);
  const [schedules, setSchedules] = useState([]);
  const [shiftId, setShiftId] = useState('');
  const [form, setForm] = useState({
    daily_rate: 60000, start1: '08:30', end1: '19:30', start2: '', end2: '',
    break_minutes: 0, grace_minutes: 5, early_leave_minutes: 10,
    check_in: '08:30', check_out: '19:30', days: 26,
  });
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    getShifts().then(({ data }) => setShifts(Array.isArray(data) ? data : [])).catch(() => {});
    getWorkSchedules().then(({ data }) => setSchedules(Array.isArray(data) ? data : [])).catch(() => {});
  }, []);

  const set = (k) => (e) => setForm((f) => ({ ...f, [k]: e.target.value }));

  const pickShift = (id) => {
    setShiftId(id);
    const s = shifts.find((x) => x.id === id);
    if (!s) return;
    const ws = schedules.find((w) => w.branch_id === s.branch_id);
    setForm((f) => ({
      ...f,
      start1: hm(s.start1), end1: hm(s.end1), start2: hm(s.start2), end2: hm(s.end2),
      break_minutes: s.break_minutes || 0,
      grace_minutes: ws ? ws.grace_minutes : f.grace_minutes,
      early_leave_minutes: ws ? ws.early_leave_minutes : f.early_leave_minutes,
      check_in: hm(s.start1), check_out: hm(s.end2 || s.end1),
    }));
  };

  const quick = (q) => {
    const start = toMin(form.start1);
    const end = toMin(form.end2 || form.end1);
    const inM = start + q.in;
    const outM = q.out === 'in+1' ? inM + 1 : end + q.out;
    setForm((f) => ({ ...f, check_in: toClock(inM), check_out: toClock(outM) }));
  };

  useEffect(() => {
    const t = setTimeout(() => {
      simulateAttendanceCut({
        daily_rate: Number(form.daily_rate) || 0,
        start1: form.start1, end1: form.end1, start2: form.start2, end2: form.end2,
        break_minutes: Number(form.break_minutes) || 0,
        grace_minutes: Number(form.grace_minutes) || 0,
        early_leave_minutes: Number(form.early_leave_minutes) || 0,
        check_in: form.check_in, check_out: form.check_out,
      })
        .then(({ data }) => { setResult(data); setError(''); })
        .catch((err) => { setResult(null); setError(err.response?.data?.error || 'Gagal menghitung'); });
    }, 250);
    return () => clearTimeout(t);
  }, [form.daily_rate, form.start1, form.end1, form.start2, form.end2, form.break_minutes,
      form.grace_minutes, form.early_leave_minutes, form.check_in, form.check_out]);

  const r = result?.result;
  const days = Number(form.days) || 0;

  return (
    <div style={{ padding: 24, maxWidth: 1000, margin: '0 auto' }}>
      <div style={{ marginBottom: 16 }}>
        <Link to="/hr/payroll" style={{ color: '#1967d2', fontSize: 13, textDecoration: 'none' }}>← Penggajian</Link>
        <h1 style={{ margin: '4px 0 4px', fontSize: 24 }}>Simulasi Potongan Telat / Pulang Awal</h1>
        <p style={{ margin: 0, color: '#667', fontSize: 14 }}>
          Telat dan pulang awal dihitung sama: upah untuk menit yang tidak dikerjakan. Hasil di sini memakai aturan yang sama dengan penggajian.
        </p>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: 16 }}>
        <div style={card}>
          <h2 style={{ margin: '0 0 12px', fontSize: 16 }}>Shift & upah</h2>
          <label style={{ ...field, marginBottom: 12 }}>Ambil dari shift
            <select value={shiftId} onChange={(e) => pickShift(e.target.value)} style={input}>
              <option value="">— isi manual —</option>
              {shifts.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.branch_name} · {s.code} {s.name} ({hm(s.start1)}–{hm(s.end1)}{s.start2 ? `, ${hm(s.start2)}–${hm(s.end2)}` : ''})
                </option>
              ))}
            </select>
          </label>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
            <label style={field}>Masuk<input type="time" value={form.start1} onChange={set('start1')} style={input} /></label>
            <label style={field}>Pulang<input type="time" value={form.end1} onChange={set('end1')} style={input} /></label>
            <label style={field}>Masuk segmen 2 <input type="time" value={form.start2} onChange={set('start2')} style={input} /></label>
            <label style={field}>Pulang segmen 2<input type="time" value={form.end2} onChange={set('end2')} style={input} /></label>
            <label style={field}>Istirahat tak dibayar (mnt)<input type="number" min="0" value={form.break_minutes} onChange={set('break_minutes')} style={input} /></label>
            <label style={field}>Upah harian<CurrencyInput value={form.daily_rate} onChange={set('daily_rate')} style={input} /></label>
            <label style={field}>Toleransi telat (mnt)<input type="number" min="0" value={form.grace_minutes} onChange={set('grace_minutes')} style={input} /></label>
            <label style={field}>Toleransi pulang (mnt)<input type="number" min="0" value={form.early_leave_minutes} onChange={set('early_leave_minutes')} style={input} /></label>
          </div>
        </div>

        <div style={card}>
          <h2 style={{ margin: '0 0 12px', fontSize: 16 }}>Absensi hari itu</h2>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginBottom: 12 }}>
            <label style={field}>Scan masuk<input type="time" value={form.check_in} onChange={set('check_in')} style={input} /></label>
            <label style={field}>Scan pulang<input type="time" value={form.check_out} onChange={set('check_out')} style={input} /></label>
          </div>
          <div style={{ fontSize: 13, color: '#556', marginBottom: 6 }}>Contoh cepat</div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
            {QUICK.map((q) => (
              <button key={q.label} onClick={() => quick(q)}
                style={{ background: '#f1f3f7', border: '1px solid #e0e3ea', borderRadius: 16, padding: '5px 12px', fontSize: 13, cursor: 'pointer' }}>
                {q.label}
              </button>
            ))}
          </div>
          <label style={{ ...field, marginTop: 14 }}>Jika terulang tiap hari selama (hari)
            <input type="number" min="0" max="31" value={form.days} onChange={set('days')} style={{ ...input, width: 100 }} />
          </label>
        </div>
      </div>

      <div style={{ ...card, marginTop: 16 }}>
        <h2 style={{ margin: '0 0 12px', fontSize: 16 }}>Hasil</h2>
        {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 12, borderRadius: 8 }}>{error}</div>}
        {result && r && (
          <>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', gap: 12, marginBottom: 16 }}>
              <Stat label="Potongan hari ini" value={fmtIDR(r.amount)} tone={r.amount ? 'bad' : 'good'} />
              <Stat label="Upah dibayar hari ini" value={fmtIDR(result.net_day_pay)} />
              <Stat label={`Jika ${days} hari`} value={fmtIDR(r.amount * days)} tone={r.amount ? 'bad' : undefined} />
              <Stat label="Nilai per jam" value={fmtIDR(r.hourly_rate)} sub={`${hours(result.paid_minutes)} jam kerja dibayar`} />
            </div>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
              <tbody>
                <Row k="Telat" v={result.is_late ? `${result.late_minutes} mnt → dibulatkan ${r.late_cut_minutes} mnt` : `tidak (dalam toleransi ${form.grace_minutes} mnt)`} />
                <Row k="Pulang awal" v={result.is_early_leave ? `${result.early_leave_minutes} mnt → dibulatkan ${r.early_cut_minutes} mnt` : `tidak (dalam toleransi ${form.early_leave_minutes} mnt)`} />
                <Row k="Total dipotong" v={`${r.cut_minutes} mnt dari ${result.paid_minutes} mnt${r.capped ? ' — maksimal satu hari' : ''}`} />
                <Row k="Rumus" v={`${fmtIDR(form.daily_rate)} × ${r.cut_minutes} ÷ ${result.paid_minutes} = ${fmtIDR(r.amount)}`} />
                {(result.is_missing_checkout || result.is_missing_checkin || result.is_no_punch) && (
                  <Row k="Catatan" v={
                    result.is_no_punch ? 'Tidak ada scan sama sekali — tidak dipotong jam, ditangani lewat aturan kinerja.'
                      : result.is_missing_checkout ? 'Tidak ada scan pulang — pulang awal tidak bisa dihitung; ditangani lewat aturan "lupa absen pulang".'
                        : 'Tidak ada scan masuk — telat tidak bisa dihitung.'
                  } />
                )}
              </tbody>
            </table>
            <p style={{ margin: '12px 0 0', fontSize: 12, color: '#889' }}>
              Setiap ujung hari dibulatkan ke atas per {result.block_minutes} menit secara terpisah. Hari yang dikoreksi menjadi setengah hari tidak dihitung di sini — dipotong lewat Potongan Setengah Hari.
            </p>
          </>
        )}
      </div>
    </div>
  );
}

function Stat({ label, value, sub, tone }) {
  const color = tone === 'bad' ? '#c5221f' : tone === 'good' ? '#1e7e34' : '#223';
  return (
    <div style={{ background: '#f8f9fb', borderRadius: 10, padding: '12px 14px' }}>
      <div style={{ fontSize: 12, color: '#667' }}>{label}</div>
      <div style={{ fontSize: 20, fontWeight: 700, color }}>{value}</div>
      {sub && <div style={{ fontSize: 12, color: '#889' }}>{sub}</div>}
    </div>
  );
}

function Row({ k, v }) {
  return (
    <tr style={{ borderTop: '1px solid #eef0f4' }}>
      <td style={{ padding: '8px 0', color: '#667', width: 160 }}>{k}</td>
      <td style={{ padding: '8px 0' }}>{v}</td>
    </tr>
  );
}
