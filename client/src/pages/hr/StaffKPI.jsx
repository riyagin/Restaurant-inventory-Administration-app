import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getStaffKpis, createStaffKpi, updateStaffKpi, deleteStaffKpi, getStaffKpiScores,
  getTaskDefinitions, createTaskDefinition, updateTaskDefinition, deleteTaskDefinition,
} from '../../api';

// KPI & daily duties.
//
// The two belong on one page because a KPI here is nothing but a target against
// a daily duty — defining one without seeing the other is guesswork. Duties are
// the operational fact; KPIs are what HR expects of the people who own them.

const METRICS = [
  { key: 'completion_rate', label: 'Kelengkapan (tim)', unit: '%',
    help: 'Berapa persen hari tugas yang diselesaikan siapa pun. Sama untuk semua staf — mengukur mejanya, bukan orangnya.' },
  { key: 'same_day_rate', label: 'Ketepatan waktu (pribadi)', unit: '%',
    help: 'Dari tugas yang diselesaikan orang ini, berapa persen dicatat pada tanggal tugasnya, bukan menyusul.' },
  { key: 'completed_count', label: 'Jumlah diselesaikan (pribadi)', unit: 'tugas',
    help: 'Berapa banyak tugas yang diselesaikan orang ini bulan itu.' },
];
const metricOf = (k) => METRICS.find((m) => m.key === k);

const TASK_TYPES = [
  { key: 'purchasing', label: 'Pembelian', help: 'Selesai bila ada invoice pembelian bertanggal hari itu.' },
  { key: 'pos_import', label: 'Import POS', help: 'Selesai bila ada import POS yang jatuh ke cabang tersebut.' },
  { key: 'manual', label: 'Manual', help: 'Tidak punya jejak data — ditandai selesai secara manual.' },
];
const taskTypeOf = (k) => TASK_TYPES.find((t) => t.key === k);

const currentMonth = () => new Date().toISOString().slice(0, 7);

const emptyKpi = { name: '', definition_id: '', metric: 'completion_rate', target_value: 95, weight: 1, is_active: true };
const emptyTask = {
  name: '', description: '', task_type: 'manual', scope: 'global',
  target_role: 'staff', link_path: '', starts_on: '', due_offset_days: 0, grace_days: 0,
  is_active: true, sort_order: 0,
};

function Field({ label, hint, children, full }) {
  return (
    <div className="form-group" style={full ? { gridColumn: '1 / -1' } : undefined}>
      <label>{label}</label>
      {children}
      {hint && <div style={{ fontSize: '0.78rem', color: 'var(--ink-3)', marginTop: 4 }}>{hint}</div>}
    </div>
  );
}

const grid = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(210px, 1fr))', gap: '0 1rem' };

