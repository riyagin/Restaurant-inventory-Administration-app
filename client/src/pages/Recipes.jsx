import { useEffect, useState } from 'react';
import { getItems } from '../api';
import { getRecipes, createRecipe, updateRecipe, deleteRecipe } from '../api';

const emptyForm = {
  name: '',
  output_item_id: '',
  batch_size: '',
  batch_unit_index: 0,
  ingredients: [],
};

const emptyIngredient = { item_id: '', quantity: '', unit_index: 0 };

export default function Recipes() {
  const [recipes, setRecipes] = useState([]);
  const [items, setItems] = useState([]);
  const [form, setForm] = useState(emptyForm);
  const [editId, setEditId] = useState(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [expanded, setExpanded] = useState(null);

  const stockItems = items.filter(i => i.is_stock);

  const load = () =>
    Promise.all([getRecipes(), getItems()]).then(([r, it]) => {
      setRecipes(r.data);
      setItems(it.data);
    });

  useEffect(() => { load(); }, []);

  const setField = (field) => (e) => setForm(f => ({ ...f, [field]: e.target.value }));

  const outputItem = items.find(i => i.id === form.output_item_id);
  const outputUnits = outputItem?.units ?? [];

  const setIngredient = (idx, field, value) => {
    setForm(f => {
      const ings = [...f.ingredients];
      if (field === 'item_id') {
        ings[idx] = { ...ings[idx], item_id: value, unit_index: 0 };
      } else {
        ings[idx] = { ...ings[idx], [field]: value };
      }
      return { ...f, ingredients: ings };
    });
  };

  const addIngredient = () =>
    setForm(f => ({ ...f, ingredients: [...f.ingredients, { ...emptyIngredient }] }));

  const removeIngredient = (idx) =>
    setForm(f => ({ ...f, ingredients: f.ingredients.filter((_, i) => i !== idx) }));

  const startEdit = (recipe) => {
    setEditId(recipe.id);
    setForm({
      name: recipe.name,
      output_item_id: recipe.output_item_id,
      batch_size: String(recipe.batch_size),
      batch_unit_index: recipe.batch_unit_index,
      ingredients: recipe.ingredients.map(ing => ({
        item_id: ing.item_id,
        quantity: String(ing.quantity),
        unit_index: ing.unit_index,
      })),
    });
    setError('');
  };

  const cancelEdit = () => {
    setEditId(null);
    setForm(emptyForm);
    setError('');
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!form.ingredients.length) { setError('Tambahkan setidaknya satu bahan baku'); return; }
    for (const ing of form.ingredients) {
      if (!ing.item_id || !ing.quantity) { setError('Semua bahan baku harus memiliki barang dan jumlah'); return; }
    }
    setLoading(true);
    try {
      const payload = {
        name: form.name,
        output_item_id: form.output_item_id,
        batch_size: Number(form.batch_size),
        batch_unit_index: Number(form.batch_unit_index),
        ingredients: form.ingredients.map(ing => ({
          item_id: ing.item_id,
          quantity: Number(ing.quantity),
          unit_index: Number(ing.unit_index),
        })),
      };
      if (editId) {
        await updateRecipe(editId, payload);
      } else {
        await createRecipe(payload);
      }
      setEditId(null);
      setForm(emptyForm);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async (id, name) => {
    if (!confirm(`Hapus resep "${name}"?`)) return;
    try {
      await deleteRecipe(id);
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus');
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Resep Produksi</h1>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 380px', gap: '1.5rem', alignItems: 'start' }}>
        {/* Recipe list */}
        <div className="card">
          <div className="card-header">
            <h2>{recipes.length} resep</h2>
          </div>
          {recipes.length === 0 ? (
            <p style={{ color: '#999', padding: '1rem 0' }}>Belum ada resep. Buat resep di sebelah kanan.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Nama Resep</th>
                  <th>Output</th>
                  <th style={{ textAlign: 'right' }}>Ukuran Batch</th>
                  <th style={{ textAlign: 'right' }}>Bahan</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {recipes.map(r => {
                  const outUnits = r.output_item_units ?? [];
                  const unitName = outUnits[Number(r.batch_unit_index)]?.name ?? '';
                  return (
                    <>
                      <tr key={r.id} style={{ cursor: 'pointer' }} onClick={() => setExpanded(expanded === r.id ? null : r.id)}>
                        <td style={{ fontWeight: 600 }}>{r.name}</td>
                        <td style={{ color: '#555' }}>{r.output_item_name ?? '—'}</td>
                        <td style={{ textAlign: 'right' }}>{Number(r.batch_size).toLocaleString('id-ID')} {unitName}</td>
                        <td style={{ textAlign: 'right', color: '#888' }}>{r.ingredients.length} bahan</td>
                        <td>
                          <div style={{ display: 'flex', gap: '0.4rem' }}>
                            <button onClick={(e) => { e.stopPropagation(); startEdit(r); }} className="btn btn-secondary btn-sm">Edit</button>
                            <button onClick={(e) => { e.stopPropagation(); handleDelete(r.id, r.name); }} className="btn btn-danger btn-sm">Hapus</button>
                          </div>
                        </td>
                      </tr>
                      {expanded === r.id && (
                        <tr key={`${r.id}-exp`}>
                          <td colSpan={5} style={{ background: '#f9f9f9', padding: '0.75rem 1rem' }}>
                            <strong style={{ fontSize: '0.8rem', color: '#666', textTransform: 'uppercase' }}>Bahan per Batch</strong>
                            <table style={{ marginTop: '0.5rem', width: 'auto' }}>
                              <thead>
                                <tr>
                                  <th style={{ fontSize: '0.8rem' }}>Barang</th>
                                  <th style={{ fontSize: '0.8rem', textAlign: 'right' }}>Jumlah</th>
                                  <th style={{ fontSize: '0.8rem' }}>Satuan</th>
                                </tr>
                              </thead>
                              <tbody>
                                {r.ingredients.map(ing => {
                                  const ingUnits = ing.item_units ?? [];
                                  const ingUnit = ingUnits[Number(ing.unit_index)]?.name ?? '';
                                  return (
                                    <tr key={ing.id}>
                                      <td style={{ fontSize: '0.85rem' }}>{ing.item_name}</td>
                                      <td style={{ textAlign: 'right', fontSize: '0.85rem', fontWeight: 600 }}>{Number(ing.quantity).toLocaleString('id-ID')}</td>
                                      <td style={{ fontSize: '0.85rem', color: '#666' }}>{ingUnit}</td>
                                    </tr>
                                  );
                                })}
                              </tbody>
                            </table>
                          </td>
                        </tr>
                      )}
                    </>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        {/* Form */}
        <div className="card">
          <h2 style={{ marginBottom: '1.25rem', fontSize: '1rem' }}>
            {editId ? 'Edit Resep' : 'Buat Resep Baru'}
          </h2>
          {error && <div className="error-msg">{error}</div>}
          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label>Nama Resep</label>
              <input value={form.name} onChange={setField('name')} required placeholder="mis. Roti Tawar, Kue Coklat..." />
            </div>

            <div className="form-group">
              <label>Produk Output</label>
              <select value={form.output_item_id} onChange={(e) => setForm(f => ({ ...f, output_item_id: e.target.value, batch_unit_index: 0 }))} required>
                <option value="">— Pilih barang output —</option>
                {stockItems.map(i => <option key={i.id} value={i.id}>{i.name} ({i.code})</option>)}
              </select>
            </div>

            {form.output_item_id && (
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
                <div className="form-group">
                  <label>Ukuran Batch</label>
                  <input type="number" min="0.001" step="any" value={form.batch_size} onChange={setField('batch_size')} required placeholder="0" />
                </div>
                <div className="form-group">
                  <label>Satuan Output</label>
                  <select value={form.batch_unit_index} onChange={(e) => setForm(f => ({ ...f, batch_unit_index: Number(e.target.value) }))}>
                    {outputUnits.map((u, i) => <option key={i} value={i}>{u.name}</option>)}
                  </select>
                </div>
              </div>
            )}

            <div style={{ marginTop: '1rem', marginBottom: '0.5rem' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
                <label style={{ margin: 0, fontWeight: 600, fontSize: '0.88rem' }}>Bahan Baku (per batch)</label>
                <button type="button" onClick={addIngredient} className="btn btn-secondary btn-sm">+ Tambah</button>
              </div>
              {form.ingredients.length === 0 && (
                <p style={{ color: '#aaa', fontSize: '0.85rem', margin: '0.5rem 0' }}>Belum ada bahan. Klik "+ Tambah".</p>
              )}
              {form.ingredients.map((ing, idx) => {
                const ingItem = items.find(i => i.id === ing.item_id);
                const ingUnits = ingItem?.units ?? [];
                return (
                  <div key={idx} style={{ display: 'grid', gridTemplateColumns: '1fr 70px 80px 28px', gap: '0.4rem', marginBottom: '0.5rem', alignItems: 'end' }}>
                    <div className="form-group" style={{ margin: 0 }}>
                      {idx === 0 && <label style={{ fontSize: '0.75rem', color: '#888' }}>Barang</label>}
                      <select value={ing.item_id} onChange={(e) => setIngredient(idx, 'item_id', e.target.value)} required>
                        <option value="">— Pilih —</option>
                        {stockItems.map(i => <option key={i.id} value={i.id}>{i.name}</option>)}
                      </select>
                    </div>
                    <div className="form-group" style={{ margin: 0 }}>
                      {idx === 0 && <label style={{ fontSize: '0.75rem', color: '#888' }}>Jumlah</label>}
                      <input type="number" min="0.001" step="any" value={ing.quantity} onChange={(e) => setIngredient(idx, 'quantity', e.target.value)} required placeholder="0" />
                    </div>
                    <div className="form-group" style={{ margin: 0 }}>
                      {idx === 0 && <label style={{ fontSize: '0.75rem', color: '#888' }}>Satuan</label>}
                      <select value={ing.unit_index} onChange={(e) => setIngredient(idx, 'unit_index', Number(e.target.value))} disabled={!ing.item_id}>
                        {ingUnits.map((u, i) => <option key={i} value={i}>{u.name}</option>)}
                        {!ing.item_id && <option value={0}>—</option>}
                      </select>
                    </div>
                    <div style={{ paddingBottom: '2px' }}>
                      {idx === 0 && <div style={{ fontSize: '0.75rem', color: 'transparent', marginBottom: '4px' }}>.</div>}
                      <button type="button" onClick={() => removeIngredient(idx)} className="btn btn-danger btn-sm" style={{ padding: '0.3rem 0.5rem' }}>×</button>
                    </div>
                  </div>
                );
              })}
            </div>

            <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1.25rem' }}>
              <button type="submit" className="btn btn-primary" style={{ flex: 1, justifyContent: 'center' }} disabled={loading}>
                {loading ? 'Menyimpan...' : editId ? 'Simpan Perubahan' : 'Buat Resep'}
              </button>
              {editId && (
                <button type="button" onClick={cancelEdit} className="btn btn-secondary">Batal</button>
              )}
            </div>
          </form>
        </div>
      </div>
    </>
  );
}
