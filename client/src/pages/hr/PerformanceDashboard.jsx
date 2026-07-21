import { useEffect, useState, Fragment } from 'react';
import { Link } from 'react-router-dom';
import {
  getPerformanceScores, getBranches, getEmployeePerformance,
  createPerformanceViolation, getEmployees, evaluatePerformance, resetAutoViolations,
  getPerformancePolicies,
} from '../../api';

const RULE_LABELS = {
  late: 'Terlambat',
  early_leave: 'Pulang Awal',
  missing_checkout: 'Tidak Absen Pulang',
  missing_checkin: 'Tidak Absen Masuk',
  no_punch: 'Tidak Absen Masuk & Pulang',
  half_day_late: 'Setengah Hari (Datang Siang)',
  half_day_early: 'Setengah Hari (Pulang Awal)',
  absent_no_leave: 'Absen Tanpa Cuti',
  consecutive_absent: 'Absen Berturut-turut',
  manual: 'Manual',
};

const monthNow = () => {
  const n = new Date();
  return `${n.getFullYear()}-${String(n.getMonth() + 1).padStart(2, '0')}`;
};

function scoreColor(score) {
  if (score >= 90) return { bg: '#e6f4ea', color: '#1e7e34' };
  if (score >= 70) return { bg: '#fff8e1', color: '#a06800' };
  return { bg: '#fce8e6', color: '#c5221f' };
}

function ScoreBadge({ score }) {
  const c = scoreColor(score);
  return (
    <span style={{ background: c.bg, color: c.color, padding: '0.2rem 0.6rem', borderRadius: '4px', fontWeight: 700, fontSize: '0.9rem' }}>
      {score}
    </span>
  );
}

const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short' }) : '-';

