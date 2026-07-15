import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import {
  getKasbons, getKasbon, approveKasbon, rejectKasbon, cancelKasbon, processKasbon,
} from '../../api';
import KasbonFormModal from './KasbonFormModal';

const STATUS_LABELS = {
  pending: 'Menunggu', approved: 'Disetujui', rejected: 'Ditolak',
  processed: 'Diproses', resolved: 'Lunas', cancelled: 'Dibatalkan',
};
const STATUS_COLORS = {
  pending: { bg: '#fff8e1', color: '#a06800' },
  approved: { bg: '#e8f0fe', color: '#1967d2' },
  rejected: { bg: '#fce8e6', color: '#c5221f' },
  processed: { bg: '#e6f4ea', color: '#1e7e34' },
  resolved: { bg: '#eef1f6', color: '#445' },
  cancelled: { bg: '#eef1f6', color: '#667' },
};

const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));
const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';
const fmtMonth = (d) => d ? new Date(d).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' }) : '-';

function getUser() {
  try { return JSON.parse(localStorage.getItem('user') || '{}'); } catch { return {}; }
}
const isManager = () => getUser()?.role === 'manager';

function StatusChip({ status }) {
  const c = STATUS_COLORS[status] || STATUS_COLORS.cancelled;
  return <span style={{ background: c.bg, color: c.color, padding: '0.15rem 0.55rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.8rem' }}>{STATUS_LABELS[status] || status}</span>;
}

// ── Approval modal (manager) ─────────────────────────────────────────────────
function ApprovalModal({ kasbon, onClose, onDone }) {
  const [detail, setDetail] = useState(null);
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    getKasbon(kasbon.id).then(r => setDetail(r.data)).catch(() => setDetail(null));
  }, [kasbon.id]);

  const run = async (action) => {
    setBusy(action); setError('');
    try {
      if (action === 'approve') await approveKasbon(kasbon.id, { note });
      else await rejectKasbon(kasbon.id, note);
      onDone();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal memproses.');
    } finally {
      setBusy('');
    }
  };

  const lr = detail?.last_resolved;

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Persetujuan Kasbon</h3>
        <div style={{ fontSize: '0.9rem', color: '#334', lineHeight: 1.7 }}>
          <div><strong>{kasbon.employee_name}</strong> ({kasbon.employee_code})</div>
          <div>Nomor: {kasbon.kasbon_number}</div>
          <div>Jumlah: <strong>{fmtIDR(kasbon.amount)}</strong></div>
          <div>Keterangan: {kasbon.details}</div>
          <div>Sumber dana: {kasbon.fund_source_name}</div>
          <div>Bulan penyelesaian: {fmtMonth(kasbon.resolution_month)}</div>
        </div>
        <div style={{ background: '#f5f7fb', borderRadius: '6px', padding: '0.6rem 0.8rem', margin: '0.75rem 0', fontSize: '0.85rem' }}>
          {detail == null ? 'Memuat info…' : lr
            ? <>Kasbon terakhir lunas: <strong>{fmtDate(lr.processed_at || lr.request_date)}</strong> ({fmtIDR(lr.amount)})</>
            : 'Belum pernah kasbon.'}
        </div>
        {error && <div style={errBox}>{error}</div>}
        <label style={lbl}>Catatan (opsional)</label>
        <textarea value={note} onChange={e => setNote(e.target.value)} rows={2} style={{ ...inp, resize: 'vertical' }} />
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={!!busy}>Tutup</button>
          <button className="btn btn-danger" onClick={() => run('reject')} disabled={!!busy}>{busy === 'reject' ? 'Memproses…' : 'Tolak'}</button>
          <button className="btn btn-primary" onClick={() => run('approve')} disabled={!!busy}>{busy === 'approve' ? 'Memproses…' : 'Setujui'}</button>
        </div>
      </div>
    </div>
  );
}

