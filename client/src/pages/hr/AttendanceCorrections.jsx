import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getAttendance, getBranches,
  setAttendanceHalfDay, clearAttendanceHalfDay,
  markAttendanceNoPunch, clearAttendanceNoPunch,
} from '../../api';
import { StatusChip, SourceBadge, fmtTime } from './AttendanceDashboard';

// ── date helpers ──────────────────────────────────────────────────────────────

function toLocalISO(d) {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}
const todayStr = () => toLocalISO(new Date());

function fmtDate(iso) {
  return new Date(iso + 'T00:00:00').toLocaleDateString('id-ID', { day: 'numeric', month: 'short', year: 'numeric' });
}

// Builds an RFC3339 timestamp from a date (YYYY-MM-DD) + time (HH:MM) using the
// browser's local UTC offset, so the server reads the wall-clock entry hour the
// manager intended (not a UTC-shifted one).
function toRFC3339(dateIso, timeHHMM) {
  const off = -new Date().getTimezoneOffset(); // minutes east of UTC
  const sign = off >= 0 ? '+' : '-';
  const abs = Math.abs(off);
  const oh = String(Math.floor(abs / 60)).padStart(2, '0');
  const om = String(abs % 60).padStart(2, '0');
  return `${dateIso}T${timeHHMM}:00${sign}${oh}:${om}`;
}

