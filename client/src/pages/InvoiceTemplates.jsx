import { useEffect, useState } from 'react';
import { getInvoiceTemplates, createInvoiceTemplate, updateInvoiceTemplate, deleteInvoiceTemplate, getItems } from '../api';

const TYPE_LABELS = { purchase: 'Pembelian (tambah stok)', expense: 'Pengeluaran (tanpa stok)' };

const emptyRow = () => ({ item_id: '', description: '', unit_index: '0', useDescription: false });

function TemplateItemRow({ row, index, allItems, onUpdate, onRemove, isLast }) {
  const selectedItem = allItems.find(it => it.id === row.item_id);

  const setField = (field) => (e) => {
    const val = e.target.value;
    if (field === 'item_id') {
      const item = allItems.find(it => it.id === val);
      onUpdate(index, { ...row, item_id: val, unit_index: item ? String(item.units.length - 1) : '0' });
    } else {
      onUpdate(index, { ...row, [field]: val });
    }
  };

  const toggleMode = (useDescription) => {
    onUpdate(index, { ...emptyRow(), useDescription });
  };

  return (
    <tr style={{ verticalAlign: 'top' }}>
      <td style={{ paddingTop: '0.3rem', minWidth: '220px' }}>
        <div style={{ display: 'flex', gap: '0.25rem', marginBottom: '0.3rem' }}>
          <button
            type="button"
            onClick={() => toggleMode(false)}
            style={{
              fontSize: '0.7rem', padding: '0.15rem 0.5rem', borderRadius: '4px', cursor: 'pointer',
              border: !row.useDescription ? '1.5px solid #4f8ef7' : '1.5px solid #e0e0e0',
              background: !row.useDescription ? '#e8f0fe' : '#f5f5f5',
              color: !row.useDescription ? '#4f8ef7' : '#888', fontWeight: 600,
            }}
          >Daftar</button>
          <button
            type="button"
            onClick={() => toggleMode(true)}
            style={{
              fontSize: '0.7rem', padding: '0.15rem 0.5rem', borderRadius: '4px', cursor: 'pointer',
              border: row.useDescription ? '1.5px solid #4f8ef7' : '1.5px solid #e0e0e0',
              background: row.useDescription ? '#e8f0fe' : '#f5f5f5',
              color: row.useDescription ? '#4f8ef7' : '#888', fontWeight: 600,
            }}
          >Manual</button>
        </div>
        {row.useDescription ? (
          <input
            value={row.description}
            onChange={setField('description')}
            placeholder="Nama item..."
            style={{ width: '100%' }}
          />
        ) : (
          <select value={row.item_id} onChange={setField('item_id')} style={{ width: '100%' }}>
            <option value="">Pilih item...</option>
            {allItems.map(it => (
              <option key={it.id} value={it.id}>{it.name} ({it.is_stock ? 'stok' : 'non-stok'})</option>
            ))}
          </select>
        )}
      </td>
      <td style={{ paddingTop: '0.3rem', minWidth: '110px' }}>
        <div style={{ height: '1.6rem', marginBottom: '0.3rem' }} />
        {!row.useDescription && selectedItem ? (
          <select value={row.unit_index} onChange={setField('unit_index')} style={{ width: '100%' }}>
            {selectedItem.units.map((u, ui) => (
              <option key={ui} value={String(ui)}>{u.name}</option>
            ))}
          </select>
        ) : (
          <select disabled style={{ width: '100%' }}>
            <option>—</option>
          </select>
        )}
      </td>
      <td style={{ paddingTop: '0.3rem', width: '40px', textAlign: 'center' }}>
        <div style={{ height: '1.6rem', marginBottom: '0.3rem' }} />
        <button type="button" onClick={() => onRemove(index)} className="btn btn-danger btn-sm" title="Hapus baris">✕</button>
      </td>
    </tr>
  );
}

