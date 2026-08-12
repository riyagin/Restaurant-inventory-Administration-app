import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getDailyPurchaseTemplates, createDailyPurchaseTemplate,
  updateDailyPurchaseTemplate, deleteDailyPurchaseTemplate, getDivisions,
} from '../../api';
import SearchSelect from '../../components/SearchSelect';
import { PanelToolbar, FormCard, EmptyRow, Muted } from './shared';

// Templates for the shopping runs that repeat.
//
// A template stores the skeleton — branch, receiving warehouse, and the lines in
// the units they are usually bought in. It stores no quantity and no price on
// purpose: a template that remembered "12 kg at 18.000" would invite someone to
// accept last month's price unread, and that stale figure would land straight in
// inventory value and the branch's expenses.

const emptyLine = () => ({ item_id: '', description: '', unit_index: 0 });
const emptyForm = () => ({
  name: '', branch_id: '', division_id: '', warehouse_id: '', vendor_id: '', notes: '',
});

export default function DailyPurchasePanel({ master, onCount }) {
  const { items, branches, warehouses, vendors } = master;
  const [entries, setEntries] = useState([]);
  const [divisions, setDivisions] = useState([]);

  const [editId, setEditId] = useState(null);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState(emptyForm());
  const [lines, setLines] = useState([emptyLine()]);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  const load = () =>
    getDailyPurchaseTemplates()
      .then(r => { setEntries(r.data || []); onCount(r.data?.length || 0); })
      .catch(() => setError('Gagal memuat template'));

  useEffect(() => { load(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    let alive = true;
    const pending = form.branch_id
      ? getDivisions({ branch_id: form.branch_id })
      : Promise.resolve({ data: [] });
    pending
      .then(r => { if (alive) setDivisions(r.data || []); })
      .catch(() => { if (alive) setDivisions([]); });
    return () => { alive = false; };
  }, [form.branch_id]);

  const itemById = useMemo(() => new Map(items.map(i => [i.id, i])), [items]);

  const reset = () => {
    setForm(emptyForm());
    setLines([emptyLine()]);
    setEditId(null);
    setShowForm(false);
    setError('');
  };

  const startEdit = (entry) => {
    const t = entry.template;
    setEditId(t.id);
    setShowForm(true);
    setError('');
    setForm({
      name: t.name,
      branch_id: t.branch_id || '',
      division_id: t.division_id || '',
      warehouse_id: t.warehouse_id || '',
      vendor_id: t.vendor_id || '',
      notes: t.notes || '',
    });
    setLines((entry.items || []).length
      ? entry.items.map(i => ({
          item_id: i.item_id || '',
          description: i.description || '',
          unit_index: i.unit_index ?? 0,
        }))
      : [emptyLine()]);
  };

  const setLine = (idx, patch) =>
    setLines(ls => ls.map((l, i) => (i === idx ? { ...l, ...patch } : l)));

  const submit = async (e) => {
    e.preventDefault();
    setError('');
    const payload = {
      ...form,
      items: lines.filter(l => l.item_id || l.description.trim()),
    };
    if (payload.items.length === 0) { setError('Minimal satu baris'); return; }

    setSaving(true);
    try {
      if (editId) await updateDailyPurchaseTemplate(editId, payload);
      else await createDailyPurchaseTemplate(payload);
      reset();
      await load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan template');
    } finally {
      setSaving(false);
    }
  };

  const remove = async (entry) => {
    if (!confirm(`Hapus template "${entry.template.name}"?`)) return;
    try {
      await deleteDailyPurchaseTemplate(entry.template.id);
      if (editId === entry.template.id) reset();
      await load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menghapus template');
    }
  };

  return (
    <>
      <PanelToolbar
        hint="Kerangka belanja yang berulang — cabang, gudang penerima, dan barang dalam satuan yang biasa dibeli. Jumlah dan harga sengaja tidak disimpan."
        open={showForm}
        onNew={() => (showForm ? reset() : setShowForm(true))}
      />

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      {showForm && (
        <FormCard title={editId ? 'Edit Template Pembelanjaan' : 'Template Pembelanjaan Baru'}>
          <form onSubmit={submit}>
            <div className="tpl-form-grid">
              <div className="form-group">
                <label>Nama Template</label>
                <input value={form.name} required placeholder="mis. Belanja pasar Selasa"
                       onChange={e => setForm({ ...form, name: e.target.value })} />
              </div>
              <div className="form-group">
                <label>Cabang <Muted>(opsional)</Muted></label>
                <select value={form.branch_id}
                        onChange={e => setForm({ ...form, branch_id: e.target.value, division_id: '' })}>
                  <option value="">— pilih saat mencatat —</option>
                  {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
                </select>
              </div>
              <div className="form-group">
                <label>Divisi <Muted>(opsional)</Muted></label>
                <select value={form.division_id} disabled={!form.branch_id}
                        onChange={e => setForm({ ...form, division_id: e.target.value })}>
                  <option value="">{form.branch_id ? '— tanpa divisi —' : 'Pilih cabang terlebih dahulu'}</option>
                  {divisions.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
                </select>
              </div>
              <div className="form-group">
                <label>Gudang Penerima <Muted>(opsional)</Muted></label>
                <select value={form.warehouse_id}
                        onChange={e => setForm({ ...form, warehouse_id: e.target.value })}>
                  <option value="">— pilih saat mencatat —</option>
                  {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
                </select>
              </div>
              <div className="form-group">
                <label>Vendor <Muted>(opsional)</Muted></label>
                <select value={form.vendor_id}
                        onChange={e => setForm({ ...form, vendor_id: e.target.value })}>
                  <option value="">— tanpa vendor —</option>
                  {vendors.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}
                </select>
              </div>
              <div className="form-group">
                <label>Catatan Bawaan <Muted>(opsional)</Muted></label>
                <input value={form.notes} onChange={e => setForm({ ...form, notes: e.target.value })} />
              </div>
            </div>

            <div className="tpl-lines-head">
              Barang Bawaan Template <Muted>(barang katalog atau keterangan bebas)</Muted>
            </div>

            <div style={{ overflowX: 'auto', marginBottom: '0.75rem' }}>
              <table className="invoice-items-table">
                <thead>
                  <tr>
                    <th style={{ width: '55%' }}>Barang / keterangan</th>
                    <th style={{ width: '35%' }}>Satuan biasa</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {lines.map((l, idx) => {
                    const units = itemById.get(l.item_id)?.units || [];
                    return (
                      <tr key={idx} style={{ verticalAlign: 'top' }}>
                        <td style={{ minWidth: '220px' }}>
                          <SearchSelect
                            options={items.map(i => ({ value: i.id, label: i.name, sub: i.code || '' }))}
                            value={l.item_id}
                            placeholder="Cari barang atau ketik keterangan…"
                            onChange={(v) => setLine(idx, { item_id: v || '', unit_index: 0 })}
                          />
                          {!l.item_id && (
                            <input value={l.description} placeholder="Keterangan"
                                   onChange={e => setLine(idx, { description: e.target.value })}
                                   style={{ width: '100%', marginTop: '0.35rem' }} />
                          )}
                        </td>
                        <td style={{ minWidth: '110px' }}>
                          {units.length > 0 ? (
                            <select value={l.unit_index} style={{ width: '100%' }}
                                    onChange={e => setLine(idx, { unit_index: Number(e.target.value) })}>
                              {units.map((u, ui) => <option key={ui} value={ui}>{u.name}</option>)}
                            </select>
                          ) : <select disabled style={{ width: '100%' }}><option>—</option></select>}
                        </td>
                        <td style={{ width: '40px', textAlign: 'center' }}>
                          {lines.length > 1 && (
                            <button type="button" className="btn btn-danger btn-sm" title="Hapus baris"
                                    onClick={() => setLines(ls => ls.filter((_, i) => i !== idx))}>✕</button>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            <button type="button" className="btn btn-secondary btn-sm"
                    onClick={() => setLines(ls => [...ls, emptyLine()])}>+ Tambah Baris</button>

            <div className="form-actions">
              <button type="submit" className="btn btn-primary" disabled={saving}>
                {saving ? 'Menyimpan…' : editId ? 'Simpan Perubahan' : 'Buat Template'}
              </button>
              <button type="button" className="btn btn-secondary" onClick={reset}>Batal</button>
            </div>
          </form>
        </FormCard>
      )}

      <div className="card">
        <div style={{ overflowX: 'auto' }}>
          <table>
            <thead>
              <tr>
                <th>Nama Template</th><th>Cabang</th><th>Gudang</th><th>Vendor</th>
                <th>Barang Bawaan</th><th></th>
              </tr>
            </thead>
            <tbody>
              {entries.length === 0 ? <EmptyRow cols={6} /> : entries.map(entry => {
                const t = entry.template;
                return (
                  <tr key={t.id}>
                    <td style={{ fontWeight: 600 }}>{t.name}</td>
                    <td style={{ color: 'var(--ink-2)' }}>{t.branch_name || <Muted>—</Muted>}</td>
                    <td style={{ color: 'var(--ink-2)' }}>{t.warehouse_name || <Muted>—</Muted>}</td>
                    <td style={{ color: 'var(--ink-2)' }}>{t.vendor_name || <Muted>—</Muted>}</td>
                    <td style={{ color: 'var(--ink-2)', fontSize: '0.85rem' }}>
                      {entry.items?.length
                        ? entry.items.map(i => i.item_name || i.description || '—').join(', ')
                        : `${t.line_count || 0} baris`}
                    </td>
                    <td>
                      <div className="actions">
                        <Link to={`/daily-purchases/new?template=${t.id}`} className="btn btn-primary btn-sm">Gunakan</Link>
                        <button onClick={() => startEdit(entry)} className="btn btn-secondary btn-sm">Edit</button>
                        <button onClick={() => remove(entry)} className="btn btn-danger btn-sm">Hapus</button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </>
  );
}
