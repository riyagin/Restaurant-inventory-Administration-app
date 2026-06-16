import { useEffect, useState } from 'react';
import { getManpowerPlanning, getLeaveTypes, createLeaveRequest, getLeaveBalance } from '../../api';

const DAY_NAMES = ['Min', 'Sen', 'Sel', 'Rab', 'Kam', 'Jum', 'Sab'];

function todayLocal() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function formatDateHeader(dateStr) {
  const d = new Date(dateStr + 'T00:00:00');
  return `${DAY_NAMES[d.getDay()]}, ${d.getDate()}/${d.getMonth() + 1}`;
}

function formatDateHeaderCompact(dateStr) {
  const d = new Date(dateStr + 'T00:00:00');
  return `${d.getDate()}/${d.getMonth() + 1}`;
}

function formatDateLabel(dateStr) {
  const d = new Date(dateStr + 'T00:00:00');
  return d.toLocaleDateString('id-ID', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
}

const overlay = {
  position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)',
  display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
};

const modal = {
  background: '#fff', borderRadius: 8, padding: '1.5rem',
  width: '100%', maxWidth: 420, boxShadow: '0 8px 32px rgba(0,0,0,0.18)',
};

const inputStyle = {
  width: '100%', boxSizing: 'border-box', padding: '7px 10px',
  border: '1px solid #d1d5db', borderRadius: 4, fontSize: 13,
};

