import { useEffect, useState } from 'react';
import {
  getPositionWageTemplates, getPositionWageTemplate, savePositionWageTemplate,
  deletePositionWageTemplate, applyPositionWageTemplate, getWageComponents,
} from '../../api';
import CurrencyInput from '../../components/CurrencyInput';
import { todayWIB } from '../../time';

const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));
const fmtDate = (d) => (d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '—');

const TYPE_LABELS = {
  allowance: 'Tunjangan',
  bonus: 'Bonus',
  deduction: 'Potongan',
  daily_allowance: 'Harian (Tunai)',
  benefit_fund: 'Dana Kesejahteraan',
};

// First day of next month (YYYY-MM-DD) — the usual date a pay change takes effect.
function firstOfNextMonth() {
  const [y, m] = todayWIB().split('-').map(Number);
  const ny = m === 12 ? y + 1 : y;
  const nm = m === 12 ? 1 : m + 1;
  return `${ny}-${String(nm).padStart(2, '0')}-01`;
}

const overlay = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'flex-start', justifyContent: 'center', zIndex: 1000, overflowY: 'auto', padding: '2rem 1rem' };
const closeBtn = { background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' };

// Template gaji per jabatan. A template only prefills an employee's wage form,
// or — through "Terapkan" — creates a new dated wage version for the people in
// the role that HR ticks. Saving a template alone changes nobody's pay.
export default function PositionWageTemplates() {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(null);   // position row being edited
  const [applying, setApplying] = useState(null); // position row being applied

  const [reloadKey, setReloadKey] = useState(0);
  const load = () => setReloadKey((k) => k + 1);
  useEffect(() => {
    getPositionWageTemplates().then((r) => setRows(r.data || [])).catch(() => setRows([])).finally(() => setLoading(false));
  }, [reloadKey]);

  const remove = async (row) => {
    if (!confirm(`Hapus template gaji jabatan "${row.position_name}"? Struktur gaji karyawan tidak berubah.`)) return;
    try {
      await deletePositionWageTemplate(row.position_id);
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus template');
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Template Gaji Jabatan</h1>
      </div>
      <p style={{ color: '#667', fontSize: '0.9rem', marginBottom: '1rem', maxWidth: 860 }}>
        Setiap jabatan bisa punya template: gaji pokok, hari kerja, dan nominal tiap komponen. Template dipakai sebagai
        <strong> isian awal</strong> saat mengubah struktur gaji karyawan (tetap bisa diubah per orang). Tombol
        <strong> Terapkan</strong> membuat versi struktur gaji baru dari template untuk karyawan yang dipilih, mulai tanggal
        berlaku tertentu. Menyimpan template saja <strong>tidak</strong> mengubah gaji siapa pun.
      </p>

      <div className="card" style={{ maxWidth: 960, overflowX: 'auto' }}>
        <table>
          <thead>
            <tr>
              <th>Jabatan</th>
              <th style={{ textAlign: 'right' }}>Gaji Pokok</th>
              <th style={{ textAlign: 'center' }}>Hari Kerja</th>
              <th style={{ textAlign: 'center' }}>Karyawan Aktif</th>
              <th>Diperbarui</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={6} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Memuat…</td></tr>
            ) : rows.length === 0 ? (
              <tr><td colSpan={6} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada jabatan</td></tr>
            ) : rows.map((r) => (
              <tr key={r.position_id} style={{ opacity: r.position_is_active ? 1 : 0.6 }}>
                <td style={{ fontWeight: 500 }}>
                  {r.position_name}
                  {!r.position_is_active && <span style={{ color: '#8a93a6', fontSize: '0.8rem' }}> · nonaktif</span>}
                </td>
                <td style={{ textAlign: 'right' }}>{r.has_template ? fmtIDR(r.base_salary) : <span style={{ color: '#aab' }}>belum ada template</span>}</td>
                <td style={{ textAlign: 'center' }}>{r.has_template ? r.working_days_per_month : '—'}</td>
                <td style={{ textAlign: 'center' }}>{r.active_employees}</td>
                <td>{r.has_template ? fmtDate(r.updated_at) : '—'}</td>
                <td>
                  <div className="actions" style={{ justifyContent: 'flex-end' }}>
                    <button onClick={() => setEditing(r)} className="btn btn-secondary btn-sm">{r.has_template ? 'Edit' : 'Buat Template'}</button>
                    {r.has_template && (
                      <>
                        <button onClick={() => setApplying(r)} className="btn btn-primary btn-sm" disabled={r.active_employees === 0}
                          title={r.active_employees === 0 ? 'Tidak ada karyawan aktif di jabatan ini' : 'Buat versi struktur gaji baru dari template'}>
                          Terapkan
                        </button>
                        <button onClick={() => remove(r)} className="btn btn-danger btn-sm">Hapus</button>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {editing && <TemplateEditor row={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); load(); }} />}
      {applying && <ApplyDialog row={applying} onClose={() => setApplying(null)} onDone={load} />}
    </>
  );
}

function TemplateEditor({ row, onClose, onSaved }) {
  const [catalog, setCatalog] = useState([]);
  const [form, setForm] = useState({ base_salary: '', working_days_per_month: '26' });
  const [picked, setPicked] = useState([]); // [{ component_id, amount }]
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    Promise.all([
      getWageComponents().then((r) => r.data || []).catch(() => []),
      row.has_template ? getPositionWageTemplate(row.position_id).then((r) => r.data).catch(() => null) : Promise.resolve(null),
    ]).then(([comps, t]) => {
      setCatalog(comps);
      if (t) {
        setForm({ base_salary: String(t.base_salary), working_days_per_month: String(t.working_days_per_month) });
        setPicked((t.components || []).map((c) => ({ component_id: c.wage_component_id, amount: String(c.amount) })));
      }
    }).finally(() => setLoading(false));
  }, [row]);

  const meta = (id) => catalog.find((c) => c.id === id) || {};
  const wd = Number(form.working_days_per_month || 0);
  const dailyRate = wd >= 1 ? Math.round(Number(form.base_salary || 0) / wd) : 0;

  const save = async (e) => {
    e.preventDefault();
    setError('');
    if (form.base_salary === '' || Number(form.base_salary) < 0) { setError('Gaji pokok wajib diisi'); return; }
    if (!wd || wd < 1 || wd > 31) { setError('Hari kerja per bulan harus antara 1 dan 31'); return; }
    for (const p of picked) {
      if (p.amount === '' || Number(p.amount) < 0) { setError('Nominal komponen tidak boleh kosong/negatif'); return; }
    }
    setSaving(true);
    try {
      await savePositionWageTemplate(row.position_id, {
        base_salary: Math.round(Number(form.base_salary)),
        working_days_per_month: wd,
        components: picked.map((p) => ({ component_id: p.component_id, amount: Math.round(Number(p.amount)) })),
      });
      onSaved();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan template');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={overlay}>
      <div className="card" style={{ width: '100%', maxWidth: 600, padding: '2rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
          <h2 style={{ margin: 0, fontSize: '1.05rem' }}>Template Gaji — {row.position_name}</h2>
          <button onClick={onClose} style={closeBtn}>✕</button>
        </div>
        {loading ? <div style={{ color: '#999' }}>Memuat…</div> : (
          <form onSubmit={save}>
            {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '0.75rem' }}>
              <div className="form-group">
                <label>Gaji Pokok (Rp)</label>
                <CurrencyInput value={form.base_salary} onChange={(e) => setForm((f) => ({ ...f, base_salary: e.target.value }))} />
              </div>
              <div className="form-group">
                <label>Hari Kerja / Bulan</label>
                <input type="number" min="1" max="31" value={form.working_days_per_month}
                  onChange={(e) => setForm((f) => ({ ...f, working_days_per_month: e.target.value }))} />
              </div>
              <div className="form-group">
                <label>Tarif Harian (otomatis)</label>
                <input value={fmtIDR(dailyRate)} readOnly style={{ background: '#f3f4f6' }} />
              </div>
            </div>

            <h3 style={{ fontSize: '0.95rem', margin: '0.5rem 0' }}>Komponen</h3>
            <div className="form-group" style={{ marginBottom: '0.75rem' }}>
              <select value="" onChange={(e) => {
                const id = e.target.value;
                if (id && !picked.some((p) => p.component_id === id)) setPicked((p) => [...p, { component_id: id, amount: '' }]);
              }}>
                <option value="">+ Tambah komponen dari katalog aktif...</option>
                {catalog.filter((c) => c.is_active && !picked.some((p) => p.component_id === c.id)).map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name} — {TYPE_LABELS[c.type] || c.type}{c.calc_method === 'per_present_day' ? ' · per hari hadir' : ''}{c.grade_linked ? ' · ikut grade' : ''}
                  </option>
                ))}
              </select>
            </div>

            {picked.length === 0 ? (
              <div style={{ color: '#999', fontSize: '0.85rem', marginBottom: '0.75rem' }}>Belum ada komponen dipilih.</div>
            ) : (
              <table style={{ width: '100%', marginBottom: '0.75rem' }}>
                <thead><tr><th>Komponen</th><th>Tipe</th><th style={{ textAlign: 'right' }}>Nominal (Rp)</th><th></th></tr></thead>
                <tbody>
                  {picked.map((p) => {
                    const m = meta(p.component_id);
                    const perDay = m.calc_method === 'per_present_day';
                    return (
                      <tr key={p.component_id}>
                        <td>
                          {m.name || '—'}
                          {perDay && <span style={{ color: '#8a93a6', fontSize: '0.8rem' }}> · per hari hadir</span>}
                          {m.grade_linked && <span style={{ color: '#5b3cc4', fontSize: '0.8rem' }}> · ikut grade</span>}
                          {m.is_active === false && <span style={{ color: '#c5221f', fontSize: '0.8rem' }}> · nonaktif (dilewati saat diterapkan)</span>}
                        </td>
                        <td>{TYPE_LABELS[m.type] || '—'}</td>
                        <td>
                          <CurrencyInput value={p.amount} onChange={(e) => setPicked((ps) => ps.map((x) => (x.component_id === p.component_id ? { ...x, amount: e.target.value } : x)))} />
                          {perDay && <div style={{ fontSize: '0.72rem', color: '#8a93a6' }}>tarif per hari hadir</div>}
                        </td>
                        <td style={{ textAlign: 'right' }}>
                          <button type="button" className="btn btn-danger btn-sm" onClick={() => setPicked((ps) => ps.filter((x) => x.component_id !== p.component_id))}>Hapus</button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}

            <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1rem' }}>
              <button type="submit" className="btn btn-primary" disabled={saving} style={{ flex: 1, justifyContent: 'center' }}>
                {saving ? 'Menyimpan...' : 'Simpan Template'}
              </button>
              <button type="button" onClick={onClose} className="btn btn-secondary">Batal</button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}

function ApplyDialog({ row, onClose, onDone }) {
  const [tmpl, setTmpl] = useState(null);
  const [effective, setEffective] = useState(firstOfNextMonth());
  const [checked, setChecked] = useState({});
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);

  useEffect(() => {
    getPositionWageTemplate(row.position_id).then((r) => {
      setTmpl(r.data);
      setChecked(Object.fromEntries((r.data?.employees || []).map((e) => [e.id, true])));
    }).catch(() => setError('Gagal memuat template'));
  }, [row]);

  const employees = tmpl?.employees || [];
  // A version can only be added after the one in force; those are skipped by the server too.
  const blocked = (e) => e.current_effective_date && e.current_effective_date.slice(0, 10) >= effective;
  const selected = employees.filter((e) => checked[e.id]);
  const willApply = selected.filter((e) => !blocked(e));

  const submit = async () => {
    setError('');
    if (!effective) { setError('Tanggal berlaku wajib diisi'); return; }
    if (selected.length === 0) { setError('Pilih minimal satu karyawan'); return; }
    if (!confirm(`Buat struktur gaji baru dari template untuk ${willApply.length} karyawan, berlaku ${fmtDate(effective)}? Perbedaan per orang pada struktur gaji mereka akan diganti dengan isi template.`)) return;
    setBusy(true);
    try {
      const r = await applyPositionWageTemplate(row.position_id, { effective_date: effective, employee_ids: selected.map((e) => e.id) });
      setResult(r.data);
      onDone();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menerapkan template');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={overlay}>
      <div className="card" style={{ width: '100%', maxWidth: 640, padding: '2rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
          <h2 style={{ margin: 0, fontSize: '1.05rem' }}>Terapkan Template — {row.position_name}</h2>
          <button onClick={onClose} style={closeBtn}>✕</button>
        </div>

        {result ? (
          <>
            <p style={{ marginTop: 0 }}>
              <strong>{result.applied.length}</strong> karyawan mendapat struktur gaji baru berlaku {fmtDate(effective)}.
            </p>
            {result.skipped.length > 0 && (
              <>
                <p style={{ marginBottom: 4 }}>Dilewati ({result.skipped.length}):</p>
                <ul style={{ marginTop: 0, fontSize: '0.85rem' }}>
                  {result.skipped.map((s) => <li key={s.employee_id}>{s.name} — {s.reason}</li>)}
                </ul>
              </>
            )}
            <button className="btn btn-primary" onClick={onClose}>Tutup</button>
          </>
        ) : !tmpl ? (
          error ? <div className="error-msg">{error}</div> : <div style={{ color: '#999' }}>Memuat…</div>
        ) : (
          <>
            <div style={{ padding: '0.6rem 0.8rem', marginBottom: '1rem', background: '#f5f9ff', border: '1px solid #d9e6fb', borderRadius: 8, fontSize: '0.85rem' }}>
              Gaji pokok <strong>{fmtIDR(tmpl.base_salary)}</strong> · {tmpl.working_days_per_month} hari kerja · tarif harian {fmtIDR(tmpl.daily_rate)}
              {(tmpl.components || []).length > 0 && (
                <div style={{ marginTop: 4, color: '#556' }}>
                  {tmpl.components.filter((c) => c.component_is_active).map((c) => `${c.component_name} ${fmtIDR(c.amount)}${c.component_calc_method === 'per_present_day' ? '/hari' : ''}`).join(' · ')}
                </div>
              )}
            </div>

            {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

            <div className="form-group" style={{ maxWidth: 220 }}>
              <label>Tanggal Berlaku</label>
              <input type="date" value={effective} onChange={(e) => setEffective(e.target.value)} />
            </div>

            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', margin: '0.5rem 0' }}>
              <strong style={{ fontSize: '0.9rem' }}>Karyawan ({selected.length}/{employees.length} dipilih)</strong>
              <span style={{ display: 'flex', gap: 6 }}>
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => setChecked(Object.fromEntries(employees.map((e) => [e.id, true])))}>Pilih semua</button>
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => setChecked({})}>Kosongkan</button>
              </span>
            </div>
            <div style={{ maxHeight: 320, overflowY: 'auto', border: '1px solid #e6e8ee', borderRadius: 8 }}>
              <table style={{ width: '100%' }}>
                <thead><tr><th></th><th>Nama</th><th style={{ textAlign: 'right' }}>Gaji Pokok Saat Ini</th><th>Berlaku Sejak</th></tr></thead>
                <tbody>
                  {employees.map((e) => (
                    <tr key={e.id} style={{ opacity: blocked(e) ? 0.55 : 1 }}>
                      <td><input type="checkbox" checked={!!checked[e.id]} onChange={() => setChecked((c) => ({ ...c, [e.id]: !c[e.id] }))} /></td>
                      <td>
                        {e.full_name} <span style={{ color: '#8a93a6', fontSize: '0.8rem' }}>{e.employee_code}</span>
                        {blocked(e) && <div style={{ fontSize: '0.75rem', color: '#a06800' }}>dilewati: struktur gaji saat ini sudah tertanggal sama/sesudahnya</div>}
                      </td>
                      <td style={{ textAlign: 'right' }}>{e.current_base_salary != null ? fmtIDR(e.current_base_salary) : <span style={{ color: '#aab' }}>belum ada</span>}</td>
                      <td>{fmtDate(e.current_effective_date)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <p style={{ fontSize: '0.8rem', color: '#a06800', margin: '0.75rem 0 0' }}>
              Struktur gaji baru berisi persis isi template: komponen atau nominal khusus per orang akan diganti. Versi lama tetap tersimpan di riwayat.
            </p>

            <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1rem' }}>
              <button className="btn btn-primary" onClick={submit} disabled={busy || willApply.length === 0} style={{ flex: 1, justifyContent: 'center' }}>
                {busy ? 'Menerapkan...' : `Terapkan ke ${willApply.length} karyawan`}
              </button>
              <button className="btn btn-secondary" onClick={onClose}>Batal</button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
