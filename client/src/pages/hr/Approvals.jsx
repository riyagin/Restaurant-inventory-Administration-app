import { useEffect, useState, useCallback } from 'react';
import {
  getLeaveRequests, approveLeaveRequest, rejectLeaveRequest,
  getOvertimeRequests, approveOvertimeRequest, rejectOvertimeRequest,
  getShiftChangeRequests, approveShiftChange, rejectShiftChange,
  getAttendancePermissions, approveAttendancePermission, rejectAttendancePermission,
  getBranches, previewBulkApproval, runBulkApproval,
} from '../../api';
import { canApprove } from '../../roles';
import { BackdatedBadge } from './LeaveRuleWidgets';

const TYPE_META = {
  leave:    { label: 'Libur',   bg: '#e3f2fd', color: '#1565c0' },
  overtime: { label: 'Lembur', bg: '#fff3e0', color: '#b45309' },
  shift:    { label: 'Shift',  bg: '#ede9fe', color: '#6d28d9' },
  permission: { label: 'Izin', bg: '#e0f2f1', color: '#00695c' },
};

const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';

const isManager = () => canApprove();

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
    detail: `${rq.day_count} hari`,
    subtitle: rq.leave_type_name + (rq.is_paid === false ? ' (tanpa gaji)' : ''),
    is_backdated: rq.is_backdated,
    reason: rq.reason,
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

// An attendance permit (Izin Masuk Siang / Izin Pulang Awal / Dinas Luar): once
// approved it changes how that day is judged (half day / no late-early flags).
function normPermission(p) {
  return {
    key: `perm-${p.id}`, id: p.id, type: 'permission',
    employee_name: p.employee_name, employee_code: p.employee_code,
    branch_id: p.branch_id ?? null,
    sortDate: p.date,
    periodLabel: fmtDate(p.date),
    detail: p.type_label,
    subtitle: p.reason,
    is_backdated: p.is_backdated,
    reason: p.reason,
  };
}

// A shift change is always about today's roster; once the day has passed it can
// only be rejected (the server refuses to approve it).
function normShift(c) {
  const code = (name, sc) => (name ? `${name} (${sc})` : 'tanpa shift');
  return {
    key: `shift-${c.id}`, id: c.id, type: 'shift',
    employee_name: c.employee_name, employee_code: c.employee_code,
    branch_id: c.branch_id ?? null,
    sortDate: c.date,
    periodLabel: fmtDate(c.date),
    detail: c.shift_code || 'hapus',
    subtitle: `${code(c.prev_shift_name, c.prev_shift_code)} → ${code(c.shift_name, c.shift_code)}`
      + (c.division_name ? ` · ${c.division_name}` : '')
      + (c.requested_by_username ? ` · oleh ${c.requested_by_username}` : ''),
    reason: c.reason,
    expired: c.expired,
  };
}

const APPROVE_FNS = { leave: approveLeaveRequest, overtime: approveOvertimeRequest, shift: approveShiftChange, permission: approveAttendancePermission };
const REJECT_FNS = { leave: rejectLeaveRequest, overtime: rejectOvertimeRequest, shift: rejectShiftChange, permission: rejectAttendancePermission };

// Same keys the norm* functions build, so a bulk result ({type, id}) can be
// matched back to the row it came from.
const KEY_PREFIX = { leave: 'leave', overtime: 'ot', shift: 'shift', permission: 'perm' };
const rowKey = (type, id) => `${KEY_PREFIX[type]}-${id}`;

const OUTCOME = {
  approved: { label: 'Disetujui', bg: '#dcfce7', color: '#166534', flash: '#bbf7d0' },
  rejected: { label: 'Ditolak',   bg: '#fee2e2', color: '#991b1b', flash: '#fecaca' },
};
const FRESH_MS = 2500; // how long a just-decided row stays highlighted

function StatusPill({ status }) {
  const o = OUTCOME[status];
  return (
    <span className="approval-status-pill" style={{ background: o.bg, color: o.color }}>
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        {status === 'approved' ? <path d="M20 6 9 17l-5-5" /> : <path d="M18 6 6 18M6 6l12 12" />}
      </svg>
      {o.label}
    </span>
  );
}