// ── Quick-create modal ────────────────────────────────────────────────────────
function QuickLeaveModal({ employee, date, onClose, onSaved }) {
  const [types, setTypes] = useState([]);
  const [form, setForm] = useState({
    employee_id: employee.id,
    leave_type_id: '',
    start_date: date,
    end_date: date,
    reason: '',
  });
  const [balance, setBalance] = useState(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getLeaveTypes({ active: 1 }).then(r => setTypes(r.data || [])).catch(() => setTypes([]));
  }, []);

  useEffect(() => {
    const t = types.find(t => t.id === form.leave_type_id);
    if (t?.uses_quota) {
      const year = new Date(form.start_date).getFullYear();
      getLeaveBalance(employee.id, year)
        .then(r => setBalance(r.data))
        .catch(() => setBalance(null));
    } else {
      setBalance(null);
    }
  }, [form.leave_type_id, form.start_date, types, employee.id]);

  const calendarDays = (() => {
    const s = new Date(form.start_date), e = new Date(form.end_date);
    if (isNaN(s) || isNaN(e) || e < s) return 0;
    return Math.round((e - s) / 86400000) + 1;
  })();

  const submit = async () => {
    setError('');
    if (!form.leave_type_id) { setError('Pilih jenis cuti.'); return; }
    if (new Date(form.end_date) < new Date(form.start_date)) {
      setError('Tanggal selesai harus setelah atau sama dengan tanggal mulai.');
      return;
    }
    setSaving(true);
    try {
      const r = await createLeaveRequest(form);
      onSaved(r.data);
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan pengajuan cuti.');
    } finally {
      setSaving(false);
    }
  };

  const field = (label, children) => (
    <div style={{ marginBottom: 12 }}>
      <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: '#374151', marginBottom: 4 }}>
        {label}
      </label>
      {children}
    </div>
  );

  return (
    <div style={overlay} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={modal}>
        <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700 }}>Ajukan Cuti</h3>
        <p style={{ margin: '0 0 16px', fontSize: 13, color: '#6b7280' }}>
          {employee.name}{employee.position ? ` · ${employee.position}` : ''}
        </p>

        {field('Jenis Cuti',
          <select
            style={inputStyle}
            value={form.leave_type_id}
            onChange={e => setForm(f => ({ ...f, leave_type_id: e.target.value }))}
          >
            <option value="">— Pilih jenis cuti —</option>
            {types.map(t => (
              <option key={t.id} value={t.id}>{t.name}</option>
            ))}
          </select>
        )}

        {balance && (
          <p style={{ fontSize: 12, color: '#6b7280', margin: '-6px 0 12px' }}>
            Sisa kuota: <strong>{balance.quota_days - balance.used_days} hari</strong>
          </p>
        )}

        {field('Tanggal Mulai',
          <input
            type="date"
            style={inputStyle}
            value={form.start_date}
            onChange={e => setForm(f => ({ ...f, start_date: e.target.value }))}
          />
        )}

        {field('Tanggal Selesai',
          <input
            type="date"
            style={inputStyle}
            value={form.end_date}
            min={form.start_date}
            onChange={e => setForm(f => ({ ...f, end_date: e.target.value }))}
          />
        )}

        {calendarDays > 0 && (
          <p style={{ fontSize: 12, color: '#6b7280', margin: '-6px 0 12px' }}>
            {calendarDays} hari kalender
          </p>
        )}

        {field('Keterangan (opsional)',
          <textarea
            style={{ ...inputStyle, resize: 'vertical', minHeight: 60 }}
            value={form.reason}
            onChange={e => setForm(f => ({ ...f, reason: e.target.value }))}
            placeholder="Alasan cuti..."
          />
        )}

        {error && <p style={{ color: '#dc2626', fontSize: 13, margin: '0 0 10px' }}>{error}</p>}

        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <button
            onClick={onClose}
            style={{ padding: '7px 16px', borderRadius: 4, border: '1px solid #d1d5db', background: '#fff', cursor: 'pointer', fontSize: 13 }}
          >
            Batal
          </button>
          <button
            onClick={submit}
            disabled={saving}
            style={{ padding: '7px 16px', borderRadius: 4, border: 'none', background: '#2563eb', color: '#fff', cursor: 'pointer', fontSize: 13, fontWeight: 600, opacity: saving ? 0.7 : 1 }}
          >
            {saving ? 'Menyimpan...' : 'Ajukan Cuti'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Info popover for existing/pending leave ──────────────────────────────────
function LeaveInfoModal({ employee, day, onClose }) {
  const isPending = day.status === 'pending';
  const boxStyle = isPending
    ? { background: '#fed7aa', border: '1px solid #fb923c', borderRadius: 6, padding: '12px 14px' }
    : { background: '#1f2937', border: '1px solid #111827', borderRadius: 6, padding: '12px 14px' };
  const titleColor = isPending ? '#9a3412' : '#f9fafb';
  const dateColor = isPending ? '#9a3412' : '#d1d5db';

  return (
    <div style={overlay} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={{ ...modal, maxWidth: 340 }}>
        <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700 }}>
          {isPending ? 'Pengajuan Cuti (Menunggu)' : 'Detail Cuti'}
        </h3>
        <p style={{ margin: '0 0 16px', fontSize: 13, color: '#6b7280' }}>
          {employee.name}{employee.position ? ` · ${employee.position}` : ''}
        </p>
        <div style={boxStyle}>
          <div style={{ fontWeight: 700, fontSize: 14, color: titleColor }}>{day.leave_type || 'Cuti'}</div>
          <div style={{ fontSize: 13, color: dateColor, marginTop: 4 }}>{formatDateLabel(day.date)}</div>
        </div>
        <p style={{ fontSize: 12, color: '#6b7280', marginTop: 10 }}>
          Untuk {isPending ? 'menyetujui/menolak' : 'mengubah atau membatalkan'} cuti ini, buka halaman <strong>Manajemen Cuti</strong>.
        </p>
        <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 12 }}>
          <button
            onClick={onClose}
            style={{ padding: '7px 16px', borderRadius: 4, border: '1px solid #d1d5db', background: '#fff', cursor: 'pointer', fontSize: 13 }}
          >
            Tutup
          </button>
        </div>
      </div>
    </div>
  );
}

function LegendItem({ color, label }) {
  return (
    <span style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: '#374151' }}>
      <span style={{ width: 14, height: 14, borderRadius: 3, background: color, border: '1px solid rgba(0,0,0,0.1)' }} />
      {label}
    </span>
  );
}

// ── Main page ─────────────────────────────────────────────────────────────────
const cellStyle = {
  padding: '8px 6px',
  textAlign: 'center',
  fontSize: 12,
  borderBottom: '1px solid #e5e7eb',
  borderRight: '1px solid #e5e7eb',
  minWidth: 90,
  maxWidth: 110,
  cursor: 'pointer',
  transition: 'filter 0.1s',
};

const headerCellStyle = {
  padding: '8px 6px',
  textAlign: 'center',
  fontSize: 12,
  fontWeight: 600,
  background: '#f9fafb',
  color: '#374151',
  borderBottom: '1px solid #e5e7eb',
  borderRight: '1px solid #e5e7eb',
  minWidth: 90,
  maxWidth: 110,
  position: 'sticky',
  top: 0,
  zIndex: 1,
};

// Compact mode (>10 days in view): narrower cells, no per-cell text — color only.
const compactCellStyle = {
  padding: '8px 2px',
  minWidth: 26,
  maxWidth: 30,
};

const compactHeaderCellStyle = {
  padding: '6px 2px',
  minWidth: 26,
  maxWidth: 30,
  fontSize: 10,
};

