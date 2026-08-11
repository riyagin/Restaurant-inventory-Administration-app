import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getCashDeposits, createCashDeposit, cancelCashDeposit,
  getBranches, getAccounts,
} from '../api';
import { isAdminRole } from '../roles';
import CurrencyInput from '../components/CurrencyInput';

// Setoran — cash leaving one place and arriving in another.
//
// Two movements share this page because they are the same event: the branch
// hands its takings to the owner (till → bank), and the office refills a
// branch's cash box (till/bank → Kas Kecil). Picking a type just preselects
// sensible from/to accounts; both remain editable, because the real world
// occasionally routes money somewhere the dropdown did not predict.

const rp = (n) => 'Rp ' + Number(n || 0).toLocaleString('id-ID');
const today = () => new Date().toISOString().slice(0, 10);

const TYPES = [
  { value: 'setoran', label: 'Setoran ke rekening', hint: 'Kas cabang → rekening bank pemilik' },
  { value: 'pengisian_kas_kecil', label: 'Pengisian kas kecil', hint: 'Kas / bank → kas kecil cabang' },
  { value: 'pengembalian_kas_kecil', label: 'Pengembalian kas kecil', hint: 'Kas kecil cabang → kas / bank' },
  { value: 'lainnya', label: 'Lainnya', hint: 'Perpindahan kas lain, jelaskan di catatan' },
];
const typeLabel = (v) => TYPES.find(t => t.value === v)?.label || v;

