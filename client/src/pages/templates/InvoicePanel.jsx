import { useEffect, useMemo, useState } from 'react';
import {
  getInvoiceTemplates, createInvoiceTemplate, updateInvoiceTemplate, deleteInvoiceTemplate,
} from '../../api';
import { PanelToolbar, FormCard, EmptyRow, Muted } from './shared';

// Invoice templates: the shortcut buttons offered when a new invoice is started.
// A purchase template lists stock items, an expense template non-stock ones —
// mixing them would let a template add stock through a path that never touches
// inventory.

const TYPE_LABELS = { purchase: 'Pembelian (tambah stok)', expense: 'Pengeluaran (tanpa stok)' };

const emptyRow = () => ({ item_id: '', description: '', unit_index: '0', useDescription: false });

function ItemRow({ row, index, invoiceType, itemList, onUpdate, onRemove }) {
  const selectedItem = itemList.find(it => it.id === row.item_id);

  const setField = (field) => (e) => {
    const val = e.target.value;
    if (field === 'item_id') {
      const item = itemList.find(it => it.id === val);
      // Default to the item's base unit (the last entry in items.units).
      onUpdate(index, { ...row, item_id: val, unit_index: item ? String(item.units.length - 1) : '0' });
    } else {
      onUpdate(index, { ...row, [field]: val });
    }
  };

  return (
    <tr style={{ verticalAlign: 'top' }}>
      <td style={{ minWidth: '220px' }}>
        {invoiceType === 'expense' && (
          <div className="tpl-mode-toggle">
            <button type="button" className={!row.useDescription ? 'on' : ''}
                    onClick={() => onUpdate(index, { ...emptyRow(), useDescription: false })}>Daftar</button>
            <button type="button" className={row.useDescription ? 'on' : ''}
                    onClick={() => onUpdate(index, { ...emptyRow(), useDescription: true })}>Manual</button>
          </div>
        )}
        {row.useDescription ? (
          <input value={row.description} onChange={setField('description')}
                 placeholder="Nama item…" style={{ width: '100%' }} />
        ) : (
          <select value={row.item_id} onChange={setField('item_id')} style={{ width: '100%' }}>
            <option value="">Pilih item…</option>
            {itemList.map(it => <option key={it.id} value={it.id}>{it.name}</option>)}
          </select>
        )}
      </td>
      <td style={{ minWidth: '110px' }}>
        {invoiceType === 'expense' && <div className="tpl-row-spacer" />}
        {!row.useDescription && selectedItem ? (
          <select value={row.unit_index} onChange={setField('unit_index')} style={{ width: '100%' }}>
            {selectedItem.units.map((u, ui) => <option key={ui} value={String(ui)}>{u.name}</option>)}
          </select>
        ) : (
          <select disabled style={{ width: '100%' }}><option>—</option></select>
        )}
      </td>
      <td style={{ width: '40px', textAlign: 'center' }}>
        {invoiceType === 'expense' && <div className="tpl-row-spacer" />}
        <button type="button" onClick={() => onRemove(index)} className="btn btn-danger btn-sm" title="Hapus baris">✕</button>
      </td>
    </tr>
  );
}