// ── Process modal ────────────────────────────────────────────────────────────
function ProcessModal({ kasbon, onClose, onDone }) {
  const [confirmed, setConfirmed] = useState(false);
  const [photo, setPhoto] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const run = async () => {
    setBusy(true); setError('');
    try {
      const form = new FormData();
      if (photo) form.append('photo', photo);
      await processKasbon(kasbon.id, form);
      onDone();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal memproses kasbon.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Proses Kasbon</h3>
        <div style={{ fontSize: '0.9rem', color: '#334', lineHeight: 1.7 }}>
          <div><strong>{kasbon.employee_name}</strong> — {fmtIDR(kasbon.amount)}</div>
          <div>Metode pengiriman: <strong>{kasbon.sending_method}</strong></div>
          <div>Sumber dana: {kasbon.fund_source_name}</div>
        </div>
        <p style={{ fontSize: '0.82rem', color: '#667' }}>
          Memproses akan <strong>mendebit sumber dana</strong> sebesar jumlah kasbon dan mencatatnya sebagai Piutang Karyawan.
        </p>
        {error && <div style={errBox}>{error}</div>}
        <label style={{ ...lbl, display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <input type="checkbox" checked={confirmed} onChange={e => setConfirmed(e.target.checked)} />
          Saya konfirmasi dana telah dikirim via {kasbon.sending_method}
        </label>
        <label style={lbl}>Bukti foto (opsional)</label>
        <input type="file" accept="image/png,image/jpeg" onChange={e => setPhoto(e.target.files?.[0] || null)} style={{ ...inp, padding: '0.4rem' }} />
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={busy}>Batal</button>
          <button className="btn btn-primary" onClick={run} disabled={busy || !confirmed}>{busy ? 'Memproses…' : 'Proses'}</button>
        </div>
      </div>
    </div>
  );
}

const ONGOING = ['approved', 'processed'];

export default function KasbonDashboard() {
  const [all, setAll] = useState([]);
  const [loading, setLoading] = useState(true);
  const [filters, setFilters] = useState({ status: '', q: '' });
  const [approval, setApproval] = useState(null);
  const [process, setProcess] = useState(null);
  const [showCreate, setShowCreate] = useState(false);
  const manager = isManager();

  const load = useCallback(() => {
    setLoading(true);
    const params = {};
    if (filters.status) params.status = filters.status;
    if (filters.q) params.q = filters.q;
    getKasbons(params)
      .then(r => setAll(r.data || []))
      .catch(() => setAll([]))
      .finally(() => setLoading(false));
  }, [filters]);

  useEffect(() => { load(); }, [load]);

  const pending = all.filter(k => k.status === 'pending');
  const approvedUnprocessed = all.filter(k => k.status === 'approved');
  const ongoing = all.filter(k => ONGOING.includes(k.status));

  return (
    <div>
      <div className="page-header">
        <h1>Kasbon</h1>
        <button onClick={() => setShowCreate(true)} className="btn btn-primary">+ Pengajuan Kasbon</button>
      </div>

      {/* Filters */}
      <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem', flexWrap: 'wrap' }}>
        <select value={filters.status} onChange={e => setFilters({ ...filters, status: e.target.value })} style={{ ...inp, width: 'auto', margin: 0 }}>
          <option value="">Semua status</option>
          {Object.entries(STATUS_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <input placeholder="Cari karyawan / nomor…" value={filters.q} onChange={e => setFilters({ ...filters, q: e.target.value })} style={{ ...inp, width: '260px', margin: 0 }} />
      </div>

      {/* Pending approval (managers act here) */}
      <section style={{ marginBottom: '1.5rem' }}>
        <h3 style={sectionH}>Menunggu Persetujuan {pending.length > 0 && <span style={badge}>{pending.length}</span>}</h3>
        <KasbonTable
          rows={pending} loading={loading} emptyText="Tidak ada pengajuan menunggu."
          renderActions={k => manager ? (
            <button className="btn btn-sm btn-primary" onClick={() => setApproval(k)}>Tinjau</button>
          ) : <span style={{ color: '#889', fontSize: '0.8rem' }}>Menunggu manajer</span>}
          extraActions={k => <CancelBtn k={k} onDone={load} />}
        />
      </section>

      {/* Approved — not yet processed */}
      <section style={{ marginBottom: '1.5rem' }}>
        <h3 style={sectionH}>Disetujui — Belum Diproses {approvedUnprocessed.length > 0 && <span style={badge}>{approvedUnprocessed.length}</span>}</h3>
        <KasbonTable
          rows={approvedUnprocessed} loading={loading} emptyText="Tidak ada kasbon menunggu proses."
          renderActions={k => <button className="btn btn-sm btn-primary" onClick={() => setProcess(k)}>Proses</button>}
          extraActions={k => <CancelBtn k={k} onDone={load} />}
        />
      </section>

      {/* Ongoing (approved/processed, not resolved) */}
      <section>
        <h3 style={sectionH}>Kasbon Berjalan</h3>
        <KasbonTable rows={ongoing} loading={loading} emptyText="Tidak ada kasbon berjalan." showInstallments />
      </section>

      {approval && <ApprovalModal kasbon={approval} onClose={() => setApproval(null)} onDone={() => { setApproval(null); load(); }} />}
      {process && <ProcessModal kasbon={process} onClose={() => setProcess(null)} onDone={() => { setProcess(null); load(); }} />}
      {showCreate && <KasbonFormModal onClose={() => setShowCreate(false)} onSaved={() => { setShowCreate(false); load(); }} />}
    </div>
  );
}

function CancelBtn({ k, onDone }) {
  const cancel = async () => {
    if (!confirm(`Batalkan kasbon ${k.kasbon_number}?`)) return;
    try { await cancelKasbon(k.id); onDone(); }
    catch (err) { alert(err?.response?.data?.error || 'Gagal membatalkan.'); }
  };
  if (k.status !== 'pending' && k.status !== 'approved') return null;
  return <button className="btn btn-sm btn-link" style={{ marginLeft: '0.35rem' }} onClick={cancel}>Batalkan</button>;
}

function KasbonTable({ rows, loading, emptyText, renderActions, extraActions, showInstallments }) {
  return (
    <div className="card" style={{ padding: 0, overflowX: 'auto' }}>
      <table style={{ width: '100%' }}>
        <thead>
          <tr>
            <th>Nomor</th><th>Karyawan</th><th style={{ textAlign: 'right' }}>Jumlah</th>
            <th>Penyelesaian</th><th>Status</th>{(renderActions || extraActions) && <th>Aksi</th>}
          </tr>
        </thead>
        <tbody>
          {loading && <tr><td colSpan={6} style={emptyTd}>Memuat…</td></tr>}
          {!loading && rows.length === 0 && <tr><td colSpan={6} style={emptyTd}>{emptyText}</td></tr>}
          {!loading && rows.map(k => (
            <tr key={k.id}>
              <td><Link to={`/hr/kasbon/${k.id}`}>{k.kasbon_number}</Link></td>
              <td>{k.employee_name}<div style={{ fontSize: '0.78rem', color: '#889' }}>{k.employee_code}</div></td>
              <td style={{ textAlign: 'right' }}>{fmtIDR(k.amount)}</td>
              <td>{fmtMonth(k.resolution_month)}</td>
              <td><StatusChip status={k.status} /></td>
              {(renderActions || extraActions) && (
                <td>
                  <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem' }}>
                    {renderActions && renderActions(k)}
                    {extraActions && extraActions(k)}
                  </span>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

const overlay = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 };
const modal = { width: '480px', maxWidth: '92vw', maxHeight: '90vh', overflowY: 'auto' };
const lbl = { display: 'block', fontSize: '0.78rem', color: '#667', marginTop: '0.6rem', marginBottom: '0.2rem' };
const inp = { width: '100%', padding: '0.5rem', border: '1px solid #d4d9e2', borderRadius: '6px', boxSizing: 'border-box' };
const errBox = { background: '#fce8e6', color: '#c5221f', padding: '0.5rem 0.75rem', borderRadius: '6px', fontSize: '0.85rem', marginBottom: '0.5rem' };
const sectionH = { fontSize: '1rem', margin: '0 0 0.6rem', display: 'flex', alignItems: 'center', gap: '0.5rem' };
const badge = { background: '#2563eb', color: '#fff', borderRadius: '10px', padding: '0.05rem 0.5rem', fontSize: '0.75rem' };
const emptyTd = { textAlign: 'center', color: '#888', padding: '1.5rem' };
