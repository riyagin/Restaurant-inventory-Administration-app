import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { login } from '../api';

export default function Login() {
  const navigate = useNavigate();
  const [form, setForm] = useState({ username: '', password: '' });
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const set = (field) => (e) => setForm(f => ({ ...f, [field]: e.target.value }));

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      const res = await login(form);
      localStorage.setItem('token', res.data.token);
      localStorage.setItem('user', JSON.stringify(res.data.user));
      navigate('/');
    } catch (err) {
      setError(err.response?.data?.error || 'Login gagal');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{minHeight:'100vh',display:'flex',alignItems:'center',justifyContent:'center',background:'#f0f2f5'}}>
      <div className="card" style={{width:'100%',maxWidth:'380px'}}>
        <div style={{textAlign:'center',marginBottom:'1.75rem'}}>
          <div style={{fontSize:'1.4rem',fontWeight:700,color:'#1a1a2e',marginBottom:'0.25rem'}}>InventoryPro</div>
          <div style={{fontSize:'0.9rem',color:'#888'}}>Masuk ke akun Anda</div>
        </div>
        {error && <div className="error-msg">{error}</div>}
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label>Nama Pengguna</label>
            <input
              autoFocus
              value={form.username}
              onChange={set('username')}
              required
              placeholder="Masukkan nama pengguna"
            />
          </div>
          <div className="form-group">
            <label>Kata Sandi</label>
            <input
              type="password"
              value={form.password}
              onChange={set('password')}
              required
              placeholder="Masukkan kata sandi"
            />
          </div>
          <button type="submit" className="btn btn-primary" style={{width:'100%',justifyContent:'center',marginTop:'0.5rem'}} disabled={loading}>
            {loading ? 'Masuk...' : 'Masuk'}
          </button>
        </form>
      </div>
    </div>
  );
}
