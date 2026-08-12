import { useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { getHRSettings, updateHRSettings, uploadHRLogo } from '../../api';

const SERVER = 'http://localhost:5002';

const ROMAN = ['', 'I', 'II', 'III', 'IV', 'V', 'VI', 'VII', 'VIII', 'IX', 'X', 'XI', 'XII'];

// Mirror of service.FormatDocNumber, for the live preview only. The server
// remains the authority — it renders the number that actually goes on a letter.
function formatDocNumber(format, counter, date = new Date()) {
  const n = Number(counter) || 1;
  const m = date.getMonth() + 1;
  const y = date.getFullYear();
  return (format || '{nomor}/HRD/{bulan_romawi}/{tahun}')
    .replaceAll('{nomor}', String(n).padStart(3, '0'))
    .replaceAll('{nomor_polos}', String(n))
    .replaceAll('{jenis}', 'PKWT')
    .replaceAll('{bulan_romawi}', ROMAN[m])
    .replaceAll('{bulan}', String(m).padStart(2, '0'))
    .replaceAll('{tahun}', String(y))
    .replaceAll('{tahun_pendek}', String(y % 100).padStart(2, '0'));
}

const PLACEHOLDERS = [
  ['{nomor}', 'nomor urut, 3 digit (001)'],
  ['{nomor_polos}', 'nomor urut tanpa nol (1)'],
  ['{jenis}', 'kode jenis dokumen (PKWT, PKWTT, SP, PKL)'],
  ['{bulan_romawi}', 'bulan angka Romawi (VII)'],
  ['{bulan}', 'bulan 2 digit (07)'],
  ['{tahun}', 'tahun 4 digit (2026)'],
  ['{tahun_pendek}', 'tahun 2 digit (26)'],
];

const emptySettings = {
  company_name: '', address: '', payslip_footer: '', absence_grace_days: 4,
  company_phone: '', company_email: '', company_city: '',
  signatory_name: '', signatory_position: 'Direktur', signatory_national_id: '',
  doc_number_format: '{nomor}/HRD/{bulan_romawi}/{tahun}', doc_number_counter: 1,
  doc_working_hours: '', doc_payment_info: '', doc_probation_months: 3,
};

function Field({ label, hint, children, full }) {
  return (
    <div className="form-group" style={full ? { gridColumn: '1 / -1' } : undefined}>
      <label>{label}</label>
      {children}
      {hint && <div style={{ fontSize: 12, color: '#889', marginTop: 6 }}>{hint}</div>}
    </div>
  );
}

const grid = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '0 1rem' };

