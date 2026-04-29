import { useState } from 'react';
import { updateUser } from '../api';

const emptyPw = { old_password: '', password: '', confirm: '' };

export default function Profile() {
  const [user, setUser] = useState(() => {
    try { return JSON.parse(localStorage.getItem('user') || '{}'); } catch { return {}; }
  });

  const [form, setForm]       = useState(emptyPw);
  const [error, setError]     = useState('');
  const [success, setSuccess] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const set = (field) => (e) => setForm(f => ({ ...f, [field]: e.target.value }));

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError(''); setSuccess('');
    if (form.password !== form.confirm) { setError('Konfirmasi password baru tidak cocok'); return; }
    if (form.password.length < 6) { setError('Password baru minimal 6 karakter'); return; }
    setSubmitting(true);
    try {
      await updateUser(user.id, { old_password: form.old_password, password: form.password });
      setForm(emptyPw);
      setSuccess('Password berhasil diubah.');
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setSubmitting(false);
    }
  };

  const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'long', year: 'numeric' }) : '—';

  return (
    <>
      <div className="page-header">
        <h1>Akun Saya</h1>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '320px 1fr', gap: '1.5rem', alignItems: 'start', maxWidth: '780px' }}>

        {/* Profile info */}
        <div className="card">
          <h2 style={{ fontSize: '1rem', marginBottom: '1.25rem' }}>Informasi Akun</h2>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
            <div>
              <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.25rem' }}>Username</div>
              <div style={{ fontWeight: 600, fontSize: '1rem' }}>{user.username}</div>
            </div>
            <div>
              <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.25rem' }}>Peran</div>
              <span className="badge">{user.role}</span>
            </div>
          </div>
        </div>

        {/* Change password */}
        <div className="card">
          <h2 style={{ fontSize: '1rem', marginBottom: '1.25rem' }}>Ganti Password</h2>
          {error   && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
          {success && (
            <div style={{ background: '#e6f9f0', border: '1px solid #b2dfdb', borderRadius: '6px', padding: '0.75rem 1rem', marginBottom: '1rem', color: '#1b5e45', fontWeight: 500, fontSize: '0.88rem' }}>
              {success}
            </div>
          )}
          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label>Password Lama</label>
              <input
                type="password"
                value={form.old_password}
                onChange={set('old_password')}
                required
                placeholder="Masukkan password saat ini"
                autoComplete="current-password"
              />
            </div>
            <div className="form-group">
              <label>Password Baru</label>
              <input
                type="password"
                value={form.password}
                onChange={set('password')}
                required
                placeholder="Minimal 6 karakter"
                autoComplete="new-password"
              />
            </div>
            <div className="form-group">
              <label>Konfirmasi Password Baru</label>
              <input
                type="password"
                value={form.confirm}
                onChange={set('confirm')}
                required
                placeholder="Ulangi password baru"
                autoComplete="new-password"
              />
            </div>
            <button
              type="submit"
              className="btn btn-primary"
              disabled={submitting}
              style={{ width: '100%', justifyContent: 'center' }}
            >
              {submitting ? 'Menyimpan...' : 'Ganti Password'}
            </button>
          </form>
        </div>

      </div>
    </>
  );
}
