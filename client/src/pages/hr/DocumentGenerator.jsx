import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  generateHrDocument, getHRSettings, getAllEmployees, getEmployee,
  getPositions, getBranches, getDivisions,
  peekHrDocumentNumber, reserveHrDocumentNumber,
  getContractTemplates, createContractTemplate, deleteContractTemplate,
} from '../../api';
import SearchSelect from '../../components/SearchSelect';

// The four document types, with the label shown in the picker and a short note
// about the legal basis (surfaced as helper text so HR knows what each produces).
const DOC_TYPES = [
  { key: 'pkwt',  label: 'Kontrak Kontrak (PKWT)', sub: 'Perjanjian Kerja Waktu Tertentu — kontrak 1 tahun', icon: '📄',
    note: 'Mengacu UU 13/2003 jo. UU 6/2023 & PP 35/2021. Tanpa masa percobaan; memuat klausul uang kompensasi.' },
  { key: 'pkwtt', label: 'Kontrak Tetap (PKWTT)', sub: 'Perjanjian Kerja Waktu Tidak Tertentu — karyawan tetap', icon: '📑',
    note: 'Memuat isi wajib Pasal 54 UU Ketenagakerjaan; masa percobaan maksimal 3 bulan.' },
  { key: 'surat_peringatan', label: 'Surat Peringatan (SP)', sub: 'SP-1 / SP-2 / SP-3', icon: '⚠️',
    note: 'Mengacu Pasal 154A UU 6/2023 & PP 35/2021. Masa berlaku 6 bulan.' },
  { key: 'paklaring', label: 'Paklaring', sub: 'Surat Keterangan Pengalaman Kerja', icon: '🧾',
    note: 'Surat referensi kerja sesuai kewajiban pemberi kerja (Pasal 1602y KUHPerdata).' },
];

const today = () => new Date().toISOString().slice(0, 10);
const toDateInput = (d) => (d ? new Date(d).toISOString().slice(0, 10) : '');
// Term end = start + n months, minus a day: one day short of the full term is
// its last day. Templates store a month count rather than an end date because a
// preset is reused months apart from whatever start date the contract gets.
const plusMonths = (d, months) => {
  if (!d || !months) return '';
  const t = new Date(d);
  t.setMonth(t.getMonth() + months);
  t.setDate(t.getDate() - 1);
  return t.toISOString().slice(0, 10);
};
const plusOneYear = (d) => plusMonths(d, 12);

// Only the per-document fields live in the form. Everything about the company —
// letterhead, signatory, standard working hours and payment wording, and the
// running letter number — is configured once in Pengaturan HR and applied by the
// server, so it is neither asked for here nor sent.
const emptyForm = {
  type: 'pkwt',
  document_number: '', document_date: today(),
  employee_name: '', employee_gender: '', employee_birth_place: '', employee_birth_date: '',
  employee_national_id: '', employee_address: '', employee_phone: '',
  employee_position: '', employee_division: '',
  place_of_work: '', start_date: '', end_date: '',
  salary: '', salary_period: 'bulan',
  job_description: '',
  warning_level: '1', violation_date: '', violation_detail: '', previous_warning_ref: '',
  consequence: '', validity_months: 6, improvement_expected: '',
  reason_leaving: '', conduct_note: '',
};

// Trigger a browser download from an Axios blob response.
function triggerDownload(data, filename) {
  const url = window.URL.createObjectURL(new Blob([data]));
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.URL.revokeObjectURL(url);
}

function Field({ label, hint, children, full }) {
  return (
    <div className="form-group" style={full ? { gridColumn: '1 / -1' } : undefined}>
      <label>{label}</label>
      {children}
      {hint && <div style={{ fontSize: '0.78rem', color: 'var(--ink-3)', marginTop: 4 }}>{hint}</div>}
    </div>
  );
}

const grid = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '1rem' };

