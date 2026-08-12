import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  createEmployee, updateEmployee, getBranches, getPositions, createPosition,
  getEmployeeDocuments, uploadEmployeeDocument, deleteEmployeeDocument,
} from '../../api';
import Icon from '../../components/Icon';

const today = () => new Date().toISOString().slice(0, 10);

const STEPS = [
  { key: 'pribadi',     title: 'Data Pribadi',  icon: 'user',        desc: 'Identitas & kontak' },
  { key: 'kepegawaian', title: 'Kepegawaian',   icon: 'briefcase',   desc: 'Jabatan & status' },
  { key: 'bank',        title: 'Rekening Bank', icon: 'bank',        desc: 'Info pembayaran' },
  { key: 'dokumen',     title: 'Dokumen',       icon: 'paperclip',   desc: 'Unggah berkas' },
  { key: 'selesai',     title: 'Selesai',       icon: 'checkCircle', desc: 'Ringkasan' },
];

const DOC_STEP = 3;
const DONE_STEP = 4;

const DOC_TYPES = [
  ['ktp', 'KTP'], ['kk', 'Kartu Keluarga'], ['ijazah', 'Ijazah'],
  ['npwp', 'NPWP'], ['bpjs_kesehatan', 'BPJS Kesehatan'],
  ['bpjs_ketenagakerjaan', 'BPJS Ketenagakerjaan'],
  ['pkwt', 'Kontrak PKWT (ditandatangani)'], ['pkwtt', 'Kontrak PKWTT (ditandatangani)'],
  ['foto', 'Pas Foto'], ['surat_lamaran', 'Surat Lamaran / CV'], ['other', 'Lainnya'],
];
const docTypeLabel = (key) => DOC_TYPES.find((d) => d[0] === key)?.[1] || key;

const emptyEmp = {
  employee_code: '', full_name: '', national_id: '', dob: '',
  phone: '', email: '', address: '',
  position_id: '', branch_id: '', join_date: today(),
  employment_type: 'permanent', contract_end_date: '', status: 'active',
  bank_name: '', bank_account_number: '', bank_account_holder: '',
};

const MAX_UPLOAD_MB = 25;

function Field({ label, hint, children, full }) {
  return (
    <div className="form-group" style={full ? { gridColumn: '1 / -1' } : undefined}>
      <label>{label}</label>
      {children}
      {hint && <div style={{ fontSize: '0.78rem', color: 'var(--ink-3)', marginTop: 4 }}>{hint}</div>}
    </div>
  );
}

function Summary({ label, value }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value || '—'}</dd>
    </div>
  );
}

const grid = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '0 1rem' };

