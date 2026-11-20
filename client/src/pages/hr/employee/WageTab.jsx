import { useEffect, useState, Fragment } from 'react';
import { getEmployeeWage, getEmployeeWageHistory, createEmployeeWage, getWageComponents, getPositionWageTemplate } from '../../../api';
import CurrencyInput from '../../../components/CurrencyInput';
import { todayWIB } from '../../../time';
import { fmtIDR } from '../../../format';
import { Field } from './Field';
import { TYPE_LABELS, fmtDateShort } from './shared';

// Base + fixed allowances - fixed deductions; per-present-day components over the
// expected working days. Bonuses and daily_allowance (paid in cash) are excluded.
function monthlyProjection(structure) {
  if (!structure) return 0;
  const workDays = Number(structure.working_days_per_month || 0);
  let total = Number(structure.base_salary || 0);
  for (const c of structure.components || []) {
    if (!c.component_is_fixed) continue;
    const perDay = c.component_calc_method === 'per_present_day';
    const value = Number(c.amount || 0) * (perDay ? workDays : 1);
    if (c.component_type === 'allowance') total += value;
    else if (c.component_type === 'deduction') total -= value;
  }
  return total;
}

function dailyPaidProjection(structure) {
  if (!structure) return 0;
  const workDays = Number(structure.working_days_per_month || 0);
  let total = 0;
  for (const c of structure.components || []) {
    if (c.component_type !== 'daily_allowance') continue;
    const perDay = c.component_calc_method === 'per_present_day';
    total += Number(c.amount || 0) * (perDay ? workDays : 1);
  }
  return total;
}

