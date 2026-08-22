import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import {
  getOperationalExpenses, createOperationalExpense,
  getOperationalExpenseCategories, getBranches, getAccounts,
} from '../api';
import CurrencyInput from '../components/CurrencyInput';
import Icon from '../components/Icon';
import { OP_KINDS, kindOf, NAMED_OP_CATEGORIES } from './operationalKinds';

// Recording one standing bill.
//
// A page per kind rather than one form with a category dropdown, because these
// are not variations of a single task: paying the electricity is a thing someone
// sits down to do, monthly, for every branch in turn. Arriving with the kind
// already chosen means the screen can be about the two facts that actually vary
// — which branch, and which month — and can show you what you have already
// recorded for that pairing, which a generic form has no way to do.
//
// The month is the point. `date` is when the money moved; `period_month` is what
// the bill covers, and July's electricity is routinely settled in August. Without
// the second, every month-on-month comparison is off by whenever the bill
// happened to be paid, and a month nobody paid for is invisible.

const rp = (n) => 'Rp ' + Number(n || 0).toLocaleString('id-ID');
const today = () => new Date().toISOString().slice(0, 10);

// Default the period to last month: a bill lands after the month it covers, so
// that is the month someone sitting down to pay is nearly always paying for.
function previousMonth() {
  const d = new Date();
  d.setDate(1);
  d.setMonth(d.getMonth() - 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
}

const monthLabel = (ym) => {
  if (!ym) return '—';
  const [y, m] = String(ym).slice(0, 7).split('-');
  return new Date(Number(y), Number(m) - 1, 1)
    .toLocaleDateString('id-ID', { month: 'long', year: 'numeric' });
};

export default function OperationalExpenseForm() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const kind = kindOf(searchParams.get('kind'));

  const [branches, setBranches] = useState([]);
  const [categories, setCategories] = useState([]);
  const [accounts, setAccounts] = useState([]);
  const [history, setHistory] = useState([]);

  const [branchId, setBranchId] = useState('');
  const [categoryId, setCategoryId] = useState('');
  const [periodMonth, setPeriodMonth] = useState(previousMonth());
  const [date, setDate] = useState(today());
  const [creditAccountId, setCreditAccountId] = useState('');
  const [amount, setAmount] = useState('');
  const [reference, setReference] = useState('');
  const [notes, setNotes] = useState('');

  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState('');

  useEffect(() => {
    Promise.all([getBranches(), getOperationalExpenseCategories(), getAccounts()])
      .then(([b, c, a]) => {
        setBranches(b.data || []);
        setCategories(c.data || []);
        // Only something that can actually pay: cash and bank accounts, or a
        // payable if the bill is booked now and settled later.
        setAccounts((a.data || []).filter(x => x.account_type === 'asset' || x.account_type === 'liability'));
      })
      .catch(() => setError('Gagal memuat data pendukung'));
  }, []);

  // Categories are per branch — "Listrik" is four rows, one per branch — so the
  // branch picks the row and the kind picks which row to look for.
  const branchCategories = useMemo(
    () => categories.filter(c => c.branch_id === branchId),
    [categories, branchId],
  );

  // "Lainnya" means everything that is not one of the three that got a button.
  const otherCategories = useMemo(
    () => branchCategories.filter(c => !NAMED_OP_CATEGORIES.includes(c.name.toLowerCase())),
    [branchCategories],
  );

  // Choosing a branch resolves the category for the named kinds, and narrows the
  // list for Lainnya. Doing it here rather than asking twice is the whole reason
  // the kind is in the URL.
  useEffect(() => {
    if (!branchId) { setCategoryId(''); return; }
    if (kind.categoryName) {
      const match = branchCategories.find(c => c.name.toLowerCase() === kind.categoryName);
      setCategoryId(match ? match.id : '');
      return;
    }
    setCategoryId(prev => (otherCategories.some(c => c.id === prev) ? prev : ''));
  }, [branchId, kind.categoryName, branchCategories, otherCategories]);

  // The branch's cash box pays most of these; it stays editable because rent and
  // electricity are usually paid by transfer.
  useEffect(() => {
    const petty = branches.find(b => b.id === branchId)?.petty_cash_account_id;
    if (petty) setCreditAccountId(prev => prev || petty);
  }, [branchId, branches]);

  // What is already recorded for this branch + category. This is the check that
  // stops a month being paid twice and shows at a glance which months are
  // missing — the reason a duplicate is warned about rather than blocked is that
  // two meters, or a corrected re-entry, are both real.
  const loadHistory = useCallback(() => {
    if (!branchId || !categoryId) { setHistory([]); return; }
    const from = new Date();
    from.setMonth(from.getMonth() - 13);
    getOperationalExpenses({
      branch_id: branchId,
      category_id: categoryId,
      from: from.toISOString().slice(0, 10),
      status: 'posted',
    })
      .then(r => setHistory(r.data?.expenses || []))
      .catch(() => setHistory([]));
  }, [branchId, categoryId]);

  useEffect(() => { loadHistory(); }, [loadHistory]);

  const paidMonths = useMemo(() => {
    const map = new Map();
    for (const row of history) {
      const key = String(row.period_month || '').slice(0, 7);
      map.set(key, (map.get(key) || 0) + Number(row.amount || 0));
    }
    return map;
  }, [history]);

  const alreadyPaid = paidMonths.get(periodMonth);
  const categoryName = categories.find(c => c.id === categoryId)?.name || '';

  // The last twelve months, oldest first, so a gap in the run reads as a gap.
  const recentMonths = useMemo(() => {
    const out = [];
    const d = new Date();
    d.setDate(1);
    for (let i = 11; i >= 0; i--) {
      const m = new Date(d.getFullYear(), d.getMonth() - i, 1);
      out.push(`${m.getFullYear()}-${String(m.getMonth() + 1).padStart(2, '0')}`);
    }
    return out;
  }, []);

  const submit = async (e) => {
    e.preventDefault();
    setError('');
    setSaved('');
    if (!branchId) { setError('Cabang wajib dipilih'); return; }
    if (!categoryId) {
      setError(kind.categoryName
        ? `Cabang ini belum punya kategori ${kind.label}`
        : 'Jenis beban wajib dipilih');
      return;
    }
    if (!creditAccountId) { setError('Sumber dana wajib dipilih'); return; }
    if (!(Number(amount) > 0)) { setError('Jumlah harus lebih dari 0'); return; }

    setSaving(true);
    try {
      const r = await createOperationalExpense({
        date,
        period_month: periodMonth,
        branch_id: branchId,
        category_id: categoryId,
        credit_account_id: creditAccountId,
        amount: Math.round(Number(amount)),
        reference,
        notes,
      });
      setSaved(`${r.data.number} tersimpan — ${categoryName} ${monthLabel(periodMonth)}`);
      setAmount('');
      setReference('');
      setNotes('');
      loadHistory();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan beban operasional');
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.85rem' }}>
          <span style={{
            width: '44px', height: '44px', borderRadius: '11px', display: 'grid', placeItems: 'center',
            background: kind.tint, color: kind.color, flexShrink: 0,
          }}>
            <Icon name={kind.icon} size={24} />
          </span>
          <div>
            <h1 style={{ marginBottom: '0.1rem' }}>{kind.label}</h1>
            <div style={{ fontSize: '0.82rem', color: '#8a93a3' }}>{kind.hint}</div>
          </div>
        </div>
        <Link to="/operational-expenses" className="btn btn-secondary">← Beban Operasional</Link>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
      {saved && (
        <div style={{
          background: '#e6f9f0', border: '1px solid #b2dfdb', color: '#1b5e45',
          borderRadius: '8px', padding: '0.6rem 0.9rem', marginBottom: '1rem', fontSize: '0.88rem', fontWeight: 600,
        }}>
          {saved}
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) minmax(300px,0.8fr)', gap: '1.5rem', alignItems: 'start' }}>
        <div className="card">
          <form onSubmit={submit}>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(190px,1fr))', gap: '0.85rem' }}>
              <Field label="Cabang">
                <select value={branchId} required style={fieldStyle} onChange={e => setBranchId(e.target.value)}>
                  <option value="">— pilih cabang —</option>
                  {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
                </select>
              </Field>

              {/* Only Lainnya asks which kind — the other three already know. */}
              {!kind.categoryName && (
                <Field label="Jenis beban">
                  <select value={categoryId} required style={fieldStyle}
                          onChange={e => setCategoryId(e.target.value)} disabled={!branchId}>
                    <option value="">{branchId ? '— pilih jenis —' : 'pilih cabang dulu'}</option>
                    {otherCategories.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
                  </select>
                </Field>
              )}

              <Field label="Bulan tagihan" hint="periode yang dibayar">
                <input type="month" value={periodMonth} required style={fieldStyle}
                       onChange={e => setPeriodMonth(e.target.value)} />
              </Field>

              <Field label="Jumlah">
                <CurrencyInput value={amount} style={fieldStyle} onChange={e => setAmount(e.target.value)} />
              </Field>

              <Field label="Tanggal bayar" hint="kapan uangnya keluar">
                <input type="date" value={date} required style={fieldStyle}
                       onChange={e => setDate(e.target.value)} />
              </Field>

              <Field label="Dibayar dari">
                <select value={creditAccountId} required style={fieldStyle}
                        onChange={e => setCreditAccountId(e.target.value)}>
                  <option value="">— pilih akun —</option>
                  {accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
                </select>
              </Field>

              <Field label="No. referensi" hint="opsional — no. meteran, no. tagihan">
                <input value={reference} style={fieldStyle} onChange={e => setReference(e.target.value)} />
              </Field>

              <Field label="Catatan" hint="opsional">
                <input value={notes} style={fieldStyle} onChange={e => setNotes(e.target.value)} />
              </Field>
            </div>

            {alreadyPaid != null && (
              <div style={{
                marginTop: '1rem', background: '#fff8e6', border: '1px solid #f0e0b0', color: '#8a6a1f',
                borderRadius: '8px', padding: '0.6rem 0.9rem', fontSize: '0.84rem', lineHeight: 1.5,
              }}>
                <strong>{monthLabel(periodMonth)}</strong> sudah tercatat sebesar {rp(alreadyPaid)} untuk
                cabang ini. Menyimpan lagi akan menambah catatan kedua — lanjutkan hanya jika memang
                ada dua tagihan, atau batalkan catatan lama lebih dulu.
              </div>
            )}

            <div style={{ display: 'flex', gap: '0.6rem', marginTop: '1.25rem' }}>
              <button type="submit" className="btn btn-primary" disabled={saving}>
                {saving ? 'Menyimpan…' : `Simpan ${kind.label}`}
              </button>
              <button type="button" className="btn btn-secondary" onClick={() => navigate('/operational-expenses')}>
                Selesai
              </button>
            </div>
          </form>
        </div>

        {/* Twelve months of this branch + category, so a missing month is visible
            as a gap rather than as an absence you have to notice. */}
        <div className="card">
          <div className="card-header" style={{ marginBottom: '0.9rem' }}>
            <h2 style={{ fontSize: '1rem', margin: 0 }}>Riwayat 12 Bulan</h2>
            <span style={{ fontSize: '0.75rem', color: '#aaa' }}>{categoryName || kind.label}</span>
          </div>

          {!branchId || !categoryId ? (
            <p style={{ color: '#bbb', fontSize: '0.88rem', padding: '0.5rem 0' }}>
              Pilih cabang{kind.categoryName ? '' : ' dan jenis beban'} untuk melihat riwayatnya.
            </p>
          ) : (
            <>
              {recentMonths.map(m => {
                const paid = paidMonths.get(m);
                const selected = m === periodMonth;
                return (
                  <button
                    key={m}
                    type="button"
                    onClick={() => setPeriodMonth(m)}
                    style={{
                      display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%',
                      gap: '0.5rem', background: selected ? '#f0f4ff' : 'none', border: 'none',
                      borderRadius: '6px', padding: '0.4rem 0.5rem', cursor: 'pointer', font: 'inherit',
                      textAlign: 'left',
                    }}
                  >
                    <span style={{ fontSize: '0.84rem', fontWeight: selected ? 700 : 500, color: '#1f2430' }}>
                      {monthLabel(m)}
                    </span>
                    {paid != null ? (
                      <span style={{ fontSize: '0.84rem', fontWeight: 600, color: '#1f9d68', whiteSpace: 'nowrap' }}>
                        {rp(paid)}
                      </span>
                    ) : (
                      <span style={{ fontSize: '0.78rem', color: '#c9a227', whiteSpace: 'nowrap' }}>belum ada</span>
                    )}
                  </button>
                );
              })}
              <p style={{ fontSize: '0.74rem', color: '#8a93a3', marginTop: '0.6rem', lineHeight: 1.5 }}>
                Bulan di sini adalah <strong>bulan tagihan</strong>, bukan tanggal bayar. Klik salah satu
                untuk mengisi formulir dengan bulan tersebut.
              </p>
            </>
          )}
        </div>
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
      <div style={{ fontSize: '0.82rem', color: '#555', marginBottom: '0.3rem' }}>
        {label}{hint && <span style={{ color: '#999' }}> · {hint}</span>}
      </div>
      {children}
    </label>
  );
}

export { OP_KINDS };