function ViolationBreakdown({ employeeId, month }) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    getEmployeePerformance(employeeId, { month })
      .then(r => setData(r.data))
      .catch(() => setData(null))
      .finally(() => setLoading(false));
  }, [employeeId, month]);

  if (loading) return <div style={{ padding: '0.75rem', color: '#888' }}>Memuat rincian…</div>;
  const viols = data?.violations || [];
  if (viols.length === 0) return <div style={{ padding: '0.75rem', color: '#888' }}>Tidak ada pelanggaran bulan ini.</div>;

  return (
    <table style={{ width: '100%', background: '#fafbfc' }}>
      <thead>
        <tr><th>Tanggal</th><th>Kebijakan</th><th>Poin</th><th>Catatan</th></tr>
      </thead>
      <tbody>
        {viols.map(v => (
          <tr key={v.id}>
            <td>{fmtDate(v.date)}</td>
            <td>{v.policy_name || (v.source === 'manual' ? 'Manual' : (RULE_LABELS[v.rule_type] || '-'))}</td>
            <td style={{ color: '#c5221f', fontWeight: 600 }}>−{v.points}</td>
            <td style={{ fontSize: '0.85rem', color: '#667' }}>{v.note || '—'}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// ManualViolationModal adds a hand-applied performance penalty. Pass `presetEmployee`
// ({ id, label }) to lock the penalty to a specific employee (opened from a row on the
// evaluation dashboard or attendance correction) — the employee picker is then hidden.
// `presetDate` (YYYY-MM-DD) prefills the date.
export function ManualViolationModal({ onClose, onSaved, presetEmployee = null, presetDate = null }) {
  const [employees, setEmployees] = useState([]);
  const [policies, setPolicies] = useState([]);
  const [form, setForm] = useState({
    employee_id: presetEmployee?.id || '',
    policy_id: '',
    date: presetDate || new Date().toISOString().slice(0, 10),
    points: 5,
    note: '',
  });
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    // The employee list is only needed for the free picker; skip it when preset.
    if (!presetEmployee) {
      getEmployees().then(r => setEmployees(r.data?.data || [])).catch(() => setEmployees([]));
    }
    // Only 'manual' policies are hand-applied here; auto policies fire from attendance.
    getPerformancePolicies()
      .then(r => setPolicies((r.data?.data || r.data || []).filter(p => p.rule_type === 'manual' && p.is_active)))
      .catch(() => setPolicies([]));
  }, [presetEmployee]);

  // Selecting a named policy prefills the points (and the note if still blank).
  const onPolicyChange = (id) => {
    const p = policies.find(x => String(x.id) === String(id));
    setForm(f => ({
      ...f,
      policy_id: id,
      points: p ? p.points : f.points,
      note: (!f.note.trim() && p) ? p.name : f.note,
    }));
  };

  const submit = async (e) => {
    e.preventDefault();
    setError('');
    if (!form.employee_id) { setError('Pilih karyawan'); return; }
    if (!form.note.trim()) { setError('Catatan wajib diisi'); return; }
    setSaving(true);
    try {
      await createPerformanceViolation({
        employee_id: form.employee_id,
        policy_id: form.policy_id || undefined,
        date: form.date,
        points: Number(form.points),
        note: form.note.trim(),
      });
      onSaved();
      onClose();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan pelanggaran');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
      <div className="card" style={{ width: '100%', maxWidth: 440, padding: '2rem', margin: '1rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1.5rem' }}>
          <h2 style={{ margin: 0, fontSize: '1.05rem' }}>Tambah Pelanggaran Manual</h2>
          <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
        </div>
        {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
        <form onSubmit={submit}>
          <div className="form-group">
            <label>Karyawan</label>
            {presetEmployee ? (
              <div style={{ fontWeight: 600, padding: '0.4rem 0' }}>{presetEmployee.label}</div>
            ) : (
              <select value={form.employee_id} onChange={e => setForm(f => ({ ...f, employee_id: e.target.value }))} required>
                <option value="">Pilih karyawan…</option>
                {employees.map(e => <option key={e.id} value={e.id}>{e.full_name} ({e.employee_code})</option>)}
              </select>
            )}
          </div>
          <div className="form-group">
            <label>Tanggal</label>
            <input type="date" value={form.date} onChange={e => setForm(f => ({ ...f, date: e.target.value }))} required />
          </div>
          <div className="form-group">
            <label>Jenis Pelanggaran (opsional)</label>
            <select value={form.policy_id} onChange={e => onPolicyChange(e.target.value)}>
              <option value="">— Bebas / lainnya —</option>
              {policies.map(p => <option key={p.id} value={p.id}>{p.name} (−{p.points})</option>)}
            </select>
          </div>
          <div className="form-group">
            <label>Poin Pengurangan</label>
            <input type="number" min="1" value={form.points} onChange={e => setForm(f => ({ ...f, points: e.target.value }))} required />
          </div>
          <div className="form-group">
            <label>Catatan</label>
            <textarea value={form.note} onChange={e => setForm(f => ({ ...f, note: e.target.value }))} rows={3} placeholder="Alasan pelanggaran…" />
          </div>
          <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1rem' }}>
            <button type="submit" className="btn btn-primary" disabled={saving} style={{ flex: 1, justifyContent: 'center' }}>
              {saving ? 'Menyimpan…' : 'Simpan'}
            </button>
            <button type="button" onClick={onClose} className="btn btn-secondary">Batal</button>
          </div>
        </form>
      </div>
    </div>
  );
}

export default function PerformanceDashboard() {
  const [month, setMonth] = useState(monthNow());
  const [branchId, setBranchId] = useState('');
  const [search, setSearch] = useState('');
  const [branches, setBranches] = useState([]);
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(false);
  const [expanded, setExpanded] = useState(null);
  const [showModal, setShowModal] = useState(false);
  const [penaltyFor, setPenaltyFor] = useState(null); // { id, label } for a per-row penalty
  const [evaluating, setEvaluating] = useState(false);

  useEffect(() => {
    getBranches().then(r => setBranches(r.data || [])).catch(() => {});
  }, []);

  const load = () => {
    setLoading(true);
    const params = { month };
    if (branchId) params.branch_id = branchId;
    if (search.trim()) params.q = search.trim();
    getPerformanceScores(params)
      .then(r => setRows(r.data?.data || []))
      .catch(() => setRows([]))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); /* eslint-disable-next-line */ }, [month, branchId]);

  const handleEvaluate = async () => {
    setEvaluating(true);
    try {
      const [y, m] = month.split('-');
      const from = `${y}-${m}-01`;
      const lastDay = new Date(Number(y), Number(m), 0).getDate();
      const to = `${y}-${m}-${String(lastDay).padStart(2, '0')}`;
      await resetAutoViolations(from, to);
      await evaluatePerformance(from, to);
      load();
    } catch {
      alert('Evaluasi gagal. Coba lagi.');
    } finally {
      setEvaluating(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Evaluasi Karyawan</h1>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <button onClick={handleEvaluate} className="btn btn-secondary" disabled={evaluating}>
            {evaluating ? 'Mengevaluasi…' : 'Evaluasi Bulan Ini'}
          </button>
          <button onClick={() => setShowModal(true)} className="btn btn-primary">Tambah Pelanggaran Manual</button>
          <Link to="/hr/performance/policies" className="btn btn-secondary">Kebijakan</Link>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '1.25rem' }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.85rem', alignItems: 'flex-end' }}>
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Bulan</label>
            <input type="month" value={month} onChange={e => setMonth(e.target.value)} />
          </div>
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Cabang</label>
            <select value={branchId} onChange={e => setBranchId(e.target.value)}>
              <option value="">Semua Cabang</option>
              {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
            </select>
          </div>
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Cari Nama / Kode</label>
            <input value={search} onChange={e => setSearch(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') load(); }} placeholder="cari…" />
          </div>
          <button onClick={load} className="btn btn-primary btn-sm">Terapkan</button>
        </div>
      </div>

      <div className="card" style={{ overflowX: 'auto' }}>
        {loading ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>
        ) : rows.length === 0 ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Tidak ada data karyawan untuk filter ini.</p>
        ) : (
          <table>
            <thead>
              <tr><th></th><th>Karyawan</th><th>Cabang</th><th>Skor</th><th>Pelanggaran</th><th></th></tr>
            </thead>
            <tbody>
              {rows.map(r => (
                <Fragment key={r.id}>
                  <tr style={{ cursor: 'pointer' }} onClick={() => setExpanded(expanded === r.id ? null : r.id)}>
                    <td style={{ width: 24, color: '#999' }}>{expanded === r.id ? '▾' : '▸'}</td>
                    <td>
                      <Link to={`/hr/employees/${r.id}`} onClick={e => e.stopPropagation()} style={{ fontWeight: 500 }}>{r.full_name}</Link>
                      <div style={{ fontSize: '0.75rem', color: '#999', fontFamily: 'monospace' }}>{r.employee_code}</div>
                    </td>
                    <td style={{ fontSize: '0.85rem' }}>{r.branch_name}</td>
                    <td><ScoreBadge score={r.score} /></td>
                    <td>{r.violation_count}</td>
                    <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                      <button
                        className="btn btn-secondary btn-sm"
                        onClick={e => { e.stopPropagation(); setPenaltyFor({ id: r.id, label: `${r.full_name} (${r.employee_code})` }); }}
                      >
                        + Pelanggaran
                      </button>
                    </td>
                  </tr>
                  {expanded === r.id && (
                    <tr>
                      <td colSpan={6} style={{ padding: 0 }}>
                        <ViolationBreakdown employeeId={r.id} month={month} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {showModal && <ManualViolationModal onClose={() => setShowModal(false)} onSaved={load} />}
      {penaltyFor && (
        <ManualViolationModal
          presetEmployee={penaltyFor}
          onClose={() => setPenaltyFor(null)}
          onSaved={load}
        />
      )}
    </>
  );
}
