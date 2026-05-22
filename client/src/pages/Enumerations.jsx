import { useEffect, useState } from 'react';
import { getEnumerations, createEnumeration, deleteEnumeration, getItems, getWarehouses } from '../api';

const fmt      = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';
const fmtRp    = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(n);
const today    = new Date().toISOString().split('T')[0];

export default function Enumerations() {
  const [records, setRecords]       = useState([]);
  const [items, setItems]           = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [form, setForm]             = useState({
    warehouse_id:    '',
    source_item_id:  '',
    source_qty:      '',
    source_unit_idx: '0',
    output_item_id:  '',
    output_qty:      '',
    output_unit_idx: '0',
    date:            today,
    notes:           '',
  });
  const [error, setError]     = useState('');
  const [loading, setLoading] = useState(false);
  const [deleting, setDeleting] = useState(null);

  const load = () =>
    Promise.all([getEnumerations(), getItems(), getWarehouses()]).then(([e, it, w]) => {
      setRecords(e.data);
      setItems(it.data.filter(i => i.is_stock));
      setWarehouses(w.data);
    });

  useEffect(() => { load(); }, []);

  const set = (field) => (e) => setForm(f => ({ ...f, [field]: e.target.value }));

  const srcItem = items.find(i => i.id === form.source_item_id);
  const outItem = items.find(i => i.id === form.output_item_id);
  const srcUnits = srcItem?.units ?? [];
  const outUnits = outItem?.units ?? [];

  // Preview: cost per output unit
  const srcUnitName = srcUnits[Number(form.source_unit_idx)]?.name ?? '';
  const outUnitName = outUnits[Number(form.output_unit_idx)]?.name ?? '';

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await createEnumeration({
        warehouse_id:    form.warehouse_id,
        source_item_id:  form.source_item_id,
        source_qty:      Number(form.source_qty),
        source_unit_idx: Number(form.source_unit_idx),
        output_item_id:  form.output_item_id,
        output_qty:      Number(form.output_qty),
        output_unit_idx: Number(form.output_unit_idx),
        date:            form.date,
        notes:           form.notes || undefined,
      });
      setForm({
        warehouse_id: '', source_item_id: '', source_qty: '', source_unit_idx: '0',
        output_item_id: '', output_qty: '', output_unit_idx: '0',
        date: today, notes: '',
      });
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async (id) => {
    if (!window.confirm('Batalkan pencacahan ini? Stok akan dikembalikan ke barang sumber.')) return;
    setDeleting(id);
    try {
      await deleteEnumeration(id);
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal membatalkan pencacahan');
    } finally {
      setDeleting(null);
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Pencacahan</h1>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 380px', gap: '1.5rem', alignItems: 'start' }}>
        {/* History table */}
        <div className="card">
          <div className="card-header">
            <h2>{records.length} catatan pencacahan</h2>
          </div>
          <table>
            <thead>
              <tr>
                <th>Tanggal</th>
                <th>Gudang</th>
                <th>Sumber</th>
                <th>Hasil</th>
                <th style={{ textAlign: 'right' }}>Nilai dipindah</th>
                <th>Catatan</th>
                <th>Oleh</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {records.length === 0 ? (
                <tr>
                  <td colSpan={8} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                    Belum ada catatan pencacahan
                  </td>
                </tr>
              ) : records.map(r => {
                const sUnits = r.source_item_units ?? [];
                const oUnits = r.output_item_units ?? [];
                const sUnit  = sUnits[Number(r.source_unit_idx)]?.name ?? '';
                const oUnit  = oUnits[Number(r.output_unit_idx)]?.name ?? '';
                const perUnit = Number(r.output_qty) > 0
                  ? Math.round(Number(r.transferred_value) / Number(r.output_qty))
                  : 0;
                return (
                  <tr key={r.id}>
                    <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(r.date)}</td>
                    <td style={{ color: '#555', fontSize: '0.85rem' }}>{r.warehouse_name}</td>
                    <td>
                      <span style={{ color: '#e74c3c', fontWeight: 600 }}>
                        -{Number(r.source_qty).toLocaleString('id-ID')} {sUnit}
                      </span>
                      <span style={{ color: '#888', fontSize: '0.82rem' }}> {r.source_item_name}</span>
                    </td>
                    <td>
                      <span style={{ color: '#27ae60', fontWeight: 600 }}>
                        +{Number(r.output_qty).toLocaleString('id-ID')} {oUnit}
                      </span>
                      <span style={{ color: '#888', fontSize: '0.82rem' }}> {r.output_item_name}</span>
                    </td>
                    <td style={{ textAlign: 'right', fontSize: '0.85rem' }}>
                      <div>{fmtRp(r.transferred_value)}</div>
                      <div style={{ color: '#888', fontSize: '0.78rem' }}>{fmtRp(perUnit)}/unit</div>
                    </td>
                    <td style={{ color: '#888', fontSize: '0.82rem' }}>{r.notes ?? '—'}</td>
                    <td style={{ color: '#888', fontSize: '0.82rem' }}>{r.created_by_name ?? '—'}</td>
                    <td>
                      <button
                        className="btn btn-danger btn-sm"
                        onClick={() => handleDelete(r.id)}
                        disabled={deleting === r.id}
                        title="Batalkan pencacahan"
                      >
                        {deleting === r.id ? '...' : 'Batal'}
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {/* Form */}
        <div className="card">
          <h2 style={{ marginBottom: '1.25rem', fontSize: '1rem' }}>Catat Pencacahan</h2>
          {error && <div className="error-msg">{error}</div>}
          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label>Gudang</label>
              <select value={form.warehouse_id} onChange={set('warehouse_id')} required>
                <option value="">— Pilih gudang —</option>
                {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
              </select>
            </div>

            {/* Source item */}
            <div style={{ background: '#fff5f5', border: '1px solid #fddede', borderRadius: '6px', padding: '0.85rem', marginBottom: '0.85rem' }}>
              <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#c0392b', textTransform: 'uppercase', letterSpacing: '0.3px', marginBottom: '0.6rem' }}>
                Barang Sumber (dikurangi)
              </div>
              <div className="form-group" style={{ marginBottom: '0.6rem' }}>
                <label>Barang</label>
                <select value={form.source_item_id} onChange={(e) => { set('source_item_id')(e); set('source_unit_idx')({ target: { value: '0' } }); }} required>
                  <option value="">— Pilih barang —</option>
                  {items.map(i => <option key={i.id} value={i.id}>{i.name}</option>)}
                </select>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem' }}>
                <div className="form-group" style={{ marginBottom: 0 }}>
                  <label>Jumlah</label>
                  <input
                    type="number" min="0.001" step="any"
                    value={form.source_qty} onChange={set('source_qty')}
                    required placeholder="mis. 1"
                  />
                </div>
                <div className="form-group" style={{ marginBottom: 0 }}>
                  <label>Satuan</label>
                  <select value={form.source_unit_idx} onChange={set('source_unit_idx')} disabled={srcUnits.length === 0}>
                    {srcUnits.length === 0
                      ? <option value="0">—</option>
                      : srcUnits.map((u, i) => <option key={i} value={i}>{u.name}</option>)
                    }
                  </select>
                </div>
              </div>
            </div>

            {/* Output item */}
            <div style={{ background: '#f0faf5', border: '1px solid #c3e6d4', borderRadius: '6px', padding: '0.85rem', marginBottom: '0.85rem' }}>
              <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#1e8449', textTransform: 'uppercase', letterSpacing: '0.3px', marginBottom: '0.6rem' }}>
                Barang Hasil (ditambah)
              </div>
              <div className="form-group" style={{ marginBottom: '0.6rem' }}>
                <label>Barang</label>
                <select value={form.output_item_id} onChange={(e) => { set('output_item_id')(e); set('output_unit_idx')({ target: { value: '0' } }); }} required>
                  <option value="">— Pilih barang —</option>
                  {items.filter(i => i.id !== form.source_item_id).map(i => <option key={i.id} value={i.id}>{i.name}</option>)}
                </select>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem' }}>
                <div className="form-group" style={{ marginBottom: 0 }}>
                  <label>Jumlah</label>
                  <input
                    type="number" min="0.001" step="any"
                    value={form.output_qty} onChange={set('output_qty')}
                    required placeholder="mis. 6"
                  />
                </div>
                <div className="form-group" style={{ marginBottom: 0 }}>
                  <label>Satuan</label>
                  <select value={form.output_unit_idx} onChange={set('output_unit_idx')} disabled={outUnits.length === 0}>
                    {outUnits.length === 0
                      ? <option value="0">—</option>
                      : outUnits.map((u, i) => <option key={i} value={i}>{u.name}</option>)
                    }
                  </select>
                </div>
              </div>
            </div>

            <div className="form-group">
              <label>Tanggal</label>
              <input type="date" value={form.date} onChange={set('date')} required />
            </div>

            <div className="form-group">
              <label>Catatan <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
              <input value={form.notes} onChange={set('notes')} placeholder="mis. Batch pagi, buah sedikit menyusut..." />
            </div>

            <button
              type="submit"
              className="btn btn-primary"
              style={{ width: '100%', justifyContent: 'center' }}
              disabled={loading}
            >
              {loading ? 'Memproses...' : 'Catat Pencacahan'}
            </button>
          </form>
        </div>
      </div>
    </>
  );
}
