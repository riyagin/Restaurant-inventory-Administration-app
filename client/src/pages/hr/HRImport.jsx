import { useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { downloadHrImportTemplate, exportHrEmployees, parseHrImport, confirmHrImport } from '../../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v || 0);

const statusBadge = (status) => {
  const map = {
    ok:      { icon: '✓', label: 'Valid',     bg: '#e8f5e9', color: '#2e7d32' },
    warning: { icon: '!', label: 'Peringatan', bg: '#fff8e1', color: '#b45309' },
    error:   { icon: '✕', label: 'Error',      bg: '#fdecea', color: '#c62828' },
  };
  const s = map[status] || map.ok;
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', background: s.bg, color: s.color, padding: '0.15rem 0.5rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.78rem' }}>
      <span>{s.icon}</span> {s.label}
    </span>
  );
};

const actionBadge = (action) => {
  const isUpdate = action === 'update';
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', padding: '0.15rem 0.5rem', borderRadius: '4px',
      fontWeight: 600, fontSize: '0.78rem',
      background: isUpdate ? '#e3f2fd' : '#f1f8e9',
      color: isUpdate ? '#1565c0' : '#33691e',
    }}>
      {isUpdate ? 'Perbarui' : 'Baru'}
    </span>
  );
};

export default function HRImport() {
  const [preview, setPreview]   = useState(null);
  const [batchId, setBatchId]   = useState('');
  const [filename, setFilename] = useState('');
  const [uploading, setUploading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone]   = useState(null); // { employees_created, employees_updated }
  const fileRef = useRef();

  const downloadBlob = (data, filename) => {
    const url = window.URL.createObjectURL(new Blob([data]));
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    window.URL.revokeObjectURL(url);
  };

  const handleDownloadTemplate = async () => {
    setError('');
    setDownloading(true);
    try {
      const r = await downloadHrImportTemplate();
      downloadBlob(r.data, 'template-impor-karyawan.xlsx');
    } catch {
      setError('Gagal mengunduh template');
    } finally {
      setDownloading(false);
    }
  };

  const handleExport = async () => {
    setError('');
    setExporting(true);
    try {
      const r = await exportHrEmployees();
      downloadBlob(r.data, 'data-karyawan.xlsx');
    } catch {
      setError('Gagal mengekspor data karyawan');
    } finally {
      setExporting(false);
    }
  };

  const handleFile = async (file) => {
    if (!file) return;
    setError('');
    setDone(null);
    setPreview(null);
    setUploading(true);
    try {
      const form = new FormData();
      form.append('file', file);
      const r = await parseHrImport(form);
      setPreview(r.data.preview);
      setBatchId(r.data.batch_id);
      setFilename(file.name);
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal membaca file');
    } finally {
      setUploading(false);
    }
  };

  const handleDrop = (e) => {
    e.preventDefault();
    const file = e.dataTransfer.files[0];
    if (file) handleFile(file);
  };

  const handleConfirm = async () => {
    setError('');
    setSubmitting(true);
    try {
      const r = await confirmHrImport({ batch_id: batchId });
      setDone(r.data);
      setPreview(null);
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan impor');
    } finally {
      setSubmitting(false);
    }
  };

  const reset = () => {
    setPreview(null); setBatchId(''); setFilename('');
    setError(''); setDone(null);
    if (fileRef.current) fileRef.current.value = '';
  };

  const hasErrors = preview && preview.error_count > 0;

  return (
    <>
      <div className="page-header">
        <h1>Impor Karyawan</h1>
        <Link to="/hr/employees" className="btn btn-secondary">← Kembali ke Karyawan</Link>
      </div>

      {/* Success state */}
      {done && (
        <div style={{ background: '#e6f9f0', border: '1px solid #b2dfdb', borderRadius: '8px', padding: '1rem 1.5rem', marginBottom: '1.5rem', color: '#1b5e45', fontWeight: 500 }}>
          Impor berhasil! {done.employees_created} karyawan baru dibuat, {done.employees_updated || 0} karyawan diperbarui.
          <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.75rem' }}>
            <Link to="/hr/employees" className="btn btn-primary btn-sm">Lihat Daftar Karyawan</Link>
            <button onClick={reset} className="btn btn-secondary btn-sm">Impor Lagi</button>
          </div>
        </div>
      )}

      {/* Template + upload */}
      {!preview && !done && (
        <div className="card" style={{ marginBottom: '1.5rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '1rem', marginBottom: '1rem' }}>
            <div>
              <h2 style={{ fontSize: '1rem', marginBottom: '0.25rem' }}>Unggah File Excel Karyawan</h2>
              <p style={{ fontSize: '0.85rem', color: '#888', margin: 0 }}>
                Unduh template (karyawan baru) atau ekspor data karyawan saat ini (untuk memperbarui), isi/ubah datanya, lalu unggah kembali untuk pratinjau.
                Baris dengan kode karyawan yang sudah ada akan memperbarui karyawan tersebut, bukan membuat duplikat.
              </p>
            </div>
            <div style={{ display: 'flex', gap: '0.5rem' }}>
              <button onClick={handleExport} disabled={exporting} className="btn btn-secondary btn-sm">
                {exporting ? 'Menyiapkan…' : 'Ekspor Data Karyawan'}
              </button>
              <button onClick={handleDownloadTemplate} disabled={downloading} className="btn btn-secondary btn-sm">
                {downloading ? 'Menyiapkan…' : 'Unduh Template'}
              </button>
            </div>
          </div>

          <div
            onDrop={handleDrop}
            onDragOver={e => e.preventDefault()}
            onClick={() => fileRef.current?.click()}
            style={{
              border: '2px dashed #c8d8f0', borderRadius: '8px', padding: '2.5rem',
              textAlign: 'center', cursor: 'pointer', background: '#f5f8ff',
            }}
          >
            <div style={{ fontSize: '2rem', marginBottom: '0.5rem' }}>📂</div>
            <div style={{ fontWeight: 600, color: '#2c3e7a' }}>Klik atau seret file Excel ke sini</div>
            <div style={{ fontSize: '0.85rem', color: '#888', marginTop: '0.3rem' }}>Format: .xlsx — gunakan template di atas</div>
            <input ref={fileRef} type="file" accept=".xlsx,.xls" style={{ display: 'none' }}
              onChange={e => handleFile(e.target.files[0])} />
          </div>
          {uploading && <p style={{ color: '#888', marginTop: '0.75rem', textAlign: 'center' }}>Memproses file…</p>}
          {error && <div className="error-msg" style={{ marginTop: '0.75rem' }}>{error}</div>}
        </div>
      )}

      {/* Preview */}
      {preview && (
        <>
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '1rem' }}>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px' }}>File</div>
                <div style={{ fontWeight: 500 }}>{filename}</div>
              </div>
              <div style={{ display: 'flex', gap: '1.25rem', flexWrap: 'wrap' }}>
                <span style={{ color: '#2e7d32', fontWeight: 700 }}>{preview.ok_count} valid</span>
                {preview.warning_count > 0 && <span style={{ color: '#b45309', fontWeight: 700 }}>{preview.warning_count} peringatan</span>}
                <span style={{ color: preview.error_count > 0 ? '#c62828' : '#aaa', fontWeight: 700 }}>{preview.error_count} error</span>
                <span style={{ color: '#33691e' }}>{preview.create_count || 0} baru</span>
                <span style={{ color: '#1565c0' }}>{preview.update_count || 0} perbarui</span>
                <span style={{ color: '#555' }}>dari {preview.total_rows} baris</span>
              </div>
              <button onClick={reset} className="btn btn-secondary btn-sm">Ganti File</button>
            </div>
            {error && <div className="error-msg" style={{ marginTop: '0.75rem' }}>{error}</div>}
          </div>

          <div className="card" style={{ marginBottom: '1.5rem', overflowX: 'auto' }}>
            <table>
              <thead>
                <tr>
                  <th>#</th>
                  <th>Status</th>
                  <th>Aksi</th>
                  <th>Kode</th>
                  <th>Nama</th>
                  <th>Jabatan</th>
                  <th>Cabang</th>
                  <th style={{ textAlign: 'right' }}>Gaji Pokok</th>
                  <th style={{ textAlign: 'center' }}>Hari Kerja</th>
                  <th>Berlaku</th>
                  <th>Komponen</th>
                  <th>Catatan</th>
                </tr>
              </thead>
              <tbody>
                {preview.rows.map((row) => (
                  <tr key={row.row_number} style={{ background: row.status === 'error' ? '#fff7f7' : row.status === 'warning' ? '#fffdf5' : undefined }}>
                    <td style={{ color: '#999', fontSize: '0.82rem' }}>{row.row_number}</td>
                    <td>{statusBadge(row.status)}</td>
                    <td>{actionBadge(row.action)}</td>
                    <td style={{ fontSize: '0.82rem', fontFamily: 'monospace' }}>{row.employee_code}</td>
                    <td style={{ fontWeight: 500 }}>{row.full_name || <span style={{ color: '#ccc' }}>—</span>}</td>
                    <td style={{ fontSize: '0.85rem' }}>{row.position || '—'}</td>
                    <td style={{ fontSize: '0.85rem' }}>{row.branch || '—'}</td>
                    <td style={{ textAlign: 'right', fontSize: '0.85rem' }}>{idr(row.base_salary)}</td>
                    <td style={{ textAlign: 'center', fontSize: '0.85rem' }}>{row.working_days_per_month || '—'}</td>
                    <td style={{ fontSize: '0.82rem' }}>{row.effective_date || '—'}</td>
                    <td style={{ fontSize: '0.8rem', color: '#555' }}>
                      {(row.components && row.components.length > 0)
                        ? row.components.map((c, i) => (
                            <div key={i}>{c.component_name}: {idr(c.amount)}</div>
                          ))
                        : <span style={{ color: '#ccc' }}>—</span>}
                    </td>
                    <td style={{ fontSize: '0.8rem' }}>
                      {(row.messages && row.messages.length > 0)
                        ? row.messages.map((m, i) => (
                            <div key={i} style={{ color: row.status === 'error' ? '#c62828' : '#b45309' }}>{m}</div>
                          ))
                        : <span style={{ color: '#ccc' }}>—</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', flexWrap: 'wrap' }}>
            <button
              onClick={handleConfirm}
              disabled={hasErrors || submitting || preview.total_rows === 0}
              className="btn btn-primary"
              title={hasErrors ? 'Perbaiki baris yang error terlebih dahulu' : undefined}
            >
              {submitting ? 'Menyimpan…' : `Konfirmasi Impor (${preview.ok_count + preview.warning_count} baris)`}
            </button>
            {hasErrors && (
              <span style={{ fontSize: '0.85rem', color: '#c62828' }}>
                Terdapat {preview.error_count} baris error. Perbaiki file lalu unggah ulang sebelum mengonfirmasi.
              </span>
            )}
          </div>
        </>
      )}
    </>
  );
}