// HH:MM in local time from a stored timestamp, for pre-filling a time input.
function timeInputValue(ts, fallback = '08:00') {
  if (!ts) return fallback;
  const d = new Date(ts);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

const fmtHours = (mins) => (mins / 60).toLocaleString('id-ID', { maximumFractionDigits: 2 });

// ── mark-half-day dialog ────────────────────────────────────────────────────

function HalfDayDialog({ record, onClose, onSaved }) {
  const [type, setType] = useState('late'); // 'late' = datang siang, 'early' = pulang awal
  const [lateTime, setLateTime]   = useState(timeInputValue(record.check_in, '08:00'));
  const [earlyTime, setEarlyTime] = useState(timeInputValue(record.check_out, '17:00'));
  const [note, setNote] = useState('');
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');

  const dateIso = record.date;

  const submit = async () => {
    if (!note.trim()) { setErr('Catatan koreksi wajib diisi.'); return; }
    setSaving(true); setErr('');
    try {
      const payload = { type, note: note.trim() };
      if (type === 'late') payload.start_time = toRFC3339(dateIso, lateTime);
      else payload.end_time = toRFC3339(dateIso, earlyTime);
      await setAttendanceHalfDay(record.id, payload);
      onSaved();
    } catch (e) {
      setErr(e?.response?.data?.error || 'Gagal menyimpan koreksi setengah hari.');
      setSaving(false);
    }
  };

  return (
    <div style={overlayStyle} onClick={onClose}>
      <div className="card" style={{ maxWidth: '440px', width: '100%' }} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Tandai Setengah Hari</h3>
        <p style={{ fontSize: '0.85rem', color: '#666', marginTop: '-0.4rem' }}>
          {record.full_name} · {fmtDate(dateIso)}
        </p>

        <label style={labelStyle}>Jenis Setengah Hari</label>
        <div style={{ display: 'flex', gap: '0.4rem' }}>
          {[
            { key: 'late', label: 'Datang Siang' },
            { key: 'early', label: 'Pulang Awal' },
          ].map(o => (
            <button key={o.key} type="button" onClick={() => setType(o.key)}
              className={`btn btn-sm ${type === o.key ? 'btn-primary' : 'btn-secondary'}`}
              style={{ flex: 1 }}>
              {o.label}
            </button>
          ))}
        </div>

        {type === 'late' ? (
          <>
            <label style={labelStyle}>Jam Mulai Kerja</label>
            <input type="time" value={lateTime} onChange={e => setLateTime(e.target.value)} style={{ width: '100%' }} />
            <div style={{ fontSize: '0.75rem', color: '#888', marginTop: '0.25rem' }}>
              Gaji dipotong sesuai jam kerja yang hilang (jam masuk terjadwal → jam mulai ini).
            </div>
          </>
        ) : (
          <>
            <label style={labelStyle}>Jam Pulang</label>
            <input type="time" value={earlyTime} onChange={e => setEarlyTime(e.target.value)} style={{ width: '100%' }} />
            <div style={{ fontSize: '0.75rem', color: '#888', marginTop: '0.25rem' }}>
              Gaji dipotong sesuai jam kerja yang hilang (jam pulang ini → jam pulang terjadwal).
            </div>
          </>
        )}

        <label style={labelStyle}>Catatan Koreksi <span style={{ color: '#c62828' }}>*</span></label>
        <textarea value={note} onChange={e => setNote(e.target.value)} rows={3} style={{ width: '100%' }}
          placeholder="Alasan koreksi (mis. pulang awal karena urusan keluarga)…" />

        {err && <div style={{ color: '#c62828', fontSize: '0.82rem', marginTop: '0.5rem' }}>{err}</div>}

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={saving}>Batal</button>
          <button className="btn btn-primary" onClick={submit} disabled={saving}>
            {saving ? 'Menyimpan…' : 'Simpan'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ── mark present-without-scan dialog (forgot to check in & out) ──────────────

function NoPunchDialog({ record, onClose, onSaved }) {
  const [note, setNote] = useState('');
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');

  const submit = async () => {
    if (!note.trim()) { setErr('Catatan koreksi wajib diisi.'); return; }
    setSaving(true); setErr('');
    try {
      await markAttendanceNoPunch(record.id, { note: note.trim() });
      onSaved();
    } catch (e) {
      setErr(e?.response?.data?.error || 'Gagal menyimpan koreksi.');
      setSaving(false);
    }
  };

  return (
    <div style={overlayStyle} onClick={onClose}>
      <div className="card" style={{ maxWidth: '440px', width: '100%' }} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Tandai Hadir Tanpa Absen</h3>
        <p style={{ fontSize: '0.85rem', color: '#666', marginTop: '-0.4rem' }}>
          {record.full_name} · {fmtDate(record.date)}
        </p>
        <div style={{ fontSize: '0.8rem', color: '#666' }}>
          Karyawan hadir bekerja tetapi lupa absen masuk dan pulang. Hari ini akan dihitung
          hadir (tanpa jam), dan poin evaluasi memakai kebijakan <em>tidak absen masuk &amp; pulang</em>.
        </div>

        <label style={labelStyle}>Catatan Koreksi <span style={{ color: '#c62828' }}>*</span></label>
        <textarea value={note} onChange={e => setNote(e.target.value)} rows={3} style={{ width: '100%' }}
          placeholder="Alasan koreksi (mis. hadir tapi mesin absen error)…" />

        {err && <div style={{ color: '#c62828', fontSize: '0.82rem', marginTop: '0.5rem' }}>{err}</div>}

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={saving}>Batal</button>
          <button className="btn btn-primary" onClick={submit} disabled={saving}>
            {saving ? 'Menyimpan…' : 'Simpan'}
          </button>
        </div>
      </div>
    </div>
  );
}

const overlayStyle = {
  position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)',
  display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '1rem', zIndex: 100,
};
const labelStyle = { display: 'block', fontSize: '0.8rem', fontWeight: 600, color: '#444', margin: '0.75rem 0 0.25rem' };

// ── main page ───────────────────────────────────────────────────────────────

export default function AttendanceCorrections() {
  const monthAgo = () => { const d = new Date(); d.setDate(d.getDate() - 30); return toLocalISO(d); };

  const [dateFrom, setDateFrom]   = useState(monthAgo());
  const [dateTo, setDateTo]       = useState(todayStr());
  const [branchId, setBranchId]   = useState('');
  const [search, setSearch]       = useState('');
  const [statusFilter, setStatusFilter] = useState(''); // '', 'present', 'absent'
  const [halfDayOnly, setHalfDayOnly] = useState(false);
  const [branches, setBranches]   = useState([]);
  const [rows, setRows]           = useState([]);
  const [loading, setLoading]     = useState(false);
  const [msg, setMsg]             = useState('');
  const [dialogRec, setDialogRec] = useState(null);   // half-day dialog
  const [noPunchRec, setNoPunchRec] = useState(null); // present-no-punch dialog
  const [busyId, setBusyId]       = useState('');

  useEffect(() => { getBranches().then(r => setBranches(r.data || [])).catch(() => {}); }, []);

  const load = () => {
    setLoading(true); setMsg('');
    const params = { date_from: dateFrom, date_to: dateTo };
    if (statusFilter)   params.status = statusFilter;
    if (branchId)       params.branch_id = branchId;
    if (search.trim())  params.search = search.trim();
    if (halfDayOnly)    params.half_day_only = 'true';
    getAttendance(params)
      .then(r => setRows(r.data?.data || []))
      .catch(() => setMsg('Gagal memuat data kehadiran'))
      .finally(() => setLoading(false));
  };

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load(); }, [dateFrom, dateTo, branchId, statusFilter, halfDayOnly]);

  const onSaved = () => { setDialogRec(null); setMsg('Koreksi setengah hari disimpan.'); load(); };
  const onNoPunchSaved = () => { setNoPunchRec(null); setMsg('Koreksi hadir tanpa absen disimpan.'); load(); };

  const handleClear = async (rec) => {
    setBusyId(rec.id); setMsg('');
    try {
      if (rec.is_half_day) {
        await clearAttendanceHalfDay(rec.id);
        setMsg('Koreksi setengah hari dibatalkan.');
      } else if (rec.is_no_punch) {
        await clearAttendanceNoPunch(rec.id);
        setMsg('Koreksi hadir tanpa absen dibatalkan (ditandai absen).');
      }
      load();
    } catch {
      setMsg('Gagal membatalkan koreksi.');
    } finally {
      setBusyId('');
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Koreksi Kehadiran</h1>
        <Link to="/hr/attendance" className="btn btn-secondary">← Absensi</Link>
      </div>

      <p style={{ color: '#666', fontSize: '0.88rem', marginTop: '-0.5rem', maxWidth: '820px' }}>
        Koreksi manual tanpa persetujuan. Tandai <strong>setengah hari</strong> (datang siang / pulang
        awal) — gaji dipotong sesuai jam kerja yang hilang dan poin evaluasi memakai kebijakan setengah
        hari. Atau tandai karyawan yang hadir tetapi <strong>lupa absen masuk &amp; pulang</strong>
        (pilih status <em>Absen</em> untuk menemukannya).
      </p>

      <div className="card" style={{ marginBottom: '1rem' }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.6rem', alignItems: 'flex-end' }}>
          <div>
            <label style={{ fontSize: '0.75rem', color: '#888', display: 'block' }}>Dari</label>
            <input type="date" value={dateFrom} max={dateTo} onChange={e => setDateFrom(e.target.value)} />
          </div>
          <div>
            <label style={{ fontSize: '0.75rem', color: '#888', display: 'block' }}>Sampai</label>
            <input type="date" value={dateTo} min={dateFrom} max={todayStr()} onChange={e => setDateTo(e.target.value)} />
          </div>
          <select value={branchId} onChange={e => setBranchId(e.target.value)} style={{ fontSize: '0.85rem' }}>
            <option value="">Semua Cabang</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
          <select value={statusFilter} onChange={e => setStatusFilter(e.target.value)} style={{ fontSize: '0.85rem' }}>
            <option value="">Semua Status</option>
            <option value="present">Hadir</option>
            <option value="absent">Absen</option>
          </select>
          <input
            value={search}
            onChange={e => setSearch(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') load(); }}
            placeholder="Cari nama / kode…"
            style={{ fontSize: '0.85rem', minWidth: '160px' }}
          />
          <label style={{ display: 'flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.82rem' }}>
            <input type="checkbox" checked={halfDayOnly} onChange={e => setHalfDayOnly(e.target.checked)} />
            Hanya setengah hari
          </label>
          <button onClick={load} className="btn btn-primary btn-sm">Terapkan</button>
        </div>
        {msg && (
          <div style={{ marginTop: '0.75rem', background: '#e8f5e9', color: '#1b5e20', padding: '0.5rem 0.75rem', borderRadius: '6px', fontSize: '0.85rem' }}>
            {msg}
          </div>
        )}
      </div>

      <div className="card" style={{ padding: 0, overflowX: 'auto' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85rem' }}>
          <thead>
            <tr style={{ background: '#fafafa', textAlign: 'left' }}>
              <th style={thStyle}>Karyawan</th>
              <th style={thStyle}>Tanggal</th>
              <th style={thStyle}>Cabang</th>
              <th style={thStyle}>Masuk</th>
              <th style={thStyle}>Pulang</th>
              <th style={thStyle}>Status</th>
              <th style={{ ...thStyle, textAlign: 'right' }}>Aksi</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={7} style={{ textAlign: 'center', padding: '2rem', color: '#aaa' }}>Memuat…</td></tr>
            ) : rows.length === 0 ? (
              <tr><td colSpan={7} style={{ textAlign: 'center', padding: '2rem', color: '#aaa' }}>Tidak ada data untuk filter ini.</td></tr>
            ) : rows.map(r => (
              <tr key={r.id} style={{ borderTop: '1px solid #f0f0f0', background: r.is_half_day ? '#fffbe9' : '#fff' }}>
                <td style={tdStyle}>
                  <Link to={`/hr/employees/${r.employee_id}`} style={{ fontWeight: 600, color: '#1a1a2e', textDecoration: 'none' }}>
                    {r.full_name}
                  </Link>
                  <div style={{ fontSize: '0.72rem', color: '#999', fontFamily: 'monospace' }}>{r.employee_code}</div>
                </td>
                <td style={tdStyle}>{fmtDate(r.date)}</td>
                <td style={tdStyle}>{r.branch_name}</td>
                <td style={tdStyle}>
                  {fmtTime(r.check_in) || <span style={{ color: '#ccc' }}>—</span>}
                  {r.check_in && <div style={{ marginTop: '0.15rem' }}><SourceBadge source={r.check_in_source} /></div>}
                </td>
                <td style={tdStyle}>
                  {fmtTime(r.check_out) || <span style={{ color: '#ccc' }}>—</span>}
                  {r.check_out && <div style={{ marginTop: '0.15rem' }}><SourceBadge source={r.check_out_source} /></div>}
                </td>
                <td style={tdStyle}>
                  {r.is_half_day ? (
                    <span style={{ background: '#fff4e5', color: '#c05621', padding: '0.15rem 0.5rem', borderRadius: '4px', fontWeight: 700, fontSize: '0.76rem' }}>
                      Setengah Hari ({r.half_day_type === 'early' ? 'Pulang Awal' : 'Datang Siang'}) · {fmtHours(r.half_day_lost_minutes)} jam hilang
                    </span>
                  ) : r.is_no_punch ? (
                    <span style={{ background: '#ede7f6', color: '#5e35b1', padding: '0.15rem 0.5rem', borderRadius: '4px', fontWeight: 700, fontSize: '0.76rem' }}>
                      Hadir Tanpa Absen
                    </span>
                  ) : r.is_late ? (
                    <span style={{ background: '#fdecea', color: '#c62828', padding: '0.15rem 0.5rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.76rem' }}>
                      Terlambat {r.late_minutes} mnt
                    </span>
                  ) : r.is_early_leave ? (
                    <span style={{ background: '#fdecea', color: '#c62828', padding: '0.15rem 0.5rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.76rem' }}>
                      Pulang Awal {r.early_leave_minutes} mnt
                    </span>
                  ) : (
                    <StatusChip status={r.status} />
                  )}
                </td>
                <td style={{ ...tdStyle, textAlign: 'right', whiteSpace: 'nowrap' }}>
                  {(r.is_half_day || r.is_no_punch) ? (
                    <button className="btn btn-secondary btn-sm" disabled={busyId === r.id} onClick={() => handleClear(r)}>
                      {busyId === r.id ? '…' : 'Batalkan'}
                    </button>
                  ) : r.status === 'absent' ? (
                    <button className="btn btn-primary btn-sm" onClick={() => setNoPunchRec(r)}>
                      Tandai Hadir (Lupa Absen)
                    </button>
                  ) : r.status === 'present' ? (
                    <button className="btn btn-primary btn-sm" onClick={() => setDialogRec(r)}>
                      Tandai Setengah Hari
                    </button>
                  ) : (
                    <span style={{ color: '#ccc' }}>—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {dialogRec && (
        <HalfDayDialog record={dialogRec} onClose={() => setDialogRec(null)} onSaved={onSaved} />
      )}
      {noPunchRec && (
        <NoPunchDialog record={noPunchRec} onClose={() => setNoPunchRec(null)} onSaved={onNoPunchSaved} />
      )}
    </>
  );
}

const thStyle = { padding: '0.6rem 0.85rem', fontSize: '0.72rem', color: '#888', textTransform: 'uppercase', letterSpacing: '0.04em', fontWeight: 700 };
const tdStyle = { padding: '0.6rem 0.85rem', verticalAlign: 'top' };
