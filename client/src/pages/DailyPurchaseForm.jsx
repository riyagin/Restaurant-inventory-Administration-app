import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import {
  createDailyPurchase, getDailyPurchaseTemplates,
  getBranches, getDivisions, getWarehouses, getVendors, getItems,
} from '../api';
import CurrencyInput from '../components/CurrencyInput';
import SearchSelect from '../components/SearchSelect';

// Recording one shopping run, on its own page.
//
// It used to be a panel that unfolded above the list, which meant the list
// re-rendered under every keystroke and a half-filled run was lost the moment
// anyone touched a filter. A shopping run has a header, a variable number of
// lines and a running total — that is a page, not a panel.

const rp = (n) => 'Rp ' + Number(n || 0).toLocaleString('id-ID');
const today = () => new Date().toISOString().slice(0, 10);
const emptyLine = () => ({ item_id: '', description: '', quantity: '', unit_index: 0, price: '' });

export default function DailyPurchaseForm() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const templateId = params.get('template');

  const [branches, setBranches] = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [vendors, setVendors] = useState([]);
  const [items, setItems] = useState([]);
  const [templates, setTemplates] = useState([]);

  const [form, setForm] = useState({
    date: today(), branch_id: '', division_id: '', warehouse_id: '',
    vendor_id: '', notes: '',
  });
  const [lines, setLines] = useState([emptyLine()]);
  const [error, setError] = useState('');
  const [warning, setWarning] = useState('');
  const [saving, setSaving] = useState(false);

  // Applying a template fills the header and the lines, leaving quantity and
  // price blank — those are exactly the two fields a template must not answer.
  const applyTemplate = (entry) => {
    if (!entry) return;
    const t = entry.template;
    setForm(f => ({
      ...f,
      branch_id: t.branch_id || '',
      division_id: t.division_id || '',
      warehouse_id: t.warehouse_id || '',
      vendor_id: t.vendor_id || '',
      notes: t.notes || f.notes,
    }));
    setLines(
      (entry.items || []).length
        ? entry.items.map(i => ({
            item_id: i.item_id || '',
            description: i.description || '',
            unit_index: i.unit_index ?? 0,
            quantity: '',
            price: '',
          }))
        : [emptyLine()],
    );
  };

  // A ?template= deep link is applied inside the fetch callback rather than in a
  // second effect watching `templates`. Doing it in an effect body would set
  // state synchronously on every render that touches the list, and it only ever
  // needs to happen once — when the templates first arrive.
  useEffect(() => {
    let alive = true;
    Promise.all([getBranches(), getWarehouses(), getVendors(), getItems(), getDailyPurchaseTemplates()])
      .then(([b, w, v, i, t]) => {
        if (!alive) return;
        setBranches(b.data || []);
        setWarehouses(w.data || []);
        setVendors(v.data || []);
        setItems(i.data || []);
        const loaded = t.data || [];
        setTemplates(loaded);
        if (templateId) applyTemplate(loaded.find(x => x.template.id === templateId));
      })
      .catch(() => { if (alive) setError('Gagal memuat data master'); });
    return () => { alive = false; };
  }, [templateId]);

  // The `alive` guard settles the race when the branch is switched twice
  // quickly: a slow first response can otherwise land after a fast second one
  // and repopulate the list with the previous branch's divisions.
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

  const branch = branches.find(b => b.id === form.branch_id);
  const itemById = useMemo(() => new Map(items.map(i => [i.id, i])), [items]);

  const total = lines.reduce((sum, l) =>
    sum + (Number(l.quantity) || 0) * (Number(l.price) || 0), 0);

  // The warehouse field appears — and is required — only once a stock item is on
  // the form. A run of pure services has nowhere to receive into.
  const needsWarehouse = lines.some(l => l.item_id && itemById.get(l.item_id)?.is_stock);

  const setLine = (idx, patch) =>
    setLines(ls => ls.map((l, i) => (i === idx ? { ...l, ...patch } : l)));

  const submit = async (e) => {
    e.preventDefault();
    setError('');
    setWarning('');
    if (!form.branch_id) { setError('Cabang wajib dipilih'); return; }

    const payload = {
      ...form,
      items: lines
        .filter(l => (Number(l.quantity) || 0) > 0 && (Number(l.price) || 0) > 0)
        .map(l => ({
          item_id: l.item_id || '',
          description: l.description || '',
          quantity: Number(l.quantity),
          unit_index: Number(l.unit_index) || 0,
          price: Math.round(Number(l.price)),
        })),
    };
    if (payload.items.length === 0) { setError('Minimal satu baris dengan jumlah dan harga'); return; }

    setSaving(true);
    try {
      const res = await createDailyPurchase(payload);
      const balance = res.data?.petty_cash_balance;
      if (balance != null && balance < 0) {
        // Saved either way — the money left the box whether or not a top-up was
        // recorded. Stay on the page so the warning is actually read.
        setWarning(`Tersimpan, tetapi kas kecil cabang ini kini minus ${rp(Math.abs(balance))}. `
          + 'Kemungkinan ada pengisian kas yang belum dicatat di Setoran.');
        setLines([emptyLine()]);
        setSaving(false);
        return;
      }
      navigate('/daily-purchases');
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan pembelanjaan');
      setSaving(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Catat Pembelanjaan Harian</h1>
        <Link to="/daily-purchases" className="btn btn-secondary">Kembali</Link>
      </div>

      {error && <div className="error-msg" style={{marginBottom:'1rem'}}>{error}</div>}
      {warning && (
        <div className="card" style={{marginBottom:'1rem',borderLeft:'4px solid #e67e22',color:'#a05a1c'}}>
          {warning} <Link to="/setoran">Catat setoran</Link>
        </div>
      )}

      <div className="card">
        <form onSubmit={submit}>
          {templates.length > 0 && (
            <div style={{marginBottom:'1rem',paddingBottom:'1rem',borderBottom:'1px solid #eee'}}>
              <label style={{fontSize:'0.82rem',color:'#555'}}>
                <div style={{marginBottom:'0.3rem'}}>Mulai dari template</div>
                <select
                  style={{...fieldStyle, maxWidth:'340px'}}
                  defaultValue=""
                  onChange={e => applyTemplate(templates.find(t => t.template.id === e.target.value))}
                >
                  <option value="">— tanpa template —</option>
                  {templates.map(t => (
                    <option key={t.template.id} value={t.template.id}>
                      {t.template.name} ({t.items.length} baris)
                    </option>
                  ))}
                </select>
              </label>
              <span style={{marginLeft:'0.75rem',fontSize:'0.82rem',color:'#999'}}>
                Jumlah dan harga tetap kosong — isi sesuai belanja hari ini.
              </span>
            </div>
          )}

          <div style={{display:'grid',gridTemplateColumns:'repeat(auto-fit,minmax(190px,1fr))',
                       gap:'0.75rem',marginBottom:'1rem'}}>
            <Field label="Tanggal">
              <input type="date" value={form.date} required style={fieldStyle}
                     onChange={e => setForm({...form, date: e.target.value})} />
            </Field>
            <Field label="Cabang" hint={branch ? `kas kecil ${rp(branch.petty_cash_balance)}` : ''}>
              <select value={form.branch_id} required style={fieldStyle}
                      onChange={e => setForm({...form, branch_id: e.target.value, division_id: ''})}>
                <option value="">— pilih cabang —</option>
                {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
              </select>
            </Field>
            <Field label="Divisi (opsional)">
              <select value={form.division_id} style={fieldStyle} disabled={!form.branch_id}
                      onChange={e => setForm({...form, division_id: e.target.value})}>
                <option value="">— tanpa divisi —</option>
                {divisions.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
              </select>
            </Field>
            {needsWarehouse && (
              <Field label="Gudang penerima" hint="wajib karena ada barang stok">
                <select value={form.warehouse_id} required style={fieldStyle}
                        onChange={e => setForm({...form, warehouse_id: e.target.value})}>
                  <option value="">— pilih gudang —</option>
                  {warehouses.map(wh => <option key={wh.id} value={wh.id}>{wh.name}</option>)}
                </select>
              </Field>
            )}
            <Field label="Vendor (opsional)">
              <select value={form.vendor_id} style={fieldStyle}
                      onChange={e => setForm({...form, vendor_id: e.target.value})}>
                <option value="">— tanpa vendor —</option>
                {vendors.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}
              </select>
            </Field>
            <Field label="Catatan">
              <input value={form.notes} placeholder="mis. belanja pasar pagi" style={fieldStyle}
                     onChange={e => setForm({...form, notes: e.target.value})} />
            </Field>
          </div>

          <table style={{marginBottom:'0.75rem'}}>
            <thead>
              <tr>
                <th style={{width:'34%'}}>Barang / keterangan</th>
                <th style={{width:'16%'}}>Satuan</th>
                <th style={{width:'14%'}}>Jumlah</th>
                <th style={{width:'18%'}}>Harga satuan</th>
                <th style={{width:'14%'}}>Subtotal</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {lines.map((l, idx) => {
                const item = itemById.get(l.item_id);
                const units = item?.units || [];
                return (
                  <tr key={idx}>
                    <td>
                      <SearchSelect
                        options={items.map(i => ({ value: i.id, label: i.name, sub: i.code || '' }))}
                        value={l.item_id}
                        placeholder="Cari barang atau ketik keterangan…"
                        onChange={(v) => setLine(idx, { item_id: v || '', unit_index: 0 })}
                      />
                      {!l.item_id && (
                        <input value={l.description} placeholder="Keterangan (wajib jika tanpa barang)"
                               onChange={e => setLine(idx, { description: e.target.value })}
                               style={{...fieldStyle, marginTop:'0.35rem'}} />
                      )}
                    </td>
                    <td>
                      {units.length > 0 ? (
                        <select value={l.unit_index} style={fieldStyle}
                                onChange={e => setLine(idx, { unit_index: Number(e.target.value) })}>
                          {units.map((u, ui) => <option key={ui} value={ui}>{u.name}</option>)}
                        </select>
                      ) : <span style={{color:'#bbb'}}>—</span>}
                    </td>
                    <td>
                      <input type="number" step="any" min="0" value={l.quantity} style={fieldStyle}
                             onChange={e => setLine(idx, { quantity: e.target.value })} />
                    </td>
                    <td>
                      <CurrencyInput value={l.price} style={fieldStyle}
                                     onChange={e => setLine(idx, { price: e.target.value })} />
                    </td>
                    <td style={{fontWeight:600}}>
                      {rp((Number(l.quantity) || 0) * (Number(l.price) || 0))}
                    </td>
                    <td>
                      {lines.length > 1 && (
                        <button type="button" className="btn btn-danger btn-sm"
                                onClick={() => setLines(ls => ls.filter((_, i) => i !== idx))}>×</button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          <div style={{display:'flex',justifyContent:'space-between',alignItems:'center',
                       flexWrap:'wrap',gap:'0.75rem'}}>
            <button type="button" className="btn btn-secondary btn-sm"
                    onClick={() => setLines(ls => [...ls, emptyLine()])}>+ Baris</button>
            <div style={{display:'flex',gap:'1rem',alignItems:'center'}}>
              <strong style={{fontSize:'1.05rem'}}>Total: {rp(total)}</strong>
              <button type="submit" className="btn btn-primary" disabled={saving}>
                {saving ? 'Menyimpan…' : 'Simpan'}
              </button>
            </div>
          </div>
        </form>
      </div>
    </>
  );
}

const fieldStyle = {
  padding: '0.45rem 0.6rem', border: '1px solid #ddd',
  borderRadius: '6px', fontSize: '0.9rem', width: '100%',
};

function Field({ label, hint, children }) {
  return (
    <label>
      <div style={{fontSize:'0.82rem',color:'#555',marginBottom:'0.3rem'}}>
        {label}{hint && <span style={{color:'#999'}}> · {hint}</span>}
      </div>
      {children}
    </label>
  );
}