export default function StaffKPI() {
  const [kpis, setKpis] = useState([]);
  const [tasks, setTasks] = useState([]);
  const [month, setMonth] = useState(currentMonth());
  const [scores, setScores] = useState([]);
  const [loading, setLoading] = useState(true);
  const [scoring, setScoring] = useState(false);
  const [error, setError] = useState('');
  const [msg, setMsg] = useState('');

  const [kpiForm, setKpiForm] = useState(null);   // null = closed, object = editing/creating
  const [taskForm, setTaskForm] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [k, t] = await Promise.all([getStaffKpis(), getTaskDefinitions()]);
      setKpis(k.data || []);
      setTasks(t.data || []);
      setError('');
    } catch {
      setError('Gagal memuat data KPI.');
    } finally {
      setLoading(false);
    }
  }, []);

  const loadScores = useCallback((m) => {
    setScoring(true);
    getStaffKpiScores({ month: m })
      .then((r) => setScores(r.data?.scorecards || []))
      .catch(() => setError('Gagal menghitung skor KPI.'))
      .finally(() => setScoring(false));
  }, []);

  useEffect(() => { load(); }, [load]);
  useEffect(() => { loadScores(month); }, [month, loadScores]);

  const activeTasks = useMemo(() => tasks.filter((t) => t.is_active), [tasks]);

  const flash = (m) => { setMsg(m); setError(''); setTimeout(() => setMsg(''), 3000); };

  const saveKpi = async () => {
    const body = {
      ...kpiForm,
      target_value: Number(kpiForm.target_value) || 0,
      weight: Math.max(1, Number(kpiForm.weight) || 1),
    };
    try {
      if (kpiForm.id) await updateStaffKpi(kpiForm.id, body);
      else await createStaffKpi(body);
      setKpiForm(null);
      await load();
      loadScores(month);
      flash('KPI tersimpan.');
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan KPI.');
    }
  };

  const removeKpi = async (k) => {
    if (!confirm(`Hapus KPI "${k.name}"?`)) return;
    try {
      await deleteStaffKpi(k.id);
      await load();
      loadScores(month);
    } catch {
      setError('Gagal menghapus KPI.');
    }
  };

  const saveTask = async () => {
    const body = {
      ...taskForm,
      due_offset_days: Math.max(0, Number(taskForm.due_offset_days) || 0),
      grace_days: Math.max(0, Number(taskForm.grace_days) || 0),
      sort_order: Number(taskForm.sort_order) || 0,
    };
    try {
      if (taskForm.id) await updateTaskDefinition(taskForm.id, body);
      else await createTaskDefinition(body);
      setTaskForm(null);
      await load();
      flash('Tugas harian tersimpan.');
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan tugas.');
    }
  };

  const removeTask = async (t) => {
    if (!confirm(`Hapus tugas "${t.name}"? KPI yang mengukurnya ikut terhapus.`)) return;
    try {
      await deleteTaskDefinition(t.id);
      await load();
      loadScores(month);
    } catch {
      setError('Gagal menghapus tugas.');
    }
  };

  if (loading) return <div style={{ padding: 24 }}>Memuat…</div>;

  return (
    <div style={{ padding: 24, maxWidth: 1000, margin: '0 auto' }}>
      <div className="page-header">
        <div>
          <h1>KPI &amp; Tugas Harian</h1>
          <p style={{ color: 'var(--ink-3)', margin: '0.2rem 0 0', maxWidth: 640 }}>
            Tugas harian diselesaikan otomatis dari data — invoice pembelian dan import POS yang
            benar-benar tercatat. KPI mengukur hasil itu per karyawan yang tertaut ke akun pengguna.
          </p>
        </div>
        <Link to="/hr/settings" className="btn btn-secondary">Pengaturan HR</Link>
      </div>

      {msg && <div style={{ background: '#e6f4ea', color: '#1e7e34', padding: 12, borderRadius: 8, marginBottom: 16 }}>{msg}</div>}
      {error && <div className="error-msg">{error}</div>}

      {/* ── Daily duties ── */}
      <div className="card" style={{ marginBottom: 20 }}>
        <div className="card-header">
          <h2>Tugas Harian</h2>
          <button className="btn btn-secondary btn-sm" onClick={() => setTaskForm({ ...emptyTask })}>+ Tugas</button>
        </div>

        {tasks.length === 0 && <p style={{ color: 'var(--ink-3)' }}>Belum ada tugas harian.</p>}
        {tasks.length > 0 && (
          <div style={{ overflowX: 'auto' }}>
            <table>
              <thead>
                <tr>
                  <th>Tugas</th><th>Jenis</th><th>Cakupan</th><th>Jeda Data</th><th>Toleransi</th><th>Status</th><th></th>
                </tr>
              </thead>
              <tbody>
                {tasks.map((t) => (
                  <tr key={t.id}>
                    <td>
                      <div style={{ fontWeight: 500 }}>{t.name}</div>
                      {t.description && <div style={{ fontSize: '0.8rem', color: 'var(--ink-3)' }}>{t.description}</div>}
                    </td>
                    <td>{taskTypeOf(t.task_type)?.label || t.task_type}</td>
                    <td>{t.scope === 'per_branch' ? 'Tiap cabang' : 'Global'}</td>
                    <td>{t.due_offset_days === 0 ? '—' : `H+${t.due_offset_days}`}</td>
                    <td>{t.grace_days === 0 ? 'Hari itu juga' : `${t.grace_days} hari`}</td>
                    <td><span className="badge">{t.is_active ? 'Aktif' : 'Nonaktif'}</span></td>
                    <td>
                      <div className="actions">
                        <button className="btn btn-secondary btn-sm"
                          onClick={() => setTaskForm({ ...t, starts_on: t.starts_on || 'always' })}>Edit</button>
                        <button className="btn btn-danger btn-sm" onClick={() => removeTask(t)}>Hapus</button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {taskForm && (
          <div style={{ marginTop: '1rem', paddingTop: '1rem', borderTop: '1px solid var(--line)' }}>
            <h3 style={{ fontSize: '0.95rem', marginBottom: '0.8rem' }}>{taskForm.id ? 'Ubah Tugas' : 'Tugas Baru'}</h3>
            <div style={grid}>
              <Field label="Nama Tugas"><input value={taskForm.name} onChange={(e) => setTaskForm((s) => ({ ...s, name: e.target.value }))} autoFocus /></Field>
              <Field label="Jenis" hint={taskTypeOf(taskForm.task_type)?.help}>
                <select value={taskForm.task_type} onChange={(e) => setTaskForm((s) => ({ ...s, task_type: e.target.value }))}>
                  {TASK_TYPES.map((t) => <option key={t.key} value={t.key}>{t.label}</option>)}
                </select>
              </Field>
              <Field label="Cakupan" hint="Tiap cabang menghasilkan satu tugas per cabang aktif per hari.">
                <select value={taskForm.scope} onChange={(e) => setTaskForm((s) => ({ ...s, scope: e.target.value }))}>
                  <option value="global">Global</option>
                  <option value="per_branch">Tiap cabang</option>
                </select>
              </Field>
              <Field label="Jeda Ketersediaan Data (hari)"
                hint="Berapa hari setelah tanggalnya data baru bisa dikerjakan. Import POS = 1, karena data penjualan baru tersedia keesokan harinya. Tugas yang belum bisa dikerjakan tidak ditampilkan.">
                <input type="number" min={0} value={taskForm.due_offset_days} onChange={(e) => setTaskForm((s) => ({ ...s, due_offset_days: e.target.value }))} />
              </Field>
              <Field label="Toleransi Keterlambatan (hari)" hint="Dihitung setelah jeda di atas. 0 = terlambat begitu harinya lewat.">
                <input type="number" min={0} value={taskForm.grace_days} onChange={(e) => setTaskForm((s) => ({ ...s, grace_days: e.target.value }))} />
              </Field>
              <Field label="Berlaku Sejak" hint="Kosongkan tanggal untuk memakai 'always' — tugas dinilai atas seluruh riwayat.">
                <input value={taskForm.starts_on} placeholder="YYYY-MM-DD atau always"
                  onChange={(e) => setTaskForm((s) => ({ ...s, starts_on: e.target.value }))} />
              </Field>
              <Field label="Tautan Halaman" hint="Tujuan saat notifikasi diklik, mis. /invoices/new">
                <input value={taskForm.link_path} onChange={(e) => setTaskForm((s) => ({ ...s, link_path: e.target.value }))} />
              </Field>
              <Field label="Keterangan" full>
                <input value={taskForm.description} onChange={(e) => setTaskForm((s) => ({ ...s, description: e.target.value }))} />
              </Field>
              <div className="form-group">
                <label style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontWeight: 400 }}>
                  <input type="checkbox" style={{ width: 'auto' }} checked={taskForm.is_active}
                    onChange={(e) => setTaskForm((s) => ({ ...s, is_active: e.target.checked }))} />
                  Aktif
                </label>
              </div>
            </div>
            <div className="form-actions">
              <button className="btn btn-primary" onClick={saveTask}>Simpan</button>
              <button className="btn btn-secondary" onClick={() => setTaskForm(null)}>Batal</button>
            </div>
          </div>
        )}
      </div>

      {/* ── KPI definitions ── */}
      <div className="card" style={{ marginBottom: 20 }}>
        <div className="card-header">
          <h2>KPI Staff</h2>
          <button className="btn btn-secondary btn-sm" disabled={activeTasks.length === 0}
            onClick={() => setKpiForm({ ...emptyKpi, definition_id: activeTasks[0]?.id || '' })}>+ KPI</button>
        </div>

        {activeTasks.length === 0 && <p style={{ color: 'var(--ink-3)' }}>Tambahkan tugas harian terlebih dahulu — KPI diukur dari tugas.</p>}
        {kpis.length === 0 && activeTasks.length > 0 && <p style={{ color: 'var(--ink-3)' }}>Belum ada KPI.</p>}

        {kpis.length > 0 && (
          <div style={{ overflowX: 'auto' }}>
            <table>
              <thead>
                <tr><th>KPI</th><th>Tugas</th><th>Metrik</th><th>Target</th><th>Bobot</th><th>Status</th><th></th></tr>
              </thead>
              <tbody>
                {kpis.map((k) => (
                  <tr key={k.id}>
                    <td style={{ fontWeight: 500 }}>{k.name}</td>
                    <td>{k.definition_name}</td>
                    <td>{metricOf(k.metric)?.label || k.metric}</td>
                    <td>{k.target_value}{metricOf(k.metric)?.unit === '%' ? '%' : ''}</td>
                    <td>{k.weight}×</td>
                    <td><span className="badge">{k.is_active ? 'Aktif' : 'Nonaktif'}</span></td>
                    <td>
                      <div className="actions">
                        <button className="btn btn-secondary btn-sm" onClick={() => setKpiForm({ ...k })}>Edit</button>
                        <button className="btn btn-danger btn-sm" onClick={() => removeKpi(k)}>Hapus</button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {kpiForm && (
          <div style={{ marginTop: '1rem', paddingTop: '1rem', borderTop: '1px solid var(--line)' }}>
            <h3 style={{ fontSize: '0.95rem', marginBottom: '0.8rem' }}>{kpiForm.id ? 'Ubah KPI' : 'KPI Baru'}</h3>
            <div style={grid}>
              <Field label="Nama KPI"><input value={kpiForm.name} onChange={(e) => setKpiForm((s) => ({ ...s, name: e.target.value }))} autoFocus /></Field>
              <Field label="Tugas yang Diukur">
                <select value={kpiForm.definition_id} onChange={(e) => setKpiForm((s) => ({ ...s, definition_id: e.target.value }))}>
                  {activeTasks.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
                </select>
              </Field>
              <Field label="Metrik" hint={metricOf(kpiForm.metric)?.help} full>
                <select value={kpiForm.metric} onChange={(e) => setKpiForm((s) => ({ ...s, metric: e.target.value }))}>
                  {METRICS.map((m) => <option key={m.key} value={m.key}>{m.label}</option>)}
                </select>
              </Field>
              <Field label={`Target (${metricOf(kpiForm.metric)?.unit || ''})`}>
                <input type="number" min={0} value={kpiForm.target_value} onChange={(e) => setKpiForm((s) => ({ ...s, target_value: e.target.value }))} />
              </Field>
              <Field label="Bobot" hint="KPI dengan bobot lebih besar lebih menentukan skor akhir.">
                <input type="number" min={1} value={kpiForm.weight} onChange={(e) => setKpiForm((s) => ({ ...s, weight: e.target.value }))} />
              </Field>
              <div className="form-group">
                <label style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontWeight: 400 }}>
                  <input type="checkbox" style={{ width: 'auto' }} checked={kpiForm.is_active}
                    onChange={(e) => setKpiForm((s) => ({ ...s, is_active: e.target.checked }))} />
                  Aktif
                </label>
              </div>
            </div>
            <div className="form-actions">
              <button className="btn btn-primary" onClick={saveKpi}>Simpan</button>
              <button className="btn btn-secondary" onClick={() => setKpiForm(null)}>Batal</button>
            </div>
          </div>
        )}
      </div>

      {/* ── Monthly scorecards ── */}
      <div className="card">
        <div className="card-header">
          <h2>Pencapaian Bulanan</h2>
          <input type="month" value={month} onChange={(e) => setMonth(e.target.value)} style={{ maxWidth: 180 }} />
        </div>

        {scoring && <p style={{ color: 'var(--ink-3)' }}>Menghitung…</p>}

        {!scoring && scores.length === 0 && (
          <p style={{ color: 'var(--ink-3)' }}>
            Belum ada karyawan yang tertaut ke akun pengguna. Tautkan pada halaman karyawan
            (<Link to="/hr/employees">Karyawan</Link> → Edit → Akun Pengguna) agar KPI dapat dihitung.
          </p>
        )}

        {!scoring && scores.map((c) => (
          <div key={c.employee_id} className="kpi-card">
            <div className="kpi-card-head">
              <div>
                <Link to={`/hr/employees/${c.employee_id}`} style={{ fontWeight: 600, color: 'var(--ink)' }}>{c.employee_name}</Link>
                <div style={{ fontSize: '0.8rem', color: 'var(--ink-3)' }}>{c.employee_code} · {c.username}</div>
              </div>
              <div className="kpi-score">
                <span className="kpi-score-value">{c.score}</span>
                <span className="kpi-score-label">skor</span>
              </div>
            </div>
            {c.results.length === 0 && <p style={{ color: 'var(--ink-3)', fontSize: '0.85rem', margin: 0 }}>Tidak ada data pada bulan ini.</p>}
            {c.results.map((r) => (
              <div key={r.kpi_id} className="kpi-row">
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: '0.88rem', fontWeight: 500 }}>{r.name}</div>
                  <div style={{ fontSize: '0.76rem', color: 'var(--ink-3)' }}>
                    {metricOf(r.metric)?.label} · {r.personal ? 'pribadi' : 'tim'} · target {r.target_value}{metricOf(r.metric)?.unit === '%' ? '%' : ''}
                  </div>
                </div>
                <div className="kpi-bar" role="img"
                  aria-label={`Pencapaian ${Math.round(r.achievement)} persen`}>
                  <div className="kpi-bar-fill" style={{ width: `${Math.min(100, r.achievement)}%` }} />
                </div>
                <div className="kpi-actual">
                  {r.actual_value}{metricOf(r.metric)?.unit === '%' ? '%' : ''}
                </div>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
