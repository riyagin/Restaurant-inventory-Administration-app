import { useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { parseFingerprintImport, confirmFingerprintImport } from '../../api';

export default function FingerprintImport() {
  const [preview, setPreview] = useState(null);
  const [file, setFile] = useState(null);
  const [uploading, setUploading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState(null);
  const fileRef = useRef();

  const handleFile = async (f) => {
    if (!f) return;
    setError(''); setDone(null); setPreview(null); setFile(f);
    setUploading(true);
    try {
      const form = new FormData();
      form.append('file', f);
      const r = await parseFingerprintImport(form);
      setPreview(r.data);
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal membaca file');
    } finally {
      setUploading(false);
    }
  };

  const handleConfirm = async () => {
    if (!file) return;
    setError(''); setSubmitting(true);
    try {
      const form = new FormData();
      form.append('file', file);
      const r = await confirmFingerprintImport(form);
      setDone(r.data);
      setPreview(null);
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan impor');
    } finally {
      setSubmitting(false);
    }
  };

  const reset = () => {
    setPreview(null); setFile(null); setError(''); setDone(null);
    if (fileRef.current) fileRef.current.value = '';
  };

  return (
    <>
      <div className="page-header">
        <h1>Impor Sidik Jari</h1>
        <Link to="/hr/attendance" className="btn btn-secondary">← Kembali ke Absensi</Link>
      </div>

      <div style={{ background: '#fff8e1', border: '1px solid #ffe0a3', borderRadius: '8px', padding: '0.85rem 1.25rem', marginBottom: '1.25rem', fontSize: '0.85rem', color: '#8a5a00' }}>
        <strong>Catatan:</strong> Data wajah (pengenalan wajah) adalah sumber utama dan <strong>tidak akan pernah ditimpa</strong> oleh impor sidik jari.
        Sidik jari hanya mengisi jam masuk/pulang yang masih kosong atau yang bersumber dari sidik jari.
      </div>

      {done && (
        <div style={{ background: '#e6f9f0', border: '1px solid #b2dfdb', borderRadius: '8px', padding: '1rem 1.5rem', marginBottom: '1.5rem', color: '#1b5e45', fontWeight: 500 }}>
          Impor berhasil! {done.applied} dari {done.total_punches} punch berhasil diterapkan ke kehadiran.
          <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.75rem' }}>
            <Link to="/hr/attendance" className="btn btn-primary btn-sm">Lihat Absensi</Link>
            <button onClick={reset} className="btn btn-secondary btn-sm">Impor Lagi</button>
          </div>
        </div>
      )}

      {!preview && !done && (
        <div className="card" style={{ marginBottom: '1.5rem' }}>
          <h2 style={{ fontSize: '1rem', marginBottom: '0.25rem' }}>Unggah File Ekspor Mesin Sidik Jari</h2>
          <p style={{ fontSize: '0.85rem', color: '#888', marginBottom: '1rem' }}>
            Format saat ini: CSV <code>employee_code,timestamp</code> (placeholder). Format mesin sebenarnya akan menyusul.
          </p>
          <div
            onClick={() => fileRef.current?.click()}
            onDrop={e => { e.preventDefault(); handleFile(e.dataTransfer.files[0]); }}
            onDragOver={e => e.preventDefault()}
            style={{ border: '2px dashed #c8d8f0', borderRadius: '8px', padding: '2.5rem', textAlign: 'center', cursor: 'pointer', background: '#f5f8ff' }}
          >
            <div style={{ fontSize: '2rem', marginBottom: '0.5rem' }}>☝</div>
            <div style={{ fontWeight: 600, color: '#2c3e7a' }}>Klik atau seret file ke sini</div>
            <div style={{ fontSize: '0.85rem', color: '#888', marginTop: '0.3rem' }}>Format: .csv</div>
            <input ref={fileRef} type="file" accept=".csv,.txt" style={{ display: 'none' }}
              onChange={e => handleFile(e.target.files[0])} />
          </div>
          {uploading && <p style={{ color: '#888', marginTop: '0.75rem', textAlign: 'center' }}>Memproses file…</p>}
          {error && <div className="error-msg" style={{ marginTop: '0.75rem' }}>{error}</div>}
        </div>
      )}

      {preview && (
        <>
          <div className="card" style={{ marginBottom: '1.25rem' }}>
            <div style={{ display: 'flex', gap: '1.5rem', flexWrap: 'wrap', alignItems: 'center' }}>
              <div><div style={{ fontSize: '0.75rem', color: '#999' }}>FILE</div><div style={{ fontWeight: 500 }}>{preview.filename}</div></div>
              <span style={{ color: '#555' }}>{preview.total_punches} punch</span>
              <span style={{ color: '#2e7d32', fontWeight: 700 }}>{preview.matched_count} cocok</span>
              <span style={{ color: preview.unmatched_count > 0 ? '#c62828' : '#aaa', fontWeight: 700 }}>{preview.unmatched_count} kode tidak cocok</span>
              <button onClick={reset} className="btn btn-secondary btn-sm">Ganti File</button>
            </div>
            {error && <div className="error-msg" style={{ marginTop: '0.75rem' }}>{error}</div>}
          </div>

          {preview.unmatched_count > 0 && (
            <div className="card" style={{ marginBottom: '1.25rem' }}>
              <h3 style={{ fontSize: '0.9rem', color: '#c62828' }}>Kode Tidak Cocok (dilewati, tidak fatal)</h3>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem', marginTop: '0.5rem' }}>
                {preview.unmatched_codes.map(c => (
                  <span key={c} style={{ background: '#fdecea', color: '#c62828', padding: '0.15rem 0.5rem', borderRadius: '4px', fontFamily: 'monospace', fontSize: '0.8rem' }}>{c}</span>
                ))}
              </div>
            </div>
          )}

          {preview.row_errors && preview.row_errors.length > 0 && (
            <div className="card" style={{ marginBottom: '1.25rem' }}>
              <h3 style={{ fontSize: '0.9rem', color: '#b45309' }}>Baris Bermasalah</h3>
              <ul style={{ fontSize: '0.82rem', color: '#b45309', marginTop: '0.5rem' }}>
                {preview.row_errors.map((m, i) => <li key={i}>{m}</li>)}
              </ul>
            </div>
          )}

          <div className="card" style={{ marginBottom: '1.5rem', overflowX: 'auto' }}>
            <table>
              <thead><tr><th>Kode</th><th>Nama</th><th>Waktu</th></tr></thead>
              <tbody>
                {preview.matched.slice(0, 500).map((m, i) => (
                  <tr key={i}>
                    <td style={{ fontFamily: 'monospace', fontSize: '0.82rem' }}>{m.employee_code}</td>
                    <td>{m.full_name}</td>
                    <td style={{ fontSize: '0.82rem' }}>{new Date(m.timestamp).toLocaleString('id-ID')}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <button onClick={handleConfirm} disabled={submitting || preview.matched_count === 0} className="btn btn-primary">
            {submitting ? 'Menyimpan…' : `Konfirmasi Impor (${preview.matched_count} punch cocok)`}
          </button>
        </>
      )}
    </>
  );
}
