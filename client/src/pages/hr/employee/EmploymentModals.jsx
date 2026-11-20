import { useState } from 'react';
import { transitionEmployeeToPermanent, resignEmployee } from '../../../api';
import { todayWIB } from '../../../time';

export function ResignModal({ employee, onClose, onDone }) {
  const today = todayWIB();
  const [date, setDate] = useState(today);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e) => {
    e.preventDefault();
    if (!date) { setError('Tanggal wajib diisi'); return; }
    setBusy(true); setError('');
    try {
      await resignEmployee(employee.id, { resign_date: date });
      onDone();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menandai resign');
      setBusy(false);
    }
  };

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'flex-start', justifyContent: 'center', zIndex: 1000, overflowY: 'auto', padding: '3rem 1rem' }}>
      <div className="card" style={{ width: '100%', maxWidth: 460, padding: '2rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
          <h2 style={{ margin: 0, fontSize: '1.1rem' }}>Tandai Resign</h2>
          <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
        </div>
        <p style={{ color: '#667', fontSize: '0.85rem', marginTop: 0 }}>
          Menandai <strong>{employee.full_name}</strong> sebagai karyawan yang mengundurkan diri (resign).
          Data karyawan dan riwayat HR tetap tersimpan; karyawan tidak lagi berstatus aktif.
        </p>
        {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
        <form onSubmit={submit}>
          <div className="form-group">
            <label>Tanggal Resign</label>
            <input type="date" value={date} onChange={e => setDate(e.target.value)} required />
          </div>
          <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', marginTop: '1rem' }}>
            <button type="button" onClick={onClose} className="btn btn-secondary" disabled={busy}>Batal</button>
            <button type="submit" className="btn btn-primary" disabled={busy} style={{ background: '#7b2cbf', borderColor: '#7b2cbf' }}>
              {busy ? 'Menyimpan…' : 'Tandai Resign'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

export function TransitionPermanentModal({ employee, onClose, onDone }) {
  const today = todayWIB();
  const [date, setDate] = useState(today);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e) => {
    e.preventDefault();
    if (!date) { setError('Tanggal wajib diisi'); return; }
    setBusy(true); setError('');
    try {
      await transitionEmployeeToPermanent(employee.id, { effective_date: date });
      onDone();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal mengubah status karyawan');
      setBusy(false);
    }
  };

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'flex-start', justifyContent: 'center', zIndex: 1000, overflowY: 'auto', padding: '3rem 1rem' }}>
      <div className="card" style={{ width: '100%', maxWidth: 460, padding: '2rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
          <h2 style={{ margin: 0, fontSize: '1.1rem' }}>Jadikan Karyawan Tetap</h2>
          <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
        </div>
        <p style={{ color: '#667', fontSize: '0.85rem', marginTop: 0 }}>
          {employee.employment_type === 'probation' ? (
            <>Mengubah <strong>{employee.full_name}</strong> dari masa percobaan menjadi karyawan tetap (PKWTT).
            Masa percobaan ikut dihitung, jadi masa kerja THR tetap dihitung sejak tanggal bergabung.</>
          ) : (
            <>Mengubah <strong>{employee.full_name}</strong> dari karyawan kontrak (PKWT) menjadi karyawan tetap (PKWTT).
            Tanggal status tetap menjadi titik awal (hari ke-0) perhitungan masa kerja THR.</>
          )}
        </p>
        {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
        <form onSubmit={submit}>
          <div className="form-group">
            <label>Tanggal Status Tetap</label>
            <input type="date" value={date} onChange={e => setDate(e.target.value)} required />
          </div>
          <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', marginTop: '1rem' }}>
            <button type="button" onClick={onClose} className="btn btn-secondary" disabled={busy}>Batal</button>
            <button type="submit" className="btn btn-primary" disabled={busy} style={{ background: '#1e7e34', borderColor: '#1e7e34' }}>
              {busy ? 'Menyimpan…' : 'Ubah Status'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
