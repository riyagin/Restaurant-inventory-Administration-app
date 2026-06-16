import { useEffect, useState, useCallback } from 'react';
import {
  getLeaveRequests, createLeaveRequest, approveLeaveRequest, rejectLeaveRequest,
  cancelLeaveRequest, getLeaveTypes, getEmployees, getBranches, getLeaveBalance,
  bulkApproveLeaveRequests, bulkRejectLeaveRequests,
} from '../../api';

const STATUS_LABELS = {
  pending: 'Menunggu',
  approved: 'Disetujui',
  rejected: 'Ditolak',
  cancelled: 'Dibatalkan',
};
const STATUS_COLORS = {
  pending: { bg: '#fff8e1', color: '#a06800' },
  approved: { bg: '#e6f4ea', color: '#1e7e34' },
  rejected: { bg: '#fce8e6', color: '#c5221f' },
  cancelled: { bg: '#eef1f6', color: '#667' },
};

const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';
const todayStr = () => new Date().toISOString().slice(0, 10);

function getUser() {
  try { return JSON.parse(localStorage.getItem('user') || '{}'); } catch { return {}; }
}
const isManager = () => getUser()?.role === 'manager';

function StatusChip({ status }) {
  const c = STATUS_COLORS[status] || STATUS_COLORS.cancelled;
  return (
    <span style={{ background: c.bg, color: c.color, padding: '0.15rem 0.55rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.8rem' }}>
      {STATUS_LABELS[status] || status}
    </span>
  );
}

// ── Create modal ─────────────────────────────────────────────────────────────
function CreateModal({ onClose, onSaved }) {
  const [employees, setEmployees] = useState([]);
  const [types, setTypes] = useState([]);
  const [form, setForm] = useState({ employee_id: '', leave_type_id: '', start_date: todayStr(), end_date: todayStr(), reason: '' });
  const [balance, setBalance] = useState(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getEmployees().then(r => setEmployees(r.data || [])).catch(() => setEmployees([]));
    getLeaveTypes({ active: 1 }).then(r => setTypes(r.data || [])).catch(() => setTypes([]));
  }, []);

  // Fetch the remaining quota preview when an employee + quota type is selected.
  useEffect(() => {
    const t = types.find(t => t.id === form.leave_type_id);
    if (form.employee_id && t?.uses_quota) {
      const year = new Date(form.start_date).getFullYear();
      getLeaveBalance(form.employee_id, year).then(r => setBalance(r.data)).catch(() => setBalance(null));
    } else {
      setBalance(null);
    }
  }, [form.employee_id, form.leave_type_id, form.start_date, types]);

  // Rough client-side calendar day span (server authoritatively computes work days).
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
    try {
      const r = await createLeaveRequest(form);
      onSaved(r.data);
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan pengajuan cuti.');
    } finally {
      setSaving(false);
    }
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
          {balance && (
            <span>Sisa kuota: <strong>{balance.quota_days - balance.used_days}</strong> / {balance.quota_days} hari</span>
          )}
        </div>
        <p style={{ fontSize: '0.78rem', color: '#888', margin: '0 0 0.5rem' }}>
          Jumlah hari kerja akan dihitung otomatis oleh sistem (mengabaikan hari libur dan hari non-kerja).
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

