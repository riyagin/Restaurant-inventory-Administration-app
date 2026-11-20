import { useState } from 'react';
import { deleteEmployeeFaceData, uploadUrl } from '../../../api';
import { fmtDateTimeOrDash } from './shared';

// Latest kiosk face photo, falling back to the HR upload.
export function ProfilePhotoCard({ emp, editable, onChanged }) {
  const [busy, setBusy] = useState(false);
  const photo = emp.face_photo_path || emp.photo_path;
  const isFacePhoto = !!emp.face_photo_path;
  const canDeleteFace = editable && emp.status === 'resigned' && (emp.has_face || emp.face_photo_path);

  const handleDeleteFace = async () => {
    if (!confirm(
      `Hapus data wajah dan semua foto absensi ${emp.full_name}?\n\n` +
      'Data ini tidak dapat dikembalikan.'
    )) return;
    setBusy(true);
    try {
      await deleteEmployeeFaceData(emp.id);
      onChanged();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus data wajah');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card" style={{ textAlign: 'center' }}>
      {photo ? (
        <img
          src={uploadUrl(photo)}
          alt={emp.full_name}
          style={{ width: '100%', maxWidth: 220, borderRadius: '8px', objectFit: 'cover', border: '1px solid #e8e8e8' }}
        />
      ) : (
        <div style={{ width: '100%', height: 200, borderRadius: '8px', background: '#eef1f6', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#8a93a6', fontSize: '3rem' }}>
          {(emp.full_name || '?').charAt(0).toUpperCase()}
        </div>
      )}
      {isFacePhoto && (
        <div style={{ marginTop: '0.5rem', fontSize: '0.8rem', color: '#8a93a6' }}>
          Foto absensi terakhir · {fmtDateTimeOrDash(emp.face_photo_at)}
        </div>
      )}
      {canDeleteFace && (
        <button
          onClick={handleDeleteFace}
          disabled={busy}
          className="btn btn-danger btn-sm"
          style={{ marginTop: '0.75rem', width: '100%' }}
        >
          {busy ? 'Menghapus...' : 'Hapus Data Wajah & Foto'}
        </button>
      )}
    </div>
  );
}