function ComponentRows({ components }) {
  if (!components || components.length === 0) {
    return <div style={{ color: '#999', fontSize: '0.85rem' }}>Tidak ada komponen.</div>;
  }
  return (
    <table style={{ width: '100%' }}>
      <thead>
        <tr><th>Komponen</th><th>Tipe</th><th>Sifat</th><th style={{ textAlign: 'right' }}>Nominal</th></tr>
      </thead>
      <tbody>
        {components.map(c => (
          <tr key={c.id}>
            <td>
              {c.component_name}
              {c.component_grade_linked && <span style={{ color: '#5b3cc4', fontSize: '0.8rem' }}> · ikut grade kinerja</span>}
            </td>
            <td>
              {TYPE_LABELS[c.component_type] || c.component_type}
              {c.component_type === 'daily_allowance' && (
                <div style={{ color: '#8a93a6', fontSize: '0.75rem' }}>di luar transfer gaji</div>
              )}
            </td>
            <td>{c.component_is_fixed ? 'Tetap' : 'Variabel'}</td>
            <td style={{ textAlign: 'right' }}>
              {fmtIDR(c.amount)}
              {c.component_calc_method === 'per_present_day' && <span style={{ color: '#8a93a6' }}> /hari</span>}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function WageTab({ employeeId, editable, isContract, positionId, positionName }) {
  const [current, setCurrent] = useState(null);
  const [history, setHistory] = useState([]);
  const [catalog, setCatalog] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [expanded, setExpanded] = useState({});
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const today = todayWIB();
  const [form, setForm] = useState({ base_salary: '', working_days_per_month: '26', effective_date: today });
  const [picked, setPicked] = useState([]); // [{ component_id, amount }]
  const [template, setTemplate] = useState(null); // the position's wage template, if any

  const load = () => {
    setLoading(true);
    Promise.all([
      getEmployeeWage(employeeId).then(r => r.data).catch(() => null),
      getEmployeeWageHistory(employeeId).then(r => r.data || []).catch(() => []),
    ]).then(([cur, hist]) => {
      setCurrent(cur);
      setHistory(hist);
    }).finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [employeeId]);

  const applyTemplate = (t) => {
    setForm(f => ({
      ...f,
      base_salary: String(t.base_salary),
      working_days_per_month: String(t.working_days_per_month),
    }));
    setPicked((t.components || [])
      .filter(c => c.component_is_active)
      .map(c => ({ component_id: c.wage_component_id, amount: String(c.amount) })));
  };

  const openForm = () => {
    setError('');
    getWageComponents({ active: 1 }).then(r => setCatalog(r.data || [])).catch(() => setCatalog([]));
    setTemplate(null);
    if (positionId) {
      getPositionWageTemplate(positionId).then(r => {
        setTemplate(r.data || null);
        if (!current && r.data) applyTemplate(r.data);
      }).catch(() => setTemplate(null));
    }
    if (current) {
      setForm({
        base_salary: String(current.base_salary || ''),
        working_days_per_month: String(current.working_days_per_month || '26'),
        effective_date: today,
      });
      setPicked((current.components || []).map(c => ({ component_id: c.wage_component_id, amount: String(c.amount) })));
    } else {
      setForm({ base_salary: '', working_days_per_month: '26', effective_date: today });
      setPicked([]);
    }
    setShowForm(true);
  };

  const dailyRatePreview = () => {
    const base = Number(form.base_salary || 0);
    const wd = Number(form.working_days_per_month || 0);
    if (!wd || wd < 1) return 0;
    return Math.round(base / wd);
  };

  const addComponent = (id) => {
    if (!id || picked.some(p => p.component_id === id)) return;
    setPicked(p => [...p, { component_id: id, amount: '' }]);
  };
  const removeComponent = (id) => setPicked(p => p.filter(x => x.component_id !== id));
  const setComponentAmount = (id, amount) => setPicked(p => p.map(x => x.component_id === id ? { ...x, amount } : x));

  const componentMeta = (id) => catalog.find(c => c.id === id) || {};

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (form.base_salary === '' || Number(form.base_salary) < 0) { setError('Gaji pokok wajib diisi'); return; }
    const wd = Number(form.working_days_per_month);
    if (!wd || wd < 1 || wd > 31) { setError('Hari kerja per bulan harus antara 1 dan 31'); return; }
    if (!form.effective_date) { setError('Tanggal berlaku wajib diisi'); return; }
    for (const p of picked) {
      if (p.amount === '' || Number(p.amount) < 0) { setError('Nominal komponen tidak boleh kosong/negatif'); return; }
    }
    setSubmitting(true);
    try {
      await createEmployeeWage(employeeId, {
        base_salary: Math.round(Number(form.base_salary)),
        working_days_per_month: wd,
        effective_date: form.effective_date,
        components: picked.map(p => ({ component_id: p.component_id, amount: Math.round(Number(p.amount)) })),
      });
      setShowForm(false);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan struktur gaji');
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) return <div style={{ color: '#999', padding: '2rem' }}>Memuat struktur gaji...</div>;

  const projection = monthlyProjection(current);
  const dailyPaid = dailyPaidProjection(current);

  return (
    <div style={{ display: 'grid', gap: '1rem' }}>
      <div className="card">
        <div className="card-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <h2>Struktur Gaji Saat Ini</h2>
          {editable && <button onClick={openForm} className="btn btn-primary btn-sm">Ubah Struktur Gaji</button>}
        </div>
        {!current ? (
          <div style={{ color: '#999', padding: '1rem 0' }}>Belum ada struktur gaji untuk karyawan ini.</div>
        ) : (
          <>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', gap: '0.5rem 1.5rem', marginBottom: '1rem' }}>
              <Field label="Gaji Pokok" value={fmtIDR(current.base_salary)} />
              <Field label="Hari Kerja / Bulan" value={current.working_days_per_month} />
              <Field label="Tarif Harian" value={fmtIDR(current.daily_rate)} />
              <Field label="Berlaku Sejak" value={fmtDateShort(current.effective_date)} />
            </div>
            <h3 style={{ fontSize: '0.95rem', margin: '0 0 0.5rem' }}>Komponen</h3>
            <ComponentRows components={current.components} />
            <div style={{ marginTop: '1rem', paddingTop: '0.75rem', borderTop: '1px solid #e8e8e8', display: 'flex', justifyContent: 'space-between', fontWeight: 600 }}>
              <span>Proyeksi Gaji Bulanan (pokok + tunjangan tetap − potongan tetap)</span>
              <span>{fmtIDR(projection)}</span>
            </div>
            {dailyPaid > 0 && (
              <div style={{ marginTop: '0.5rem', display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem', color: '#667' }}>
                <span>Dibayar harian secara tunai (di luar transfer gaji, estimasi {current.working_days_per_month} hari kerja)</span>
                <span>{fmtIDR(dailyPaid)}</span>
              </div>
            )}
            {current.thr ? (
              <div style={{ marginTop: '0.75rem', padding: '0.75rem 1rem', background: '#f5f9ff', border: '1px solid #d9e6fb', borderRadius: 8 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontWeight: 600 }}>
                  <span>THR (Tunjangan Hari Raya)</span>
                  <span style={{ color: '#1967d2' }}>{fmtIDR(current.thr.amount)}</span>
                </div>
                <div style={{ fontSize: '0.78rem', color: '#667', marginTop: 4 }}>
                  Masa kerja {current.thr.months_worked} bulan → {current.thr.months_worked >= 12 ? '1 bulan penuh' : `${current.thr.months_worked}/12`} × gaji pokok
                  {' '}(estimasi per {fmtDateShort(current.thr.as_of)}). Dibayarkan melalui run THR.
                  {current.thr.transitioned && (
                    <span> Dihitung sejak tanggal status tetap ({fmtDateShort(current.thr.tenure_since)}).</span>
                  )}
                </div>
              </div>
            ) : isContract && (
              <div style={{ marginTop: '0.75rem', padding: '0.75rem 1rem', background: '#f6f7fa', border: '1px solid #e6e8ee', borderRadius: 8, fontSize: '0.82rem', color: '#667' }}>
                <strong>THR (Tunjangan Hari Raya):</strong> Karyawan kontrak (PKWT) tidak menerima THR.
              </div>
            )}
          </>
        )}
      </div>

      <div className="card">
        <div className="card-header"><h2>Riwayat Struktur Gaji</h2></div>
        {history.length === 0 ? (
          <div style={{ color: '#999', padding: '0.5rem 0' }}>Belum ada riwayat.</div>
        ) : (
          <table style={{ width: '100%' }}>
            <thead>
              <tr><th>Berlaku</th><th>Berakhir</th><th style={{ textAlign: 'right' }}>Gaji Pokok</th><th style={{ textAlign: 'right' }}>Tarif Harian</th><th></th></tr>
            </thead>
            <tbody>
              {history.map(v => (
                <Fragment key={v.id}>
                  <tr>
                    <td>{fmtDateShort(v.effective_date)}</td>
                    <td>{v.end_date ? fmtDateShort(v.end_date) : <span className="badge" style={{ background: '#e6f4ea', color: '#1e7e34' }}>Aktif</span>}</td>
                    <td style={{ textAlign: 'right' }}>{fmtIDR(v.base_salary)}</td>
                    <td style={{ textAlign: 'right' }}>{fmtIDR(v.daily_rate)}</td>
                    <td style={{ textAlign: 'right' }}>
                      <button className="btn btn-secondary btn-sm" onClick={() => setExpanded(e => ({ ...e, [v.id]: !e[v.id] }))}>
                        {expanded[v.id] ? 'Tutup' : 'Detail'}
                      </button>
                    </td>
                  </tr>
                  {expanded[v.id] && (
                    <tr>
                      <td colSpan={5} style={{ background: '#f8f9fb' }}>
                        <div style={{ padding: '0.5rem' }}>
                          <ComponentRows components={v.components} />
                        </div>
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {showForm && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'flex-start', justifyContent: 'center', zIndex: 1000, overflowY: 'auto', padding: '2rem 1rem' }}>
          <div className="card" style={{ width: '100%', maxWidth: 560, padding: '2rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
              <h2 style={{ margin: 0, fontSize: '1.05rem' }}>Ubah Struktur Gaji</h2>
              <button onClick={() => setShowForm(false)} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
            </div>
            <p style={{ color: '#667', fontSize: '0.85rem', marginTop: 0 }}>
              Menyimpan akan membuat <strong>versi baru</strong>. Versi sebelumnya tetap tersimpan sebagai riwayat (tidak diubah).
            </p>
            {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
            {template && (
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.75rem', flexWrap: 'wrap', padding: '0.6rem 0.8rem', marginBottom: '1rem', background: '#f5f9ff', border: '1px solid #d9e6fb', borderRadius: 8, fontSize: '0.85rem' }}>
                <span>
                  Template jabatan <strong>{positionName}</strong>: gaji pokok {fmtIDR(template.base_salary)}, {(template.components || []).length} komponen
                </span>
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => applyTemplate(template)}>Isi dari Template</button>
              </div>
            )}
            <form onSubmit={handleSubmit}>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
                <div className="form-group">
                  <label>Gaji Pokok (Rp)</label>
                  <CurrencyInput value={form.base_salary} onChange={e => setForm(f => ({ ...f, base_salary: e.target.value }))} />
                </div>
                <div className="form-group">
                  <label>Hari Kerja / Bulan</label>
                  <input type="number" min="1" max="31" value={form.working_days_per_month} onChange={e => setForm(f => ({ ...f, working_days_per_month: e.target.value }))} />
                </div>
                <div className="form-group">
                  <label>Tanggal Berlaku</label>
                  <input type="date" value={form.effective_date} onChange={e => setForm(f => ({ ...f, effective_date: e.target.value }))} />
                </div>
                <div className="form-group">
                  <label>Tarif Harian (otomatis)</label>
                  <input value={fmtIDR(dailyRatePreview())} readOnly style={{ background: '#f3f4f6' }} />
                </div>
              </div>

              <h3 style={{ fontSize: '0.95rem', margin: '0.5rem 0' }}>Komponen</h3>
              <div className="form-group" style={{ marginBottom: '0.75rem' }}>
                <select value="" onChange={e => { addComponent(e.target.value); e.target.value = ''; }}>
                  <option value="">+ Tambah komponen dari katalog aktif...</option>
                  {catalog.filter(c => !picked.some(p => p.component_id === c.id)).map(c => (
                    <option key={c.id} value={c.id}>{c.name} — {TYPE_LABELS[c.type]} ({c.is_fixed ? 'Tetap' : 'Variabel'}){c.calc_method === 'per_present_day' ? ' · per hari hadir' : ''}</option>
                  ))}
                </select>
              </div>

              {picked.length === 0 ? (
                <div style={{ color: '#999', fontSize: '0.85rem', marginBottom: '0.75rem' }}>Belum ada komponen dipilih.</div>
              ) : (
                <table style={{ width: '100%', marginBottom: '0.75rem' }}>
                  <thead>
                    <tr><th>Komponen</th><th>Tipe</th><th style={{ textAlign: 'right' }}>Nominal (Rp)</th><th></th></tr>
                  </thead>
                  <tbody>
                    {picked.map(p => {
                      const meta = componentMeta(p.component_id);
                      const perDay = meta.calc_method === 'per_present_day';
                      return (
                        <tr key={p.component_id}>
                          <td>
                            {meta.name || '-'}
                            {perDay && <span style={{ color: '#8a93a6', fontSize: '0.8rem' }}> · per hari hadir</span>}
                            {meta.grade_linked && <span style={{ color: '#5b3cc4', fontSize: '0.8rem' }}> · ikut grade</span>}
                          </td>
                          <td>{TYPE_LABELS[meta.type] || '-'}</td>
                          <td>
                            <CurrencyInput value={p.amount} onChange={e => setComponentAmount(p.component_id, e.target.value)} />
                            {perDay && <div style={{ fontSize: '0.72rem', color: '#8a93a6' }}>tarif per hari hadir</div>}
                          </td>
                          <td style={{ textAlign: 'right' }}>
                            <button type="button" className="btn btn-danger btn-sm" onClick={() => removeComponent(p.component_id)}>Hapus</button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              )}

              <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1rem' }}>
                <button type="submit" className="btn btn-primary" disabled={submitting} style={{ flex: 1, justifyContent: 'center' }}>
                  {submitting ? 'Menyimpan...' : 'Simpan Versi Baru'}
                </button>
                <button type="button" onClick={() => setShowForm(false)} className="btn btn-secondary">Batal</button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