// ── Decision modal (approve / reject / cancel with note) ─────────────────────
function DecisionModal({ action, request, onClose, onDone }) {
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const titles = { approve: 'Setujui Pengajuan', reject: 'Tolak Pengajuan', cancel: 'Batalkan Pengajuan' };
  const fns = { approve: approveLeaveRequest, reject: rejectLeaveRequest, cancel: cancelLeaveRequest };

  const run = async () => {
    setBusy(true); setError('');
    try {
      const r = await fns[action](request.id, note);
      onDone(action, r.data);
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal memproses.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>{titles[action]}</h3>
        <p style={{ fontSize: '0.9rem', color: '#445' }}>
          {request.employee_name} — {request.leave_type_name}<br />
          {fmtDate(request.start_date)} s/d {fmtDate(request.end_date)} ({request.day_count} hari kerja)
        </p>
        {error && <div style={errBox}>{error}</div>}
        <label style={lbl}>Catatan {action === 'reject' ? '(disarankan)' : '(opsional)'}</label>
        <textarea value={note} onChange={e => setNote(e.target.value)} rows={2} style={{ ...inp, resize: 'vertical' }} />
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={busy}>Tutup</button>
          <button className="btn btn-primary" onClick={run} disabled={busy}>{busy ? 'Memproses…' : titles[action]}</button>
        </div>
      </div>
    </div>
  );
}

// ── Bulk decision modal (approve / reject multiple at once with one note) ────
function BulkDecisionModal({ action, ids, onClose, onDone }) {
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [results, setResults] = useState(null);

  const titles = { approve: 'Setujui Pengajuan Terpilih', reject: 'Tolak Pengajuan Terpilih' };
  const fns = { approve: bulkApproveLeaveRequests, reject: bulkRejectLeaveRequests };

  const run = async () => {
    setBusy(true); setError('');
    try {
      const r = await fns[action](ids, note);
      setResults(r.data?.results || []);
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal memproses.');
    } finally {
      setBusy(false);
    }
  };

  const successCount = results ? results.filter(res => res.success).length : 0;
  const failCount = results ? results.filter(res => !res.success).length : 0;

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>{titles[action]}</h3>
        <p style={{ fontSize: '0.9rem', color: '#445' }}>{ids.length} pengajuan dipilih.</p>
        {error && <div style={errBox}>{error}</div>}

        {!results && (<>
          <label style={lbl}>Catatan {action === 'reject' ? '(disarankan)' : '(opsional)'}</label>
          <textarea value={note} onChange={e => setNote(e.target.value)} rows={2} style={{ ...inp, resize: 'vertical' }} />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
            <button className="btn btn-secondary" onClick={onClose} disabled={busy}>Tutup</button>
            <button className="btn btn-primary" onClick={run} disabled={busy}>{busy ? 'Memproses…' : titles[action]}</button>
          </div>
        </>)}

        {results && (<>
          <div style={{ fontSize: '0.88rem', margin: '0.5rem 0' }}>
            <strong>{successCount}</strong> berhasil{failCount > 0 && <>, <strong>{failCount}</strong> gagal</>}.
          </div>
          {failCount > 0 && (
            <ul style={{ fontSize: '0.82rem', color: '#c5221f', paddingLeft: '1.1rem', margin: '0 0 0.5rem' }}>
              {results.filter(res => !res.success).map(res => (
                <li key={res.id}>{res.error}</li>
              ))}
            </ul>
          )}
          <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '1rem' }}>
            <button className="btn btn-primary" onClick={() => onDone()}>Tutup</button>
          </div>
        </>)}
      </div>
    </div>
  );
}