export default function HRSettings() {
  const [s, setS] = useState(emptySettings);
  const [logoPath, setLogoPath] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [msg, setMsg] = useState('');
  const [error, setError] = useState('');
  const fileRef = useRef(null);

  const set = (k) => (e) => setS((v) => ({ ...v, [k]: e.target.value }));

  const load = async () => {
    setLoading(true);
    try {
      const { data } = await getHRSettings();
      setS({
        ...emptySettings,
        ...Object.fromEntries(Object.keys(emptySettings).map((k) => [k, data[k] ?? emptySettings[k]])),
      });
      setLogoPath(data.logo_path?.String ?? data.logo_path ?? '');
    } catch {
      setError('Gagal memuat pengaturan');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const numberPreview = useMemo(
    () => formatDocNumber(s.doc_number_format, s.doc_number_counter),
    [s.doc_number_format, s.doc_number_counter],
  );

  const save = async () => {
    setSaving(true); setMsg(''); setError('');
    try {
      await updateHRSettings({
        ...s,
        absence_grace_days: Math.max(0, Number(s.absence_grace_days) || 0),
        doc_number_counter: Math.max(1, Number(s.doc_number_counter) || 1),
        doc_probation_months: Math.min(3, Math.max(0, Number(s.doc_probation_months) || 0)),
      });
      setMsg('Pengaturan tersimpan.');
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan pengaturan');
    } finally {
      setSaving(false);
    }
  };

  const onUpload = async (e) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploading(true); setMsg(''); setError('');
    try {
      const fd = new FormData();
      fd.append('logo', file);
      const { data } = await uploadHRLogo(fd);
      setLogoPath(data.logo_path?.String ?? data.logo_path ?? '');
      setMsg('Logo berhasil diunggah.');
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal mengunggah logo');
    } finally {
      setUploading(false);
      if (fileRef.current) fileRef.current.value = '';
    }
  };

  if (loading) return <div style={{ padding: 24 }}>Memuat…</div>;

  return (
    <div style={{ padding: 24, maxWidth: 820, margin: '0 auto' }}>
      <h1 style={{ margin: '0 0 4px', fontSize: 24 }}>Pengaturan HR</h1>
      <p style={{ color: '#667', marginTop: 0, marginBottom: 20 }}>
        Data perusahaan untuk slip gaji dan dokumen HR, penomoran surat, serta pintasan ke pengelolaan karyawan &amp; absensi.
      </p>

      {msg && <div style={{ background: '#e6f4ea', color: '#1e7e34', padding: 12, borderRadius: 8, marginBottom: 16 }}>{msg}</div>}
      {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 12, borderRadius: 8, marginBottom: 16 }}>{error}</div>}

      <div className="card" style={{ marginBottom: 20 }}>
        <h2 style={{ margin: '0 0 12px', fontSize: 16 }}>Pintasan</h2>

        <div style={{ fontSize: 13, fontWeight: 600, color: '#556', marginBottom: 8 }}>Karyawan</div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, marginBottom: 16 }}>
          <Link to="/hr/employees" className="btn btn-secondary">Karyawan</Link>
          <Link to="/hr/positions" className="btn btn-secondary">Jabatan</Link>
          <Link to="/hr/wage-components" className="btn btn-secondary">Komponen Gaji</Link>
          <Link to="/hr/import" className="btn btn-secondary">Impor Karyawan</Link>
          <Link to="/hr/documents" className="btn btn-secondary">Dokumen HR</Link>
          <Link to="/hr/kpi" className="btn btn-secondary">KPI &amp; Tugas Harian</Link>
        </div>

        <div style={{ fontSize: 13, fontWeight: 600, color: '#556', marginBottom: 8 }}>Kinerja &amp; Absensi</div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
          <Link to="/hr/performance/policies" className="btn btn-secondary">Kebijakan Kinerja</Link>
          <Link to="/hr/attendance/settings" className="btn btn-secondary">Pengaturan Absensi</Link>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 20 }}>
        <h2 style={{ margin: '0 0 4px', fontSize: 16 }}>Data Perusahaan</h2>
        <p style={{ color: '#889', fontSize: 13, margin: '0 0 16px' }}>
          Dipakai pada kop slip gaji dan seluruh dokumen HR (PKWT, PKWTT, Surat Peringatan, Paklaring).
        </p>

        <div style={grid}>
          <Field label="Nama Perusahaan" full><input value={s.company_name} onChange={set('company_name')} placeholder="PT Contoh Sejahtera" /></Field>
          <Field label="Alamat" full><textarea value={s.address} onChange={set('address')} rows={2} /></Field>
          <Field label="Telepon"><input value={s.company_phone} onChange={set('company_phone')} placeholder="022-1234567" /></Field>
          <Field label="Email"><input value={s.company_email} onChange={set('company_email')} placeholder="hrd@contoh.co.id" /></Field>
          <Field label="Kota Penandatanganan" hint="Kota yang tercetak di atas blok tanda tangan.">
            <input value={s.company_city} onChange={set('company_city')} placeholder="Bandung" />
          </Field>
        </div>

        <div className="form-group">
          <label>Logo Perusahaan</label>
          <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
            {logoPath ? (
              <img src={`${SERVER}/uploads/${logoPath}`} alt="Logo perusahaan" style={{ width: 90, height: 90, objectFit: 'contain', border: '1px solid #e8e8e8', borderRadius: 8, background: '#fafafa' }} />
            ) : (
              <div style={{ width: 90, height: 90, display: 'flex', alignItems: 'center', justifyContent: 'center', border: '1px dashed #ccd', borderRadius: 8, color: '#aab', fontSize: 12 }}>Tidak ada</div>
            )}
            <div>
              <input ref={fileRef} type="file" accept=".jpg,.jpeg,.png" onChange={onUpload} disabled={uploading} />
              <div style={{ fontSize: 12, color: '#889', marginTop: 4 }}>{uploading ? 'Mengunggah…' : 'Format: JPG, JPEG, PNG'}</div>
            </div>
          </div>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 20 }}>
        <h2 style={{ margin: '0 0 4px', fontSize: 16 }}>Penanda Tangan</h2>
        <p style={{ color: '#889', fontSize: 13, margin: '0 0 16px' }}>
          Pihak perusahaan yang menandatangani kontrak dan surat.
        </p>
        <div style={grid}>
          <Field label="Nama"><input value={s.signatory_name} onChange={set('signatory_name')} /></Field>
          <Field label="Jabatan"><input value={s.signatory_position} onChange={set('signatory_position')} placeholder="Direktur / Manajer HRD" /></Field>
          <Field label="NIK / KTP (opsional)"><input value={s.signatory_national_id} onChange={set('signatory_national_id')} /></Field>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 20 }}>
        <h2 style={{ margin: '0 0 4px', fontSize: 16 }}>Penomoran Surat</h2>
        <p style={{ color: '#889', fontSize: 13, margin: '0 0 16px' }}>
          Setiap dokumen yang dibuat mengambil nomor berikutnya lalu menaikkan hitungan secara otomatis.
        </p>
        <div style={grid}>
          <Field label="Format Nomor" full>
            <input value={s.doc_number_format} onChange={set('doc_number_format')} style={{ fontFamily: 'ui-monospace, monospace' }} />
          </Field>
          <Field label="Nomor Urut Berikutnya" hint="Ubah bila penomoran perlu diselaraskan dengan arsip lama, mis. saat pergantian tahun.">
            <input type="number" min={1} value={s.doc_number_counter} onChange={set('doc_number_counter')} />
          </Field>
          <Field label="Pratinjau">
            <input value={numberPreview} readOnly style={{ background: 'var(--surface-2)', fontFamily: 'ui-monospace, monospace' }} />
          </Field>
        </div>
        <div style={{ fontSize: 12, color: '#889', display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: '2px 16px', marginTop: 4 }}>
          {PLACEHOLDERS.map(([k, d]) => (
            <div key={k}><code style={{ color: 'var(--ink-2)' }}>{k}</code> — {d}</div>
          ))}
        </div>
      </div>

      <div className="card" style={{ marginBottom: 20 }}>
        <h2 style={{ margin: '0 0 4px', fontSize: 16 }}>Ketentuan Baku Dokumen</h2>
        <p style={{ color: '#889', fontSize: 13, margin: '0 0 16px' }}>
          Klausul yang sama untuk semua karyawan, dicetak pada kontrak kerja.
        </p>
        <div style={grid}>
          <Field label="Waktu Kerja" full>
            <input value={s.doc_working_hours} onChange={set('doc_working_hours')}
              placeholder="5 (lima) hari kerja per minggu, 8 (delapan) jam per hari" />
          </Field>
          <Field label="Cara Pembayaran Upah" full>
            <input value={s.doc_payment_info} onChange={set('doc_payment_info')}
              placeholder="transfer ke rekening bank karyawan setiap akhir bulan" />
          </Field>
          <Field label="Masa Percobaan PKWTT (bulan)" hint="Maksimal 3 bulan sesuai Pasal 60 UU Ketenagakerjaan.">
            <input type="number" min={0} max={3} value={s.doc_probation_months} onChange={set('doc_probation_months')} />
          </Field>
        </div>
      </div>

      <div className="card">
        <h2 style={{ margin: '0 0 16px', fontSize: 16 }}>Slip Gaji &amp; Absensi</h2>

        <div className="form-group">
          <label>Teks Footer Slip Gaji</label>
          <textarea value={s.payslip_footer} onChange={set('payslip_footer')} rows={2}
            placeholder="mis. Dokumen ini sah tanpa tanda tangan basah." />
        </div>

        <Field label="Toleransi Absen per Bulan (hari)"
          hint="Jumlah hari absen tanpa izin yang tidak mengurangi skor evaluasi tiap bulan. Absen melebihi angka ini baru menurunkan skor.">
          <input type="number" min={0} value={s.absence_grace_days} onChange={set('absence_grace_days')} style={{ maxWidth: 140 }} />
        </Field>
      </div>

      {/* One save for every section above — the page is long enough that a
          per-card button would be easy to miss. */}
      <div style={{ position: 'sticky', bottom: 0, background: 'var(--page-bg)', padding: '12px 0', marginTop: 4 }}>
        <button className="btn btn-primary" onClick={save} disabled={saving}>
          {saving ? 'Menyimpan…' : 'Simpan Pengaturan'}
        </button>
      </div>
    </div>
  );
}
