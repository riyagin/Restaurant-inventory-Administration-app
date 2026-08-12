import { useEffect, useMemo, useState } from 'react';
import {
  getDispatchTemplates, createDispatchTemplate, updateDispatchTemplate, deleteDispatchTemplate,
  getDivisions,
} from '../../api';
import { PanelToolbar, FormCard, EmptyRow, Muted } from './shared';

// Dispatch templates: source warehouse, destination, and the goods usually sent.
// Quantity is never stored — same rule as the other two: a remembered quantity
// is a number nobody re-reads.

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
    let alive = true;
    const pending = branchId ? getDivisions({ branch_id: branchId }) : Promise.resolve({ data: [] });
    pending
      .then(r => { if (alive) setDivisions(r.data || []); })
      .catch(() => { if (alive) setDivisions([]); });
    return () => { alive = false; };
  }, [branchId]);

  const updateRow = (index, updated) => setRows(rs => rs.map((r, i) => i === index ? updated : r));
  const removeRow = (index) => setRows(rs => rs.filter((_, i) => i !== index));

  const setRowField = (index, field) => (e) => {
    const val = e.target.value;
    if (field === 'item_id') {
      // Default to the item's base unit (the last entry in items.units).
      const item = items.find(it => it.id === val);
      updateRow(index, { item_id: val, unit_index: item ? String(item.units.length - 1) : '0' });
    } else {
      updateRow(index, { ...rows[index], [field]: val });
    }
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!name.trim()) { setError('Nama template wajib diisi'); return; }
    const payloadItems = rows
      .filter(r => r.item_id)
      .map((r, idx) => ({ item_id: r.item_id, unit_index: Number(r.unit_index), sort_order: idx }));
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

      <div className="tpl-form-grid">
        <div className="form-group">
          <label>Nama Template</label>
          <input value={name} onChange={e => setName(e.target.value)} placeholder="Contoh: Kirim Harian Dapur" required />
        </div>
        <div className="form-group">
          <label>Gudang Asal <Muted>(opsional)</Muted></label>
          <select value={warehouseId} onChange={e => setWarehouseId(e.target.value)}>
            <option value="">Pilih gudang…</option>
            {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
          </select>
        </div>
        <div className="form-group">
          <label>Cabang Tujuan <Muted>(opsional)</Muted></label>
          <select value={branchId} onChange={e => { setBranchId(e.target.value); setDivisionId(''); }}>
            <option value="">Pilih cabang…</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
        </div>
        <div className="form-group">
          <label>Divisi Tujuan <Muted>(opsional)</Muted></label>
          <select value={divisionId} onChange={e => setDivisionId(e.target.value)} disabled={!branchId}>
            <option value="">{branchId ? 'Pilih divisi…' : 'Pilih cabang terlebih dahulu'}</option>
            {divisions.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
          </select>
        </div>
        <div className="form-group">
          <label>Catatan Bawaan <Muted>(opsional)</Muted></label>
          <input value={notes} onChange={e => setNotes(e.target.value)} placeholder="Alasan atau deskripsi…" />
        </div>
      </div>

      <div className="tpl-lines-head">
        Barang Bawaan Template <Muted>(opsional — bisa ditambah saat input pengiriman)</Muted>
      </div>
      <p className="tpl-lines-note">Jumlah tidak disimpan di template — diisi setiap kali membuat pengiriman.</p>

      <div style={{ overflowX: 'auto', marginBottom: '0.75rem' }}>
        <table className="invoice-items-table">
          <thead>
            <tr><th>Barang</th><th>Satuan</th><th></th></tr>
          </thead>
          <tbody>
            {rows.map((row, i) => {
              const selectedItem = items.find(it => it.id === row.item_id);
              return (
                <tr key={i} style={{ verticalAlign: 'top' }}>
                  <td style={{ minWidth: '220px' }}>
                    <select value={row.item_id} onChange={setRowField(i, 'item_id')} style={{ width: '100%' }}>
                      <option value="">Pilih barang…</option>
                      {items.map(it => <option key={it.id} value={it.id}>{it.name}</option>)}
                    </select>
                  </td>
                  <td style={{ minWidth: '110px' }}>
                    {selectedItem ? (
                      <select value={row.unit_index} onChange={setRowField(i, 'unit_index')} style={{ width: '100%' }}>
                        {selectedItem.units.map((u, ui) => <option key={ui} value={String(ui)}>{u.name}</option>)}
                      </select>
                    ) : (
                      <select disabled style={{ width: '100%' }}><option>—</option></select>
                    )}
                  </td>
                  <td style={{ width: '40px', textAlign: 'center' }}>
                    <button type="button" onClick={() => removeRow(i)} className="btn btn-danger btn-sm" title="Hapus baris">✕</button>
                  </td>
                </tr>
              );
            })}
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

export default function DispatchPanel({ master, onCount }) {
  const { items, warehouses, branches } = master;
  const [templates, setTemplates] = useState([]);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState(null);
  const [error, setError] = useState('');

  // Only stock items can be dispatched — a non-stock item has no lots to move.
  const stockItems = useMemo(() => items.filter(i => i.is_stock !== false), [items]);

  const load = () => getDispatchTemplates()
    .then(r => { setTemplates(r.data || []); onCount(r.data?.length || 0); })
    .catch(() => setError('Gagal memuat template'));

  useEffect(() => { load(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const close = () => { setCreating(false); setEditing(null); };

  const handleSave = async (data) => {
    if (editing) await updateDispatchTemplate(editing.id, data);
    else await createDispatchTemplate(data);
    close();
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

  const formOpen = creating || !!editing;

  return (
    <>
      <PanelToolbar
        hint="Pintasan yang muncul saat membuat pengiriman baru — mengisi otomatis gudang asal, tujuan, dan daftar barang bawaan."
        open={formOpen}
        onNew={() => (formOpen ? close() : setCreating(true))}
      />

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      {formOpen && (
        <FormCard title={editing ? `Edit Template — ${editing.name}` : 'Template Pengiriman Baru'}>
          <TemplateForm
            key={editing?.id || 'new'}
            initial={editing ?? undefined}
            items={stockItems}
            warehouses={warehouses}
            branches={branches}
            onSave={handleSave}
            onCancel={close}
          />
        </FormCard>
      )}

      <div className="card">
        <div style={{ overflowX: 'auto' }}>
          <table>
            <thead>
              <tr><th>Nama Template</th><th>Gudang Asal</th><th>Tujuan</th><th>Barang Bawaan</th><th></th></tr>
            </thead>
            <tbody>
              {templates.length === 0 ? <EmptyRow cols={5} /> : templates.map(tpl => (
                <tr key={tpl.id}>
                  <td style={{ fontWeight: 600 }}>{tpl.name}</td>
                  <td style={{ color: 'var(--ink-2)' }}>{tpl.warehouse_name || <Muted>—</Muted>}</td>
                  <td style={{ color: 'var(--ink-2)' }}>
                    {tpl.branch_name
                      ? `${tpl.branch_name}${tpl.division_name ? ` / ${tpl.division_name}` : ''}`
                      : <Muted>—</Muted>}
                  </td>
                  <td style={{ color: 'var(--ink-2)', fontSize: '0.85rem' }}>
                    {tpl.items?.length ? tpl.items.map(it => it.item_name || '—').join(', ') : <Muted>—</Muted>}
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
