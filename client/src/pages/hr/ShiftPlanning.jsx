// Shift pieces of manpower planning: shift settings per branch, the roster cell
// editor, the employee shift profile (home division + delivery-order flag) and
// the per-day coverage view (shift × division).
//
// The date rule lives on the server; the UI only explains it:
//   - days before today are read-only,
//   - today's changes by non-managers become a request a manager approves,
//   - future days apply directly.
import { useEffect, useState } from 'react';
import {
  getShifts, createShift, updateShift, deleteShift,
  saveShiftAssignments, updateEmployeeShiftProfile,
} from '../../api';
import { shiftTimeLabel, shiftHoursLabel, dayDivisionId, NO_DIVISION } from './shiftRules';

const overlay = {
  position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)',
  display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
};
const modalBox = {
  background: '#fff', borderRadius: 8, padding: '1.5rem', width: '100%', maxWidth: 460,
  maxHeight: '90vh', overflowY: 'auto', boxShadow: '0 8px 32px rgba(0,0,0,0.18)',
};
const inputStyle = {
  width: '100%', boxSizing: 'border-box', padding: '7px 10px',
  border: '1px solid #d1d5db', borderRadius: 4, fontSize: 13,
};
const labelStyle = { display: 'block', fontSize: 12, fontWeight: 600, color: '#374151', marginBottom: 4 };
const btn = { padding: '7px 16px', borderRadius: 4, border: '1px solid #d1d5db', background: '#fff', cursor: 'pointer', fontSize: 13 };
const btnPrimary = { ...btn, border: 'none', background: '#2563eb', color: '#fff', fontWeight: 600 };
const errText = { color: '#dc2626', fontSize: 13, margin: '0 0 10px' };