export default function Approvals() {
  const manager = isManager();
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [branches, setBranches] = useState([]);
  const [filters, setFilters] = useState({ type: '', branch_id: '' });
  const [decision, setDecision] = useState(null); // { action, request }
  const [bulkOpen, setBulkOpen] = useState(false);
  // Requests decided on this page since it was opened: key → { row, status, at }.
  // They drop out of the pending fetch, so they are merged back in, greyed.
  const [decided, setDecided] = useState({});

  const markDecided = useCallback((entries) => {
    const at = Date.now();
    const keys = entries.filter(e => e.row).map(e => e.row.key);
    setDecided(prev => {
      const next = { ...prev };
      for (const { row, status } of entries) if (row) next[row.key] = { row, status, at, fresh: true };
      return next;
    });
    // Take them out of the pending list now rather than waiting for the reload.
    const gone = new Set(keys);
    setRows(prev => prev.filter(r => !gone.has(r.key)));
    // End the highlight so a later remount (filter change, reload) shows them plain grey.
    setTimeout(() => setDecided(prev => {
      const next = { ...prev };
      for (const k of keys) if (next[k]?.at === at) next[k] = { ...next[k], fresh: false };
      return next;
    }), FRESH_MS);
  }, []);

  useEffect(() => { getBranches().then(r => setBranches(r.data || [])).catch(() => setBranches([])); }, []);

  const load = useCallback(async () => {
    setLoading(true); setError('');
    const params = { status: 'pending' };
    if (filters.branch_id) params.branch_id = filters.branch_id;
    try {
      const want = (t) => !filters.type || filters.type === t;
      const none = Promise.resolve({ data: [] });
      const [lv, ot, sh, pm] = await Promise.all([
        want('leave') ? getLeaveRequests(params).catch(() => ({ data: [] })) : none,
        want('overtime') ? getOvertimeRequests(params).catch(() => ({ data: [] })) : none,
        want('shift') ? getShiftChangeRequests(params).catch(() => ({ data: [] })) : none,
        want('permission') ? getAttendancePermissions(params).catch(() => ({ data: [] })) : none,
      ]);
      const merged = [
        ...(Array.isArray(lv.data) ? lv.data : []).map(normLeave),
        ...(Array.isArray(ot.data) ? ot.data : []).map(normOvertime),
        ...(Array.isArray(sh.data) ? sh.data : []).map(normShift),
        ...(Array.isArray(pm.data) ? pm.data : []).map(normPermission),
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

  const pendingKeys = new Set(rows.map(r => r.key));
  const doneRows = Object.values(decided)
    .filter(d => !pendingKeys.has(d.row.key))
    .filter(d => (!filters.type || d.row.type === filters.type)
      && (!filters.branch_id || d.row.branch_id === filters.branch_id))
    .sort((a, b) => b.at - a.at); // most recent decision first
  const doneCount = { approved: 0, rejected: 0 };
  doneRows.forEach(d => { doneCount[d.status]++; });

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.35rem', flexWrap: 'wrap', gap: '0.5rem' }}>
        <h2 style={{ margin: 0 }}>Persetujuan</h2>
        <span style={{ fontSize: '0.85rem', color: '#667' }}>
          {rows.length} menunggu
          {doneCount.approved > 0 && <> · <span style={{ color: OUTCOME.approved.color }}>{doneCount.approved} disetujui</span></>}
          {doneCount.rejected > 0 && <> · <span style={{ color: OUTCOME.rejected.color }}>{doneCount.rejected} ditolak</span></>}
        </span>
      </div>
      <p style={{ color: '#667', marginTop: 0, fontSize: '0.88rem' }}>Pengajuan libur, lembur, izin, dan perubahan shift hari ini yang menunggu keputusan manajer.</p>

      {!manager && (
        <div style={{ background: '#fff8e1', color: '#a06800', padding: '0.6rem 0.9rem', borderRadius: 8, marginBottom: '1rem', fontSize: '0.88rem' }}>
          Hanya manajer yang dapat menyetujui atau menolak. Anda dapat melihat daftar ini.
        </div>
      )}
      {error && <div style={errBox}>{error}</div>}

      <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem', flexWrap: 'wrap' }}>
        <select value={filters.type} onChange={e => setFilters({ ...filters, type: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }}>
          <option value="">Semua jenis</option>
          <option value="leave">Libur</option>
          <option value="overtime">Lembur</option>
          <option value="shift">Perubahan shift</option>
          <option value="permission">Izin (masuk siang / pulang awal / dinas luar)</option>
        </select>
        <select value={filters.branch_id} onChange={e => setFilters({ ...filters, branch_id: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }}>
          <option value="">Semua cabang</option>
          {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
        </select>
        {manager && rows.length > 0 && (
          <button className="btn btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setBulkOpen(true)}>
            Setujui Semua…
          </button>
        )}
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
            {!loading && rows.length === 0 && <tr><td colSpan={6} style={emptyCell}>{doneRows.length > 0 ? 'Semua pengajuan sudah diputuskan.' : 'Tidak ada pengajuan menunggu.'}</td></tr>}
            {!loading && rows.map(r => (
              <tr key={r.key}>
                <td><TypeBadge type={r.type} /></td>
                <td>{r.employee_name}<div style={{ fontSize: '0.78rem', color: '#889' }}>{r.employee_code}</div></td>
                <td style={{ fontSize: '0.88rem', color: '#445' }}>
                  {r.subtitle}<BackdatedBadge request={r} />
                  {r.type === 'shift' && r.reason && <div style={{ fontSize: '0.78rem', color: '#889' }}>Alasan: {r.reason}</div>}
                  {r.expired && <div style={{ fontSize: '0.78rem', color: '#b91c1c' }}>Tanggal sudah lewat — hanya dapat ditolak</div>}
                </td>
                <td style={{ whiteSpace: 'nowrap', fontSize: '0.88rem' }}>{r.periodLabel}</td>
                <td style={{ textAlign: 'center' }}>{r.detail}</td>
                <td style={{ whiteSpace: 'nowrap' }}>
                  {manager && (
                    <span style={{ display: 'flex', gap: '0.3rem' }}>
                      {!r.expired && <button className="btn btn-sm btn-primary" onClick={() => setDecision({ action: 'approve', request: r })}>Setujui</button>}
                      <button className="btn btn-sm btn-secondary" onClick={() => setDecision({ action: 'reject', request: r })}>Tolak</button>
                    </span>
                  )}
                </td>
              </tr>
            ))}
            {!loading && doneRows.map(({ row: r, status, fresh }) => {
              return (
                <tr key={r.key}
                  className={`approval-row-done${fresh ? ' approval-row-fresh' : ''}`}
                  style={{ '--flash': OUTCOME[status].flash }}>
                  <td><TypeBadge type={r.type} /></td>
                  <td>{r.employee_name}<div style={{ fontSize: '0.78rem' }}>{r.employee_code}</div></td>
                  <td style={{ fontSize: '0.88rem' }}><span>{r.subtitle}</span></td>
                  <td style={{ whiteSpace: 'nowrap', fontSize: '0.88rem' }}><span>{r.periodLabel}</span></td>
                  <td style={{ textAlign: 'center' }}><span>{r.detail}</span></td>
                  <td className="approval-status-cell" style={{ whiteSpace: 'nowrap' }}><StatusPill status={status} /></td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {decision && (
        <DecisionModal {...decision} onClose={() => setDecision(null)}
          onDone={() => {
            markDecided([{ row: decision.request, status: decision.action === 'approve' ? 'approved' : 'rejected' }]);
            setDecision(null); load();
          }} />
      )}
      {bulkOpen && (
        <BulkApproveModal
          onApproved={(items) => {
            const byKey = Object.fromEntries(rows.map(r => [r.key, r]));
            markDecided(items.map(it => ({ row: byKey[rowKey(it.type, it.id)], status: 'approved' })));
          }}
          filters={filters}
          branchName={branches.find(b => b.id === filters.branch_id)?.name}
          onClose={(changed) => { setBulkOpen(false); if (changed) load(); }}
        />
      )}
    </div>
  );
}

const CHUNK = 50;

// Bulk approval of everything pending under the current filters. Step 1 is a
// server-side dry run (changes nothing) that reports, per request, whether it
// would be approved and why not. Step 2 approves exactly the previewed items,
// in the same order, 50 per call.
function BulkApproveModal({ filters, branchName, onClose, onApproved }) {
  const [includeBackdated, setIncludeBackdated] = useState(false);
  const [note, setNote] = useState('');
  const [preview, setPreview] = useState(null);   // { items, summary, truncated }
  const [phase, setPhase] = useState('options');  // options | previewing | preview | running | done
  const [progress, setProgress] = useState(0);
  const [results, setResults] = useState([]);
  const [error, setError] = useState('');

  const scope = [
    filters.type ? TYPE_META[filters.type].label : 'semua jenis',
    branchName || 'semua cabang',
  ].join(' · ');

  const runPreview = async () => {
    setPhase('previewing'); setError('');
    try {
      const { data } = await previewBulkApproval({
        types: filters.type ? [filters.type] : [],
        branch_id: filters.branch_id || '',
        include_backdated: includeBackdated,
      });
      setPreview(data); setPhase('preview');
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal membuat pratinjau.'); setPhase('options');
    }
  };

  const toApprove = (preview?.items || []).filter(it => it.status === 'ok');

  const runApprove = async () => {
    setPhase('running'); setProgress(0); setError('');
    const done = [];
    for (let i = 0; i < toApprove.length; i += CHUNK) {
      const chunk = toApprove.slice(i, i + CHUNK).map(it => ({ type: it.type, id: it.id }));
      try {
        const { data } = await runBulkApproval(chunk, note, includeBackdated);
        done.push(...(data.results || []));
        onApproved?.((data.results || []).filter(r => r.status === 'ok'));
      } catch (err) {
        // A failed call (network, timeout) stops here; what was approved stays approved.
        setError(`Berhenti setelah ${done.length} dari ${toApprove.length}: ${err?.response?.data?.error || err.message}. Tutup lalu buka lagi untuk melanjutkan sisanya.`);
        break;
      }
      setProgress(done.length);
      setResults([...done]);
    }
    setResults(done);
    setPhase('done');
  };

  const okCount = results.filter(r => r.status === 'ok').length;
  const failed = (phase === 'done' ? results : preview?.items || []).filter(it => it.status === 'failed');
  const skipped = (preview?.items || []).filter(it => it.status === 'skipped');
  const byType = preview?.summary?.by_type || {};
  const busy = phase === 'previewing' || phase === 'running';

  return (
    <div style={overlay} onClick={() => !busy && onClose(phase === 'done')}>
      <div className="card" style={{ ...modal, width: 640 }} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Setujui Semua Pengajuan Menunggu</h3>
        <p style={{ fontSize: '0.85rem', color: '#667', marginTop: 0 }}>Cakupan: <strong>{scope}</strong> (mengikuti filter halaman).</p>

        {(phase === 'options' || phase === 'previewing') && (
          <>
            <p style={{ fontSize: '0.88rem', color: '#445' }}>
              Langkah pertama adalah <strong>pratinjau</strong>: server mencoba menyetujui setiap pengajuan lalu membatalkan semuanya,
              sehingga Anda melihat mana yang akan berhasil dan mana yang gagal (mis. jatah libur tidak cukup) sebelum ada yang diubah.
            </p>
            <label style={{ display: 'flex', gap: '0.5rem', alignItems: 'flex-start', fontSize: '0.88rem', marginTop: '0.5rem' }}>
              <input type="checkbox" checked={includeBackdated} onChange={e => setIncludeBackdated(e.target.checked)} disabled={busy} />
              <span>
                Sertakan pengajuan <strong>MUNDUR TANGGAL</strong>
                <div style={{ fontSize: '0.78rem', color: '#9a3412' }}>
                  Biasanya disetujui satu per satu. Jika disertakan, tetap ditandai mundur tanggal dan tercatat di log aktivitas sebagai persetujuan massal.
                </div>
              </span>
            </label>
          </>
        )}

        {error && <div style={{ ...errBox, marginTop: '0.75rem' }}>{error}</div>}

        {preview && phase !== 'options' && phase !== 'previewing' && (
          <div style={{ marginTop: '0.5rem' }}>
            <table style={{ width: '100%', fontSize: '0.85rem' }}>
              <thead><tr><th>Jenis</th><th style={num}>Disetujui</th><th style={num}>Gagal</th><th style={num}>Dilewati</th></tr></thead>
              <tbody>
                {Object.entries(byType).map(([t, c]) => (
                  <tr key={t}><td><TypeBadge type={t} /></td><td style={num}>{c.ok}</td><td style={num}>{c.failed}</td><td style={num}>{c.skipped}</td></tr>
                ))}
              </tbody>
            </table>
            {preview.truncated && (
              <div style={{ ...errBox, background: '#fff8e1', color: '#a06800', marginTop: '0.5rem' }}>
                Daftar dibatasi {preview.items.length} pengajuan. Jalankan lagi setelah selesai untuk sisanya.
              </div>
            )}
            {skipped.length > 0 && phase === 'preview' && (
              <p style={{ fontSize: '0.8rem', color: '#667' }}>{skipped.length} pengajuan mundur tanggal dilewati — centang opsi di atas untuk menyertakannya.</p>
            )}
          </div>
        )}

        {phase === 'running' && (
          <div style={{ marginTop: '0.75rem' }}>
            <div style={{ fontSize: '0.85rem', marginBottom: 4 }}>Memproses {progress} / {toApprove.length}…</div>
            <div style={{ height: 8, background: '#e5e7eb', borderRadius: 4 }}>
              <div style={{ height: 8, width: `${toApprove.length ? (progress / toApprove.length) * 100 : 0}%`, background: '#2563eb', borderRadius: 4, transition: 'width .2s' }} />
            </div>
          </div>
        )}

        {phase === 'done' && (
          <div style={{ ...errBox, background: '#e8f5e9', color: '#1b5e20', marginTop: '0.75rem' }}>
            {okCount} pengajuan disetujui.{failed.length > 0 && ` ${failed.length} gagal — tetap menunggu dan dapat diputuskan satu per satu.`}
          </div>
        )}

        {failed.length > 0 && phase !== 'running' && (
          <details style={{ marginTop: '0.5rem' }} open={failed.length <= 10}>
            <summary style={{ cursor: 'pointer', fontSize: '0.85rem', color: '#c5221f' }}>
              {failed.length} pengajuan {phase === 'done' ? 'gagal' : 'akan gagal'} (tetap menunggu)
            </summary>
            <div style={{ maxHeight: 220, overflowY: 'auto', marginTop: '0.35rem' }}>
              <table style={{ width: '100%', fontSize: '0.8rem' }}>
                <tbody>
                  {failed.map(it => (
                    <tr key={it.type + it.id}>
                      <td style={{ whiteSpace: 'nowrap' }}><TypeBadge type={it.type} /></td>
                      <td>{it.employee_name}<div style={{ color: '#889' }}>{it.detail} · {fmtDate(it.date)}</div></td>
                      <td style={{ color: '#c5221f' }}>{it.error}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </details>
        )}

        {phase === 'preview' && toApprove.length > 0 && (
          <>
            <label style={lbl}>Catatan untuk semua (opsional)</label>
            <input value={note} onChange={e => setNote(e.target.value)} style={inp} placeholder="mis. Persetujuan antrian sebelum perubahan sistem" />
          </>
        )}

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={() => onClose(phase === 'done' || results.length > 0)} disabled={busy}>
            {phase === 'done' ? 'Selesai' : 'Tutup'}
          </button>
          {(phase === 'options' || phase === 'previewing') && (
            <button className="btn btn-primary" onClick={runPreview} disabled={busy}>{busy ? 'Memeriksa…' : 'Pratinjau'}</button>
          )}
          {phase === 'preview' && (
            <>
              <button className="btn btn-secondary" onClick={() => { setPreview(null); setPhase('options'); }}>Ubah opsi</button>
              <button className="btn btn-primary" onClick={runApprove} disabled={toApprove.length === 0}>
                Setujui {toApprove.length} pengajuan
              </button>
            </>
          )}
        </div>
        <p style={{ fontSize: '0.72rem', color: '#99a', marginBottom: 0 }}>
          Setiap pengajuan diproses dengan aturan yang sama seperti tombol Setujui biasa, satu transaksi per pengajuan.
        </p>
      </div>
    </div>
  );
}

const num = { textAlign: 'center', width: 90 };

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
          {request.periodLabel} ({request.detail})<BackdatedBadge request={request} />
        </p>
        {action === 'approve' && request.is_backdated && (
          <div style={{ ...errBox, background: '#fff4e5', color: '#9a3412' }}>
            Pengajuan ini dibuat setelah tanggal mulainya lewat. Pastikan alasannya sah sebelum menyetujui.
            {request.reason && <><br />Alasan: <em>{request.reason}</em></>}
          </div>
        )}
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