function TemplateForm({ initial, stockItems, nonStockItems, vendors, warehouses, onSave, onCancel }) {
  const [name, setName] = useState(initial?.name ?? '');
  const [invoiceType, setInvoiceType] = useState(initial?.invoice_type ?? 'expense');
  const [vendorId, setVendorId] = useState(initial?.vendor_id ?? '');
  const [warehouseId, setWarehouseId] = useState(initial?.warehouse_id ?? '');
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

  const itemList = invoiceType === 'purchase' ? stockItems : nonStockItems;

  // Switching type changes which catalogue the rows are drawn from, so the rows
  // and the type-specific defaults are cleared rather than left pointing at
  // items the new type cannot use.
  const handleTypeChange = (newType) => {
    setInvoiceType(newType);
    setWarehouseId('');
    setVendorId('');
    setRows([emptyRow()]);
  };

  const updateRow = (index, updated) => setRows(rs => rs.map((r, i) => i === index ? updated : r));
  const removeRow = (index) => setRows(rs => rs.filter((_, i) => i !== index));

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
      await onSave({
        name: name.trim(),
        invoice_type: invoiceType,
        vendor_id: vendorId || null,
        warehouse_id: warehouseId || null,
        items,
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

      <div className="tpl-form-grid">
        <div className="form-group">
          <label>Nama Template</label>
          <input value={name} onChange={e => setName(e.target.value)} placeholder="Contoh: Belanja Harian" required />
        </div>
        <div className="form-group">
          <label>Tipe Invoice</label>
          <select value={invoiceType} onChange={e => handleTypeChange(e.target.value)}>
            <option value="expense">Pengeluaran (tanpa stok)</option>
            <option value="purchase">Pembelian (tambah stok)</option>
          </select>
        </div>
        {invoiceType === 'purchase' && (
          <>
            <div className="form-group">
              <label>Gudang Default <Muted>(opsional)</Muted></label>
              <select value={warehouseId} onChange={e => setWarehouseId(e.target.value)}>
                <option value="">Pilih gudang…</option>
                {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
              </select>
            </div>
            <div className="form-group">
              <label>Vendor Default <Muted>(opsional)</Muted></label>
              <select value={vendorId} onChange={e => setVendorId(e.target.value)}>
                <option value="">Pilih vendor…</option>
                {vendors.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}
              </select>
            </div>
          </>
        )}
      </div>

      <div className="tpl-lines-head">
        Item Bawaan Template <Muted>(opsional — bisa ditambah saat input invoice)</Muted>
      </div>
      <p className="tpl-lines-note">
        {invoiceType === 'purchase'
          ? 'Hanya menampilkan item stok.'
          : 'Hanya menampilkan item non-stok. Gunakan mode Manual untuk item bebas.'}
      </p>

      <div style={{ overflowX: 'auto', marginBottom: '0.75rem' }}>
        <table className="invoice-items-table">
          <thead>
            <tr><th>Item</th><th>Satuan</th><th></th></tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <ItemRow key={i} row={row} index={i} invoiceType={invoiceType} itemList={itemList}
                       onUpdate={updateRow} onRemove={removeRow} />
            ))}
          </tbody>
        </table>
      </div>

      <button type="button" onClick={() => setRows(rs => [...rs, emptyRow()])} className="btn btn-secondary btn-sm">
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

export default function InvoicePanel({ master, onCount }) {
  const { items, vendors, warehouses } = master;
  const [templates, setTemplates] = useState([]);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState(null);
  const [error, setError] = useState('');

  const stockItems = useMemo(() => items.filter(i => i.is_stock !== false), [items]);
  const nonStockItems = useMemo(() => items.filter(i => i.is_stock === false), [items]);

  const load = () => getInvoiceTemplates()
    .then(r => { setTemplates(r.data || []); onCount(r.data?.length || 0); })
    .catch(() => setError('Gagal memuat template'));

  useEffect(() => { load(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const close = () => { setCreating(false); setEditing(null); };

  const handleSave = async (data) => {
    if (editing) await updateInvoiceTemplate(editing.id, data);
    else await createInvoiceTemplate(data);
    close();
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

  const formOpen = creating || !!editing;

  return (
    <>
      <PanelToolbar
        hint="Pintasan yang muncul saat membuat invoice baru. Setiap template bisa membawa gudang, vendor, dan daftar item bawaan."
        open={formOpen}
        onNew={() => (formOpen ? close() : setCreating(true))}
      />

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      {formOpen && (
        <FormCard title={editing ? `Edit Template — ${editing.name}` : 'Template Invoice Baru'}>
          <TemplateForm
            key={editing?.id || 'new'}
            initial={editing ?? undefined}
            stockItems={stockItems}
            nonStockItems={nonStockItems}
            vendors={vendors}
            warehouses={warehouses}
            onSave={handleSave}
            onCancel={close}
          />
        </FormCard>
      )}

      <div className="card">
        <div style={{ overflowX: 'auto' }}>
          <table>
            <thead>
              <tr><th>Nama Template</th><th>Tipe</th><th>Item Bawaan</th><th></th></tr>
            </thead>
            <tbody>
              {templates.length === 0 ? <EmptyRow cols={4} /> : templates.map(tpl => (
                <tr key={tpl.id}>
                  <td style={{ fontWeight: 600 }}>{tpl.name}</td>
                  <td>
                    <span className={`tpl-type-badge ${tpl.invoice_type === 'purchase' ? 'purchase' : 'expense'}`}>
                      {TYPE_LABELS[tpl.invoice_type]}
                    </span>
                  </td>
                  <td style={{ color: 'var(--ink-2)', fontSize: '0.85rem' }}>
                    {tpl.items?.length
                      ? tpl.items.map(it => it.item_name || it.description || '—').join(', ')
                      : <Muted>—</Muted>}
                  </td>
                  <td>
                    <div className="actions">
                      <button onClick={() => { setCreating(false); setEditing(tpl); }} className="btn btn-secondary btn-sm">Edit</button>
                      <button onClick={() => handleDelete(tpl.id, tpl.name)} className="btn btn-danger btn-sm">Hapus</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </>
  );
}
