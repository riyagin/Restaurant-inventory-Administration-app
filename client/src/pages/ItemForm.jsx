import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams, Link } from 'react-router-dom';
import { getItem, createItem, updateItem } from '../api';

const empty = { name: '', code: '', is_stock: true, min_stock: '', units: [{ name: '', perPrev: null }] };

const unitKey = (name) => (name || '').trim().toLowerCase();

const qty = (n) => Number(n).toLocaleString('id-ID', { maximumFractionDigits: 4 });

export default function ItemForm() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [form, setForm] = useState(empty);
  const [savedUnits, setSavedUnits] = useState(null);
  const [error, setError] = useState('');
  const isEdit = Boolean(id);

  useEffect(() => {
    if (id) getItem(id).then(r => {
      setForm({
        ...r.data,
        is_stock: r.data.is_stock ?? true,
        min_stock: Number(r.data.min_stock) > 0 ? String(r.data.min_stock) : '',
      });
      setSavedUnits(r.data.units || []);
    });
  }, [id]);

  // How the saved base unit maps onto the one being submitted. Everything
  // denominated in the base unit — every inventory lot, and the minimum-stock
  // threshold — is restated by this factor when the item is saved.
  const baseConversion = useMemo(() => {
    if (!isEdit || !savedUnits?.length || !form.is_stock) return null;
    const oldBase = savedUnits[savedUnits.length - 1];
    if (!oldBase?.name) return null;

    const idx = form.units.findIndex(u => unitKey(u.name) === unitKey(oldBase.name));
    if (idx === -1) {
      // A rename keeps the same shape and is handled positionally; anything
      // else means the unit the stock is held in is gone.
      if (form.units.length === savedUnits.length) return null;
      return { dropped: true, oldBase: oldBase.name };
    }

    let factor = 1;
    for (let j = idx + 1; j < form.units.length; j++) factor *= Number(form.units[j].perPrev) || 1;
    if (factor === 1) return null;

    return {
      factor,
      oldBase: oldBase.name,
      newBase: form.units[form.units.length - 1]?.name || 'satuan terkecil',
    };
  }, [isEdit, savedUnits, form.units, form.is_stock]);

  // Adding a smaller unit moves the item's base unit down a level, and the
  // backend restates existing stock into it. Spell out that conversion before
  // the user saves — the stock figures on every other page will change.
  const unitNotice = useMemo(() => {
    if (!baseConversion) return '';
    if (baseConversion.dropped)
      return `Satuan "${baseConversion.oldBase}" dihapus. Stok yang tersimpan dalam satuan itu tidak bisa dikonversi — kosongkan stoknya dulu.`;
    const { oldBase, newBase, factor } = baseConversion;
    let msg = `Satuan terkecil berubah. Stok yang ada akan dikonversi otomatis: 1 ${oldBase} = ${factor} ${newBase}. Nilai rupiah stok tidak berubah.`;
    if (Number(form.min_stock) > 0) {
      msg += ` Stok minimum ikut dikonversi: ${qty(form.min_stock)} ${oldBase} → ${qty(Number(form.min_stock) * factor)} ${newBase}.`;
    }
    return msg;
  }, [baseConversion, form.min_stock]);

  // The unit the minimum-stock figure is read in. While the units are being
  // edited it stays the *saved* base unit: that is what the backend converts
  // from, so the label has to keep saying what the number currently means.
  const minStockUnit =
    (isEdit && savedUnits?.length ? savedUnits[savedUnits.length - 1]?.name : '') ||
    form.units[form.units.length - 1]?.name ||
    'satuan terkecil';

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
      min_stock: form.is_stock ? Number(form.min_stock) || 0 : 0,
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
      {unitNotice && <div className="notice-msg">{unitNotice}</div>}
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

        {form.is_stock && (
          <div className="form-group">
            <label>Stok Minimum <span style={{fontWeight:400,color:'#888'}}>(dalam {minStockUnit})</span></label>
            <input
              type="number"
              min="0"
              step="any"
              value={form.min_stock}
              onChange={setField('min_stock')}
              placeholder="0 = tanpa batas minimum"
            />
            <span style={{fontSize:'0.78rem',color:'#888'}}>
              Barang ditandai stok menipis saat total stok di seluruh gudang berada di bawah angka ini.
            </span>
          </div>
        )}

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
