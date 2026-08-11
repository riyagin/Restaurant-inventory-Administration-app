import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getDailyPurchases, createDailyPurchase, cancelDailyPurchase,
  getBranches, getDivisions, getWarehouses, getVendors, getItems,
} from '../api';
import { isAdminRole } from '../roles';
import CurrencyInput from '../components/CurrencyInput';
import SearchSelect from '../components/SearchSelect';

// Pembelanjaan Harian — the branch's daily shopping, paid from its cash box.
//
// The form is a purchase form with the payment half removed: no vendor credit,
// no due date, no payment status, because the money already changed hands at the
// stall. What it gains instead is the box balance, shown live, because the
// number that matters to whoever is filling this in is "how much is left".

const rp = (n) => 'Rp ' + Number(n || 0).toLocaleString('id-ID');
const today = () => new Date().toISOString().slice(0, 10);
const emptyLine = () => ({ item_id: '', description: '', quantity: '', unit_index: 0, price: '' });

export default function DailyPurchases() {
  const isAdmin = isAdminRole();

  const [rows, setRows] = useState([]);
  const [branches, setBranches] = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [vendors, setVendors] = useState([]);
  const [items, setItems] = useState([]);

  const [filterBranch, setFilterBranch] = useState('');
  const [from, setFrom] = useState(today());
  const [to, setTo] = useState(today());

  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({
    date: today(), branch_id: '', division_id: '', warehouse_id: '',
    vendor_id: '', notes: '',
  });
  const [lines, setLines] = useState([emptyLine()]);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() =>
    getDailyPurchases({ branch_id: filterBranch || undefined, from, to })
      .then(r => setRows(r.data || []))
      .catch(() => setError('Gagal memuat pembelanjaan'))
      .finally(() => setLoading(false)),
  [filterBranch, from, to]);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    Promise.all([getBranches(), getWarehouses(), getVendors(), getItems()])
      .then(([b, w, v, i]) => {
        setBranches(b.data || []);
        setWarehouses(w.data || []);
        setVendors(v.data || []);
        setItems(i.data || []);
      })
      .catch(() => {});
  }, []);

  // Divisions are branch-scoped, and an expense posted to another branch's
  // division would debit books this purchase does not belong to.
  // The `alive` guard also settles the race when the branch is switched twice
  // quickly: without it a slow first response can land after a fast second one
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

  // A stock line needs somewhere to land; the warehouse field only appears —
  // and is only required — once one is on the form.
  const needsWarehouse = lines.some(l => l.item_id && itemById.get(l.item_id)?.is_stock);

  const setLine = (idx, patch) =>
    setLines(ls => ls.map((l, i) => (i === idx ? { ...l, ...patch } : l)));

  const resetForm = () => {
    setForm({ date: today(), branch_id: '', division_id: '', warehouse_id: '', vendor_id: '', notes: '' });
    setLines([emptyLine()]);
    setError('');
  };

  const submit = async (e) => {
    e.preventDefault();
    setError('');
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
        setError(`Tersimpan, tetapi kas kecil cabang ini kini minus ${rp(Math.abs(balance))}. `
               + 'Kemungkinan ada pengisian kas yang belum dicatat di Setoran.');
      } else {
        setShowForm(false);
        resetForm();
      }
      await load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan pembelanjaan');
    } finally {
      setSaving(false);
    }
  };

  const cancel = async (row) => {
    const reason = prompt(`Alasan pembatalan ${row.number}?`);
    if (!reason?.trim()) return;
    try {
      await cancelDailyPurchase(row.id, reason.trim());
      await load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal membatalkan');
    }
  };

  const grandTotal = rows
    .filter(r => r.status === 'posted')
    .reduce((s, r) => s + r.total_amount, 0);

  return (
    <>
      <div className="page-header">
        <h1>Pembelanjaan Harian</h1>
        <div style={{display:'flex',gap:'0.6rem',alignItems:'center',flexWrap:'wrap'}}>
          <Link to="/petty-cash" className="btn btn-secondary btn-sm">Kas Kecil</Link>
          <button className="btn btn-primary" onClick={() => { setShowForm(s => !s); setError(''); }}>
            {showForm ? 'Tutup' : 'Catat Pembelanjaan'}
          </button>
        </div>
      </div>

      {error && <div className="error-msg" style={{marginBottom:'1rem'}}>{error}</div>}

      {showForm && (
        <div className="card" style={{marginBottom:'1.5rem'}}>
          <form onSubmit={submit}>
            <div style={{display:'grid',gridTemplateColumns:'repeat(auto-fit,minmax(190px,1fr))',
                         gap:'0.75rem',marginBottom:'1rem'}}>
              <Field label="Tanggal">
                <input type="date" value={form.date} required
                       onChange={e => setForm({...form, date: e.target.value})} style={fieldStyle} />
              </Field>
              <Field label="Cabang" hint={branch ? `Kas kecil: ${rp(branch.petty_cash_balance)}` : ''}>
                <select value={form.branch_id} required style={fieldStyle}
                        onChange={e => setForm({...form, branch_id: e.target.value, division_id: ''})}>
                  <option value="">— pilih cabang —</option>
                  {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
                </select>
              </Field>
              <Field label="Divisi (opsional)">
                <select value={form.division_id} style={fieldStyle}
                        disabled={!form.branch_id}
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
                <input value={form.notes} placeholder="mis. belanja pasar pagi"
                       onChange={e => setForm({...form, notes: e.target.value})} style={fieldStyle} />
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
                        <input type="number" step="any" min="0" value={l.quantity}
                               onChange={e => setLine(idx, { quantity: e.target.value })} style={fieldStyle} />
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
      )}

      <div className="card">
        <div style={{display:'flex',gap:'0.75rem',flexWrap:'wrap',alignItems:'center',marginBottom:'1rem'}}>
          <select value={filterBranch} onChange={e => setFilterBranch(e.target.value)} style={fieldStyle}>
            <option value="">Semua cabang</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
          <input type="date" value={from} onChange={e => setFrom(e.target.value)} style={fieldStyle} />
          <span style={{color:'#999'}}>s/d</span>
          <input type="date" value={to} onChange={e => setTo(e.target.value)} style={fieldStyle} />
          <span style={{marginLeft:'auto',fontWeight:600}}>Total: {rp(grandTotal)}</span>
        </div>

        <table>
          <thead>
            <tr>
              <th>Nomor</th><th>Tanggal</th><th>Cabang</th><th>Keterangan</th>
              <th>Baris</th><th style={{textAlign:'right'}}>Total</th><th>Dicatat</th><th></th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={8} style={{textAlign:'center',color:'#999',padding:'2rem'}}>Memuat…</td></tr>
            ) : rows.length === 0 ? (
              <tr><td colSpan={8} style={{textAlign:'center',color:'#999',padding:'2rem'}}>
                Belum ada pembelanjaan pada rentang ini
              </td></tr>
            ) : rows.map(r => (
              <tr key={r.id} style={r.status === 'cancelled'
                    ? {opacity:0.55, textDecoration:'line-through'} : undefined}>
                <td style={{fontFamily:'monospace'}}>{r.number}</td>
                <td>{r.date?.slice(0, 10)}</td>
                <td>{r.branch_name}{r.division_name && <span style={{color:'#888'}}> / {r.division_name}</span>}</td>
                <td style={{color:'#777',fontSize:'0.88rem'}}>{r.notes || (r.vendor_name ?? '—')}</td>
                <td>{r.line_count}</td>
                <td style={{textAlign:'right',fontWeight:600}}>{rp(r.total_amount)}</td>
                <td style={{color:'#888',fontSize:'0.85rem'}}>{r.created_by_name || '—'}</td>
                <td>
                  {isAdmin && r.status === 'posted' && (
                    <button className="btn btn-danger btn-sm" onClick={() => cancel(r)}>Batalkan</button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
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
