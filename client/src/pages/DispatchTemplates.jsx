import { useEffect, useState } from 'react';
import {
  getDispatchTemplates, createDispatchTemplate, updateDispatchTemplate, deleteDispatchTemplate,
  getItems, getWarehouses, getBranches, getDivisions,
} from '../api';

const emptyRow = () => ({ item_id: '', unit_index: '0' });

function TemplateForm({ initial, items, warehouses, branches, onSave, onCancel }) {
  const [name, setName] = useState(initial?.name ?? '');
  const [warehouseId, setWarehouseId] = useState(initial?.warehouse_id ?? '');
  const [branchId, setBranchId] = useState(initial?.branch_id ?? '');
  const [divisionId, setDivisionId] = useState(initial?.division_id ?? '');
  const [notes, setNotes] = useState(initial?.notes ?? '');
  const [divisions, setDivisions] = useState([]);
  const [rows, setRows] = useState(() => {
    if (initial?.items?.length) {
      return initial.items.map(ti => ({
        item_id: ti.item_id ?? '',
        unit_index: String(ti.unit_index ?? 0),
      }));
    }
    return [emptyRow()];
  });
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  // Divisions belong to a branch, so the list follows the selected branch.
  useEffect(() => {
    if (!branchId) { setDivisions([]); return; }
    getDivisions({ branch_id: branchId }).then(r => setDivisions(r.data));
  }, [branchId]);

  const handleBranchChange = (val) => {
    setBranchId(val);
    setDivisionId('');
  };

  const updateRow = (index, updated) => setRows(rs => rs.map((r, i) => i === index ? updated : r));
  const removeRow = (index) => setRows(rs => rs.filter((_, i) => i !== index));
  const addRow = () => setRows(rs => [...rs, emptyRow()]);

  const setRowField = (index, field) => (e) => {
    const val = e.target.value;
    const row = rows[index];
    if (field === 'item_id') {
      // Default to the item's base unit (the last entry in items.units).
      const item = items.find(it => it.id === val);
      updateRow(index, { item_id: val, unit_index: item ? String(item.units.length - 1) : '0' });
    } else {
      updateRow(index, { ...row, [field]: val });
    }
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!name.trim()) { setError('Nama template wajib diisi'); return; }
    const payloadItems = rows
      .filter(r => r.item_id)
      .map((r, idx) => ({
        item_id: r.item_id,
        unit_index: Number(r.unit_index),
        sort_order: idx,
      }));
    setSaving(true);
    try {
      await onSave({
        name: name.trim(),
        warehouse_id: warehouseId || null,
        branch_id: branchId || null,
        division_id: divisionId || null,
        notes: notes.trim() || null,
        items: payloadItems,
      });
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={handleSubmit}>
      {error && <div className="error-msg" style={{ marginBottom: '0.75rem' }}>{error}</div>}

      <div className="form-row" style={{ marginBottom: '1rem' }}>
        <div className="form-group" style={{ margin: 0, flex: 2 }}>
          <label>Nama Template</label>
          <input
            value={name}
            onChange={e => setName(e.target.value)}
            placeholder="Contoh: Kirim Harian Dapur"
            required
          />
        </div>
        <div className="form-group" style={{ margin: 0, flex: 1 }}>
          <label>Gudang Asal <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
          <select value={warehouseId} onChange={e => setWarehouseId(e.target.value)}>
            <option value="">Pilih gudang...</option>
            {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
          </select>
        </div>
      </div>

      <div className="form-row" style={{ marginBottom: '1rem' }}>
        <div className="form-group" style={{ margin: 0 }}>
          <label>Cabang Tujuan <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
          <select value={branchId} onChange={e => handleBranchChange(e.target.value)}>
            <option value="">Pilih cabang...</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
        </div>
        <div className="form-group" style={{ margin: 0 }}>
          <label>Divisi Tujuan <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
          <select value={divisionId} onChange={e => setDivisionId(e.target.value)} disabled={!branchId}>
            <option value="">{branchId ? 'Pilih divisi...' : 'Pilih cabang terlebih dahulu'}</option>
            {divisions.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
          </select>
        </div>
        <div className="form-group" style={{ margin: 0 }}>
          <label>Catatan Bawaan <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
          <input value={notes} onChange={e => setNotes(e.target.value)} placeholder="Alasan atau deskripsi..." />
        </div>
      </div>

      <div style={{ fontWeight: 600, fontSize: '0.85rem', color: '#444', marginBottom: '0.25rem' }}>
        Barang Bawaan Template <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional — bisa ditambah saat input pengiriman)</span>
      </div>
      <div style={{ fontSize: '0.78rem', color: '#888', marginBottom: '0.5rem' }}>
        Jumlah tidak disimpan di template — diisi setiap kali membuat pengiriman.
      </div>

      <div style={{ overflowX: 'auto', marginBottom: '0.75rem' }}>
        <table className="invoice-items-table">
          <thead>
            <tr>
              <th>Barang</th>
              <th>Satuan</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => {
              const selectedItem = items.find(it => it.id === row.item_id);
              return (
                <tr key={i} style={{ verticalAlign: 'top' }}>
                  <td style={{ paddingTop: '0.3rem', minWidth: '220px' }}>
                    <select value={row.item_id} onChange={setRowField(i, 'item_id')} style={{ width: '100%' }}>
                      <option value="">Pilih barang...</option>
                      {items.map(it => <option key={it.id} value={it.id}>{it.name}</option>)}
                    </select>
                  </td>
                  <td style={{ paddingTop: '0.3rem', minWidth: '110px' }}>
                    {selectedItem ? (
                      <select value={row.unit_index} onChange={setRowField(i, 'unit_index')} style={{ width: '100%' }}>
                        {selectedItem.units.map((u, ui) => (
                          <option key={ui} value={String(ui)}>{u.name}</option>
                        ))}
                      </select>
                    ) : (
                      <select disabled style={{ width: '100%' }}><option>—</option></select>
                    )}
                  </td>
                  <td style={{ paddingTop: '0.3rem', width: '40px', textAlign: 'center' }}>
                    <button type="button" onClick={() => removeRow(i)} className="btn btn-danger btn-sm" title="Hapus baris">✕</button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <button type="button" onClick={addRow} className="btn btn-secondary btn-sm" style={{ marginBottom: '1.25rem' }}>
        + Tambah Baris
      </button>

      <div className="form-actions">
        <button type="submit" className="btn btn-primary" disabled={saving}>
          {saving ? 'Menyimpan…' : initial ? 'Simpan Perubahan' : 'Buat Template'}
        </button>
        <button type="button" onClick={onCancel} className="btn btn-secondary">Batal</button>
      </div>
    </form>
  );
}

export default function DispatchTemplates() {
  const [templates, setTemplates] = useState([]);
  const [items, setItems] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [branches, setBranches] = useState([]);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState(null);
  const [error, setError] = useState('');

  const load = () => getDispatchTemplates().then(r => setTemplates(r.data));

  useEffect(() => {
    load();
    getItems({ is_stock: 'true' }).then(r => setItems(r.data));
    getWarehouses().then(r => setWarehouses(r.data));
    getBranches().then(r => setBranches(r.data));
  }, []);

  const handleCreate = async (data) => {
    await createDispatchTemplate(data);
    setCreating(false);
    load();
  };

  const handleUpdate = async (data) => {
    await updateDispatchTemplate(editing.id, data);
    setEditing(null);
    load();
  };

  const handleDelete = async (id, name) => {
    if (!confirm(`Yakin hapus template "${name}"?`)) return;
    setError('');
    try {
      await deleteDispatchTemplate(id);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Tidak bisa dihapus');
    }
  };

  if (creating || editing) {
    return (
      <div className="card" style={{ maxWidth: '800px' }}>
        <h2 style={{ marginBottom: '1.5rem' }}>
          {editing ? `Edit Template — ${editing.name}` : 'Template Baru'}
        </h2>
        <TemplateForm
          initial={editing ?? undefined}
          items={items}
          warehouses={warehouses}
          branches={branches}
          onSave={editing ? handleUpdate : handleCreate}
          onCancel={() => { setCreating(false); setEditing(null); }}
        />
      </div>
    );
  }

  return (
    <>
      <div className="page-header">
        <h1>Template Pengiriman</h1>
        <button className="btn btn-primary" onClick={() => setCreating(true)}>+ Template Baru</button>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      <div className="card">
        <p style={{ color: '#666', fontSize: '0.9rem', marginBottom: '1.25rem' }}>
          Template menentukan tombol pintasan yang muncul saat membuat pengiriman baru. Setiap template bisa mengisi otomatis gudang asal, tujuan, dan daftar barang bawaan.
        </p>

        {templates.length === 0 ? (
          <div style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada template</div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Nama Template</th>
                <th>Gudang Asal</th>
                <th>Tujuan</th>
                <th>Barang Bawaan</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {templates.map(tpl => (
                <tr key={tpl.id}>
                  <td style={{ fontWeight: 600 }}>{tpl.name}</td>
                  <td style={{ color: '#555' }}>{tpl.warehouse_name || <span style={{ color: '#bbb' }}>—</span>}</td>
                  <td style={{ color: '#555' }}>
                    {tpl.branch_name
                      ? `${tpl.branch_name}${tpl.division_name ? ` / ${tpl.division_name}` : ''}`
                      : <span style={{ color: '#bbb' }}>—</span>}
                  </td>
                  <td style={{ color: '#666', fontSize: '0.85rem' }}>
                    {tpl.items?.length
                      ? tpl.items.map(it => it.item_name || '—').join(', ')
                      : <span style={{ color: '#bbb' }}>—</span>}
                  </td>
                  <td>
                    <div className="actions">
                      <button onClick={() => setEditing(tpl)} className="btn btn-secondary btn-sm">Edit</button>
                      <button onClick={() => handleDelete(tpl.id, tpl.name)} className="btn btn-danger btn-sm">Hapus</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
