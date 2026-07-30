import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  createEmployee, updateEmployee, getBranches, getPositions, createPosition,
  getEmployeeDocuments, uploadEmployeeDocument, deleteEmployeeDocument,
} from '../../api';

const today = () => new Date().toISOString().slice(0, 10);

const STEPS = [
  { key: 'pribadi',    title: 'Data Pribadi',    icon: '👤', desc: 'Identitas & kontak' },
  { key: 'kepegawaian', title: 'Kepegawaian',    icon: '💼', desc: 'Jabatan & status' },
  { key: 'bank',       title: 'Rekening Bank',   icon: '🏦', desc: 'Info pembayaran' },
  { key: 'dokumen',    title: 'Dokumen',         icon: '📎', desc: 'Unggah berkas' },
  { key: 'selesai',    title: 'Selesai',         icon: '✅', desc: 'Ringkasan' },
];

const DOC_TYPES = [
  ['ktp', 'KTP'], ['kk', 'Kartu Keluarga'], ['ijazah', 'Ijazah'],
  ['npwp', 'NPWP'], ['bpjs_kesehatan', 'BPJS Kesehatan'],
  ['bpjs_ketenagakerjaan', 'BPJS Ketenagakerjaan'],
  ['pkwt', 'Kontrak PKWT (ditandatangani)'], ['pkwtt', 'Kontrak PKWTT (ditandatangani)'],
  ['foto', 'Pas Foto'], ['surat_lamaran', 'Surat Lamaran / CV'], ['other', 'Lainnya'],
];

const emptyEmp = {
  employee_code: '', full_name: '', national_id: '', dob: '',
  phone: '', email: '', address: '',
  position_id: '', branch_id: '', join_date: today(),
  employment_type: 'permanent', contract_end_date: '', status: 'active',
  bank_name: '', bank_account_number: '', bank_account_holder: '',
};

function Field({ label, children, full }) {
  return (
    <div className="form-group" style={full ? { gridColumn: '1 / -1' } : undefined}>
      <label>{label}</label>
      {children}
    </div>
  );
}

const grid = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '1rem' };

