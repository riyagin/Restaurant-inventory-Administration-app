import { useEffect, useState } from 'react';
import { useNavigate, useParams, Link } from 'react-router-dom';
import { getItem, createItem, updateItem } from '../api';

const empty = { name: '', code: '', is_stock: true, units: [{ name: '', perPrev: null }] };

export default function ItemForm() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [form, setForm] = useState(empty);
  const [error, setError] = useState('');
  const isEdit = Boolean(id);

  useEffect(() => {
    if (id) getItem(id).then(r => setForm({ ...r.data, is_stock: r.data.is_stock ?? true }));
  }, [id]);

  const setField = (field) => (e) => setForm(f => ({ ...f, [field]: e.target.value }));

  const setUnit = (index, field) => (e) => {
    const val = e.target.value;
    setForm(f => ({
      ...f,
      units: f.units.map((u, i) => i === index ? { ...u, [field]: val } : u),
    }));
  };

  const addUnit = () => setForm(f => ({
    ...f,
    units: [...f.units, { name: '', perPrev: '' }],
  }));

  const removeUnit = () => setForm(f => ({
    ...f,
    units: f.units.slice(0, -1),
  }));

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    for (let i = 1; i < form.units.length; i++) {
      if (!form.units[i].perPrev || Number(form.units[i].perPrev) <= 0) {
        setError(`Konversi untuk Satuan ${i + 1} harus berupa angka positif`);
        return;
      }
    }
    const payload = {
      name: form.name,
      code: form.code,
      is_stock: form.is_stock,
      units: form.units.map((u, i) => ({
        name: u.name,
        perPrev: i === 0 ? null : Number(u.perPrev),
      })),
    };
    try {
      if (isEdit) await updateItem(id, payload);
      else await createItem(payload);
      navigate('/items');
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    }
  };

  const canAdd = form.units.length < 3;
  const canRemove = form.units.length > 1;

  return (
    <div className="card form-card" style={{maxWidth:'620px'}}>
      <h2>{isEdit ? 'Edit Barang' : 'Tambah Barang Baru'}</h2>
      {error && <div className="error-msg">{error}</div>}
      <form onSubmit={handleSubmit}>
        <div className="form-row">
          <div className="form-group">
            <label>Nama</label>
            <input value={form.name} onChange={setField('name')} required placeholder="Nama produk" />
          </div>
          <div className="form-group">
            <label>Kode</label>
            <input value={form.code} onChange={setField('code')} required placeholder="mis. PRD-001" />
          </div>
        </div>

        <div className="form-group">
          <label>Tipe Barang</label>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            {[
              { value: true,  label: 'Barang Stok',     desc: 'dicatat di inventaris' },
              { value: false, label: 'Barang Non-Stok',  desc: 'pengeluaran / habis pakai' },
            ].map(opt => (
              <button
                key={String(opt.value)}
                type="button"
                onClick={() => setForm(f => ({ ...f, is_stock: opt.value }))}
                style={{
                  padding: '0.45rem 1rem', borderRadius: '6px', fontWeight: 600,
                  fontSize: '0.88rem', cursor: 'pointer',
                  border: form.is_stock === opt.value ? '2px solid #4f8ef7' : '2px solid #e0e0e0',
                  background: form.is_stock === opt.value ? '#e8f0fe' : '#f9f9f9',
                  color: form.is_stock === opt.value ? '#4f8ef7' : '#666',
                }}
              >
                {opt.label}
                <span style={{ fontWeight: 400, fontSize: '0.78rem', marginLeft: '0.4rem', color: form.is_stock === opt.value ? '#7aabf7' : '#aaa' }}>
                  ({opt.desc})
                </span>
              </button>
            ))}
          </div>
        </div>

        <div style={{marginBottom:'1.1rem'}}>
          <div style={{display:'flex',justifyContent:'space-between',alignItems:'center',marginBottom:'0.6rem'}}>
            <span style={{fontSize:'0.85rem',fontWeight:500,color:'#444'}}>Satuan &amp; Konversi</span>
            <div style={{display:'flex',gap:'0.5rem'}}>
              {canRemove && (
                <button type="button" onClick={removeUnit} className="btn btn-secondary btn-sm">− Hapus Satuan</button>
              )}
              {canAdd && (
                <button type="button" onClick={addUnit} className="btn btn-secondary btn-sm">+ Tambah Satuan</button>
              )}
            </div>
          </div>

          <div style={{display:'flex',flexDirection:'column',gap:'0.75rem'}}>
            {form.units.map((unit, i) => (
              <div key={i} style={{display:'grid',gridTemplateColumns:'1fr 1fr',gap:'0.75rem',alignItems:'end'}}>
                <div className="form-group" style={{margin:0}}>
                  <label>
                    Satuan {i + 1}
                    {i === 0 && form.units.length > 1 ? ' — terbesar' : ''}
                    {i === form.units.length - 1 && form.units.length > 1 ? ' — terkecil' : ''}
                  </label>
                  <input
                    value={unit.name}
                    onChange={setUnit(i, 'name')}
                    required
                    placeholder={i === 0 ? 'mis. Karton' : i === 1 ? 'mis. Pak' : 'mis. Pcs'}
                  />
                </div>
                {i > 0 ? (
                  <div className="form-group" style={{margin:0}}>
                    <label>
                      {unit.name || `Satuan ${i + 1}`} per {form.units[i - 1].name || `Satuan ${i}`}
                    </label>
                    <input
                      type="number"
                      min="1"
                      value={unit.perPrev}
                      onChange={setUnit(i, 'perPrev')}
                      required
                      placeholder="e.g. 12"
                    />
                  </div>
                ) : (
                  <div />
                )}
              </div>
            ))}
          </div>
        </div>

        <div className="form-actions">
          <button type="submit" className="btn btn-primary">{isEdit ? 'Simpan Perubahan' : 'Tambah Barang'}</button>
          <Link to="/items" className="btn btn-secondary">Batal</Link>
        </div>
      </form>
    </div>
  );
}
