import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getBranches, getWorkSchedules, saveWorkSchedule,
  getPublicHolidays, createPublicHoliday, deletePublicHoliday,
  getAttendanceDevices, createAttendanceDevice, setAttendanceDeviceActive, deleteAttendanceDevice,
} from '../../api';

const ISO_DAYS = [
  { n: 1, label: 'Sen' }, { n: 2, label: 'Sel' }, { n: 3, label: 'Rab' },
  { n: 4, label: 'Kam' }, { n: 5, label: 'Jum' }, { n: 6, label: 'Sab' }, { n: 7, label: 'Min' },
];

// pgtype.Time serializes as microseconds; convert to HH:MM for the inputs.
const microsToHHMM = (t) => {
  if (t == null) return '08:00';
  const micros = typeof t === 'object' ? t.Microseconds ?? t.microseconds : t;
  if (micros == null) return '08:00';
  const totalMin = Math.floor(Number(micros) / 1_000_000 / 60);
  const h = String(Math.floor(totalMin / 60)).padStart(2, '0');
  const m = String(totalMin % 60).padStart(2, '0');
  return `${h}:${m}`;
};

function ScheduleSection({ branches }) {
  const [schedules, setSchedules] = useState([]);
  const [form, setForm] = useState(null);
  const [msg, setMsg] = useState('');

  const load = () => getWorkSchedules().then(r => setSchedules(r.data || [])).catch(() => {});
  useEffect(() => { load(); }, []);

  const byBranch = {};
  schedules.forEach(s => { byBranch[s.branch_id] = s; });

  const startEdit = (branch) => {
    const s = byBranch[branch.id];
    setForm({
      branch_id: branch.id,
      branch_name: branch.name,
      work_start: s ? microsToHHMM(s.work_start) : '08:00',
      work_end: s ? microsToHHMM(s.work_end) : '17:00',
      grace_minutes: s ? s.grace_minutes : 15,
      early_leave_minutes: s ? s.early_leave_minutes : 30,
      work_days: s && s.work_days ? [...s.work_days] : [1, 2, 3, 4, 5, 6],
    });
  };

  const toggleDay = (n) => {
    setForm(f => ({ ...f, work_days: f.work_days.includes(n) ? f.work_days.filter(d => d !== n) : [...f.work_days, n].sort() }));
  };

  const save = async () => {
    setMsg('');
    try {
      await saveWorkSchedule({
        branch_id: form.branch_id,
        work_start: form.work_start,
        work_end: form.work_end,
        grace_minutes: Number(form.grace_minutes),
        early_leave_minutes: Number(form.early_leave_minutes),
        work_days: form.work_days,
      });
      setForm(null);
      load();
      setMsg('Jadwal kerja tersimpan.');
    } catch (e) {
      setMsg(e.response?.data?.error || 'Gagal menyimpan jadwal');
    }
  };

  return (
    <div className="card" style={{ marginBottom: '1.5rem' }}>
      <h2 style={{ fontSize: '1.05rem', marginBottom: '0.75rem' }}>Jadwal Kerja per Cabang</h2>
      {msg && <div style={{ color: '#1b5e45', fontSize: '0.85rem', marginBottom: '0.5rem' }}>{msg}</div>}
      <table>
        <thead><tr><th>Cabang</th><th>Masuk</th><th>Pulang</th><th>Toleransi</th><th>Pulang Awal</th><th>Hari Kerja</th><th></th></tr></thead>
        <tbody>
          {branches.map(b => {
            const s = byBranch[b.id];
            return (
              <tr key={b.id}>
                <td style={{ fontWeight: 500 }}>{b.name}</td>
                <td>{s ? microsToHHMM(s.work_start) : <span style={{ color: '#bbb' }}>default 08:00</span>}</td>
                <td>{s ? microsToHHMM(s.work_end) : <span style={{ color: '#bbb' }}>default 17:00</span>}</td>
                <td>{s ? `${s.grace_minutes} mnt` : <span style={{ color: '#bbb' }}>15 mnt</span>}</td>
                <td>{s ? `${s.early_leave_minutes} mnt` : <span style={{ color: '#bbb' }}>30 mnt</span>}</td>
                <td style={{ fontSize: '0.8rem' }}>
                  {(s && s.work_days ? s.work_days : [1, 2, 3, 4, 5, 6]).map(n => ISO_DAYS.find(d => d.n === n)?.label).join(', ')}
                </td>
                <td><button className="btn btn-secondary btn-sm" onClick={() => startEdit(b)}>Atur</button></td>
              </tr>
            );
          })}
        </tbody>
      </table>

      {form && (
        <div style={{ marginTop: '1rem', borderTop: '1px solid #eee', paddingTop: '1rem' }}>
          <h3 style={{ fontSize: '0.95rem' }}>Atur Jadwal — {form.branch_name}</h3>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.85rem', alignItems: 'flex-end', marginTop: '0.5rem' }}>
            <div><label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Jam Masuk</label>
              <input type="time" value={form.work_start} onChange={e => setForm({ ...form, work_start: e.target.value })} /></div>
            <div><label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Jam Pulang</label>
              <input type="time" value={form.work_end} onChange={e => setForm({ ...form, work_end: e.target.value })} /></div>
            <div><label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Toleransi (mnt)</label>
              <input type="number" min="0" value={form.grace_minutes} onChange={e => setForm({ ...form, grace_minutes: e.target.value })} style={{ width: '80px' }} /></div>
            <div><label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Batas Pulang Awal (mnt)</label>
              <input type="number" min="0" value={form.early_leave_minutes} onChange={e => setForm({ ...form, early_leave_minutes: e.target.value })} style={{ width: '80px' }} /></div>
          </div>
          <div style={{ marginTop: '0.75rem' }}>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888', marginBottom: '0.3rem' }}>Hari Kerja</label>
            <div style={{ display: 'flex', gap: '0.4rem' }}>
              {ISO_DAYS.map(d => (
                <button key={d.n} type="button" onClick={() => toggleDay(d.n)}
                  style={{ padding: '0.3rem 0.6rem', borderRadius: '4px', border: '1px solid #ccc', cursor: 'pointer',
                    background: form.work_days.includes(d.n) ? '#1a56b0' : '#fff', color: form.work_days.includes(d.n) ? '#fff' : '#555' }}>
                  {d.label}
                </button>
              ))}
            </div>
          </div>
          <div style={{ marginTop: '1rem', display: 'flex', gap: '0.5rem' }}>
            <button className="btn btn-primary btn-sm" onClick={save}>Simpan</button>
            <button className="btn btn-secondary btn-sm" onClick={() => setForm(null)}>Batal</button>
          </div>
        </div>
      )}
    </div>
  );
}

