import { useEffect, useState, useCallback } from 'react';
import {
  getLeaveRequests, createLeaveRequest, cancelLeaveRequest, getLeaveTypes, getLeaveBalance,
  getOvertimeRequests, createOvertimeRequest, cancelOvertimeRequest,
  getEmployees, getBranches,
} from '../../api';

// ── shared status styling ─────────────────────────────────────────────────────
const STATUS_LABELS = { pending: 'Menunggu', approved: 'Disetujui', rejected: 'Ditolak', cancelled: 'Dibatalkan' };
const STATUS_COLORS = {
  pending: { bg: '#fff8e1', color: '#a06800' },
  approved: { bg: '#e6f4ea', color: '#1e7e34' },
  rejected: { bg: '#fce8e6', color: '#c5221f' },
  cancelled: { bg: '#eef1f6', color: '#667' },
};
const TYPE_META = {
  leave:    { label: 'Cuti',   bg: '#e3f2fd', color: '#1565c0' },
  overtime: { label: 'Lembur', bg: '#fff3e0', color: '#b45309' },
};

const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';
const todayStr = () => new Date().toISOString().slice(0, 10);
const monthOf = (d) => (d ? new Date(d).toISOString().slice(0, 7) : '');

function StatusChip({ status }) {
  const c = STATUS_COLORS[status] || STATUS_COLORS.cancelled;
  return <span style={{ background: c.bg, color: c.color, padding: '0.15rem 0.55rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.8rem' }}>{STATUS_LABELS[status] || status}</span>;
}
function TypeBadge({ type }) {
  const m = TYPE_META[type] || TYPE_META.leave;
  return <span style={{ background: m.bg, color: m.color, padding: '0.15rem 0.55rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.78rem' }}>{m.label}</span>;
}

// Normalize a leave request into the unified row shape.
function normLeave(rq) {
  return {
    key: `leave-${rq.id}`, id: rq.id, type: 'leave',
    employee_name: rq.employee_name, employee_code: rq.employee_code,
    branch_id: rq.branch_id ?? null,
    sortDate: rq.start_date,
    periodLabel: `${fmtDate(rq.start_date)} – ${fmtDate(rq.end_date)}`,
    detail: `${rq.day_count} hari`,
    subtitle: rq.leave_type_name + (rq.is_paid === false ? ' (tanpa gaji)' : ''),
    status: rq.status,
  };
}
function normOvertime(o) {
  return {
    key: `ot-${o.id}`, id: o.id, type: 'overtime',
    employee_name: o.employee_name, employee_code: o.employee_code,
    branch_id: o.branch_id ?? null,
    sortDate: o.date,
    periodLabel: fmtDate(o.date),
    detail: `${Number(o.hours).toFixed(1)} jam`,
    subtitle: o.reason || '—',
    status: o.status,
  };
}

