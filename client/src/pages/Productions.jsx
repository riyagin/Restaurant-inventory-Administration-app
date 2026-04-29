import { useEffect, useState } from 'react';
import { getRecipes, getWarehouses, getProductions, createProduction } from '../api';

const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';
const today = new Date().toISOString().split('T')[0];

export default function Productions() {
  const [productions, setProductions] = useState([]);
  const [recipes, setRecipes] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [form, setForm] = useState({ recipe_id: '', warehouse_id: '', batches: '', date: today, notes: '' });
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const load = () =>
    Promise.all([getProductions(), getRecipes(), getWarehouses()]).then(([p, r, w]) => {
      setProductions(p.data);
      setRecipes(r.data);
      setWarehouses(w.data);
    });

  useEffect(() => { load(); }, []);

  const set = (field) => (e) => setForm(f => ({ ...f, [field]: e.target.value }));

  const selectedRecipe = recipes.find(r => r.id === form.recipe_id);
  const numBatches = Number(form.batches) || 0;

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await createProduction({
        recipe_id: form.recipe_id,
        warehouse_id: form.warehouse_id,
        batches: Number(form.batches),
        date: form.date,
        notes: form.notes || undefined,
      });
      setForm({ recipe_id: '', warehouse_id: '', batches: '', date: today, notes: '' });
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Produksi</h1>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 360px', gap: '1.5rem', alignItems: 'start' }}>
        {/* Production history */}
        <div className="card">
          <div className="card-header">
            <h2>{productions.length} catatan produksi</h2>
          </div>
          <table>
            <thead>
              <tr>
                <th>Tanggal</th>
                <th>Resep</th>
                <th>Gudang</th>
                <th style={{ textAlign: 'right' }}>Batch</th>
                <th>Output</th>
                <th>Catatan</th>
                <th>Oleh</th>
              </tr>
            </thead>
            <tbody>
              {productions.length === 0 ? (
                <tr><td colSpan={7} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada catatan produksi</td></tr>
              ) : productions.map(p => {
                const outUnits = p.output_item_units ?? [];
                const unitName = outUnits[Number(p.batch_unit_index ?? 0)]?.name ?? '';
                return (
                  <tr key={p.id}>
                    <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(p.date)}</td>
                    <td style={{ fontWeight: 500 }}>{p.recipe_name}</td>
                    <td style={{ color: '#555', fontSize: '0.85rem' }}>{p.warehouse_name}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{Number(p.batches).toLocaleString('id-ID')}</td>
                    <td style={{ color: '#27ae60', fontWeight: 600 }}>
                      {Number(p.output_quantity).toLocaleString('id-ID')} {unitName}
                      <span style={{ color: '#888', fontWeight: 400, fontSize: '0.82rem' }}> {p.output_item_name}</span>
                    </td>
                    <td style={{ color: '#888', fontSize: '0.82rem' }}>{p.notes ?? '—'}</td>
                    <td style={{ color: '#888', fontSize: '0.82rem' }}>{p.created_by_name ?? '—'}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {/* Run production form */}
        <div className="card">
          <h2 style={{ marginBottom: '1.25rem', fontSize: '1rem' }}>Jalankan Produksi</h2>
          {error && <div className="error-msg">{error}</div>}
          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label>Resep</label>
              <select value={form.recipe_id} onChange={set('recipe_id')} required>
                <option value="">— Pilih resep —</option>
                {recipes.map(r => <option key={r.id} value={r.id}>{r.name}</option>)}
              </select>
            </div>

            <div className="form-group">
              <label>Gudang</label>
              <select value={form.warehouse_id} onChange={set('warehouse_id')} required>
                <option value="">— Pilih gudang —</option>
                {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
              </select>
            </div>

            <div className="form-group">
              <label>Jumlah Batch</label>
              <input type="number" min="1" step="any" value={form.batches} onChange={set('batches')} required placeholder="1" />
            </div>

            <div className="form-group">
              <label>Tanggal</label>
              <input type="date" value={form.date} onChange={set('date')} required />
            </div>

            <div className="form-group">
              <label>Catatan <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
              <input value={form.notes} onChange={set('notes')} placeholder="mis. Produksi batch pagi..." />
            </div>

            {/* Preview of what will be consumed/produced */}
            {selectedRecipe && numBatches > 0 && (
              <div style={{ background: '#f5f8ff', border: '1px solid #d0dff8', borderRadius: '6px', padding: '0.85rem', marginBottom: '1rem', fontSize: '0.85rem' }}>
                <div style={{ fontWeight: 600, marginBottom: '0.6rem', color: '#2c3e7a' }}>Ringkasan Produksi</div>
                <div style={{ marginBottom: '0.5rem', color: '#555' }}>
                  <span style={{ color: '#888' }}>Output: </span>
                  <strong style={{ color: '#27ae60' }}>
                    {(Number(selectedRecipe.batch_size) * numBatches).toLocaleString('id-ID')} {(selectedRecipe.output_item_units ?? [])[Number(selectedRecipe.batch_unit_index)]?.name ?? ''}
                  </strong>
                  <span style={{ color: '#555' }}> {selectedRecipe.output_item_name}</span>
                </div>
                <div style={{ color: '#888', marginBottom: '0.3rem', fontSize: '0.8rem', textTransform: 'uppercase', letterSpacing: '0.3px' }}>Bahan yang dibutuhkan</div>
                {selectedRecipe.ingredients.map((ing, i) => {
                  const units = ing.item_units ?? [];
                  const unitName = units[Number(ing.unit_index)]?.name ?? '';
                  const totalNeeded = Number(ing.quantity) * numBatches;
                  return (
                    <div key={i} style={{ display: 'flex', justifyContent: 'space-between', padding: '0.2rem 0', borderBottom: i < selectedRecipe.ingredients.length - 1 ? '1px solid #e8eef8' : 'none' }}>
                      <span style={{ color: '#333' }}>{ing.item_name}</span>
                      <span style={{ fontWeight: 600, color: '#e67e22' }}>
                        {totalNeeded.toLocaleString('id-ID')} {unitName}
                      </span>
                    </div>
                  );
                })}
              </div>
            )}

            <button
              type="submit"
              className="btn btn-primary"
              style={{ width: '100%', justifyContent: 'center' }}
              disabled={loading}
            >
              {loading ? 'Memproses...' : 'Jalankan Produksi'}
            </button>
          </form>
        </div>
      </div>
    </>
  );
}
