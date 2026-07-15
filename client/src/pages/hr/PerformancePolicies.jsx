import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getPerformancePolicies, createPerformancePolicy,
  updatePerformancePolicy, deletePerformancePolicy,
} from '../../api';

const RULE_LABELS = {
  late: 'Terlambat',
  early_leave: 'Pulang Awal',
  missing_checkout: 'Tidak Absen Pulang',
  absent_no_leave: 'Absen Tanpa Cuti',
  manual: 'Manual',
};

const THRESHOLD_RULES = ['late', 'early_leave'];

const SEED_EXAMPLES = [
  { name: 'Terlambat > 15 menit', rule_type: 'late', threshold_minutes: 15, points: 2, max_occurrences_per_month: null },
  { name: 'Terlambat > 60 menit', rule_type: 'late', threshold_minutes: 60, points: 5, max_occurrences_per_month: null },
  { name: 'Pulang lebih awal', rule_type: 'early_leave', threshold_minutes: 30, points: 2, max_occurrences_per_month: null },
  { name: 'Tidak absen pulang', rule_type: 'missing_checkout', threshold_minutes: null, points: 1, max_occurrences_per_month: null },
  { name: 'Absen tanpa cuti', rule_type: 'absent_no_leave', threshold_minutes: null, points: 10, max_occurrences_per_month: null },
];

const emptyForm = { name: '', rule_type: 'late', threshold_minutes: '', points: 2, max_occurrences_per_month: '', is_active: true };

function toPayload(f) {
  const hasThreshold = THRESHOLD_RULES.includes(f.rule_type);
  return {
    name: f.name.trim(),
    rule_type: f.rule_type,
    threshold_minutes: hasThreshold && f.threshold_minutes !== '' ? Number(f.threshold_minutes) : null,
    points: Number(f.points),
    max_occurrences_per_month: f.max_occurrences_per_month !== '' ? Number(f.max_occurrences_per_month) : null,
    is_active: f.is_active,
  };
}