export default function Requests() {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [branches, setBranches] = useState([]);

  const [filters, setFilters] = useState({ type: '', status: '', branch_id: '', month: '' });
  const [showAjukan, setShowAjukan] = useState(false);
  const [createType, setCreateType] = useState(null); // 'leave' | 'overtime'
  const [cancelTarget, setCancelTarget] = useState(null);

  useEffect(() => { getBranches().then(r => setBranches(r.data || [])).catch(() => setBranches([])); }, []);

  const load = useCallback(async () => {
    setLoading(true); setError('');
    const shared = {};
    if (filters.status) shared.status = filters.status;
    if (filters.branch_id) shared.branch_id = filters.branch_id;
    try {
      const [lv, ot] = await Promise.all([
        filters.type === 'overtime' ? Promise.resolve({ data: [] }) : getLeaveRequests(shared).catch(() => ({ data: [] })),
        filters.type === 'leave' ? Promise.resolve({ data: [] }) : getOvertimeRequests(shared).catch(() => ({ data: [] })),
      ]);
      let merged = [
        ...(Array.isArray(lv.data) ? lv.data : []).map(normLeave),
        ...(Array.isArray(ot.data) ? ot.data : []).map(normOvertime),
      ];
      if (filters.month) merged = merged.filter(r => monthOf(r.sortDate) === filters.month);
      merged.sort((a, b) => new Date(b.sortDate) - new Date(a.sortDate));
      setRows(merged);
    } catch {
      setError('Gagal memuat pengajuan');
      setRows([]);
    } finally {
      setLoading(false);
    }
  }, [filters]);

  useEffect(() => { load(); }, [load]);

  const doCancel = async (note) => {
    const t = cancelTarget;
    const fn = t.type === 'leave' ? cancelLeaveRequest : cancelOvertimeRequest;
    await fn(t.id, note);
    setCancelTarget(null);
    load();
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', flexWrap: 'wrap', gap: '0.5rem' }}>
        <h2 style={{ margin: 0 }}>Pengajuan</h2>
        <div style={{ position: 'relative' }}>
          <button className="btn btn-primary" onClick={() => setShowAjukan(v => !v)}>+ Ajukan ▾</button>
          {showAjukan && (
            <div style={{ position: 'absolute', right: 0, top: '110%', background: '#fff', border: '1px solid #e3e6ec', borderRadius: 8, boxShadow: '0 4px 16px rgba(0,0,0,.12)', zIndex: 50, minWidth: 160 }}>
              <button style={menuItem} onClick={() => { setCreateType('leave'); setShowAjukan(false); }}>Cuti</button>
              <button style={menuItem} onClick={() => { setCreateType('overtime'); setShowAjukan(false); }}>Lembur</button>
            </div>
          )}
        </div>
      </div>

      {error && <div style={errBox}>{error}</div>}

      {/* Filters */}
      <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem', flexWrap: 'wrap' }}>
        <select value={filters.type} onChange={e => setFilters({ ...filters, type: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }}>
          <option value="">Semua jenis</option>
          <option value="leave">Cuti</option>
          <option value="overtime">Lembur</option>
        </select>
        <select value={filters.status} onChange={e => setFilters({ ...filters, status: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }}>
          <option value="">Semua status</option>
          {Object.entries(STATUS_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <select value={filters.branch_id} onChange={e => setFilters({ ...filters, branch_id: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }}>
          <option value="">Semua cabang</option>
          {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
        </select>
        <input type="month" value={filters.month} onChange={e => setFilters({ ...filters, month: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }} />
        {filters.month && <button className="btn btn-secondary btn-sm" onClick={() => setFilters({ ...filters, month: '' })}>Reset bulan</button>}
      </div>

      <div className="card" style={{ padding: 0, overflowX: 'auto' }}>
        <table style={{ width: '100%' }}>
          <thead>
            <tr>
              <th style={{ width: 80 }}>Jenis</th><th>Karyawan</th><th>Detail</th>
              <th>Tanggal</th><th style={{ width: 90, textAlign: 'center' }}>Jumlah</th>
              <th style={{ width: 100 }}>Status</th><th style={{ width: 1 }}>Aksi</th>
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={7} style={emptyCell}>Memuat…</td></tr>}
            {!loading && rows.length === 0 && <tr><td colSpan={7} style={emptyCell}>Tidak ada pengajuan.</td></tr>}
            {!loading && rows.map(r => (
              <tr key={r.key}>
                <td><TypeBadge type={r.type} /></td>
                <td>{r.employee_name}<div style={{ fontSize: '0.78rem', color: '#889' }}>{r.employee_code}</div></td>
                <td style={{ fontSize: '0.88rem', color: '#445' }}>{r.subtitle}</td>
                <td style={{ whiteSpace: 'nowrap', fontSize: '0.88rem' }}>{r.periodLabel}</td>
                <td style={{ textAlign: 'center' }}>{r.detail}</td>
                <td><StatusChip status={r.status} /></td>
                <td style={{ whiteSpace: 'nowrap' }}>
                  {(r.status === 'pending' || r.status === 'approved') && (
                    <button className="btn btn-sm btn-link" onClick={() => setCancelTarget(r)}>Batalkan</button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {createType === 'leave' && <LeaveCreateModal onClose={() => setCreateType(null)} onSaved={() => { setCreateType(null); load(); }} />}
      {createType === 'overtime' && <OvertimeCreateModal onClose={() => setCreateType(null)} onSaved={() => { setCreateType(null); load(); }} />}
      {cancelTarget && <CancelModal target={cancelTarget} onClose={() => setCancelTarget(null)} onDone={doCancel} />}
    </div>
  );
}

// ── Leave create modal (with quota preview) ──────────────────────────────────
function LeaveCreateModal({ onClose, onSaved }) {
  const [employees, setEmployees] = useState([]);
  const [types, setTypes] = useState([]);
  const [form, setForm] = useState({ employee_id: '', leave_type_id: '', start_date: todayStr(), end_date: todayStr(), reason: '' });
  const [balance, setBalance] = useState(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getEmployees().then(r => setEmployees(r.data?.data || [])).catch(() => setEmployees([]));
    getLeaveTypes({ active: 1 }).then(r => setTypes(r.data || [])).catch(() => setTypes([]));
  }, []);

  useEffect(() => {
    const t = types.find(t => t.id === form.leave_type_id);
    if (form.employee_id && t?.uses_quota) {
      const year = new Date(form.start_date).getFullYear();
      getLeaveBalance(form.employee_id, year).then(r => setBalance(r.data)).catch(() => setBalance(null));
    } else setBalance(null);
  }, [form.employee_id, form.leave_type_id, form.start_date, types]);

  const calendarDays = (() => {
    const s = new Date(form.start_date), e = new Date(form.end_date);
    if (isNaN(s) || isNaN(e) || e < s) return 0;
    return Math.round((e - s) / 86400000) + 1;
  })();

  const submit = async () => {
    setError('');
    if (!form.employee_id) { setError('Pilih karyawan.'); return; }
    if (!form.leave_type_id) { setError('Pilih jenis cuti.'); return; }
    if (new Date(form.end_date) < new Date(form.start_date)) { setError('Tanggal selesai harus setelah atau sama dengan tanggal mulai.'); return; }
    setSaving(true);
    try { await createLeaveRequest(form); onSaved(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal menyimpan pengajuan cuti.'); }
    finally { setSaving(false); }
  };

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Pengajuan Cuti Baru</h3>
        {error && <div style={errBox}>{error}</div>}

        <label style={lbl}>Karyawan</label>
        <select value={form.employee_id} onChange={e => setForm({ ...form, employee_id: e.target.value })} style={inp}>
          <option value="">— Pilih karyawan —</option>
          {employees.map(emp => <option key={emp.id} value={emp.id}>{emp.full_name} ({emp.employee_code})</option>)}
        </select>

        <label style={lbl}>Jenis Cuti</label>
        <select value={form.leave_type_id} onChange={e => setForm({ ...form, leave_type_id: e.target.value })} style={inp}>
          <option value="">— Pilih jenis —</option>
          {types.map(t => <option key={t.id} value={t.id}>{t.name}{t.uses_quota ? ' (kuota)' : ''}{!t.is_paid ? ' — tanpa gaji' : ''}</option>)}
        </select>

        <div style={{ display: 'flex', gap: '0.75rem' }}>
          <div style={{ flex: 1 }}>
            <label style={lbl}>Tanggal Mulai</label>
            <input type="date" value={form.start_date} onChange={e => setForm({ ...form, start_date: e.target.value })} style={inp} />
          </div>
          <div style={{ flex: 1 }}>
            <label style={lbl}>Tanggal Selesai</label>
            <input type="date" value={form.end_date} onChange={e => setForm({ ...form, end_date: e.target.value })} style={inp} />
          </div>
        </div>

        <div style={{ display: 'flex', gap: '1rem', margin: '0.5rem 0', fontSize: '0.85rem', color: '#445' }}>
          <span>Rentang: <strong>{calendarDays}</strong> hari kalender</span>
          {balance && <span>Sisa kuota: <strong>{balance.quota_days - balance.used_days}</strong> / {balance.quota_days} hari</span>}
        </div>
        <p style={{ fontSize: '0.78rem', color: '#888', margin: '0 0 0.5rem' }}>
          Jumlah hari kerja dihitung otomatis oleh sistem (mengabaikan hari libur dan hari non-kerja).
        </p>

        <label style={lbl}>Alasan</label>
        <textarea value={form.reason} onChange={e => setForm({ ...form, reason: e.target.value })} rows={2} style={{ ...inp, resize: 'vertical' }} />

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={saving}>Batal</button>
          <button className="btn btn-primary" onClick={submit} disabled={saving}>{saving ? 'Menyimpan…' : 'Ajukan'}</button>
        </div>
      </div>
    </div>
  );
}

// ── Overtime create modal ────────────────────────────────────────────────────
function OvertimeCreateModal({ onClose, onSaved }) {
  const [employees, setEmployees] = useState([]);
  const [form, setForm] = useState({ employee_id: '', date: todayStr(), hours: '', reason: '' });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getEmployees().then(r => setEmployees(r.data?.data || [])).catch(() => setEmployees([]));
  }, []);

  const submit = async () => {
    setError('');
    if (!form.employee_id) { setError('Pilih karyawan.'); return; }
    if (!form.date) { setError('Masukkan tanggal.'); return; }
    if (!form.hours || Number(form.hours) <= 0) { setError('Jam lembur harus lebih dari 0.'); return; }
    setSaving(true);
    try { await createOvertimeRequest({ ...form, hours: Number(form.hours) }); onSaved(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal menyimpan pengajuan lembur.'); }
    finally { setSaving(false); }
  };

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Pengajuan Lembur Baru</h3>
        {error && <div style={errBox}>{error}</div>}

        <label style={lbl}>Karyawan</label>
        <select value={form.employee_id} onChange={e => setForm({ ...form, employee_id: e.target.value })} style={inp}>
          <option value="">— Pilih karyawan —</option>
          {employees.map(emp => <option key={emp.id} value={emp.id}>{emp.full_name} ({emp.employee_code})</option>)}
        </select>

        <label style={lbl}>Tanggal Lembur</label>
        <input type="date" value={form.date} onChange={e => setForm({ ...form, date: e.target.value })} style={inp} />

        <label style={lbl}>Jumlah Jam Lembur</label>
        <input type="number" step="0.5" min="0.5" max="24" value={form.hours} placeholder="mis. 2.5"
          onChange={e => setForm({ ...form, hours: e.target.value })} style={inp} />

        <label style={lbl}>Keterangan (opsional)</label>
        <textarea value={form.reason} onChange={e => setForm({ ...form, reason: e.target.value })} rows={2} style={{ ...inp, resize: 'vertical' }} />

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={saving}>Batal</button>
          <button className="btn btn-primary" onClick={submit} disabled={saving}>{saving ? 'Menyimpan…' : 'Ajukan'}</button>
        </div>
      </div>
    </div>
  );
}

// ── Cancel modal (optional note) ─────────────────────────────────────────────
function CancelModal({ target, onClose, onDone }) {
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const run = async () => {
    setBusy(true); setError('');
    try { await onDone(note); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal membatalkan.'); setBusy(false); }
  };

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Batalkan Pengajuan</h3>
        <p style={{ fontSize: '0.9rem', color: '#445' }}>
          {TYPE_META[target.type].label} — {target.employee_name}<br />{target.periodLabel} ({target.detail})
        </p>
        {error && <div style={errBox}>{error}</div>}
        <label style={lbl}>Catatan (opsional)</label>
        <textarea value={note} onChange={e => setNote(e.target.value)} rows={2} style={{ ...inp, resize: 'vertical' }} />
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={busy}>Tutup</button>
          <button className="btn btn-primary" onClick={run} disabled={busy}>{busy ? 'Memproses…' : 'Batalkan Pengajuan'}</button>
        </div>
      </div>
    </div>
  );
}

const overlay = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 };
const modal = { width: '480px', maxWidth: '92vw', maxHeight: '90vh', overflowY: 'auto' };
const lbl = { display: 'block', fontSize: '0.78rem', color: '#667', marginTop: '0.6rem', marginBottom: '0.2rem' };
const inp = { width: '100%', padding: '0.5rem', border: '1px solid #d4d9e2', borderRadius: '6px', boxSizing: 'border-box' };
const errBox = { background: '#fce8e6', color: '#c5221f', padding: '0.5rem 0.75rem', borderRadius: '6px', fontSize: '0.85rem', marginBottom: '0.5rem' };
const emptyCell = { textAlign: 'center', color: '#888', padding: '1.5rem' };
const menuItem = { display: 'block', width: '100%', textAlign: 'left', padding: '0.55rem 0.9rem', background: 'none', border: 'none', cursor: 'pointer', fontSize: '0.9rem' };
