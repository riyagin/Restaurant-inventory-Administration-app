import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
  getEmployee, createEmployee, updateEmployee,
  getBranches, getPositions, createPosition,
  uploadEmployeePhoto, deleteEmployeePhoto,
} from '../../api';

const SERVER = 'http://localhost:5000';

const empty = {
  employee_code: '', full_name: '', dob: '', join_date: '',
  position_id: '', branch_id: '',
  phone: '', email: '', address: '', national_id: '',
  bank_name: '', bank_account_number: '', bank_account_holder: '',
  status: 'active', employment_type: 'permanent', contract_end_date: '',
};

const toDateInput = (d) => d ? new Date(d).toISOString().slice(0, 10) : '';

export default function EmployeeForm() {
  const { id } = useParams();
  const isEdit = Boolean(id);
  const navigate = useNavigate();

  const [form, setForm]         = useState(empty);
  const [branches, setBranches] = useState([]);
  const [positions, setPositions] = useState([]);
  const [photoPath, setPhotoPath] = useState('');
  const [error, setError]       = useState('');
  const [submitting, setSubmitting] = useState(false);

  const [showAddPos, setShowAddPos] = useState(false);
  const [newPosName, setNewPosName] = useState('');
  const [addingPos, setAddingPos]   = useState(false);

  useEffect(() => {
    getBranches().then(r => setBranches(r.data)).catch(() => {});
    getPositions().then(r => setPositions(r.data)).catch(() => {});
  }, []);

  useEffect(() => {
    if (!isEdit) return;
    getEmployee(id).then(r => {
      const e = r.data;
      setForm({
        employee_code: e.employee_code || '',
        full_name: e.full_name || '',
        dob: toDateInput(e.dob),
        join_date: toDateInput(e.join_date),
        position_id: e.position_id || '',
        branch_id: e.branch_id || '',
        phone: e.phone || '',
        email: e.email || '',
        address: e.address || '',
        national_id: e.national_id || '',
        bank_name: e.bank_name || '',
        bank_account_number: e.bank_account_number || '',
        bank_account_holder: e.bank_account_holder || '',
        status: e.status || 'active',
        employment_type: e.employment_type || 'permanent',
        contract_end_date: toDateInput(e.contract_end_date),
      });
      setPhotoPath(e.photo_path || '');
    }).catch(() => setError('Gagal memuat data karyawan'));
  }, [id, isEdit]);

  const set = (k) => (e) => setForm(f => ({ ...f, [k]: e.target.value }));

  const handleAddPosition = async () => {
    const name = newPosName.trim();
    if (!name) return;
    setAddingPos(true);
    try {
      const r = await createPosition({ name });
      setPositions(p => [...p, r.data].sort((a, b) => a.name.localeCompare(b.name)));
      setForm(f => ({ ...f, position_id: r.data.id }));
      setNewPosName('');
      setShowAddPos(false);
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menambah jabatan');
    } finally {
      setAddingPos(false);
    }
  };

  const handlePhotoUpload = async (e) => {
    const file = e.target.files?.[0];
    if (!file || !isEdit) return;
    try {
      const r = await uploadEmployeePhoto(id, file);
      setPhotoPath(r.data.photo_path);
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal mengunggah foto');
    }
  };

  const handlePhotoDelete = async () => {
    if (!confirm('Hapus foto karyawan?')) return;
    try {
      await deleteEmployeePhoto(id);
      setPhotoPath('');
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus foto');
    }
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!form.full_name.trim()) { setError('Nama lengkap wajib diisi'); return; }
    if (!form.position_id) { setError('Jabatan wajib dipilih'); return; }
    if (!form.branch_id) { setError('Cabang wajib dipilih'); return; }
    if (!form.join_date) { setError('Tanggal bergabung wajib diisi'); return; }
    if (form.employment_type === 'contract' && !form.contract_end_date) {
      setError('Tanggal berakhir kontrak wajib diisi untuk karyawan kontrak'); return;
    }

    setSubmitting(true);
    try {
      if (isEdit) {
        await updateEmployee(id, form);
        navigate(`/hr/employees/${id}`);
      } else {
        const r = await createEmployee(form);
        navigate(`/hr/employees/${r.data.id}`);
      }
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>{isEdit ? 'Edit Karyawan' : 'Tambah Karyawan'}</h1>
        <button onClick={() => navigate('/hr/employees')} className="btn btn-secondary">Kembali</button>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      <form onSubmit={handleSubmit}>
        <div className="card" style={{ marginBottom: '1rem' }}>
          <div className="card-header"><h2>Data Diri</h2></div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '1rem' }}>
            <div className="form-group">
              <label>Kode Karyawan</label>
              <input value={form.employee_code} onChange={set('employee_code')} placeholder={isEdit ? '' : 'Otomatis (mis. EMP-0001)'} />
            </div>
            <div className="form-group">
              <label>Nama Lengkap *</label>
              <input value={form.full_name} onChange={set('full_name')} required />
            </div>
            <div className="form-group">
              <label>Tanggal Lahir</label>
              <input type="date" value={form.dob} onChange={set('dob')} />
            </div>
            <div className="form-group">
              <label>Tanggal Bergabung *</label>
              <input type="date" value={form.join_date} onChange={set('join_date')} required />
            </div>
            <div className="form-group">
              <label>Jabatan *</label>
              <div style={{ display: 'flex', gap: '0.4rem' }}>
                <select value={form.position_id} onChange={set('position_id')} style={{ flex: 1 }}>
                  <option value="">Pilih jabatan</option>
                  {positions.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => setShowAddPos(s => !s)}>+ Baru</button>
              </div>
              {showAddPos && (
                <div style={{ display: 'flex', gap: '0.4rem', marginTop: '0.5rem' }}>
                  <input value={newPosName} onChange={e => setNewPosName(e.target.value)} placeholder="Nama jabatan baru" style={{ flex: 1 }} />
                  <button type="button" className="btn btn-primary btn-sm" disabled={addingPos} onClick={handleAddPosition}>Simpan</button>
                </div>
              )}
            </div>
            <div className="form-group">
              <label>Cabang *</label>
              <select value={form.branch_id} onChange={set('branch_id')}>
                <option value="">Pilih cabang</option>
                {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
              </select>
            </div>
            <div className="form-group">
              <label>Status</label>
              <select value={form.status} onChange={set('status')}>
                <option value="active">Aktif</option>
                <option value="inactive">Nonaktif</option>
                <option value="resigned">Resign</option>
              </select>
            </div>
            <div className="form-group">
              <label>Tipe Kepegawaian</label>
              <select
                value={form.employment_type}
                onChange={e => setForm(f => ({
                  ...f,
                  employment_type: e.target.value,
                  // Clear the contract date when switching back to permanent.
                  contract_end_date: e.target.value === 'permanent' ? '' : f.contract_end_date,
                }))}
              >
                <option value="permanent">Tetap</option>
                <option value="contract">Kontrak</option>
              </select>
            </div>
            {form.employment_type === 'contract' && (
              <div className="form-group">
                <label>Tanggal Berakhir Kontrak *</label>
                <input type="date" value={form.contract_end_date} onChange={set('contract_end_date')} required />
              </div>
            )}
            <div className="form-group">
              <label>NIK / KTP</label>
              <input value={form.national_id} onChange={set('national_id')} />
            </div>
          </div>
        </div>

        <div className="card" style={{ marginBottom: '1rem' }}>
          <div className="card-header"><h2>Kontak</h2></div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '1rem' }}>
            <div className="form-group">
              <label>Telepon</label>
              <input value={form.phone} onChange={set('phone')} />
            </div>
            <div className="form-group">
              <label>Email</label>
              <input type="email" value={form.email} onChange={set('email')} />
            </div>
            <div className="form-group" style={{ gridColumn: '1 / -1' }}>
              <label>Alamat</label>
              <textarea value={form.address} onChange={set('address')} rows={2} />
            </div>
          </div>
        </div>

        <div className="card" style={{ marginBottom: '1rem' }}>
          <div className="card-header"><h2>Rekening Bank</h2></div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '1rem' }}>
            <div className="form-group">
              <label>Nama Bank</label>
              <input value={form.bank_name} onChange={set('bank_name')} />
            </div>
            <div className="form-group">
              <label>Nomor Rekening</label>
              <input value={form.bank_account_number} onChange={set('bank_account_number')} />
            </div>
            <div className="form-group">
              <label>Atas Nama</label>
              <input value={form.bank_account_holder} onChange={set('bank_account_holder')} />
            </div>
          </div>
        </div>

        {isEdit && (
          <div className="card" style={{ marginBottom: '1rem' }}>
            <div className="card-header"><h2>Foto</h2></div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
              {photoPath ? (
                <img src={`${SERVER}/uploads/${photoPath}`} alt="Foto" style={{ width: 96, height: 96, borderRadius: '8px', objectFit: 'cover', border: '1px solid #e8e8e8' }} />
              ) : (
                <div style={{ width: 96, height: 96, borderRadius: '8px', background: '#eef1f6', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#8a93a6' }}>Tidak ada</div>
              )}
              <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                <label className="btn btn-secondary btn-sm" style={{ cursor: 'pointer' }}>
                  Unggah Foto
                  <input type="file" accept="image/jpeg,image/png" onChange={handlePhotoUpload} style={{ display: 'none' }} />
                </label>
                {photoPath && <button type="button" className="btn btn-danger btn-sm" onClick={handlePhotoDelete}>Hapus Foto</button>}
              </div>
            </div>
          </div>
        )}

        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <button type="submit" className="btn btn-primary" disabled={submitting}>
            {submitting ? 'Menyimpan...' : (isEdit ? 'Simpan Perubahan' : 'Simpan Karyawan')}
          </button>
          <button type="button" className="btn btn-secondary" onClick={() => navigate('/hr/employees')}>Batal</button>
        </div>
        {!isEdit && <p style={{ color: '#888', fontSize: '0.8rem', marginTop: '0.75rem' }}>Foto dapat diunggah setelah karyawan disimpan.</p>}
      </form>
    </>
  );
}