function PolicyForm({ form, setForm, onSubmit, submitting, submitLabel }) {
  const hasThreshold = THRESHOLD_RULES.includes(form.rule_type);
  return (
    <form onSubmit={onSubmit} style={{ display: 'flex', gap: '0.5rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
      <div className="form-group" style={{ margin: 0, flex: 2, minWidth: 180 }}>
        <label>Nama Kebijakan</label>
        <input value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} placeholder="mis. Terlambat > 15 menit" />
      </div>
      <div className="form-group" style={{ margin: 0, flex: 1, minWidth: 150 }}>
        <label>Tipe Aturan</label>
        <select value={form.rule_type} onChange={e => setForm(f => ({ ...f, rule_type: e.target.value }))}>
          {Object.entries(RULE_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
      </div>
      <div className="form-group" style={{ margin: 0, width: 120 }}>
        <label>Ambang (menit)</label>
        <input type="number" min="0" value={form.threshold_minutes} disabled={!hasThreshold}
          onChange={e => setForm(f => ({ ...f, threshold_minutes: e.target.value }))}
          placeholder={hasThreshold ? 'mis. 15' : '—'} />
      </div>
      <div className="form-group" style={{ margin: 0, width: 90 }}>
        <label>Poin</label>
        <input type="number" min="1" value={form.points} onChange={e => setForm(f => ({ ...f, points: e.target.value }))} />
      </div>
      <div className="form-group" style={{ margin: 0, width: 110 }}>
        <label>Batas/Bulan</label>
        <input type="number" min="1" value={form.max_occurrences_per_month}
          onChange={e => setForm(f => ({ ...f, max_occurrences_per_month: e.target.value }))} placeholder="∞" />
      </div>
      <button type="submit" className="btn btn-primary" disabled={submitting}>{submitLabel}</button>
    </form>
  );
}

export default function PerformancePolicies() {
  const [rows, setRows] = useState([]);
  const [form, setForm] = useState(emptyForm);
  const [editId, setEditId] = useState(null);
  const [editForm, setEditForm] = useState(emptyForm);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const load = () => getPerformancePolicies().then(r => setRows(r.data || [])).catch(() => setRows([]));
  useEffect(() => { load(); }, []);

  const handleCreate = async (e) => {
    e.preventDefault();
    setError('');
    if (!form.name.trim()) { setError('Nama kebijakan wajib diisi'); return; }
    setSubmitting(true);
    try {
      await createPerformancePolicy(toPayload(form));
      setForm(emptyForm);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menambah kebijakan');
    } finally {
      setSubmitting(false);
    }
  };

  const openEdit = (p) => {
    setEditId(p.id);
    setEditForm({
      name: p.name,
      rule_type: p.rule_type,
      threshold_minutes: p.threshold_minutes ?? '',
      points: p.points,
      max_occurrences_per_month: p.max_occurrences_per_month ?? '',
      is_active: p.is_active,
    });
    setError('');
  };

  const handleUpdate = async (e) => {
    e.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      await updatePerformancePolicy(editId, toPayload(editForm));
      setEditId(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal memperbarui kebijakan');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (p) => {
    if (!confirm(`Hapus kebijakan "${p.name}"? Jika sudah dipakai pelanggaran, kebijakan hanya akan dinonaktifkan.`)) return;
    try {
      const res = await deletePerformancePolicy(p.id);
      if (res.data?.deactivated) alert(res.data.message || 'Kebijakan masih dipakai, dinonaktifkan.');
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus kebijakan');
    }
  };

  const toggleActive = async (p) => {
    try {
      await updatePerformancePolicy(p.id, {
        name: p.name, rule_type: p.rule_type, threshold_minutes: p.threshold_minutes,
        points: p.points, max_occurrences_per_month: p.max_occurrences_per_month, is_active: !p.is_active,
      });
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal mengubah status kebijakan');
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Kebijakan Evaluasi</h1>
        <Link to="/hr/performance" className="btn btn-secondary">Dasbor Evaluasi</Link>
      </div>

      <p style={{ color: '#667', fontSize: '0.9rem', marginBottom: '1rem' }}>
        Setiap karyawan memulai bulan dengan skor <strong>100</strong>. Pelanggaran mengurangi poin secara otomatis dari
        data absensi. Untuk aturan <strong>Terlambat</strong> dan <strong>Pulang Awal</strong>, hanya ambang tertinggi yang
        cocok yang diterapkan (mis. 70 menit terlambat hanya kena kebijakan ≥60 menit).
      </p>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      <div className="card" style={{ marginBottom: '1rem' }}>
        <PolicyForm form={form} setForm={setForm} onSubmit={handleCreate} submitting={submitting} submitLabel="+ Tambah" />
      </div>

      <div className="card" style={{ overflowX: 'auto' }}>
        <table>
          <thead>
            <tr>
              <th>Nama</th><th>Tipe Aturan</th><th>Ambang (menit)</th><th>Poin</th><th>Batas/Bulan</th><th>Status</th><th></th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr><td colSpan={7} style={{ padding: '1rem' }}>
                <p style={{ color: '#999', marginBottom: '0.75rem' }}>Belum ada kebijakan. Contoh kebijakan yang disarankan:</p>
                <table style={{ width: '100%' }}>
                  <thead><tr><th>Nama</th><th>Tipe</th><th>Ambang</th><th>Poin</th></tr></thead>
                  <tbody>
                    {SEED_EXAMPLES.map((s, i) => (
                      <tr key={i} style={{ color: '#888' }}>
                        <td>{s.name}</td>
                        <td>{RULE_LABELS[s.rule_type]}</td>
                        <td>{s.threshold_minutes ?? '—'}</td>
                        <td>{s.points}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </td></tr>
            ) : rows.map(p => (
              <tr key={p.id}>
                <td style={{ fontWeight: 500 }}>{p.name}</td>
                <td>{RULE_LABELS[p.rule_type] || p.rule_type}</td>
                <td>{p.threshold_minutes ?? '—'}</td>
                <td>−{p.points}</td>
                <td>{p.max_occurrences_per_month ?? '∞'}</td>
                <td>
                  <button onClick={() => toggleActive(p)} className="badge"
                    style={{ border: 'none', cursor: 'pointer', background: p.is_active ? '#e6f4ea' : '#fce8e6', color: p.is_active ? '#1e7e34' : '#c5221f' }}
                    title="Klik untuk mengubah status">
                    {p.is_active ? 'Aktif' : 'Nonaktif'}
                  </button>
                </td>
                <td>
                  <div className="actions">
                    <button onClick={() => openEdit(p)} className="btn btn-secondary btn-sm">Edit</button>
                    <button onClick={() => handleDelete(p)} className="btn btn-danger btn-sm">Hapus</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {editId && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '100%', maxWidth: 560, padding: '2rem', margin: '1rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1.5rem' }}>
              <h2 style={{ margin: 0, fontSize: '1.05rem' }}>Edit Kebijakan Evaluasi</h2>
              <button onClick={() => setEditId(null)} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
            </div>
            <PolicyForm form={editForm} setForm={setEditForm} onSubmit={handleUpdate} submitting={submitting} submitLabel="Simpan" />
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '1rem' }}>
              <input type="checkbox" checked={editForm.is_active} onChange={e => setEditForm(f => ({ ...f, is_active: e.target.checked }))} />
              Aktif
            </label>
            <div style={{ marginTop: '1rem' }}>
              <button type="button" onClick={() => setEditId(null)} className="btn btn-secondary">Tutup</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