export default function DocumentGenerator() {
  const navigate = useNavigate();
  const [f, setF] = useState(emptyForm);
  const [settings, setSettings] = useState(null);
  const [employees, setEmployees] = useState([]);
  const [positions, setPositions] = useState([]);
  const [branches, setBranches] = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [selectedEmp, setSelectedEmp] = useState('');
  const [templates, setTemplates] = useState([]);
  const [selectedTpl, setSelectedTpl] = useState('');
  const [savingTpl, setSavingTpl] = useState(false);
  const [manualNumber, setManualNumber] = useState(false);
  // The counter only advances once per letter: the first successful download
  // claims the previewed number, further downloads of the same letter (e.g. PDF
  // after DOCX) reuse it.
  const [numberClaimed, setNumberClaimed] = useState(false);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');

  const set = (k) => (e) => setF((s) => ({ ...s, [k]: e.target.value }));

  const activeType = useMemo(() => DOC_TYPES.find((d) => d.key === f.type), [f.type]);

  useEffect(() => {
    getHRSettings().then((r) => setSettings(r.data)).catch(() => {});
    getAllEmployees({ status: 'active' })
      .then(setEmployees)
      .catch(() => {});
    getPositions().then((r) => setPositions(r.data || [])).catch(() => {});
    getBranches().then((r) => setBranches(r.data || [])).catch(() => {});
    getDivisions().then((r) => setDivisions(r.data || [])).catch(() => {});
  }, []);

  // Templates are offered per contract type; one marked for both shows on both.
  const loadTemplates = useCallback(() => {
    if (f.type !== 'pkwt' && f.type !== 'pkwtt') { setTemplates([]); return; }
    getContractTemplates({ type: f.type })
      .then((r) => setTemplates(r.data || []))
      .catch(() => {});
  }, [f.type]);

  useEffect(() => { loadTemplates(); setSelectedTpl(''); }, [loadTemplates]);

  // Preview the next letter number for the chosen type/date. Re-run whenever
  // either changes, since the format may embed the month or the type code.
  const refreshNumber = useCallback(() => {
    if (manualNumber) return;
    peekHrDocumentNumber({ type: f.type, date: f.document_date })
      .then((r) => setF((s) => ({ ...s, document_number: r.data.number })))
      .catch(() => {});
  }, [f.type, f.document_date, manualNumber]);

  useEffect(() => {
    if (numberClaimed) return; // keep the number this letter already claimed
    refreshNumber();
  }, [refreshNumber, numberClaimed]);

  const employeeOptions = useMemo(
    () => employees.map((e) => ({ value: e.id, label: e.full_name, sub: [e.employee_code, e.position_name].filter(Boolean).join(' · ') })),
    [employees],
  );
  const positionOptions = useMemo(() => positions.map((p) => ({ value: p.name, label: p.name })), [positions]);
  const branchOptions = useMemo(() => branches.map((b) => ({ value: b.name, label: b.name })), [branches]);
  const divisionOptions = useMemo(
    () => divisions.map((d) => ({ value: d.name, label: d.name, sub: d.branch_name })),
    [divisions],
  );

  // Prefill identity fields from an existing employee record. Fields stay
  // editable — the letter may need to differ from the master record.
  const prefillFromEmployee = async (id) => {
    setSelectedEmp(id);
    setNumberClaimed(false);
    if (!id) return;
    try {
      const r = await getEmployee(id);
      const e = r.data;
      setF((s) => ({
        ...s,
        employee_name: e.full_name || '',
        employee_national_id: e.national_id || '',
        employee_address: e.address || '',
        employee_phone: e.phone || '',
        employee_birth_date: toDateInput(e.dob),
        employee_position: e.position_name || '',
        employee_division: e.branch_name || '',
        place_of_work: s.place_of_work || e.branch_name || '',
        start_date: s.start_date || toDateInput(e.join_date),
      }));
    } catch {
      setError('Gagal memuat data karyawan');
    }
  };

  // Applying a template fills the terms and leaves every field editable — it is
  // a starting point for this contract, not a binding to the preset.
  const applyTemplate = (id) => {
    setSelectedTpl(id);
    const t = templates.find((x) => x.id === id);
    if (!t) return;
    setF((s) => ({
      ...s,
      employee_position: t.position_name || s.employee_position,
      employee_division: t.division_name || s.employee_division,
      place_of_work: t.place_of_work || s.place_of_work,
      salary: t.salary ? String(t.salary) : s.salary,
      salary_period: t.salary_period || s.salary_period,
      job_description: t.job_description || s.job_description,
      end_date: s.type === 'pkwt' && s.start_date && t.contract_months
        ? plusMonths(s.start_date, t.contract_months)
        : s.end_date,
    }));
  };

  const saveAsTemplate = async () => {
    const name = prompt('Nama template (mis. "Kasir — Cabang Bogor"):', f.employee_position || '');
    if (!name?.trim()) return;
    setSavingTpl(true);
    try {
      const months = f.start_date && f.end_date
        ? Math.max(0, Math.round((new Date(f.end_date) - new Date(f.start_date)) / 2629800000))
        : 12;
      const { data } = await createContractTemplate({
        name: name.trim(),
        doc_type: f.type,
        position_name: f.employee_position,
        division_name: f.employee_division,
        place_of_work: f.place_of_work,
        salary: parseInt(f.salary, 10) || 0,
        salary_period: f.salary_period,
        job_description: f.job_description,
        contract_months: f.type === 'pkwt' ? months : 0,
      });
      setTemplates((t) => [...t, data].sort((a, b) => a.name.localeCompare(b.name)));
      setSelectedTpl(data.id);
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan template.');
    } finally {
      setSavingTpl(false);
    }
  };

  const removeTemplate = async () => {
    const t = templates.find((x) => x.id === selectedTpl);
    if (!t || !confirm(`Hapus template "${t.name}"?`)) return;
    try {
      await deleteContractTemplate(t.id);
      setTemplates((list) => list.filter((x) => x.id !== t.id));
      setSelectedTpl('');
    } catch {
      setError('Gagal menghapus template.');
    }
  };

  const onStartDateChange = (e) => {
    const v = e.target.value;
    setF((s) => ({
      ...s,
      start_date: v,
      // For a 1-year PKWT, auto-fill the end date if the user hasn't set one.
      end_date: s.type === 'pkwt' && !s.end_date ? plusOneYear(v) : s.end_date,
    }));
  };

  const download = async (format) => {
    setError('');
    if (!f.employee_name.trim()) { setError('Nama karyawan wajib diisi.'); return; }
    setBusy(format);
    try {
      const payload = {
        ...f,
        salary: parseInt(f.salary, 10) || 0,
        validity_months: parseInt(f.validity_months, 10) || 6,
      };
      const r = await generateHrDocument(payload, format);
      const safe = (f.employee_name || 'dokumen').replace(/[^\w\s-]/g, '').trim().replace(/\s+/g, '-');
      triggerDownload(r.data, `${activeType.label.split(' ')[0]}-${safe}.${format}`);
      if (!manualNumber && !numberClaimed) {
        // Burn the previewed number so the next letter gets a fresh one.
        await reserveHrDocumentNumber({ type: f.type, date: f.document_date }).catch(() => {});
        setNumberClaimed(true);
      }
    } catch {
      setError('Gagal membuat dokumen. Periksa kembali isian Anda.');
    } finally {
      setBusy('');
    }
  };

  const startNewLetter = () => {
    setF({ ...emptyForm, type: f.type });
    setSelectedEmp('');
    setNumberClaimed(false);
    setError('');
  };

  const isContract = f.type === 'pkwt' || f.type === 'pkwtt';
  const companyReady = Boolean(settings?.company_name && settings?.signatory_name);

  return (
    <>
      <div className="page-header">
        <h1>Dokumen HR</h1>
        <button className="btn btn-secondary" onClick={() => navigate('/hr/employees')}>Kembali</button>
      </div>

      <p style={{ color: 'var(--ink-3)', marginTop: '-0.6rem', marginBottom: '1.2rem', maxWidth: 720 }}>
        Buat dokumen HR sesuai ketentuan ketenagakerjaan Indonesia, lalu unduh sebagai DOCX (dapat diedit) atau PDF (siap tanda tangan).
        Data perusahaan, penanda tangan, dan penomoran surat diambil dari Pengaturan HR.
      </p>

      {/* Document type picker */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(215px, 1fr))', gap: '0.8rem', marginBottom: '1.2rem' }}>
        {DOC_TYPES.map((d) => {
          const active = d.key === f.type;
          return (
            <button
              key={d.key}
              type="button"
              onClick={() => { setF((s) => ({ ...s, type: d.key })); setNumberClaimed(false); }}
              style={{
                textAlign: 'left', cursor: 'pointer', padding: '0.9rem 1rem', borderRadius: 10,
                border: `1.5px solid ${active ? 'var(--accent)' : '#d5d9e0'}`,
                background: active ? 'var(--accent-soft)' : 'var(--surface)',
                boxShadow: active ? '0 2px 10px rgba(0,0,0,0.06)' : 'none',
                transition: 'all 0.18s ease', display: 'flex', gap: '0.7rem', alignItems: 'flex-start',
              }}
            >
              <span style={{ fontSize: '1.5rem', lineHeight: 1 }}>{d.icon}</span>
              <span>
                <span style={{ display: 'block', fontWeight: 600, color: 'var(--ink)' }}>{d.label}</span>
                <span style={{ display: 'block', fontSize: '0.8rem', color: 'var(--ink-3)', marginTop: 2 }}>{d.sub}</span>
              </span>
            </button>
          );
        })}
      </div>

      {activeType?.note && (
        <div style={{ background: '#eef4ff', border: '1px solid #d3e0fb', color: '#28407a', padding: '0.7rem 0.9rem', borderRadius: 8, fontSize: '0.85rem', marginBottom: '1.2rem' }}>
          ⚖️ {activeType.note}
        </div>
      )}

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      {/* Company block: read-only summary of what HR settings will stamp on the
          letter, so it is visible without being re-entered. */}
      <div className="card" style={{ marginBottom: '1rem' }}>
        <div className="card-header">
          <h2>Kop Surat & Penanda Tangan</h2>
          <Link to="/hr/settings" className="btn btn-secondary btn-sm">Ubah di Pengaturan HR</Link>
        </div>
        {!companyReady && (
          <div style={{ background: 'var(--st-partial-soft, #fdf4e0)', border: '1px solid #e0c383', color: '#7a4a00', padding: '0.6rem 0.8rem', borderRadius: 8, fontSize: '0.85rem', marginBottom: '0.8rem' }}>
            ⚠ Data perusahaan atau penanda tangan belum lengkap. Lengkapi di Pengaturan HR agar kop surat dan blok tanda tangan tercetak benar.
          </div>
        )}
        <div style={{ ...grid, gap: '0.75rem 1rem' }}>
          <Summary label="Perusahaan" value={settings?.company_name} />
          <Summary label="Alamat" value={settings?.address} />
          <Summary label="Telepon" value={settings?.company_phone} />
          <Summary label="Email" value={settings?.company_email} />
          <Summary label="Kota Penandatanganan" value={settings?.company_city} />
          <Summary label="Penanda Tangan" value={settings?.signatory_name} sub={settings?.signatory_position} />
        </div>
      </div>

      <div className="card" style={{ marginBottom: '1rem' }}>
        <div className="card-header"><h2>Identitas Surat</h2></div>
        <div style={grid}>
          <Field
            label="Nomor Surat"
            hint={manualNumber
              ? 'Nomor diisi manual — penomoran otomatis tidak bertambah.'
              : `Otomatis dari Pengaturan HR${numberClaimed ? ' — nomor ini sudah dipakai' : ''}.`}
          >
            <input value={f.document_number} onChange={set('document_number')} readOnly={!manualNumber}
              style={!manualNumber ? { background: 'var(--surface-2)' } : undefined} />
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', marginTop: '0.45rem', fontWeight: 400, fontSize: '0.82rem' }}>
              <input type="checkbox" style={{ width: 'auto' }} checked={manualNumber}
                onChange={(e) => { setManualNumber(e.target.checked); if (!e.target.checked) setNumberClaimed(false); }} />
              Isi nomor manual
            </label>
          </Field>
          <Field label="Tanggal Surat">
            <input type="date" value={f.document_date} onChange={set('document_date')} />
          </Field>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '1rem' }}>
        <div className="card-header">
          <h2>Data Karyawan</h2>
        </div>
        <div style={grid}>
          <Field label="Pilih Karyawan" hint="Ketik untuk mencari; data identitas terisi otomatis." full>
            <SearchSelect
              options={employeeOptions}
              value={selectedEmp}
              onChange={prefillFromEmployee}
              placeholder="Cari nama atau kode karyawan…"
              emptyText="Karyawan tidak ditemukan"
            />
          </Field>
          <Field label="Nama Lengkap *"><input value={f.employee_name} onChange={set('employee_name')} /></Field>
          <Field label="Jenis Kelamin">
            <select value={f.employee_gender} onChange={set('employee_gender')}>
              <option value="">—</option>
              <option value="L">Laki-laki</option>
              <option value="P">Perempuan</option>
            </select>
          </Field>
          <Field label="Tempat Lahir"><input value={f.employee_birth_place} onChange={set('employee_birth_place')} /></Field>
          <Field label="Tanggal Lahir"><input type="date" value={f.employee_birth_date} onChange={set('employee_birth_date')} /></Field>
          <Field label="NIK / KTP"><input value={f.employee_national_id} onChange={set('employee_national_id')} /></Field>
          <Field label="No. Telepon"><input value={f.employee_phone} onChange={set('employee_phone')} /></Field>
          <Field label="Jabatan">
            <SearchSelect
              options={positionOptions}
              value={f.employee_position}
              onChange={(v) => setF((s) => ({ ...s, employee_position: v }))}
              placeholder="Cari jabatan…"
              emptyText="Jabatan tidak ditemukan"
            />
          </Field>
          <Field label="Divisi / Unit">
            <SearchSelect
              options={divisionOptions}
              value={f.employee_division}
              onChange={(v) => setF((s) => ({ ...s, employee_division: v }))}
              placeholder="Cari divisi…"
              emptyText="Divisi tidak ditemukan"
            />
          </Field>
          <Field label="Alamat" full><textarea value={f.employee_address} onChange={set('employee_address')} /></Field>
        </div>
      </div>

      {/* Type-specific terms */}
      <div className="card" style={{ marginBottom: '1rem' }}>
        <div className="card-header"><h2>Ketentuan Dokumen</h2></div>

        {isContract && (
          <div className="doc-tpl-bar">
            <div style={{ flex: 1, minWidth: 220 }}>
              <SearchSelect
                options={templates.map((t) => ({
                  value: t.id,
                  label: t.name,
                  sub: [t.position_name, t.place_of_work].filter(Boolean).join(' · '),
                }))}
                value={selectedTpl}
                onChange={applyTemplate}
                placeholder={templates.length ? 'Muat dari template…' : 'Belum ada template'}
                emptyText="Template tidak ditemukan"
              />
            </div>
            <button type="button" className="btn btn-secondary btn-sm" onClick={saveAsTemplate} disabled={savingTpl}>
              {savingTpl ? 'Menyimpan…' : '+ Simpan sebagai template'}
            </button>
            {selectedTpl && (
              <button type="button" className="btn btn-secondary btn-sm" onClick={removeTemplate}>Hapus template</button>
            )}
          </div>
        )}
        {isContract && (
          <p style={{ color: 'var(--ink-3)', fontSize: '0.82rem', margin: '0 0 1rem' }}>
            Template mengisi ketentuan untuk peran serupa. Semua isian tetap dapat diubah setelah dimuat.
          </p>
        )}

        <div style={grid}>
          {isContract && (
            <>
              <Field label="Tempat Pekerjaan">
                <SearchSelect
                  options={branchOptions}
                  value={f.place_of_work}
                  onChange={(v) => setF((s) => ({ ...s, place_of_work: v }))}
                  placeholder="Cari cabang…"
                  emptyText="Cabang tidak ditemukan"
                />
              </Field>
              <Field label="Tanggal Mulai Kerja"><input type="date" value={f.start_date} onChange={onStartDateChange} /></Field>
              {f.type === 'pkwt' && (
                <Field label="Tanggal Berakhir Kontrak"><input type="date" value={f.end_date} onChange={set('end_date')} /></Field>
              )}
              {f.type === 'pkwtt' && (
                <Field label="Masa Percobaan" hint={`${settings?.doc_probation_months ?? 3} bulan — diatur di Pengaturan HR.`}>
                  <input value={`${settings?.doc_probation_months ?? 3} bulan`} readOnly style={{ background: 'var(--surface-2)' }} />
                </Field>
              )}
              <Field label="Upah (Rp)"><input type="number" min="0" value={f.salary} onChange={set('salary')} placeholder="5000000" /></Field>
              <Field label="Periode Upah">
                <select value={f.salary_period} onChange={set('salary_period')}>
                  <option value="bulan">per bulan</option>
                  <option value="hari">per hari</option>
                </select>
              </Field>
              <Field label="Cara Pembayaran Upah" hint="Diatur di Pengaturan HR." full>
                <input value={settings?.doc_payment_info || ''} readOnly style={{ background: 'var(--surface-2)' }} />
              </Field>
              <Field label="Waktu Kerja" hint="Diatur di Pengaturan HR." full>
                <input value={settings?.doc_working_hours || ''} readOnly style={{ background: 'var(--surface-2)' }} />
              </Field>
              <Field label="Uraian Tugas / Pekerjaan" full><textarea value={f.job_description} onChange={set('job_description')} /></Field>
            </>
          )}

          {f.type === 'surat_peringatan' && (
            <>
              <Field label="Tingkat Peringatan">
                <select value={f.warning_level} onChange={set('warning_level')}>
                  <option value="1">SP-1 (Pertama)</option>
                  <option value="2">SP-2 (Kedua)</option>
                  <option value="3">SP-3 (Ketiga / Terakhir)</option>
                </select>
              </Field>
              <Field label="Tanggal Pelanggaran"><input type="date" value={f.violation_date} onChange={set('violation_date')} /></Field>
              <Field label="Masa Berlaku (bulan)"><input type="number" min="1" value={f.validity_months} onChange={set('validity_months')} /></Field>
              {(f.warning_level === '2' || f.warning_level === '3') && (
                <Field label="Referensi Peringatan Sebelumnya" full><input value={f.previous_warning_ref} onChange={set('previous_warning_ref')} placeholder="mis. SP-1 No. 001/HRD/…" /></Field>
              )}
              <Field label="Uraian Pelanggaran" full><textarea value={f.violation_detail} onChange={set('violation_detail')} /></Field>
              <Field label="Konsekuensi jika Berulang" full><textarea value={f.consequence} onChange={set('consequence')} placeholder="mis. peringatan lebih berat hingga pemutusan hubungan kerja" /></Field>
              <Field label="Perbaikan yang Diharapkan" full><textarea value={f.improvement_expected} onChange={set('improvement_expected')} /></Field>
            </>
          )}

          {f.type === 'paklaring' && (
            <>
              <Field label="Jabatan Terakhir">
                <SearchSelect
                  options={positionOptions}
                  value={f.employee_position}
                  onChange={(v) => setF((s) => ({ ...s, employee_position: v }))}
                  placeholder="Cari jabatan…"
                  emptyText="Jabatan tidak ditemukan"
                />
              </Field>
              <Field label="Tanggal Mulai Kerja"><input type="date" value={f.start_date} onChange={set('start_date')} /></Field>
              <Field label="Tanggal Berakhir Kerja"><input type="date" value={f.end_date} onChange={set('end_date')} /></Field>
              <Field label="Alasan Berhenti" full><input value={f.reason_leaving} onChange={set('reason_leaving')} placeholder="mis. mengundurkan diri / berakhirnya kontrak" /></Field>
              <Field label="Catatan Kinerja / Perilaku" full><textarea value={f.conduct_note} onChange={set('conduct_note')} placeholder="Dikosongkan untuk memakai kalimat baku." /></Field>
            </>
          )}
        </div>
      </div>

      <div style={{ display: 'flex', gap: '0.8rem', flexWrap: 'wrap', alignItems: 'center', position: 'sticky', bottom: 0, background: 'var(--page-bg)', padding: '0.8rem 0' }}>
        <button className="btn btn-primary" disabled={busy} onClick={() => download('docx')}>
          {busy === 'docx' ? 'Membuat…' : '⬇ Unduh DOCX'}
        </button>
        <button className="btn btn-secondary" disabled={busy} onClick={() => download('pdf')}>
          {busy === 'pdf' ? 'Membuat…' : '⬇ Unduh PDF'}
        </button>
        {numberClaimed && (
          <button className="btn btn-secondary" onClick={startNewLetter}>+ Surat Baru</button>
        )}
      </div>
    </>
  );
}

function Summary({ label, value, sub }) {
  return (
    <div>
      <div style={{ fontSize: '0.72rem', textTransform: 'uppercase', letterSpacing: '0.4px', color: 'var(--ink-3)' }}>{label}</div>
      <div style={{ color: value ? 'var(--ink)' : 'var(--ink-4)', fontWeight: value ? 500 : 400 }}>{value || '—'}</div>
      {sub && <div style={{ fontSize: '0.8rem', color: 'var(--ink-3)' }}>{sub}</div>}
    </div>
  );
}
