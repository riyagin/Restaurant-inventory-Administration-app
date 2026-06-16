import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { getHRSettings, updateHRSettings, uploadHRLogo } from '../../api';

const SERVER = 'http://localhost:5000';

export default function HRSettings() {
  const [companyName, setCompanyName] = useState('');
  const [address, setAddress] = useState('');
  const [footer, setFooter] = useState('');
  const [logoPath, setLogoPath] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [msg, setMsg] = useState('');
  const [error, setError] = useState('');
  const fileRef = useRef(null);

  const load = async () => {
    setLoading(true);
    try {
      const { data } = await getHRSettings();
      setCompanyName(data.company_name || '');
      setAddress(data.address || '');
      setFooter(data.payslip_footer || '');
      setLogoPath(data.logo_path?.String ?? data.logo_path ?? '');
    } catch {
      setError('Gagal memuat pengaturan');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const save = async () => {
    setSaving(true); setMsg(''); setError('');
    try {
      await updateHRSettings({ company_name: companyName, address, payslip_footer: footer });
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
    <div style={{ padding: 24, maxWidth: 720, margin: '0 auto' }}>
      <div style={{ marginBottom: 8 }}><Link to="/hr/payroll" style={{ color: '#1967d2' }}>← Penggajian</Link></div>
      <h1 style={{ margin: '0 0 4px', fontSize: 24 }}>Pengaturan HR</h1>
      <p style={{ color: '#667', marginTop: 0 }}>Informasi perusahaan yang tampil pada kepala slip gaji (Slip Gaji).</p>

      {msg && <div style={{ background: '#e6f4ea', color: '#1e7e34', padding: 12, borderRadius: 8, marginBottom: 12 }}>{msg}</div>}
      {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 12, borderRadius: 8, marginBottom: 12 }}>{error}</div>}

      <div style={{ background: '#fff', borderRadius: 10, boxShadow: '0 1px 3px rgba(0,0,0,.08)', padding: 20 }}>
        <label style={{ display: 'block', fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Nama Perusahaan</label>
        <input value={companyName} onChange={(e) => setCompanyName(e.target.value)}
          style={{ width: '100%', padding: 9, borderRadius: 6, border: '1px solid #ccd', marginBottom: 14, boxSizing: 'border-box' }} />

        <label style={{ display: 'block', fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Alamat</label>
        <textarea value={address} onChange={(e) => setAddress(e.target.value)} rows={2}
          style={{ width: '100%', padding: 9, borderRadius: 6, border: '1px solid #ccd', marginBottom: 14, boxSizing: 'border-box' }} />

        <label style={{ display: 'block', fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Teks Footer Slip Gaji</label>
        <textarea value={footer} onChange={(e) => setFooter(e.target.value)} rows={2}
          placeholder="mis. Dokumen ini sah tanpa tanda tangan basah."
          style={{ width: '100%', padding: 9, borderRadius: 6, border: '1px solid #ccd', marginBottom: 14, boxSizing: 'border-box' }} />

        <label style={{ display: 'block', fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Logo Perusahaan</label>
        <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 14 }}>
          {logoPath ? (
            <img src={`${SERVER}/uploads/${logoPath}`} alt="Logo" style={{ width: 90, height: 90, objectFit: 'contain', border: '1px solid #e8e8e8', borderRadius: 8, background: '#fafafa' }} />
          ) : (
            <div style={{ width: 90, height: 90, display: 'flex', alignItems: 'center', justifyContent: 'center', border: '1px dashed #ccd', borderRadius: 8, color: '#aab', fontSize: 12 }}>Tidak ada</div>
          )}
          <div>
            <input ref={fileRef} type="file" accept=".jpg,.jpeg,.png" onChange={onUpload} disabled={uploading} />
            <div style={{ fontSize: 12, color: '#889', marginTop: 4 }}>{uploading ? 'Mengunggah…' : 'Format: JPG, JPEG, PNG'}</div>
          </div>
        </div>

        <button onClick={save} disabled={saving}
          style={{ background: '#1e7e34', color: '#fff', border: 0, borderRadius: 8, padding: '10px 18px', fontWeight: 600, cursor: 'pointer' }}>
          {saving ? 'Menyimpan…' : 'Simpan Pengaturan'}
        </button>
      </div>
    </div>
  );
}
