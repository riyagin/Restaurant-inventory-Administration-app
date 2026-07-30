import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  generateHrDocument, getHRSettings, getEmployees, getEmployee,
} from '../../api';

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
const plusOneYear = (d) => {
  if (!d) return '';
  const t = new Date(d);
  t.setFullYear(t.getFullYear() + 1);
  t.setDate(t.getDate() - 1); // one day short of a full year = last day of the term
  return t.toISOString().slice(0, 10);
};

const emptyForm = {
  type: 'pkwt',
  company_name: '', company_address: '', company_phone: '', company_email: '',
  city: '', document_number: '', document_date: today(),
  signatory_name: '', signatory_position: 'Direktur', signatory_national_id: '',
  employee_name: '', employee_gender: '', employee_birth_place: '', employee_birth_date: '',
  employee_national_id: '', employee_address: '', employee_phone: '',
  employee_position: '', employee_division: '',
  place_of_work: '', start_date: '', end_date: '',
  salary: '', salary_period: 'bulan', payment_info: 'transfer ke rekening bank karyawan setiap akhir bulan',
  job_description: '', working_hours: '5 (lima) hari kerja per minggu, 8 (delapan) jam per hari',
  probation_months: 3,
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

function Field({ label, children, full }) {
  return (
    <div className="form-group" style={full ? { gridColumn: '1 / -1' } : undefined}>
      <label>{label}</label>
      {children}
    </div>
  );
}

export default function DocumentGenerator() {
  const navigate = useNavigate();
  const [f, setF] = useState(emptyForm);
  const [employees, setEmployees] = useState([]);
  const [selectedEmp, setSelectedEmp] = useState('');
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');

  const set = (k) => (e) => setF((s) => ({ ...s, [k]: e.target.value }));

  const activeType = useMemo(() => DOC_TYPES.find((d) => d.key === f.type), [f.type]);

  useEffect(() => {
    getHRSettings()
      .then((r) => setF((s) => ({
        ...s,
        company_name: s.company_name || r.data?.company_name || '',
        company_address: s.company_address || r.data?.address || '',
      })))
      .catch(() => {});
    getEmployees({ status: 'active' })
      .then((r) => setEmployees(Array.isArray(r.data) ? r.data : (r.data?.items || [])))
      .catch(() => {});
  }, []);

  // Prefill identity fields from an existing employee record (convenience only —
  // every field stays editable).
  const prefillFromEmployee = async (id) => {
    setSelectedEmp(id);
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
        start_date: s.start_date || toDateInput(e.join_date),
      }));
    } catch {
      setError('Gagal memuat data karyawan');
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
      const payload = { ...f, salary: parseInt(f.salary, 10) || 0, probation_months: parseInt(f.probation_months, 10) || 0, validity_months: parseInt(f.validity_months, 10) || 6 };
      const r = await generateHrDocument(payload, format);
      const safe = (f.employee_name || 'dokumen').replace(/[^\w\s-]/g, '').trim().replace(/\s+/g, '-');
      triggerDownload(r.data, `${activeType.label.split(' ')[0]}-${safe}.${format}`);
    } catch (err) {
      setError('Gagal membuat dokumen. Periksa kembali isian Anda.');
    } finally {
      setBusy('');
    }
  };

  const isContract = f.type === 'pkwt' || f.type === 'pkwtt';

  return (
    <>
      <div className="page-header">
        <h1>Dokumen HR</h1>
        <button className="btn btn-secondary" onClick={() => navigate('/hr/employees')}>Kembali</button>
      </div>

      <p style={{ color: 'var(--ink-3)', marginTop: '-0.6rem', marginBottom: '1.2rem', maxWidth: 720 }}>
        Buat dokumen HR sesuai ketentuan ketenagakerjaan Indonesia, lalu unduh sebagai DOCX (dapat diedit) atau PDF (siap tanda tangan).
      </p>

      {/* Document type picker */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(215px, 1fr))', gap: '0.8rem', marginBottom: '1.2rem' }}>
        {DOC_TYPES.map((d) => {
          const active = d.key === f.type;
          return (
            <button
              key={d.key}
              type="button"
              onClick={() => setF((s) => ({ ...s, type: d.key }))}
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

      <div className="card" style={{ marginBottom: '1rem' }}>
        <div className="card-header"><h2>Kop Surat & Perusahaan</h2></div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '1rem' }}>
          <Field label="Nama Perusahaan"><input value={f.company_name} onChange={set('company_name')} placeholder="PT Contoh Sejahtera" /></Field>
          <Field label="Alamat Perusahaan" full><input value={f.company_address} onChange={set('company_address')} /></Field>
          <Field label="Telepon"><input value={f.company_phone} onChange={set('company_phone')} /></Field>
          <Field label="Email"><input value={f.company_email} onChange={set('company_email')} /></Field>
          <Field label="Kota (penandatanganan)"><input value={f.city} onChange={set('city')} placeholder="Bandung" /></Field>
          <Field label="Nomor Surat"><input value={f.document_number} onChange={set('document_number')} placeholder="001/HRD/VII/2026" /></Field>
          <Field label="Tanggal Surat"><input type="date" value={f.document_date} onChange={set('document_date')} /></Field>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '1rem' }}>
        <div className="card-header"><h2>Penanda Tangan (Perusahaan)</h2></div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '1rem' }}>
          <Field label="Nama"><input value={f.signatory_name} onChange={set('signatory_name')} /></Field>
          <Field label="Jabatan"><input value={f.signatory_position} onChange={set('signatory_position')} placeholder="Direktur / Manajer HRD" /></Field>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '1rem' }}>
        <div className="card-header">
          <h2>Data Karyawan</h2>
          <select className="select-clean" style={{ maxWidth: 260 }} value={selectedEmp} onChange={(e) => prefillFromEmployee(e.target.value)}>
            <option value="">Prapopulasi dari karyawan…</option>
            {employees.map((e) => <option key={e.id} value={e.id}>{e.full_name} ({e.employee_code})</option>)}
          </select>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '1rem' }}>
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
          <Field label="Jabatan"><input value={f.employee_position} onChange={set('employee_position')} /></Field>
          <Field label="Divisi / Unit"><input value={f.employee_division} onChange={set('employee_division')} /></Field>
          <Field label="Alamat" full><textarea value={f.employee_address} onChange={set('employee_address')} /></Field>
        </div>
      </div>

      {/* Type-specific terms */}
      <div className="card" style={{ marginBottom: '1rem' }}>
        <div className="card-header"><h2>Ketentuan Dokumen</h2></div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '1rem' }}>
          {isContract && (
            <>
              <Field label="Tempat Pekerjaan"><input value={f.place_of_work} onChange={set('place_of_work')} placeholder="Kantor Pusat / Cabang…" /></Field>
              <Field label="Tanggal Mulai Kerja"><input type="date" value={f.start_date} onChange={onStartDateChange} /></Field>
              {f.type === 'pkwt' && (
                <Field label="Tanggal Berakhir Kontrak"><input type="date" value={f.end_date} onChange={set('end_date')} /></Field>
              )}
              {f.type === 'pkwtt' && (
                <Field label="Masa Percobaan (bulan, maks 3)">
                  <input type="number" min="0" max="3" value={f.probation_months} onChange={set('probation_months')} />
                </Field>
              )}
              <Field label="Upah (Rp)"><input type="number" min="0" value={f.salary} onChange={set('salary')} placeholder="5000000" /></Field>
              <Field label="Periode Upah">
                <select value={f.salary_period} onChange={set('salary_period')}>
                  <option value="bulan">per bulan</option>
                  <option value="hari">per hari</option>
                </select>
              </Field>
              <Field label="Cara Pembayaran Upah" full><input value={f.payment_info} onChange={set('payment_info')} /></Field>
              <Field label="Waktu Kerja" full><input value={f.working_hours} onChange={set('working_hours')} /></Field>
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
              <Field label="Jabatan Terakhir"><input value={f.employee_position} onChange={set('employee_position')} /></Field>
              <Field label="Tanggal Mulai Kerja"><input type="date" value={f.start_date} onChange={set('start_date')} /></Field>
              <Field label="Tanggal Berakhir Kerja"><input type="date" value={f.end_date} onChange={set('end_date')} /></Field>
              <Field label="Alasan Berhenti" full><input value={f.reason_leaving} onChange={set('reason_leaving')} placeholder="mis. mengundurkan diri / berakhirnya kontrak" /></Field>
              <Field label="Catatan Kinerja / Perilaku" full><textarea value={f.conduct_note} onChange={set('conduct_note')} placeholder="Dikosongkan untuk memakai kalimat baku." /></Field>
            </>
          )}
        </div>
      </div>

      <div style={{ display: 'flex', gap: '0.8rem', flexWrap: 'wrap', position: 'sticky', bottom: 0, background: 'var(--page-bg)', padding: '0.8rem 0' }}>
        <button className="btn btn-primary" disabled={busy} onClick={() => download('docx')}>
          {busy === 'docx' ? 'Membuat…' : '⬇ Unduh DOCX'}
        </button>
        <button className="btn btn-secondary" disabled={busy} onClick={() => download('pdf')}>
          {busy === 'pdf' ? 'Membuat…' : '⬇ Unduh PDF'}
        </button>
      </div>
    </>
  );
}