const formatBytes = (n) => (n >= 1048576 ? `${(n / 1048576).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);

export default function OnboardingWizard() {
  const navigate = useNavigate();
  const [step, setStep] = useState(0);
  const [direction, setDirection] = useState('next');
  const [emp, setEmp] = useState(emptyEmp);
  const [branches, setBranches] = useState([]);
  const [positions, setPositions] = useState([]);
  const [empId, setEmpId] = useState(null);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  const [newPos, setNewPos] = useState('');
  const [showAddPos, setShowAddPos] = useState(false);

  const [docs, setDocs] = useState([]);
  const [docForm, setDocForm] = useState({ doc_type: 'ktp', title: '', notes: '', file: null });
  const [uploading, setUploading] = useState(false);
  const fileRef = useRef(null);
  const errorRef = useRef(null);

  const set = (k) => (e) => setEmp((s) => ({ ...s, [k]: e.target.value }));

  useEffect(() => {
    getBranches().then((r) => setBranches(r.data)).catch(() => {});
    getPositions().then((r) => setPositions(r.data)).catch(() => {});
  }, []);

  // The error banner sits above a long panel; on a short viewport a validation
  // message triggered from the footer button would otherwise land off-screen.
  const fail = (msg) => {
    setError(msg);
    requestAnimationFrame(() => errorRef.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }));
  };

  const addPosition = async () => {
    const name = newPos.trim();
    if (!name) return;
    try {
      const r = await createPosition({ name });
      setPositions((p) => [...p, r.data].sort((a, b) => a.name.localeCompare(b.name)));
      setEmp((s) => ({ ...s, position_id: r.data.id }));
      setNewPos('');
      setShowAddPos(false);
      setError('');
    } catch (err) {
      fail(err.response?.data?.error || 'Gagal menambah jabatan.');
    }
  };

  const validateStep = () => {
    if (step === 0 && !emp.full_name.trim()) return 'Nama lengkap wajib diisi.';
    if (step === 1) {
      if (!emp.position_id) return 'Jabatan wajib dipilih.';
      if (!emp.branch_id) return 'Cabang wajib dipilih.';
      if (!emp.join_date) return 'Tanggal bergabung wajib diisi.';
      if (emp.employment_type === 'contract' && !emp.contract_end_date)
        return 'Tanggal berakhir kontrak wajib diisi untuk karyawan kontrak.';
    }
    return '';
  };

  // Persist the employee (create once, then update on subsequent passes) when
  // leaving the Bank step, so the Documents step has an employee id to attach
  // to. Returns the id — reading empId straight after setEmpId would still see
  // the previous render's value.
  const saveEmployee = async () => {
    setSaving(true);
    setError('');
    try {
      if (empId) {
        await updateEmployee(empId, emp);
        return empId;
      }
      const r = await createEmployee(emp);
      setEmpId(r.data.id);
      // Pick up server-assigned fields, notably the generated employee code.
      setEmp((s) => ({ ...s, employee_code: r.data.employee_code || s.employee_code }));
      return r.data.id;
    } catch (err) {
      fail(err.response?.data?.error || 'Gagal menyimpan data karyawan.');
      return null;
    } finally {
      setSaving(false);
    }
  };

  const goTo = (target) => {
    setDirection(target < step ? 'back' : 'next');
    setStep(target);
  };

  const next = async () => {
    const v = validateStep();
    if (v) { fail(v); return; }
    setError('');
    if (step === DOC_STEP - 1) {
      const id = await saveEmployee();
      if (!id) return;
      getEmployeeDocuments(id).then((r) => setDocs(r.data)).catch(() => {});
    }
    goTo(Math.min(step + 1, STEPS.length - 1));
  };

  const back = () => { setError(''); goTo(Math.max(step - 1, 0)); };

  const uploadDoc = async () => {
    if (!docForm.file) { fail('Pilih berkas terlebih dahulu.'); return; }
    if (docForm.file.size > MAX_UPLOAD_MB * 1048576) {
      fail(`Ukuran berkas melebihi ${MAX_UPLOAD_MB} MB.`);
      return;
    }
    if (!empId) { fail('Data karyawan belum tersimpan.'); return; }
    setUploading(true);
    setError('');
    try {
      const fd = new FormData();
      fd.append('file', docForm.file);
      fd.append('doc_type', docForm.doc_type);
      fd.append('title', docForm.title.trim() || docTypeLabel(docForm.doc_type));
      fd.append('notes', docForm.notes);
      const r = await uploadEmployeeDocument(empId, fd);
      setDocs((d) => [r.data, ...d]);
      setDocForm({ doc_type: 'ktp', title: '', notes: '', file: null });
      if (fileRef.current) fileRef.current.value = '';
    } catch (err) {
      fail(err.response?.data?.error || 'Gagal mengunggah dokumen.');
    } finally {
      setUploading(false);
    }
  };

  const removeDoc = async (doc) => {
    if (!confirm(`Hapus dokumen "${doc.title}"?`)) return;
    try {
      await deleteEmployeeDocument(empId, doc.id);
      setDocs((d) => d.filter((x) => x.id !== doc.id));
    } catch {
      fail('Gagal menghapus dokumen.');
    }
  };

  // Leaving mid-flow before the employee is saved loses everything typed so far.
  const exit = () => {
    const dirty = !empId && (emp.full_name.trim() || emp.national_id.trim() || emp.phone.trim());
    if (dirty && !confirm('Keluar dari onboarding? Data yang sudah diisi akan hilang.')) return;
    navigate('/hr/employees');
  };

  const posName = positions.find((p) => p.id === emp.position_id)?.name || '';
  const brName = branches.find((b) => b.id === emp.branch_id)?.name || '';
  const stepInfo = STEPS[step];

  return (
    <>
      <div className="page-header">
        <h1>Onboarding Karyawan Baru</h1>
        <button className="btn btn-secondary" onClick={exit}>Keluar</button>
      </div>

      <p className="onb-step-count">Langkah {step + 1} dari {STEPS.length}</p>

      {/* Stepper. Completed steps are clickable so a correction doesn't require
          walking the whole flow backwards.
          The icon sits above its label rather than beside it: side by side, the
          label had to share a narrow column with the connector line and got
          clipped as soon as the window narrowed. Stacked, each label owns the
          full width of its column and wraps instead of truncating. */}
      <ol className="onb-stepper">
        {STEPS.map((s, i) => {
          const state = i < step ? 'done' : i === step ? 'active' : 'todo';
          const reachable = i < step && step !== DONE_STEP;
          return (
            <li key={s.key} className={`onb-step-item ${state}`}>
              <button
                type="button"
                className="onb-step-btn"
                aria-disabled={!reachable}
                aria-current={i === step ? 'step' : undefined}
                onClick={reachable ? () => goTo(i) : undefined}
              >
                <span className="onb-dot">
                  <Icon name={i < step ? 'check' : s.icon} size={20} />
                </span>
                <span className="onb-step-label">
                  <span className="onb-step-title">{s.title}</span>
                  <span className="onb-step-desc">{s.desc}</span>
                </span>
              </button>
            </li>
          );
        })}
      </ol>

      <div ref={errorRef} aria-live="polite">
        {error && <div className="error-msg" style={{ margin: '1rem 0' }}>{error}</div>}
      </div>

      {/* key remounts the panel so the slide-in replays on each step change. */}
      <div key={step} className={`onb-panel card${direction === 'back' ? ' back' : ''}`}>
        <div className="card-header">
          <h2 className="onb-panel-title">
            <span className="onb-panel-icon"><Icon name={stepInfo.icon} size={18} /></span>
            <span>{stepInfo.title}</span>
            <span className="onb-panel-desc">{stepInfo.desc}</span>
          </h2>
        </div>

        {step === 0 && (
          <div style={grid}>
            <Field label="Nama Lengkap *"><input value={emp.full_name} onChange={set('full_name')} autoFocus /></Field>
            <Field label="Kode Karyawan" hint="Kosongkan untuk penomoran otomatis.">
              <input value={emp.employee_code} onChange={set('employee_code')} placeholder="mis. EMP-0001" />
            </Field>
            <Field label="NIK / KTP"><input value={emp.national_id} onChange={set('national_id')} inputMode="numeric" /></Field>
            <Field label="Tanggal Lahir"><input type="date" value={emp.dob} onChange={set('dob')} /></Field>
            <Field label="Telepon"><input value={emp.phone} onChange={set('phone')} inputMode="tel" placeholder="08…" /></Field>
            <Field label="Email"><input type="email" value={emp.email} onChange={set('email')} /></Field>
            <Field label="Alamat" full><textarea value={emp.address} onChange={set('address')} /></Field>
          </div>
        )}

        {step === 1 && (
          <div style={grid}>
            <Field label="Jabatan *">
              <div style={{ display: 'flex', gap: '0.4rem' }}>
                <select value={emp.position_id} onChange={set('position_id')} style={{ flex: 1 }}>
                  <option value="">Pilih jabatan</option>
                  {positions.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => setShowAddPos((v) => !v)}
                  aria-expanded={showAddPos}>
                  {showAddPos ? 'Batal' : '+ Baru'}
                </button>
              </div>
              {showAddPos && (
                <div style={{ display: 'flex', gap: '0.4rem', marginTop: '0.5rem' }}>
                  <input value={newPos} onChange={(e) => setNewPos(e.target.value)} placeholder="Nama jabatan baru"
                    style={{ flex: 1 }} autoFocus
                    onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addPosition(); } }} />
                  <button type="button" className="btn btn-primary btn-sm" onClick={addPosition}>Simpan</button>
                </div>
              )}
            </Field>
            <Field label="Cabang *">
              <select value={emp.branch_id} onChange={set('branch_id')}>
                <option value="">Pilih cabang</option>
                {branches.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
              </select>
            </Field>
            <Field label="Tanggal Bergabung *"><input type="date" value={emp.join_date} onChange={set('join_date')} /></Field>
            <Field label="Tipe Kepegawaian">
              <select
                value={emp.employment_type}
                onChange={(e) => setEmp((s) => ({ ...s, employment_type: e.target.value, contract_end_date: e.target.value === 'permanent' ? '' : s.contract_end_date }))}
              >
                <option value="permanent">Tetap (PKWTT)</option>
                <option value="contract">Kontrak (PKWT)</option>
              </select>
            </Field>
            {emp.employment_type === 'contract' && (
              <Field label="Tanggal Berakhir Kontrak *"><input type="date" value={emp.contract_end_date} onChange={set('contract_end_date')} /></Field>
            )}
            <Field label="Status">
              <select value={emp.status} onChange={set('status')}>
                <option value="active">Aktif</option>
                <option value="inactive">Nonaktif</option>
              </select>
            </Field>
          </div>
        )}

        {step === 2 && (
          <>
            <div style={grid}>
              <Field label="Nama Bank"><input value={emp.bank_name} onChange={set('bank_name')} placeholder="mis. BCA / Mandiri" /></Field>
              <Field label="Nomor Rekening"><input value={emp.bank_account_number} onChange={set('bank_account_number')} inputMode="numeric" /></Field>
              <Field label="Nama Pemilik Rekening" hint="Isi bila berbeda dengan nama karyawan.">
                <input value={emp.bank_account_holder} onChange={set('bank_account_holder')} placeholder={emp.full_name} />
              </Field>
            </div>
            <p style={{ color: 'var(--ink-3)', fontSize: '0.85rem', margin: '0.4rem 0 0' }}>
              Menekan “Simpan &amp; Lanjut” akan menyimpan data karyawan, sehingga dokumen yang telah ditandatangani dapat diunggah.
            </p>
          </>
        )}

        {step === DOC_STEP && (
          <div>
            <div style={{ ...grid, gap: '0 1rem' }}>
              <Field label="Jenis Dokumen">
                <select value={docForm.doc_type} onChange={(e) => setDocForm((s) => ({ ...s, doc_type: e.target.value }))}>
                  {DOC_TYPES.map(([k, l]) => <option key={k} value={k}>{l}</option>)}
                </select>
              </Field>
              <Field label="Judul" hint={`Kosongkan untuk memakai “${docTypeLabel(docForm.doc_type)}”.`}>
                <input value={docForm.title} onChange={(e) => setDocForm((s) => ({ ...s, title: e.target.value }))} />
              </Field>
              <Field label="Berkas" hint={`PDF, JPG, PNG, atau DOCX. Maksimal ${MAX_UPLOAD_MB} MB.`}>
                <input ref={fileRef} type="file" accept=".pdf,.jpg,.jpeg,.png,.doc,.docx"
                  onChange={(e) => setDocForm((s) => ({ ...s, file: e.target.files?.[0] || null }))} />
              </Field>
              <Field label="Catatan"><input value={docForm.notes} onChange={(e) => setDocForm((s) => ({ ...s, notes: e.target.value }))} /></Field>
            </div>
            <button className="btn btn-primary onb-btn-icon" disabled={uploading || !docForm.file} onClick={uploadDoc}>
              {uploading ? 'Mengunggah…' : <><Icon name="upload" size={16} />Unggah Dokumen</>}
            </button>

            <div style={{ marginTop: '1.4rem' }}>
              <h3 style={{ fontSize: '0.95rem', marginBottom: '0.6rem' }}>Dokumen Terunggah ({docs.length})</h3>
              {docs.length === 0 ? (
                <p className="onb-doc-empty">
                  Belum ada dokumen. Unggah KTP, ijazah, kontrak yang telah ditandatangani, dan berkas lainnya.
                  Langkah ini boleh dilewati dan dilengkapi nanti dari halaman karyawan.
                </p>
              ) : docs.map((d) => (
                <div key={d.id} className="onb-doc-row">
                  <span className="onb-doc-icon"><Icon name="file" size={18} /></span>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div className="onb-doc-name">{d.title}</div>
                    <div className="onb-doc-meta">
                      {docTypeLabel(d.doc_type)} · {d.original_name}{d.size_bytes ? ` · ${formatBytes(d.size_bytes)}` : ''}
                    </div>
                  </div>
                  <button className="btn btn-secondary btn-sm" onClick={() => removeDoc(d)}
                    aria-label={`Hapus dokumen ${d.title}`}>Hapus</button>
                </div>
              ))}
            </div>
          </div>
        )}

        {step === DONE_STEP && (
          <div className="onb-summary">
            <div style={{ textAlign: 'center', padding: '0.6rem 0 1.4rem' }}>
              <div className="onb-check-badge"><Icon name="check" size={32} strokeWidth={2.6} /></div>
              <h2 style={{ margin: '0.6rem 0 0.2rem' }}>{emp.full_name} berhasil di-onboarding</h2>
              <p style={{ color: 'var(--ink-3)', margin: 0 }}>
                Data karyawan dan {docs.length} dokumen telah tersimpan.
              </p>
            </div>
            <dl style={grid}>
              <Summary label="Kode Karyawan" value={emp.employee_code} />
              <Summary label="Jabatan" value={posName} />
              <Summary label="Cabang" value={brName} />
              <Summary label="Tipe" value={emp.employment_type === 'contract' ? 'Kontrak (PKWT)' : 'Tetap (PKWTT)'} />
              <Summary label="Tgl. Bergabung" value={emp.join_date} />
              <Summary label="Dokumen" value={`${docs.length} berkas`} />
            </dl>
          </div>
        )}
      </div>

      <div className="onb-actions">
        {step > 0 && step < DONE_STEP
          ? <button className="btn btn-secondary onb-btn-icon" onClick={back} disabled={saving}><Icon name="arrowLeft" size={16} />Kembali</button>
          : <span />}

        {step < DOC_STEP && (
          <button className="btn btn-primary onb-btn-icon" onClick={next} disabled={saving}>
            {saving ? 'Menyimpan…' : <>{step === DOC_STEP - 1 ? 'Simpan & Lanjut' : 'Lanjut'}<Icon name="arrowRight" size={16} /></>}
          </button>
        )}
        {step === DOC_STEP && (
          <button className="btn btn-primary onb-btn-icon" onClick={() => goTo(DONE_STEP)}>
            {docs.length === 0 ? 'Lewati & Selesai' : 'Selesai'}<Icon name="arrowRight" size={16} />
          </button>
        )}
        {step === DONE_STEP && (
          <div style={{ display: 'flex', gap: '0.6rem', flexWrap: 'wrap' }}>
            <button className="btn btn-secondary" onClick={() => navigate('/hr/employees')}>Ke Daftar Karyawan</button>
            <button className="btn btn-primary" onClick={() => navigate(`/hr/employees/${empId}`)}>Lihat Karyawan</button>
          </div>
        )}
      </div>
    </>
  );
}
