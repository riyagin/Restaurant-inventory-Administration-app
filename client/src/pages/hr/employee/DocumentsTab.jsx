import { useEffect, useState, useCallback } from 'react';
import { getEmployeeDocuments, uploadEmployeeDocument, downloadEmployeeDocument, deleteEmployeeDocument } from '../../../api';
import { DOC_TYPE_LABELS, fmtDate } from './shared';

export function DokumenTab({ employeeId, editable }) {
  const [docs, setDocs] = useState([]);
  const [loading, setLoading] = useState(true);
  const [form, setForm] = useState({ doc_type: 'ktp', title: '', notes: '', file: null });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    setLoading(true);
    getEmployeeDocuments(employeeId)
      .then((r) => setDocs(r.data))
      .catch(() => setError('Gagal memuat dokumen'))
      .finally(() => setLoading(false));
  }, [employeeId]);

  useEffect(() => { load(); }, [load]);

  const upload = async () => {
    if (!form.file) { setError('Pilih file terlebih dahulu.'); return; }
    setBusy(true); setError('');
    try {
      const fd = new FormData();
      fd.append('file', form.file);
      fd.append('doc_type', form.doc_type);
      fd.append('title', form.title || DOC_TYPE_LABELS[form.doc_type] || form.file.name);
      fd.append('notes', form.notes);
      const r = await uploadEmployeeDocument(employeeId, fd);
      setDocs((d) => [r.data, ...d]);
      setForm({ doc_type: 'ktp', title: '', notes: '', file: null });
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal mengunggah dokumen.');
    } finally { setBusy(false); }
  };

  const download = async (doc) => {
    try {
      const r = await downloadEmployeeDocument(employeeId, doc.id);
      const url = window.URL.createObjectURL(new Blob([r.data]));
      const a = document.createElement('a');
      a.href = url; a.download = doc.original_name || doc.title;
      document.body.appendChild(a); a.click(); a.remove();
      window.URL.revokeObjectURL(url);
    } catch { alert('Gagal mengunduh dokumen'); }
  };

  const remove = async (doc) => {
    if (!confirm(`Hapus dokumen "${doc.title}"?`)) return;
    try {
      await deleteEmployeeDocument(employeeId, doc.id);
      setDocs((d) => d.filter((x) => x.id !== doc.id));
    } catch { alert('Gagal menghapus dokumen'); }
  };

  return (
    <div className="card">
      <div className="card-header"><h2>Dokumen Karyawan</h2></div>
      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      {editable && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '1rem', alignItems: 'end', marginBottom: '1.2rem', paddingBottom: '1.2rem', borderBottom: '1px solid #e8e8e8' }}>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label>Jenis Dokumen</label>
            <select value={form.doc_type} onChange={(e) => setForm((s) => ({ ...s, doc_type: e.target.value }))}>
              {Object.entries(DOC_TYPE_LABELS).map(([k, l]) => <option key={k} value={k}>{l}</option>)}
            </select>
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label>Judul (opsional)</label>
            <input value={form.title} onChange={(e) => setForm((s) => ({ ...s, title: e.target.value }))} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label>Berkas</label>
            <input type="file" accept=".pdf,.jpg,.jpeg,.png,.doc,.docx" onChange={(e) => setForm((s) => ({ ...s, file: e.target.files?.[0] || null }))} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <button className="btn btn-primary" disabled={busy} onClick={upload}>{busy ? 'Mengunggah…' : '⬆ Unggah'}</button>
          </div>
        </div>
      )}

      {loading ? <p style={{ color: 'var(--ink-3)' }}>Memuat…</p> : (
        docs.length === 0 ? <p style={{ color: 'var(--ink-3)' }}>Belum ada dokumen.</p> : (
          <table style={{ width: '100%' }}>
            <thead>
              <tr>
                <th style={{ textAlign: 'left', padding: 8 }}>Judul</th>
                <th style={{ textAlign: 'left', padding: 8 }}>Jenis</th>
                <th style={{ textAlign: 'left', padding: 8 }}>Diunggah</th>
                <th style={{ textAlign: 'right', padding: 8 }}>Aksi</th>
              </tr>
            </thead>
            <tbody>
              {docs.map((d) => (
                <tr key={d.id} style={{ borderTop: '1px solid #eee' }}>
                  <td style={{ padding: 8 }}>📄 {d.title}</td>
                  <td style={{ padding: 8 }}>{DOC_TYPE_LABELS[d.doc_type] || d.doc_type}</td>
                  <td style={{ padding: 8 }}>{fmtDate(d.created_at)}{d.uploaded_by ? ` · ${d.uploaded_by}` : ''}</td>
                  <td style={{ padding: 8, textAlign: 'right', whiteSpace: 'nowrap' }}>
                    <button className="btn btn-secondary btn-sm" onClick={() => download(d)}>Unduh</button>
                    {editable && <button className="btn btn-danger btn-sm" style={{ marginLeft: 6 }} onClick={() => remove(d)}>Hapus</button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )
      )}
    </div>
  );
}