export default function Setoran() {
  const isAdmin = isAdminRole();

  const [rows, setRows] = useState([]);
  const [branches, setBranches] = useState([]);
  const [accounts, setAccounts] = useState([]);

  const [filterBranch, setFilterBranch] = useState('');
  const [filterType, setFilterType] = useState('');
  const [from, setFrom] = useState(() => {
    const d = new Date(); d.setDate(d.getDate() - 30);
    return d.toISOString().slice(0, 10);
  });
  const [to, setTo] = useState(today());

  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({
    date: today(), branch_id: '', movement_type: 'setoran',
    from_account_id: '', to_account_id: '', amount: '',
    reference: '', handed_to: '', notes: '',
  });
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() =>
    getCashDeposits({
      branch_id: filterBranch || undefined,
      type: filterType || undefined,
      from, to,
    })
      .then(r => setRows(r.data || []))
      .catch(() => setError('Gagal memuat setoran'))
      .finally(() => setLoading(false)),
  [filterBranch, filterType, from, to]);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    Promise.all([getBranches(), getAccounts()])
      .then(([b, a]) => {
        setBranches(b.data || []);
        // Only asset accounts can hold cash; offering a revenue account here
        // would produce a balanced entry that means nothing.
        setAccounts((a.data || []).filter(x => x.account_type === 'asset'));
      })
      .catch(() => {});
  }, []);

  const pettyOf = (branchId) => branches.find(b => b.id === branchId)?.petty_cash_account_id || '';

  // Choosing a type fills in whichever side of the movement it implies. The
  // fields stay editable — this is a shortcut, not a rule.
  const applyType = (movement_type, branch_id = form.branch_id) => {
    const petty = pettyOf(branch_id);
    const next = { ...form, movement_type, branch_id };
    if (movement_type === 'pengisian_kas_kecil') { next.to_account_id = petty; }
    else if (movement_type === 'pengembalian_kas_kecil' || movement_type === 'setoran') {
      next.from_account_id = movement_type === 'pengembalian_kas_kecil' ? petty : next.from_account_id;
    }
    setForm(next);
  };

  const accountById = useMemo(() => new Map(accounts.map(a => [a.id, a])), [accounts]);
  const fromAcct = accountById.get(form.from_account_id);

  const submit = async (e) => {
    e.preventDefault();
    setError('');
    if (!form.from_account_id || !form.to_account_id) { setError('Akun asal dan tujuan wajib dipilih'); return; }
    if (form.from_account_id === form.to_account_id) { setError('Akun asal dan tujuan tidak boleh sama'); return; }
    if (!(Number(form.amount) > 0)) { setError('Jumlah harus lebih dari 0'); return; }

    setSaving(true);
    try {
      await createCashDeposit({ ...form, amount: Math.round(Number(form.amount)) });
      setShowForm(false);
      setForm(f => ({ ...f, amount: '', reference: '', handed_to: '', notes: '' }));
      await load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan setoran');
    } finally {
      setSaving(false);
    }
  };

  const cancel = async (row) => {
    const reason = prompt(`Alasan pembatalan ${row.number}?`);
    if (!reason?.trim()) return;
    try {
      await cancelCashDeposit(row.id, reason.trim());
      await load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal membatalkan');
    }
  };

  const total = rows.filter(r => r.status === 'posted').reduce((s, r) => s + r.amount, 0);

  return (
    <>
      <div className="page-header">
        <h1>Setoran</h1>
        <div style={{display:'flex',gap:'0.6rem',alignItems:'center'}}>
          <Link to="/petty-cash" className="btn btn-secondary btn-sm">Kas Kecil</Link>
          {isAdmin && (
            <button className="btn btn-primary" onClick={() => { setShowForm(s => !s); setError(''); }}>
              {showForm ? 'Tutup' : 'Catat Setoran'}
            </button>
          )}
        </div>
      </div>

      {error && <div className="error-msg" style={{marginBottom:'1rem'}}>{error}</div>}

      {showForm && (
        <div className="card" style={{marginBottom:'1.5rem'}}>
          <form onSubmit={submit}>
            <div style={{display:'grid',gridTemplateColumns:'repeat(auto-fit,minmax(190px,1fr))',
                         gap:'0.75rem',marginBottom:'1rem'}}>
              <Field label="Tanggal">
                <input type="date" value={form.date} required style={fieldStyle}
                       onChange={e => setForm({...form, date: e.target.value})} />
              </Field>
              <Field label="Jenis" hint={TYPES.find(t => t.value === form.movement_type)?.hint}>
                <select value={form.movement_type} style={fieldStyle}
                        onChange={e => applyType(e.target.value)}>
                  {TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                </select>
              </Field>
              <Field label="Cabang">
                <select value={form.branch_id} style={fieldStyle}
                        onChange={e => applyType(form.movement_type, e.target.value)}>
                  <option value="">— pusat / tanpa cabang —</option>
                  {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
                </select>
              </Field>
              <Field label="Dari akun" hint={fromAcct ? `saldo ${rp(fromAcct.balance)}` : ''}>
                <select value={form.from_account_id} required style={fieldStyle}
                        onChange={e => setForm({...form, from_account_id: e.target.value})}>
                  <option value="">— pilih akun —</option>
                  {accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
                </select>
              </Field>
              <Field label="Ke akun">
                <select value={form.to_account_id} required style={fieldStyle}
                        onChange={e => setForm({...form, to_account_id: e.target.value})}>
                  <option value="">— pilih akun —</option>
                  {accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
                </select>
              </Field>
              <Field label="Jumlah">
                <CurrencyInput value={form.amount} style={fieldStyle}
                               onChange={e => setForm({...form, amount: e.target.value})} />
              </Field>
              <Field label="No. referensi / slip">
                <input value={form.reference} style={fieldStyle} placeholder="mis. nomor bukti transfer"
                       onChange={e => setForm({...form, reference: e.target.value})} />
              </Field>
              <Field label="Diserahkan kepada">
                <input value={form.handed_to} style={fieldStyle} placeholder="nama penerima"
                       onChange={e => setForm({...form, handed_to: e.target.value})} />
              </Field>
              <Field label="Catatan">
                <input value={form.notes} style={fieldStyle}
                       onChange={e => setForm({...form, notes: e.target.value})} />
              </Field>
            </div>
            <button type="submit" className="btn btn-primary" disabled={saving}>
              {saving ? 'Menyimpan…' : 'Simpan Setoran'}
            </button>
          </form>
        </div>
      )}

      <div className="card">
        <div style={{display:'flex',gap:'0.75rem',flexWrap:'wrap',alignItems:'center',marginBottom:'1rem'}}>
          <select value={filterBranch} onChange={e => setFilterBranch(e.target.value)} style={fieldStyle}>
            <option value="">Semua cabang</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
          <select value={filterType} onChange={e => setFilterType(e.target.value)} style={fieldStyle}>
            <option value="">Semua jenis</option>
            {TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
          </select>
          <input type="date" value={from} onChange={e => setFrom(e.target.value)} style={fieldStyle} />
          <span style={{color:'#999'}}>s/d</span>
          <input type="date" value={to} onChange={e => setTo(e.target.value)} style={fieldStyle} />
          <span style={{marginLeft:'auto',fontWeight:600}}>Total: {rp(total)}</span>
        </div>

        <table>
          <thead>
            <tr>
              <th>Nomor</th><th>Tanggal</th><th>Jenis</th><th>Cabang</th>
              <th>Dari → Ke</th><th style={{textAlign:'right'}}>Jumlah</th>
              <th>Referensi</th><th></th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={8} style={{textAlign:'center',color:'#999',padding:'2rem'}}>Memuat…</td></tr>
            ) : rows.length === 0 ? (
              <tr><td colSpan={8} style={{textAlign:'center',color:'#999',padding:'2rem'}}>
                Belum ada setoran pada rentang ini
              </td></tr>
            ) : rows.map(r => (
              <tr key={r.id} style={r.status === 'cancelled'
                    ? {opacity:0.55, textDecoration:'line-through'} : undefined}>
                <td style={{fontFamily:'monospace'}}>{r.number}</td>
                <td>{r.date?.slice(0, 10)}</td>
                <td>{typeLabel(r.movement_type)}</td>
                <td>{r.branch_name || <span style={{color:'#aaa'}}>Pusat</span>}</td>
                <td style={{fontSize:'0.87rem'}}>
                  {r.from_account_name} <span style={{color:'#aaa'}}>→</span> {r.to_account_name}
                </td>
                <td style={{textAlign:'right',fontWeight:600}}>{rp(r.amount)}</td>
                <td style={{color:'#777',fontSize:'0.85rem'}}>
                  {r.reference || '—'}
                  {r.handed_to && <div style={{color:'#999'}}>kpd. {r.handed_to}</div>}
                </td>
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