export default function OnboardingWizard() {
  const navigate = useNavigate();
  const [step, setStep] = useState(0);
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

  const set = (k) => (e) => setEmp((s) => ({ ...s, [k]: e.target.value }));

  useEffect(() => {
    getBranches().then((r) => setBranches(r.data)).catch(() => {});
    getPositions().then((r) => setPositions(r.data)).catch(() => {});
  }, []);

  const addPosition = async () => {
    const name = newPos.trim();
    if (!name) return;
    try {
      const r = await createPosition({ name });
      setPositions((p) => [...p, r.data].sort((a, b) => a.name.localeCompare(b.name)));
      setEmp((s) => ({ ...s, position_id: r.data.id }));
      setNewPos('');
      setShowAddPos(false);
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menambah jabatan');
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
  // leaving the Bank step, so the Documents step has an employee id to attach to.
  const saveEmployee = async () => {
    setSaving(true);
    setError('');
    try {
      if (empId) {
        await updateEmployee(empId, emp);
      } else {
        const r = await createEmployee(emp);
        setEmpId(r.data.id);
      }
      return true;
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan data karyawan.');
      return false;
    } finally {
      setSaving(false);
    }
  };

  const next = async () => {
    const v = validateStep();
    if (v) { setError(v); return; }
    setError('');
    if (step === 2) {
      const ok = await saveEmployee();
      if (!ok) return;
      if (empId) {
        getEmployeeDocuments(empId).then((r) => setDocs(r.data)).catch(() => {});
      }
    }
    setStep((s) => Math.min(s + 1, STEPS.length - 1));
  };

  const back = () => { setError(''); setStep((s) => Math.max(s - 1, 0)); };

  const uploadDoc = async () => {
    if (!docForm.file) { setError('Pilih file terlebih dahulu.'); return; }
    if (!empId) { setError('Karyawan belum tersimpan.'); return; }
    setUploading(true);
    setError('');
    try {
      const fd = new FormData();
      fd.append('file', docForm.file);
      fd.append('doc_type', docForm.doc_type);
      fd.append('title', docForm.title || DOC_TYPES.find((d) => d[0] === docForm.doc_type)?.[1] || docForm.file.name);
      fd.append('notes', docForm.notes);
      const r = await uploadEmployeeDocument(empId, fd);
      setDocs((d) => [r.data, ...d]);
      setDocForm({ doc_type: 'ktp', title: '', notes: '', file: null });
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal mengunggah dokumen.');
    } finally {
      setUploading(false);
    }
  };

  const removeDoc = async (docId) => {
    if (!confirm('Hapus dokumen ini?')) return;
    try {
      await deleteEmployeeDocument(empId, docId);
      setDocs((d) => d.filter((x) => x.id !== docId));
    } catch {
      alert('Gagal menghapus dokumen');
    }
  };

  const posName = positions.find((p) => p.id === emp.position_id)?.name || '—';
  const brName = branches.find((b) => b.id === emp.branch_id)?.name || '—';

  return (
    <>
      <div className="page-header">
        <h1>Onboarding Karyawan Baru</h1>
        <button className="btn btn-secondary" onClick={() => navigate('/hr/employees')}>Keluar</button>
      </div>

      {/* Stepper */}
      <div className="onb-stepper">
        {STEPS.map((s, i) => {
          const state = i < step ? 'done' : i === step ? 'active' : 'todo';
          return (
            <div key={s.key} className={`onb-step-item ${state}`}>
              <div className="onb-dot">{i < step ? '✓' : s.icon}</div>
              <div className="onb-step-label">
                <span className="onb-step-title">{s.title}</span>
                <span className="onb-step-desc">{s.desc}</span>
              </div>
              {i < STEPS.length - 1 && <div className={`onb-line ${i < step ? 'filled' : ''}`} />}
            </div>
          );
        })}
      </div>

      {error && <div className="error-msg" style={{ margin: '1rem 0' }}>{error}</div>}

      {/* Animated step panel; key remounts to replay the slide-in animation. */}
      <div key={step} className="onb-panel card">
        <div className="card-header"><h2>{STEPS[step].icon} {STEPS[step].title}</h2></div>

        {step === 0 && (
          <div style={grid}>
            <Field label="Kode Karyawan"><input value={emp.employee_code} onChange={set('employee_code')} placeholder="Otomatis (mis. EMP-0001)" /></Field>
            <Field label="Nama Lengkap *"><input value={emp.full_name} onChange={set('full_name')} /></Field>
            <Field label="NIK / KTP"><input value={emp.national_id} onChange={set('national_id')} /></Field>
            <Field label="Tanggal Lahir"><input type="date" value={emp.dob} onChange={set('dob')} /></Field>
            <Field label="Telepon"><input value={emp.phone} onChange={set('phone')} /></Field>
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
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => setShowAddPos((v) => !v)}>+ Baru</button>
              </div>
              {showAddPos && (
                <div style={{ display: 'flex', gap: '0.4rem', marginTop: '0.5rem' }}>
                  <input value={newPos} onChange={(e) => setNewPos(e.target.value)} placeholder="Nama jabatan baru" style={{ flex: 1 }} />
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
          <div style={grid}>
            <Field label="Nama Bank"><input value={emp.bank_name} onChange={set('bank_name')} placeholder="mis. BCA / Mandiri" /></Field>
            <Field label="Nomor Rekening"><input value={emp.bank_account_number} onChange={set('bank_account_number')} /></Field>
            <Field label="Nama Pemilik Rekening"><input value={emp.bank_account_holder} onChange={set('bank_account_holder')} /></Field>
            <p style={{ gridColumn: '1 / -1', color: 'var(--ink-3)', fontSize: '0.85rem', margin: 0 }}>
              Setelah langkah ini, data karyawan akan disimpan sehingga Anda dapat mengunggah dokumen yang telah ditandatangani.
            </p>
          </div>
        )}

        {step === 3 && (
          <div>
            <div style={{ ...grid, alignItems: 'end' }}>
              <Field label="Jenis Dokumen">
                <select value={docForm.doc_type} onChange={(e) => setDocForm((s) => ({ ...s, doc_type: e.target.value }))}>
                  {DOC_TYPES.map(([k, l]) => <option key={k} value={k}>{l}</option>)}
                </select>
              </Field>
              <Field label="Judul (opsional)"><input value={docForm.title} onChange={(e) => setDocForm((s) => ({ ...s, title: e.target.value }))} /></Field>
              <Field label="Berkas (pdf, jpg, png, docx)">
                <input type="file" accept=".pdf,.jpg,.jpeg,.png,.doc,.docx" onChange={(e) => setDocForm((s) => ({ ...s, file: e.target.files?.[0] || null }))} />
              </Field>
              <Field label="Catatan (opsional)"><input value={docForm.notes} onChange={(e) => setDocForm((s) => ({ ...s, notes: e.target.value }))} /></Field>
              <div className="form-group">
                <button className="btn btn-primary" disabled={uploading} onClick={uploadDoc}>{uploading ? 'Mengunggah…' : '⬆ Unggah'}</button>
              </div>
            </div>

            <div style={{ marginTop: '1rem' }}>
              <h3 style={{ fontSize: '0.95rem', marginBottom: '0.6rem' }}>Dokumen Terunggah ({docs.length})</h3>
              {docs.length === 0 && <p style={{ color: 'var(--ink-3)', fontSize: '0.9rem' }}>Belum ada dokumen. Unggah KTP, ijazah, kontrak yang ditandatangani, dan berkas lainnya.</p>}
              {docs.map((d) => (
                <div key={d.id} className="onb-doc-row">
                  <span style={{ fontSize: '1.2rem' }}>📄</span>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontWeight: 600 }}>{d.title}</div>
                    <div style={{ fontSize: '0.8rem', color: 'var(--ink-3)' }}>{d.doc_type} · {d.original_name}</div>
                  </div>
                  <button className="btn btn-secondary btn-sm" onClick={() => removeDoc(d.id)}>Hapus</button>
                </div>
              ))}
            </div>
          </div>
        )}

        {step === 4 && (
          <div className="onb-summary">
            <div style={{ textAlign: 'center', padding: '0.6rem 0 1rem' }}>
              <div className="onb-check-badge">✓</div>
              <h2 style={{ margin: '0.6rem 0 0.2rem' }}>{emp.full_name} berhasil di-onboarding!</h2>
              <p style={{ color: 'var(--ink-3)', margin: 0 }}>Data karyawan dan {docs.length} dokumen telah tersimpan.</p>
            </div>
            <div style={grid}>
              <div><strong>Jabatan</strong><div>{posName}</div></div>
              <div><strong>Cabang</strong><div>{brName}</div></div>
              <div><strong>Tipe</strong><div>{emp.employment_type === 'contract' ? 'Kontrak (PKWT)' : 'Tetap (PKWTT)'}</div></div>
              <div><strong>Tgl. Bergabung</strong><div>{emp.join_date}</div></div>
              <div><strong>Dokumen</strong><div>{docs.length} berkas</div></div>
            </div>
          </div>
        )}
      </div>

      {/* Nav bar */}
      <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: '1.2rem', gap: '0.8rem' }}>
        <button className="btn btn-secondary" onClick={back} disabled={step === 0 || saving}>← Kembali</button>
        {step < 3 && (
          <button className="btn btn-primary" onClick={next} disabled={saving}>
            {saving ? 'Menyimpan…' : (step === 2 ? 'Simpan & Lanjut →' : 'Lanjut →')}
          </button>
        )}
        {step === 3 && (
          <button className="btn btn-primary" onClick={() => setStep(4)}>Selesai →</button>
        )}
        {step === 4 && (
          <div style={{ display: 'flex', gap: '0.6rem' }}>
            <button className="btn btn-secondary" onClick={() => navigate('/hr/employees')}>Ke Daftar Karyawan</button>
            <button className="btn btn-primary" onClick={() => navigate(`/hr/employees/${empId}`)}>Lihat Karyawan</button>
          </div>
        )}
      </div>
    </>
  );
}