const nameCellStyle = {
  padding: '8px 10px',
  fontSize: 13,
  borderBottom: '1px solid #e5e7eb',
  borderRight: '1px solid #e5e7eb',
  minWidth: 180,
  position: 'sticky',
  left: 0,
  background: '#fff',
  zIndex: 0,
};

const nameHeaderStyle = {
  ...nameCellStyle,
  fontWeight: 600,
  background: '#f9fafb',
  color: '#374151',
  position: 'sticky',
  top: 0,
  left: 0,
  zIndex: 2,
  fontSize: 12,
};

const RANGE_PRESETS = [
  { label: '1 Minggu', days: 7 },
  { label: '2 Minggu', days: 14 },
  { label: '3 Minggu', days: 21 },
  { label: '1 Bulan', days: 31 },
];
const MIN_DAYS = 7;
const MAX_DAYS = 31;
const COMPACT_THRESHOLD = 10; // > this many days: hide per-cell text, color-only + legend

export default function ManpowerPlanning() {
  const [date, setDate] = useState(todayLocal());
  const [days, setDays] = useState(7);
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [modal, setModal] = useState(null); // { type: 'create'|'info', employee, day }

  const compact = days > COMPACT_THRESHOLD;

  const load = () => {
    setLoading(true);
    setError('');
    getManpowerPlanning({ date, days })
      .then(r => setData(r.data))
      .catch(() => setError('Gagal memuat data perencanaan tenaga kerja.'))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [date, days]);

  const clampDays = (v) => Math.min(MAX_DAYS, Math.max(MIN_DAYS, v));

  const handleCellClick = (employee, day) => {
    if (day.status === 'cuti' || day.status === 'pending') {
      setModal({ type: 'info', employee, day });
    } else {
      setModal({ type: 'create', employee, day });
    }
  };

  const handleSaved = () => {
    setModal(null);
    load();
  };

  const totalEmployees = data?.branches.reduce((s, b) => s + b.employees.length, 0) ?? 0;
  const countOnLeave = (employees) =>
    employees.reduce((s, e) => s + e.days.filter(d => d.status === 'cuti').length, 0);
  const countPending = (employees) =>
    employees.reduce((s, e) => s + e.days.filter(d => d.status === 'pending').length, 0);

  return (
    <div style={{ padding: '1.5rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem', flexWrap: 'wrap', gap: 8 }}>
        <h2 style={{ margin: 0, fontSize: 20, fontWeight: 700 }}>Perencanaan Tenaga Kerja</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          <label style={{ fontSize: 13, color: '#6b7280' }}>Mulai tanggal:</label>
          <input
            type="date"
            value={date}
            onChange={e => setDate(e.target.value)}
            style={{ fontSize: 13, padding: '4px 8px', border: '1px solid #d1d5db', borderRadius: 4 }}
          />
          <div style={{ display: 'flex', gap: 4 }}>
            {RANGE_PRESETS.map(p => (
              <button
                key={p.days}
                onClick={() => setDays(p.days)}
                style={{
                  fontSize: 12, padding: '4px 10px', borderRadius: 4, cursor: 'pointer',
                  border: days === p.days ? '1px solid #2563eb' : '1px solid #d1d5db',
                  background: days === p.days ? '#2563eb' : '#fff',
                  color: days === p.days ? '#fff' : '#374151',
                  fontWeight: days === p.days ? 600 : 400,
                }}
              >
                {p.label}
              </button>
            ))}
          </div>
          <label style={{ fontSize: 13, color: '#6b7280' }}>Hari:</label>
          <input
            type="number"
            min={MIN_DAYS}
            max={MAX_DAYS}
            value={days}
            onChange={e => setDays(clampDays(Number(e.target.value) || MIN_DAYS))}
            style={{ width: 56, fontSize: 13, padding: '4px 8px', border: '1px solid #d1d5db', borderRadius: 4 }}
          />
        </div>
      </div>

      <p style={{ fontSize: 12, color: '#9ca3af', margin: '0 0 1rem' }}>
        {compact ? 'Arahkan kursor ke kotak untuk detail. ' : ''}Klik kotak hari untuk mengajukan cuti karyawan.
      </p>

      {error && <p style={{ color: '#dc2626' }}>{error}</p>}
      {loading && <p style={{ color: '#6b7280' }}>Memuat...</p>}

      {!loading && data && (
        <>
          <p style={{ fontSize: 13, color: '#6b7280', marginBottom: '1rem' }}>
            {totalEmployees} karyawan aktif &mdash; {data.dates[0]} s/d {data.dates[data.dates.length - 1]}
          </p>

          {data.branches.length === 0 && (
            <p style={{ color: '#6b7280' }}>Tidak ada karyawan aktif.</p>
          )}

          {data.branches.map(branch => {
            const onLeaveCount = countOnLeave(branch.employees);
            const pendingCount = countPending(branch.employees);
            return (
              <div key={branch.id} style={{ marginBottom: '2rem' }}>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 6 }}>
                  <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700, color: '#111827' }}>
                    {branch.name}
                  </h3>
                  <span style={{ fontSize: 12, color: '#6b7280' }}>
                    {branch.employees.length} karyawan
                    {onLeaveCount > 0 && (
                      <span style={{ color: '#374151', marginLeft: 6 }}>
                        · {onLeaveCount} hari cuti di periode ini
                      </span>
                    )}
                    {pendingCount > 0 && (
                      <span style={{ color: '#c2410c', marginLeft: 6 }}>
                        · {pendingCount} hari menunggu persetujuan
                      </span>
                    )}
                  </span>
                </div>

                <div style={{ overflowX: 'auto', border: '1px solid #e5e7eb', borderRadius: 6 }}>
                  <table style={{ borderCollapse: 'collapse', width: '100%', minWidth: 600 }}>
                    <thead>
                      <tr>
                        <th style={nameHeaderStyle}>Karyawan</th>
                        {data.dates.map(d => (
                          <th key={d} style={compact ? { ...headerCellStyle, ...compactHeaderCellStyle } : headerCellStyle}>
                            {compact ? formatDateHeaderCompact(d) : formatDateHeader(d)}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {branch.employees.map(emp => (
                        <tr key={emp.id}>
                          <td style={nameCellStyle}>
                            <div style={{ fontWeight: 500, color: '#111827' }}>{emp.name}</div>
                            {emp.position && (
                              <div style={{ fontSize: 11, color: '#9ca3af' }}>{emp.position}</div>
                            )}
                          </td>
                          {emp.days.map(day => {
                            const cellColors = {
                              cuti: { background: '#1f2937', color: '#f9fafb' },
                              pending: { background: '#fed7aa', color: '#9a3412' },
                              hadir: { background: '#dcfce7', color: '#15803d' },
                            }[day.status] || { background: '#dcfce7', color: '#15803d' };
                            const cellTitle = day.status === 'cuti'
                              ? `${day.leave_type} (disetujui) — klik untuk detail`
                              : day.status === 'pending'
                                ? `${day.leave_type} (menunggu persetujuan) — klik untuk detail`
                                : 'Klik untuk ajukan cuti';
                            return (
                              <td
                                key={day.date}
                                title={cellTitle}
                                onClick={() => handleCellClick(emp, day)}
                                style={{ ...cellStyle, ...(compact ? compactCellStyle : null), ...cellColors }}
                                onMouseEnter={e => e.currentTarget.style.filter = 'brightness(0.93)'}
                                onMouseLeave={e => e.currentTarget.style.filter = ''}
                              >
                                {compact ? null : day.status === 'cuti' || day.status === 'pending' ? (
                                  <span style={{
                                    display: 'block',
                                    fontSize: 11,
                                    fontWeight: 600,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                  }}>
                                    {day.leave_type || 'Cuti'}
                                  </span>
                                ) : (
                                  <span style={{ fontSize: 15, fontWeight: 700 }}>✓</span>
                                )}
                              </td>
                            );
                          })}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            );
          })}

          <div style={{ display: 'flex', alignItems: 'center', gap: 16, flexWrap: 'wrap', marginTop: 4, padding: '10px 12px', background: '#f9fafb', border: '1px solid #e5e7eb', borderRadius: 6 }}>
            <span style={{ fontSize: 12, fontWeight: 600, color: '#374151' }}>Keterangan:</span>
            <LegendItem color="#dcfce7" label="Hadir" />
            <LegendItem color="#fed7aa" label="Menunggu Persetujuan" />
            <LegendItem color="#1f2937" label="Cuti (Disetujui)" />
          </div>
        </>
      )}

      {modal?.type === 'create' && (
        <QuickLeaveModal
          employee={modal.employee}
          date={modal.day.date}
          onClose={() => setModal(null)}
          onSaved={handleSaved}
        />
      )}

      {modal?.type === 'info' && (
        <LeaveInfoModal
          employee={modal.employee}
          day={modal.day}
          onClose={() => setModal(null)}
        />
      )}
    </div>
  );
}