function TemplateForm({ initial, allItems, onSave, onCancel }) {
  const [name, setName] = useState(initial?.name ?? '');
  const [invoiceType, setInvoiceType] = useState(initial?.invoice_type ?? 'expense');
  const [rows, setRows] = useState(() => {
    if (initial?.items?.length) {
      return initial.items.map(ti => ({
        item_id: ti.item_id ?? '',
        description: ti.description ?? '',
        unit_index: String(ti.unit_index ?? 0),
        useDescription: !ti.item_id && !!ti.description,
      }));
    }
    return [emptyRow()];
  });
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  const updateRow = (index, updated) => setRows(rs => rs.map((r, i) => i === index ? updated : r));
  const removeRow = (index) => setRows(rs => rs.filter((_, i) => i !== index));
  const addRow = () => setRows(rs => [...rs, emptyRow()]);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!name.trim()) { setError('Nama template wajib diisi'); return; }
    const items = rows.map((r, idx) => ({
      item_id: r.useDescription ? null : (r.item_id || null),
      description: r.useDescription ? (r.description.trim() || null) : null,
      unit_index: Number(r.unit_index),
      sort_order: idx,
    }));
    setSaving(true);
    try {
      await onSave({ name: name.trim(), invoice_type: invoiceType, items });
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
            placeholder="Contoh: Belanja Harian"
            required
          />
        </div>
        <div className="form-group" style={{ margin: 0, flex: 1 }}>
          <label>Tipe Invoice</label>
          <select value={invoiceType} onChange={e => setInvoiceType(e.target.value)}>
            <option value="expense">Pengeluaran (tanpa stok)</option>
            <option value="purchase">Pembelian (tambah stok)</option>
          </select>
        </div>
      </div>

      <div style={{ fontWeight: 600, fontSize: '0.85rem', color: '#444', marginBottom: '0.5rem' }}>
        Item Bawaan Template <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional — bisa ditambah saat input invoice)</span>
      </div>

      <div style={{ overflowX: 'auto', marginBottom: '0.75rem' }}>
        <table className="invoice-items-table">
          <thead>
            <tr>
              <th>Item</th>
              <th>Satuan</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <TemplateItemRow
                key={i}
                row={row}
                index={i}
                allItems={allItems}
                onUpdate={updateRow}
                onRemove={removeRow}
                isLast={rows.length === 1}
              />
            ))}
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

export default function InvoiceTemplates() {
  const [templates, setTemplates] = useState([]);
  const [allItems, setAllItems] = useState([]);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState(null);
  const [error, setError] = useState('');

  const load = () => getInvoiceTemplates().then(r => setTemplates(r.data));

  useEffect(() => {
    load();
    Promise.all([
      getItems({ is_stock: 'true' }),
      getItems({ is_stock: 'false' }),
    ]).then(([stk, nonstk]) => setAllItems([...stk.data, ...nonstk.data]));
  }, []);

  const handleCreate = async (data) => {
    await createInvoiceTemplate(data);
    setCreating(false);
    load();
  };

  const handleUpdate = async (data) => {
    await updateInvoiceTemplate(editing.id, data);
    setEditing(null);
    load();
  };

  const handleDelete = async (id, name) => {
    if (!confirm(`Yakin hapus template "${name}"?`)) return;
    setError('');
    try {
      await deleteInvoiceTemplate(id);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Tidak bisa dihapus');
    }
  };

  if (creating) {
    return (
      <div className="card" style={{ maxWidth: '800px' }}>
        <h2 style={{ marginBottom: '1.5rem' }}>Template Baru</h2>
        <TemplateForm
          allItems={allItems}
          onSave={handleCreate}
          onCancel={() => setCreating(false)}
        />
      </div>
    );
  }

  if (editing) {
    return (
      <div className="card" style={{ maxWidth: '800px' }}>
        <h2 style={{ marginBottom: '1.5rem' }}>Edit Template — {editing.name}</h2>
        <TemplateForm
          initial={editing}
          allItems={allItems}
          onSave={handleUpdate}
          onCancel={() => setEditing(null)}
        />
      </div>
    );
  }

  return (
    <>
      <div className="page-header">
        <h1>Template Invoice</h1>
        <button className="btn btn-primary" onClick={() => setCreating(true)}>+ Template Baru</button>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      <div className="card">
        <p style={{ color: '#666', fontSize: '0.9rem', marginBottom: '1.25rem' }}>
          Template menentukan tombol pintasan yang muncul saat membuat invoice baru. Setiap template bisa menyertakan daftar item bawaan yang langsung terisi otomatis.
        </p>

        {templates.length === 0 ? (
          <div style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Belum ada template</div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Nama Template</th>
                <th>Tipe</th>
                <th>Item Bawaan</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {templates.map(tpl => (
                <tr key={tpl.id}>
                  <td style={{ fontWeight: 600 }}>{tpl.name}</td>
                  <td>
                    <span style={{
                      display: 'inline-block', padding: '0.15rem 0.55rem', borderRadius: '4px', fontSize: '0.78rem', fontWeight: 600,
                      background: tpl.invoice_type === 'purchase' ? '#e8f5e9' : '#fff3e0',
                      color: tpl.invoice_type === 'purchase' ? '#2e7d32' : '#e65100',
                    }}>
                      {TYPE_LABELS[tpl.invoice_type]}
                    </span>
                  </td>
                  <td style={{ color: '#666', fontSize: '0.85rem' }}>
                    {tpl.items?.length
                      ? tpl.items.map(it => it.item_name || it.description || '—').join(', ')
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
