import { useEffect, useState, useCallback } from 'react';
import {
  getLeaveRequests, approveLeaveRequest, rejectLeaveRequest,
  getOvertimeRequests, approveOvertimeRequest, rejectOvertimeRequest,
  getBranches,
} from '../../api';

const TYPE_META = {
  leave:    { label: 'Cuti',   bg: '#e3f2fd', color: '#1565c0' },
  overtime: { label: 'Lembur', bg: '#fff3e0', color: '#b45309' },
};

const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';

function getUser() {
  try { return JSON.parse(localStorage.getItem('user') || '{}'); } catch { return {}; }
}
const isManager = () => getUser()?.role === 'manager';

function TypeBadge({ type }) {
  const m = TYPE_META[type] || TYPE_META.leave;
  return <span style={{ background: m.bg, color: m.color, padding: '0.15rem 0.55rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.78rem' }}>{m.label}</span>;
}

function normLeave(rq) {
  return {
    key: `leave-${rq.id}`, id: rq.id, type: 'leave',
    employee_name: rq.employee_name, employee_code: rq.employee_code,
    branch_id: rq.branch_id ?? null,
    sortDate: rq.start_date,
    periodLabel: `${fmtDate(rq.start_date)} – ${fmtDate(rq.end_date)}`,
    detail: `${rq.day_count} hari kerja`,
    subtitle: rq.leave_type_name + (rq.is_paid === false ? ' (tanpa gaji)' : ''),
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
  };
}

const APPROVE_FNS = { leave: approveLeaveRequest, overtime: approveOvertimeRequest };
const REJECT_FNS = { leave: rejectLeaveRequest, overtime: rejectOvertimeRequest };

export default function Approvals() {
  const manager = isManager();
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [branches, setBranches] = useState([]);
  const [filters, setFilters] = useState({ type: '', branch_id: '' });
  const [decision, setDecision] = useState(null); // { action, request }

  useEffect(() => { getBranches().then(r => setBranches(r.data || [])).catch(() => setBranches([])); }, []);

  const load = useCallback(async () => {
    setLoading(true); setError('');
    const params = { status: 'pending' };
    if (filters.branch_id) params.branch_id = filters.branch_id;
    try {
      const [lv, ot] = await Promise.all([
        filters.type === 'overtime' ? Promise.resolve({ data: [] }) : getLeaveRequests(params).catch(() => ({ data: [] })),
        filters.type === 'leave' ? Promise.resolve({ data: [] }) : getOvertimeRequests(params).catch(() => ({ data: [] })),
      ]);
      const merged = [
        ...(Array.isArray(lv.data) ? lv.data : []).map(normLeave),
        ...(Array.isArray(ot.data) ? ot.data : []).map(normOvertime),
      ].sort((a, b) => new Date(a.sortDate) - new Date(b.sortDate)); // oldest first — process queue
      setRows(merged);
    } catch {
      setError('Gagal memuat antrian persetujuan');
      setRows([]);
    } finally {
      setLoading(false);
    }
  }, [filters]);

  useEffect(() => { load(); }, [load]);

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.35rem', flexWrap: 'wrap', gap: '0.5rem' }}>
        <h2 style={{ margin: 0 }}>Persetujuan</h2>
        <span style={{ fontSize: '0.85rem', color: '#667' }}>{rows.length} menunggu</span>
      </div>
      <p style={{ color: '#667', marginTop: 0, fontSize: '0.88rem' }}>Pengajuan cuti dan lembur yang menunggu keputusan manajer.</p>

      {!manager && (
        <div style={{ background: '#fff8e1', color: '#a06800', padding: '0.6rem 0.9rem', borderRadius: 8, marginBottom: '1rem', fontSize: '0.88rem' }}>
          Hanya manajer yang dapat menyetujui atau menolak. Anda dapat melihat daftar ini.
        </div>
      )}
      {error && <div style={errBox}>{error}</div>}

      <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem', flexWrap: 'wrap' }}>
        <select value={filters.type} onChange={e => setFilters({ ...filters, type: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }}>
          <option value="">Semua jenis</option>
          <option value="leave">Cuti</option>
          <option value="overtime">Lembur</option>
        </select>
        <select value={filters.branch_id} onChange={e => setFilters({ ...filters, branch_id: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }}>
          <option value="">Semua cabang</option>
          {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
        </select>
      </div>

      <div className="card" style={{ padding: 0, overflowX: 'auto' }}>
        <table style={{ width: '100%' }}>
          <thead>
            <tr>
              <th style={{ width: 80 }}>Jenis</th><th>Karyawan</th><th>Detail</th>
              <th>Tanggal</th><th style={{ width: 110, textAlign: 'center' }}>Jumlah</th>
              <th style={{ width: 1 }}>Aksi</th>
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={6} style={emptyCell}>Memuat…</td></tr>}
            {!loading && rows.length === 0 && <tr><td colSpan={6} style={emptyCell}>Tidak ada pengajuan menunggu.</td></tr>}
            {!loading && rows.map(r => (
              <tr key={r.key}>
                <td><TypeBadge type={r.type} /></td>
                <td>{r.employee_name}<div style={{ fontSize: '0.78rem', color: '#889' }}>{r.employee_code}</div></td>
                <td style={{ fontSize: '0.88rem', color: '#445' }}>{r.subtitle}</td>
                <td style={{ whiteSpace: 'nowrap', fontSize: '0.88rem' }}>{r.periodLabel}</td>
                <td style={{ textAlign: 'center' }}>{r.detail}</td>
                <td style={{ whiteSpace: 'nowrap' }}>
                  {manager && (
                    <span style={{ display: 'flex', gap: '0.3rem' }}>
                      <button className="btn btn-sm btn-primary" onClick={() => setDecision({ action: 'approve', request: r })}>Setujui</button>
                      <button className="btn btn-sm btn-secondary" onClick={() => setDecision({ action: 'reject', request: r })}>Tolak</button>
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {decision && <DecisionModal {...decision} onClose={() => setDecision(null)} onDone={() => { setDecision(null); load(); }} />}
    </div>
  );
}

function DecisionModal({ action, request, onClose, onDone }) {
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const titles = { approve: 'Setujui Pengajuan', reject: 'Tolak Pengajuan' };
  const fn = (action === 'approve' ? APPROVE_FNS : REJECT_FNS)[request.type];

  const run = async () => {
    setBusy(true); setError('');
    try { await fn(request.id, note); onDone(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal memproses.'); setBusy(false); }
  };

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>{titles[action]}</h3>
        <p style={{ fontSize: '0.9rem', color: '#445' }}>
          {TYPE_META[request.type].label} — {request.employee_name}<br />
          {request.periodLabel} ({request.detail})
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

const overlay = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 };
const modal = { width: '480px', maxWidth: '92vw', maxHeight: '90vh', overflowY: 'auto' };
const lbl = { display: 'block', fontSize: '0.78rem', color: '#667', marginTop: '0.6rem', marginBottom: '0.2rem' };
const inp = { width: '100%', padding: '0.5rem', border: '1px solid #d4d9e2', borderRadius: '6px', boxSizing: 'border-box' };
const errBox = { background: '#fce8e6', color: '#c5221f', padding: '0.5rem 0.75rem', borderRadius: '6px', fontSize: '0.85rem', marginBottom: '0.5rem' };
const emptyCell = { textAlign: 'center', color: '#888', padding: '1.5rem' };