function formatDateLabel(dateStr) {
  const d = new Date(dateStr + 'T00:00:00');
  return d.toLocaleDateString('id-ID', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
}

export function ShiftChip({ shift, pending, faded }) {
  if (!shift) return null;
  return (
    <span style={{
      display: 'inline-block', padding: '1px 6px', borderRadius: 4, fontSize: 11, fontWeight: 700,
      background: shift.color, color: '#1f2937', opacity: faded ? 0.6 : 1,
      border: pending ? '1px dashed #c2410c' : '1px solid rgba(0,0,0,0.08)',
    }}>{shift.code}</span>
  );
}

// ── Roster cell editor ───────────────────────────────────────────────────────

export function ShiftCellModal({ employee, day, branch, dates, today, approver, onClose, onSaved, onRequestLeave }) {
  const shiftsById = Object.fromEntries(branch.shifts.map(s => [s.id, s]));
  const past = day.date < today;
  const isToday = day.date === today;
  const needsApproval = isToday && !approver;
  const onLeave = day.status === 'cuti';

  const [shiftId, setShiftId] = useState(day.shift_id || '');
  const [divisionId, setDivisionId] = useState(day.division_id || '');
  const [endDate, setEndDate] = useState(day.date);
  const [reason, setReason] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const current = shiftsById[day.shift_id];
  const pendingShift = day.pending_request_id ? shiftsById[day.pending_shift_id] : null;
  const homeDivision = branch.divisions.find(d => d.id === employee.division_id);
  const selectable = branch.shifts.filter(s => s.is_active || s.id === day.shift_id);
  const lastDate = dates[dates.length - 1];

  const submit = async () => {
    setError('');
    if (endDate < day.date) { setError('Tanggal akhir tidak boleh sebelum tanggal mulai.'); return; }
    // Fill the range across the days in view. Approved-leave days are skipped
    // when rostering (the server refuses them); removing a shift touches all.
    const days = employee.days.filter(d => d.date >= day.date && d.date <= endDate);
    const target = shiftId && days.some(d => d.status === 'cuti') ? days.filter(d => d.status !== 'cuti') : days;
    const skipped = days.length - target.length;
    if (target.length === 0) { setError('Semua tanggal di rentang ini adalah hari cuti.'); return; }
    setSaving(true);
    try {
      const { data } = await saveShiftAssignments({
        reason,
        entries: target.map(d => ({
          employee_id: employee.id, date: d.date,
          shift_id: shiftId || null, division_id: divisionId || null,
        })),
      });
      const parts = [];
      if (data.applied) parts.push(`${data.applied} hari disimpan`);
      if (data.pending) parts.push(`${data.pending} hari menunggu persetujuan manajer`);
      if (skipped) parts.push(`${skipped} hari cuti dilewati`);
      onSaved(parts.join(', ') || 'Tidak ada perubahan');
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan jadwal shift.');
      setSaving(false);
    }
  };

  return (
    <div style={overlay} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={modalBox}>
        <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700 }}>Jadwal Shift</h3>
        <p style={{ margin: '0 0 12px', fontSize: 13, color: '#6b7280' }}>
          {employee.name}{employee.position ? ` · ${employee.position}` : ''}{employee.is_delivery_staff ? ' · Staf DO' : ''}<br />
          {formatDateLabel(day.date)}
        </p>

        <div style={{ fontSize: 13, marginBottom: 12, color: '#374151' }}>
          Berlaku: {current ? <><ShiftChip shift={current} /> {current.name} ({shiftTimeLabel(current)})</> : <em>belum ada shift</em>}
          {day.pending_request_id && (
            <div style={{ marginTop: 6, color: '#9a3412' }}>
              Menunggu persetujuan: {pendingShift ? <><ShiftChip shift={pendingShift} pending /> {pendingShift.name}</> : 'hapus shift'}
            </div>
          )}
        </div>

        {onLeave && (
          <div style={{ background: '#1f2937', color: '#f9fafb', borderRadius: 6, padding: '10px 12px', fontSize: 13, marginBottom: 12 }}>
            Cuti disetujui: <b>{day.leave_type || 'Cuti'}</b>. Batalkan cutinya di Manajemen Cuti untuk menjadwalkan shift.
          </div>
        )}
        {day.status === 'pending' && (
          <div style={{ background: '#fed7aa', color: '#9a3412', borderRadius: 6, padding: '10px 12px', fontSize: 13, marginBottom: 12 }}>
            Ada pengajuan cuti <b>{day.leave_type}</b> yang menunggu persetujuan pada hari ini.
          </div>
        )}

        {past ? (
          <p style={{ fontSize: 13, color: '#6b7280', background: '#f3f4f6', padding: '10px 12px', borderRadius: 6 }}>
            Tanggal ini sudah lewat — jadwalnya tidak dapat diubah lagi.
          </p>
        ) : !onLeave && (
          <>
            {needsApproval && (
              <div style={{ background: '#fff7ed', color: '#9a3412', border: '1px solid #fdba74', borderRadius: 6, padding: '10px 12px', fontSize: 13, marginBottom: 12 }}>
                Perubahan jadwal <b>hari ini</b> memerlukan persetujuan manajer. Jadwal yang berlaku tetap dipakai sampai disetujui.
              </div>
            )}

            <label style={labelStyle}>Shift</label>
            <div style={{ display: 'grid', gap: 6, marginBottom: 12 }}>
              {selectable.map(s => {
                const blocked = s.delivery_only && !employee.is_delivery_staff;
                return (
                  <label key={s.id} title={blocked ? 'Khusus staf delivery order' : ''}
                    style={{
                      display: 'flex', alignItems: 'center', gap: 8, padding: '6px 8px', borderRadius: 6, fontSize: 13,
                      border: shiftId === s.id ? '2px solid #2563eb' : '1px solid #e5e7eb',
                      opacity: blocked ? 0.5 : 1, cursor: blocked ? 'not-allowed' : 'pointer',
                    }}>
                    <input type="radio" name="shift" disabled={blocked} checked={shiftId === s.id} onChange={() => setShiftId(s.id)} />
                    <ShiftChip shift={s} />
                    <span style={{ flex: 1 }}>
                      {s.name}{!s.is_active && ' (nonaktif)'}
                      <span style={{ color: '#6b7280' }}> · {shiftTimeLabel(s)} · {shiftHoursLabel(s)}</span>
                      {s.delivery_only && <span style={{ color: '#7c3aed' }}> · khusus DO</span>}
                    </span>
                  </label>
                );
              })}
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 8px', borderRadius: 6, fontSize: 13, cursor: 'pointer',
                border: shiftId === '' ? '2px solid #2563eb' : '1px solid #e5e7eb' }}>
                <input type="radio" name="shift" checked={shiftId === ''} onChange={() => setShiftId('')} />
                <span>Tanpa shift</span>
              </label>
              {branch.shifts.length === 0 && (
                <p style={{ fontSize: 12, color: '#b45309', margin: 0 }}>Cabang ini belum punya shift. Tambahkan lewat tombol "Atur Shift".</p>
              )}
            </div>

            {shiftId && (
              <div style={{ marginBottom: 12 }}>
                <label style={labelStyle}>Divisi</label>
                <select style={inputStyle} value={divisionId} onChange={e => setDivisionId(e.target.value)}>
                  <option value="">Divisi utama ({homeDivision?.name || 'belum diatur'})</option>
                  {branch.divisions.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
                </select>
              </div>
            )}

            <div style={{ marginBottom: 12 }}>
              <label style={labelStyle}>Terapkan sampai tanggal</label>
              <input type="date" style={inputStyle} value={endDate} min={day.date} max={lastDate}
                onChange={e => setEndDate(e.target.value)} />
              <p style={{ fontSize: 11, color: '#9ca3af', margin: '4px 0 0' }}>
                Isi rentang untuk menjadwalkan beberapa hari sekaligus (dalam periode yang ditampilkan). Hari cuti dilewati.
              </p>
            </div>

            {needsApproval && (
              <div style={{ marginBottom: 12 }}>
                <label style={labelStyle}>Alasan perubahan</label>
                <textarea style={{ ...inputStyle, resize: 'vertical', minHeight: 50 }} value={reason}
                  onChange={e => setReason(e.target.value)} placeholder="Mis. tukar shift dengan rekan, sakit mendadak…" />
              </div>
            )}
          </>
        )}

        {error && <p style={errText}>{error}</p>}

        <div style={{ display: 'flex', gap: 8, justifyContent: 'space-between', flexWrap: 'wrap' }}>
          <div>
            {!onLeave && day.status !== 'pending' && (
              <button style={btn} onClick={onRequestLeave} title={past ? 'Pengajuan mundur tanggal perlu persetujuan manajer/pemilik' : ''}>
                Ajukan Cuti…
              </button>
            )}
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button style={btn} onClick={onClose}>{past || onLeave ? 'Tutup' : 'Batal'}</button>
            {!past && !onLeave && (
              <button style={{ ...btnPrimary, opacity: saving ? 0.7 : 1 }} disabled={saving} onClick={submit}>
                {saving ? 'Menyimpan…' : needsApproval ? 'Ajukan Perubahan' : 'Simpan'}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ── Employee shift profile ───────────────────────────────────────────────────

export function ShiftProfileModal({ employee, branch, onClose, onSaved }) {
  const [divisionId, setDivisionId] = useState(employee.division_id || '');
  const [isDO, setIsDO] = useState(!!employee.is_delivery_staff);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const submit = async () => {
    setSaving(true); setError('');
    try {
      await updateEmployeeShiftProfile(employee.id, { division_id: divisionId || null, is_delivery_staff: isDO });
      onSaved();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan.');
      setSaving(false);
    }
  };

  return (
    <div style={overlay} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={{ ...modalBox, maxWidth: 380 }}>
        <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700 }}>Profil Shift</h3>
        <p style={{ margin: '0 0 16px', fontSize: 13, color: '#6b7280' }}>{employee.name} · {branch.name}</p>
        <label style={labelStyle}>Divisi utama</label>
        <select style={{ ...inputStyle, marginBottom: 14 }} value={divisionId} onChange={e => setDivisionId(e.target.value)}>
          <option value="">— Tanpa divisi —</option>
          {branch.divisions.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
        </select>
        <label style={{ display: 'flex', gap: 8, alignItems: 'flex-start', fontSize: 13, marginBottom: 14, cursor: 'pointer' }}>
          <input type="checkbox" checked={isDO} onChange={e => setIsDO(e.target.checked)} style={{ marginTop: 2 }} />
          <span>Staf delivery order (DO)<br />
            <span style={{ fontSize: 11, color: '#6b7280' }}>Hanya staf DO yang dapat dijadwalkan di shift khusus DO (mis. Shift Middle).</span>
          </span>
        </label>
        {error && <p style={errText}>{error}</p>}
        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <button style={btn} onClick={onClose}>Batal</button>
          <button style={{ ...btnPrimary, opacity: saving ? 0.7 : 1 }} disabled={saving} onClick={submit}>
            {saving ? 'Menyimpan…' : 'Simpan'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Shift settings per branch ────────────────────────────────────────────────

const EMPTY_SHIFT = {
  code: '', name: '', start1: '08:00', end1: '17:00', start2: '', end2: '',
  break_minutes: 0, delivery_only: false, color: '#dbeafe', sort_order: 0, is_active: true,
};

export function ShiftSettingsModal({ branch, onClose }) {
  const [shifts, setShifts] = useState([]);
  const [form, setForm] = useState(null); // null = list; {id?, ...} = editing
  const [split, setSplit] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const load = () =>
    getShifts({ branch_id: branch.id, include_inactive: 1 })
      .then(r => setShifts(r.data || []))
      .catch(() => setError('Gagal memuat daftar shift.'));
  useEffect(() => { load(); }, [branch.id]);

  const edit = (s) => {
    setError('');
    setForm(s ? { ...s, start2: s.start2 || '', end2: s.end2 || '' } : { ...EMPTY_SHIFT, sort_order: shifts.length + 1 });
    setSplit(!!s?.start2);
  };

  const save = async () => {
    setSaving(true); setError('');
    const body = {
      ...form, branch_id: branch.id,
      start2: split ? form.start2 : '', end2: split ? form.end2 : '',
      break_minutes: Number(form.break_minutes) || 0, sort_order: Number(form.sort_order) || 0,
    };
    try {
      if (form.id) await updateShift(form.id, body); else await createShift(body);
      setForm(null);
      await load();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan shift.');
    } finally {
      setSaving(false);
    }
  };

  const remove = async (s) => {
    if (!window.confirm(`Hapus shift ${s.name}? Shift yang sudah pernah dijadwalkan akan dinonaktifkan, bukan dihapus.`)) return;
    try {
      const { data } = await deleteShift(s.id);
      if (data?.deactivated) window.alert('Shift masih dipakai di jadwal, jadi dinonaktifkan.');
      await load();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menghapus shift.');
    }
  };

  const set = (k) => (e) => setForm(f => ({ ...f, [k]: e.target.type === 'checkbox' ? e.target.checked : e.target.value }));
  const previewMinutes = form ? (() => {
    const m = (t) => { const [h, mm] = (t || '').split(':').map(Number); return h * 60 + mm; };
    let total = m(form.end1) - m(form.start1);
    if (split && form.start2 && form.end2) total += m(form.end2) - m(form.start2);
    total -= Number(form.break_minutes) || 0;
    return Number.isFinite(total) && total > 0 ? total : 0;
  })() : 0;

  return (
    <div style={overlay} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={{ ...modalBox, maxWidth: 560 }}>
        <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700 }}>Shift — {branch.name}</h3>
        <p style={{ margin: '0 0 14px', fontSize: 12, color: '#6b7280' }}>
          Setiap cabang punya versi shiftnya sendiri. Jam shift dipakai untuk menilai keterlambatan dan pulang cepat pada absensi.
        </p>
        {error && <p style={errText}>{error}</p>}

        {!form && (
          <>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13, marginBottom: 12 }}>
              <tbody>
                {shifts.map(s => (
                  <tr key={s.id} style={{ borderBottom: '1px solid #e5e7eb', opacity: s.is_active ? 1 : 0.5 }}>
                    <td style={{ padding: '6px 4px' }}><ShiftChip shift={s} /></td>
                    <td style={{ padding: '6px 4px' }}>
                      {s.name}{!s.is_active && ' (nonaktif)'}
                      {s.delivery_only && <span style={{ color: '#7c3aed', fontSize: 11 }}> · khusus DO</span>}
                      <div style={{ fontSize: 11, color: '#6b7280' }}>
                        {shiftTimeLabel(s)} · {shiftHoursLabel(s)}{s.break_minutes ? ` (istirahat ${s.break_minutes} mnt)` : ''}
                      </div>
                    </td>
                    <td style={{ padding: '6px 4px', whiteSpace: 'nowrap', textAlign: 'right' }}>
                      <button style={{ ...btn, padding: '3px 10px', fontSize: 12 }} onClick={() => edit(s)}>Ubah</button>{' '}
                      <button style={{ ...btn, padding: '3px 10px', fontSize: 12, color: '#b91c1c' }} onClick={() => remove(s)}>Hapus</button>
                    </td>
                  </tr>
                ))}
                {shifts.length === 0 && <tr><td style={{ padding: 12, color: '#6b7280' }}>Belum ada shift.</td></tr>}
              </tbody>
            </table>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}>
              <button style={btnPrimary} onClick={() => edit(null)}>+ Tambah Shift</button>
              <button style={btn} onClick={onClose}>Tutup</button>
            </div>
          </>
        )}

        {form && (
          <>
            <div style={{ display: 'grid', gridTemplateColumns: '90px 1fr', gap: 10, marginBottom: 10 }}>
              <div><label style={labelStyle}>Kode</label><input style={inputStyle} maxLength={8} value={form.code} onChange={set('code')} placeholder="OPN" /></div>
              <div><label style={labelStyle}>Nama</label><input style={inputStyle} value={form.name} onChange={set('name')} placeholder="Shift Opening" /></div>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginBottom: 10 }}>
              <div><label style={labelStyle}>{split ? 'Sesi 1 mulai' : 'Mulai'}</label><input type="time" style={inputStyle} value={form.start1} onChange={set('start1')} /></div>
              <div><label style={labelStyle}>{split ? 'Sesi 1 selesai' : 'Selesai'}</label><input type="time" style={inputStyle} value={form.end1} onChange={set('end1')} /></div>
            </div>
            <label style={{ display: 'flex', gap: 6, alignItems: 'center', fontSize: 13, marginBottom: 10, cursor: 'pointer' }}>
              <input type="checkbox" checked={split} onChange={e => setSplit(e.target.checked)} />
              Shift terpisah (dua sesi, mis. Shift Jumping)
            </label>
            {split && (
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginBottom: 10 }}>
                <div><label style={labelStyle}>Sesi 2 mulai</label><input type="time" style={inputStyle} value={form.start2} onChange={set('start2')} /></div>
                <div><label style={labelStyle}>Sesi 2 selesai</label><input type="time" style={inputStyle} value={form.end2} onChange={set('end2')} /></div>
              </div>
            )}
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 10, marginBottom: 10 }}>
              <div><label style={labelStyle}>Istirahat (menit)</label><input type="number" min={0} style={inputStyle} value={form.break_minutes} onChange={set('break_minutes')} /></div>
              <div><label style={labelStyle}>Urutan</label><input type="number" style={inputStyle} value={form.sort_order} onChange={set('sort_order')} /></div>
              <div><label style={labelStyle}>Warna</label><input type="color" style={{ ...inputStyle, padding: 2, height: 33 }} value={form.color} onChange={set('color')} /></div>
            </div>
            <label style={{ display: 'flex', gap: 6, alignItems: 'center', fontSize: 13, marginBottom: 6, cursor: 'pointer' }}>
              <input type="checkbox" checked={form.delivery_only} onChange={set('delivery_only')} />
              Khusus staf delivery order (DO)
            </label>
            <label style={{ display: 'flex', gap: 6, alignItems: 'center', fontSize: 13, marginBottom: 10, cursor: 'pointer' }}>
              <input type="checkbox" checked={form.is_active} onChange={set('is_active')} />
              Aktif
            </label>
            <p style={{ fontSize: 12, color: '#374151', margin: '0 0 12px' }}>
              Durasi kerja: <b>{shiftHoursLabel({ work_minutes: previewMinutes })}</b>
            </p>
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button style={btn} onClick={() => setForm(null)}>Kembali</button>
              <button style={{ ...btnPrimary, opacity: saving ? 0.7 : 1 }} disabled={saving} onClick={save}>
                {saving ? 'Menyimpan…' : 'Simpan Shift'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

// ── Coverage view: one day, shift × division ─────────────────────────────────

/**
 * For one date, every branch as a table: a row per division, a column per
 * shift, the names of who is rostered in each cell. A division nobody covers
 * that day is flagged — that is the question this view exists to answer.
 */
export function ShiftCoverageView({ data, date, onCellClick }) {
  const idx = data.dates.indexOf(date);
  if (idx < 0) return null;

  return data.branches.map(branch => {
    const shiftsById = Object.fromEntries(branch.shifts.map(s => [s.id, s]));
    // Columns: active shifts plus any inactive one still rostered that day.
    const usedIds = new Set(branch.employees.map(e => e.days[idx].shift_id).filter(Boolean));
    const columns = branch.shifts.filter(s => s.is_active || usedIds.has(s.id));
    const rows = [
      ...branch.divisions.map(d => ({ id: d.id, name: d.name })),
      { id: NO_DIVISION, name: 'Tanpa divisi' },
    ];
    // cell[divisionId][shiftId | 'none' | 'leave'] = [{emp, day}]
    const cell = {};
    for (const emp of branch.employees) {
      const day = emp.days[idx];
      const div = dayDivisionId(emp, day) || NO_DIVISION;
      const col = day.status === 'cuti' ? 'leave' : (day.shift_id && shiftsById[day.shift_id] ? day.shift_id : 'none');
      ((cell[div] ||= {})[col] ||= []).push({ emp, day });
    }
    const visibleRows = rows.filter(r => r.id !== NO_DIVISION || cell[NO_DIVISION]);
    const rostered = (divId) => columns.reduce((n, s) => n + (cell[divId]?.[s.id]?.length || 0), 0);
    const uncovered = branch.divisions.filter(d => rostered(d.id) === 0);

    const names = (list, pendingAware) => (list || []).map(({ emp, day }) => (
      <div key={emp.id} onClick={() => onCellClick(emp, day, branch)}
        title={day.pending_request_id ? 'Ada perubahan shift menunggu persetujuan' : 'Klik untuk ubah'}
        style={{ cursor: 'pointer', padding: '1px 0', color: '#111827' }}>
        {emp.name}
        {emp.is_delivery_staff && <span style={{ fontSize: 10, color: '#7c3aed' }}> DO</span>}
        {pendingAware && day.pending_request_id && (
          <span style={{ fontSize: 10, color: '#c2410c' }}> → {shiftsById[day.pending_shift_id]?.code || 'hapus'}</span>
        )}
        {day.status === 'pending' && <span style={{ fontSize: 10, color: '#c2410c' }}> (cuti?)</span>}
      </div>
    ));

    const th = { padding: '8px 10px', fontSize: 12, fontWeight: 600, background: '#f9fafb', borderBottom: '1px solid #e5e7eb', borderRight: '1px solid #e5e7eb', textAlign: 'left', verticalAlign: 'bottom' };
    const td = { padding: '6px 10px', fontSize: 12, borderBottom: '1px solid #e5e7eb', borderRight: '1px solid #e5e7eb', verticalAlign: 'top', minWidth: 120 };

    return (
      <div key={branch.id} style={{ marginBottom: '2rem' }}>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 6, flexWrap: 'wrap' }}>
          <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700 }}>{branch.name}</h3>
          {uncovered.length > 0 ? (
            <span style={{ fontSize: 12, color: '#b91c1c', fontWeight: 600 }}>
              ⚠ Divisi tanpa staf: {uncovered.map(d => d.name).join(', ')}
            </span>
          ) : branch.divisions.length > 0 && (
            <span style={{ fontSize: 12, color: '#15803d' }}>Semua divisi terisi</span>
          )}
        </div>
        <div style={{ overflowX: 'auto', border: '1px solid #e5e7eb', borderRadius: 6 }}>
          <table style={{ borderCollapse: 'collapse', width: '100%' }}>
            <thead>
              <tr>
                <th style={{ ...th, position: 'sticky', left: 0, zIndex: 1 }}>Divisi</th>
                {columns.map(s => (
                  <th key={s.id} style={{ ...th, background: s.color }}>
                    {s.name}{s.delivery_only && ' (DO)'}
                    <div style={{ fontWeight: 400, fontSize: 11, color: '#374151' }}>{shiftTimeLabel(s)}</div>
                  </th>
                ))}
                <th style={{ ...th, color: '#6b7280' }}>Belum dijadwalkan</th>
                <th style={{ ...th, background: '#1f2937', color: '#f9fafb' }}>Cuti / Libur</th>
              </tr>
            </thead>
            <tbody>
              {visibleRows.map(r => {
                const empty = r.id !== NO_DIVISION && rostered(r.id) === 0;
                return (
                  <tr key={r.id}>
                    <td style={{ ...td, fontWeight: 600, position: 'sticky', left: 0, background: empty ? '#fee2e2' : '#fff', minWidth: 110 }}>
                      {r.name}
                      <div style={{ fontWeight: 400, fontSize: 11, color: empty ? '#b91c1c' : '#6b7280' }}>
                        {empty ? 'tidak ada yang bertugas' : `${rostered(r.id)} bertugas`}
                      </div>
                    </td>
                    {columns.map(s => (
                      <td key={s.id} style={{ ...td, background: cell[r.id]?.[s.id] ? `${s.color}55` : '#fff' }}>
                        {names(cell[r.id]?.[s.id], true)}
                      </td>
                    ))}
                    <td style={{ ...td, color: '#6b7280' }}>{names(cell[r.id]?.none, true)}</td>
                    <td style={{ ...td, background: '#f3f4f6' }}>
                      {(cell[r.id]?.leave || []).map(({ emp, day }) => (
                        <div key={emp.id} title={day.leave_type}>{emp.name} <span style={{ fontSize: 10, color: '#6b7280' }}>{day.leave_type}</span></div>
                      ))}
                    </td>
                  </tr>
                );
              })}
              <tr>
                <td style={{ ...td, fontWeight: 700, background: '#f9fafb', position: 'sticky', left: 0 }}>Total</td>
                {columns.map(s => (
                  <td key={s.id} style={{ ...td, fontWeight: 700, background: '#f9fafb' }}>
                    {visibleRows.reduce((n, r) => n + (cell[r.id]?.[s.id]?.length || 0), 0)} orang
                  </td>
                ))}
                <td style={{ ...td, fontWeight: 700, background: '#f9fafb' }}>{visibleRows.reduce((n, r) => n + (cell[r.id]?.none?.length || 0), 0)} orang</td>
                <td style={{ ...td, fontWeight: 700, background: '#f9fafb' }}>{visibleRows.reduce((n, r) => n + (cell[r.id]?.leave?.length || 0), 0)} orang</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    );
  });
}