function HolidaySection() {
  const [holidays, setHolidays] = useState([]);
  const [date, setDate] = useState('');
  const [name, setName] = useState('');
  const [msg, setMsg] = useState('');

  const load = () => getPublicHolidays().then(r => setHolidays(r.data || [])).catch(() => {});
  useEffect(() => { load(); }, []);

  const add = async () => {
    setMsg('');
    try {
      await createPublicHoliday({ date, name });
      setDate(''); setName(''); load();
    } catch (e) {
      setMsg(e.response?.data?.error || 'Gagal menambah hari libur');
    }
  };
  const remove = async (id) => {
    if (!window.confirm('Hapus hari libur ini?')) return;
    await deletePublicHoliday(id); load();
  };

  return (
    <div className="card" style={{ marginBottom: '1.5rem' }}>
      <h2 style={{ fontSize: '1.05rem', marginBottom: '0.75rem' }}>Hari Libur Nasional</h2>
      <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'flex-end', marginBottom: '0.75rem' }}>
        <div><label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Tanggal</label>
          <input type="date" value={date} onChange={e => setDate(e.target.value)} /></div>
        <div style={{ flex: 1 }}><label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Nama</label>
          <input value={name} onChange={e => setName(e.target.value)} placeholder="cth. Hari Kemerdekaan" style={{ width: '100%' }} /></div>
        <button className="btn btn-primary btn-sm" onClick={add} disabled={!date || !name.trim()}>Tambah</button>
      </div>
      {msg && <div className="error-msg" style={{ marginBottom: '0.5rem' }}>{msg}</div>}
      <table>
        <thead><tr><th>Tanggal</th><th>Nama</th><th></th></tr></thead>
        <tbody>
          {holidays.length === 0 ? (
            <tr><td colSpan="3" style={{ color: '#999' }}>Belum ada hari libur.</td></tr>
          ) : holidays.map(h => (
            <tr key={h.id}>
              <td>{new Date(h.date).toLocaleDateString('id-ID')}</td>
              <td>{h.name}</td>
              <td><button className="btn btn-secondary btn-sm" onClick={() => remove(h.id)}>Hapus</button></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function DeviceSection({ branches }) {
  const [devices, setDevices] = useState([]);
  const [name, setName] = useState('');
  const [branchId, setBranchId] = useState('');
  const [newKey, setNewKey] = useState(null); // { api_key, name }
  const [msg, setMsg] = useState('');

  const load = () => getAttendanceDevices().then(r => setDevices(r.data || [])).catch(() => {});
  useEffect(() => { load(); }, []);

  const register = async () => {
    setMsg('');
    try {
      const r = await createAttendanceDevice({ name, branch_id: branchId || undefined });
      setNewKey({ api_key: r.data.api_key, name: r.data.name });
      setName(''); setBranchId(''); load();
    } catch (e) {
      setMsg(e.response?.data?.error || 'Gagal mendaftarkan perangkat');
    }
  };

  const toggle = async (d) => { await setAttendanceDeviceActive(d.id, { is_active: !d.is_active }); load(); };
  const remove = async (id) => { if (window.confirm('Hapus perangkat ini?')) { try { await deleteAttendanceDevice(id); load(); } catch (e) { setMsg(e.response?.data?.error || 'Gagal menghapus'); } } };

  return (
    <div className="card">
      <h2 style={{ fontSize: '1.05rem', marginBottom: '0.75rem' }}>Perangkat Absensi</h2>
      <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'flex-end', marginBottom: '0.75rem' }}>
        <div style={{ flex: 1 }}><label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Nama Perangkat</label>
          <input value={name} onChange={e => setName(e.target.value)} placeholder="cth. Tablet Cabang Pusat" style={{ width: '100%' }} /></div>
        <div><label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Cabang</label>
          <select value={branchId} onChange={e => setBranchId(e.target.value)}>
            <option value="">— pilih —</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select></div>
        <button className="btn btn-primary btn-sm" onClick={register} disabled={!name.trim()}>Daftarkan</button>
      </div>
      {msg && <div className="error-msg" style={{ marginBottom: '0.5rem' }}>{msg}</div>}
      <table>
        <thead><tr><th>Nama</th><th>Cabang</th><th>Status</th><th></th></tr></thead>
        <tbody>
          {devices.length === 0 ? (
            <tr><td colSpan="4" style={{ color: '#999' }}>Belum ada perangkat.</td></tr>
          ) : devices.map(d => (
            <tr key={d.id}>
              <td style={{ fontWeight: 500 }}>{d.name}</td>
              <td>{d.branch_name || <span style={{ color: '#bbb' }}>—</span>}</td>
              <td>
                <span style={{ background: d.is_active ? '#e8f5e9' : '#fdecea', color: d.is_active ? '#2e7d32' : '#c62828', padding: '0.1rem 0.5rem', borderRadius: '4px', fontSize: '0.78rem', fontWeight: 600 }}>
                  {d.is_active ? 'Aktif' : 'Nonaktif'}
                </span>
              </td>
              <td style={{ display: 'flex', gap: '0.4rem' }}>
                <button className="btn btn-secondary btn-sm" onClick={() => toggle(d)}>{d.is_active ? 'Nonaktifkan' : 'Aktifkan'}</button>
                <button className="btn btn-secondary btn-sm" onClick={() => remove(d.id)}>Hapus</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {newKey && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div className="card" style={{ maxWidth: '480px', width: '90%' }}>
            <h3 style={{ marginTop: 0 }}>Kunci API Perangkat</h3>
            <p style={{ fontSize: '0.85rem', color: '#c62828', fontWeight: 600 }}>
              Salin kunci ini sekarang. Kunci TIDAK akan ditampilkan lagi.
            </p>
            <div style={{ fontSize: '0.8rem', color: '#888', marginBottom: '0.5rem' }}>Perangkat: {newKey.name}</div>
            <div style={{ display: 'flex', gap: '0.5rem' }}>
              <input readOnly value={newKey.api_key} style={{ flex: 1, fontFamily: 'monospace', fontSize: '0.8rem' }} onFocus={e => e.target.select()} />
              <button className="btn btn-secondary btn-sm" onClick={() => navigator.clipboard?.writeText(newKey.api_key)}>Salin</button>
            </div>
            <div style={{ marginTop: '1rem', textAlign: 'right' }}>
              <button className="btn btn-primary btn-sm" onClick={() => setNewKey(null)}>Saya sudah menyimpan kunci</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default function AttendanceSettings() {
  const [branches, setBranches] = useState([]);
  useEffect(() => { getBranches().then(r => setBranches(r.data || [])).catch(() => {}); }, []);

  return (
    <>
      <div className="page-header">
        <h1>Pengaturan Absensi</h1>
        <Link to="/hr/attendance" className="btn btn-secondary">← Kembali ke Absensi</Link>
      </div>
      <ScheduleSection branches={branches} />
      <HolidaySection />
      <DeviceSection branches={branches} />
    </>
  );
}