export default function LeaveRequests() {
  const [tab, setTab] = useState('pending');
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [branches, setBranches] = useState([]);
  const [filters, setFilters] = useState({ status: '', branch_id: '', year: '' });
  const [showCreate, setShowCreate] = useState(false);
  const [decision, setDecision] = useState(null); // { action, request }
  const [bulkDecision, setBulkDecision] = useState(null); // { action, ids }
  const [selected, setSelected] = useState(new Set());
  const manager = isManager();

  useEffect(() => { getBranches().then(r => setBranches(r.data || [])).catch(() => setBranches([])); }, []);

  const load = useCallback(() => {
    setLoading(true);
    const params = tab === 'pending'
      ? { status: 'pending' }
      : { status: filters.status, branch_id: filters.branch_id, year: filters.year };
    Object.keys(params).forEach(k => { if (!params[k]) delete params[k]; });
    getLeaveRequests(params)
      .then(r => setRows(r.data || []))
      .catch(() => setRows([]))
      .finally(() => setLoading(false));
  }, [tab, filters]);

  useEffect(() => { load(); }, [load]);

  const switchTab = (t) => { setTab(t); setSelected(new Set()); };

  const onDecision = () => { setDecision(null); load(); };
  const onBulkDecision = () => { setBulkDecision(null); setSelected(new Set()); load(); };

  const pendingRows = rows.filter(rq => rq.status === 'pending');
  const allPendingSelected = pendingRows.length > 0 && pendingRows.every(rq => selected.has(rq.id));

  const toggleSelected = (id) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };

  const toggleSelectAll = () => {
    setSelected(allPendingSelected ? new Set() : new Set(pendingRows.map(rq => rq.id)));
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
        <h2 style={{ margin: 0 }}>Cuti</h2>
        <button className="btn btn-primary" onClick={() => setShowCreate(true)}>+ Pengajuan Cuti</button>
      </div>

      <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem', borderBottom: '1px solid #e3e6ec' }}>
        <TabBtn active={tab === 'pending'} onClick={() => switchTab('pending')}>Menunggu Persetujuan</TabBtn>
        <TabBtn active={tab === 'all'} onClick={() => switchTab('all')}>Semua Pengajuan</TabBtn>
      </div>

      {tab === 'all' && (
        <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem', flexWrap: 'wrap' }}>
          <select value={filters.status} onChange={e => setFilters({ ...filters, status: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }}>
            <option value="">Semua status</option>
            {Object.entries(STATUS_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
          </select>
          <select value={filters.branch_id} onChange={e => setFilters({ ...filters, branch_id: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }}>
            <option value="">Semua cabang</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
          <input type="number" placeholder="Tahun" value={filters.year} onChange={e => setFilters({ ...filters, year: e.target.value })} style={{ ...inp, width: '110px', margin: 0 }} />
        </div>
      )}

      {tab === 'pending' && manager && selected.size > 0 && (
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.75rem' }}>
          <span style={{ fontSize: '0.85rem', color: '#445' }}>{selected.size} dipilih</span>
          <button className="btn btn-sm btn-primary" onClick={() => setBulkDecision({ action: 'approve', ids: [...selected] })}>Setujui Terpilih</button>
          <button className="btn btn-sm btn-secondary" onClick={() => setBulkDecision({ action: 'reject', ids: [...selected] })}>Tolak Terpilih</button>
        </div>
      )}

      <div className="card" style={{ padding: 0, overflowX: 'auto' }}>
        <table style={{ width: '100%' }}>
          <thead>
            <tr>
              {tab === 'pending' && manager && (
                <th style={{ width: 36, textAlign: 'center' }}>
                  <input type="checkbox" checked={allPendingSelected} onChange={toggleSelectAll} disabled={pendingRows.length === 0} />
                </th>
              )}
              <th>Karyawan</th><th>Jenis</th><th>Periode</th>
              <th style={{ textAlign: 'center', width: 60 }}>Hari</th><th style={{ width: 100 }}>Status</th><th style={{ width: 1 }}>Aksi</th>
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={7} style={{ textAlign: 'center', color: '#888', padding: '1.5rem' }}>Memuat…</td></tr>}
            {!loading && rows.length === 0 && <tr><td colSpan={7} style={{ textAlign: 'center', color: '#888', padding: '1.5rem' }}>Tidak ada pengajuan.</td></tr>}
            {!loading && rows.map(rq => (
              <tr key={rq.id}>
                {tab === 'pending' && manager && (
                  <td style={{ textAlign: 'center' }}>
                    <input type="checkbox" checked={selected.has(rq.id)} onChange={() => toggleSelected(rq.id)} />
                  </td>
                )}
                <td>{rq.employee_name}<div style={{ fontSize: '0.78rem', color: '#889' }}>{rq.employee_code}</div></td>
                <td>{rq.leave_type_name}{!rq.is_paid && <span style={{ fontSize: '0.72rem', color: '#c5221f' }}> (tanpa gaji)</span>}</td>
                <td style={{ whiteSpace: 'nowrap', fontSize: '0.88rem' }}>{fmtDate(rq.start_date)} – {fmtDate(rq.end_date)}</td>
                <td style={{ textAlign: 'center' }}>{rq.day_count}</td>
                <td><StatusChip status={rq.status} /></td>
                <td style={{ whiteSpace: 'nowrap' }}>
                  <span style={{ display: 'flex', gap: '0.3rem', flexWrap: 'nowrap' }}>
                    {rq.status === 'pending' && manager && (<>
                      <button className="btn btn-sm btn-primary" onClick={() => setDecision({ action: 'approve', request: rq })}>Setujui</button>
                      <button className="btn btn-sm btn-secondary" onClick={() => setDecision({ action: 'reject', request: rq })}>Tolak</button>
                    </>)}
                    {(rq.status === 'pending' || rq.status === 'approved') && (
                      <button className="btn btn-sm btn-link" onClick={() => setDecision({ action: 'cancel', request: rq })}>Batalkan</button>
                    )}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {showCreate && <CreateModal onClose={() => setShowCreate(false)} onSaved={() => { setShowCreate(false); load(); }} />}
      {decision && <DecisionModal action={decision.action} request={decision.request} onClose={() => setDecision(null)} onDone={onDecision} />}
      {bulkDecision && <BulkDecisionModal action={bulkDecision.action} ids={bulkDecision.ids} onClose={() => setBulkDecision(null)} onDone={onBulkDecision} />}
    </div>
  );
}

function TabBtn({ active, onClick, children }) {
  return (
    <button onClick={onClick} style={{
      background: 'none', border: 'none', padding: '0.6rem 1rem', cursor: 'pointer',
      borderBottom: active ? '2px solid #2563eb' : '2px solid transparent',
      color: active ? '#2563eb' : '#667', fontWeight: active ? 600 : 500,
    }}>{children}</button>
  );
}

const overlay = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 };
const modal = { width: '480px', maxWidth: '92vw', maxHeight: '90vh', overflowY: 'auto' };
const lbl = { display: 'block', fontSize: '0.78rem', color: '#667', marginTop: '0.6rem', marginBottom: '0.2rem' };
const inp = { width: '100%', padding: '0.5rem', border: '1px solid #d4d9e2', borderRadius: '6px', boxSizing: 'border-box' };
const errBox = { background: '#fce8e6', color: '#c5221f', padding: '0.5rem 0.75rem', borderRadius: '6px', fontSize: '0.85rem', marginBottom: '0.5rem' };
