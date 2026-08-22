import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getOperationalExpenses, cancelOperationalExpense,
  getOperationalExpenseCategories, createOperationalExpenseCategory,
  deleteOperationalExpenseCategory,
  getBranches,
} from '../api';
import { isAdminRole } from '../roles';
import Icon from '../components/Icon';
import { OP_KINDS } from './operationalKinds';

// Beban Operasional — the standing bills of keeping a branch open.
//
// Listrik, air, sewa, iuran keamanan. Not goods, so no items, no units, no
// warehouse; and settled the moment they are recorded, so no payment status. One
// row is one journal entry: Dr the category's sub-account, Cr whatever paid it.
//
// The breakdown down the right-hand side is the reason the screen exists. All of
// this used to land on a single "Beban - <cabang> / Operasional" account, where
// the total was visible and the composition was not — a branch's overhead being
// high told you nothing about which bill had moved.

const rp = (n) => 'Rp ' + Number(n || 0).toLocaleString('id-ID');

const monthLabel = (ym) => {
  if (!ym) return '—';
  const [y, m] = String(ym).slice(0, 7).split('-');
  return new Date(Number(y), Number(m) - 1, 1)
    .toLocaleDateString('id-ID', { month: 'short', year: 'numeric' });
};
const today = () => new Date().toISOString().slice(0, 10);
const monthStart = () => today().slice(0, 8) + '01';

export default function OperationalExpenses() {
  const isAdmin = isAdminRole();

  const [rows, setRows] = useState([]);
  const [byCategory, setByCategory] = useState([]);
  const [total, setTotal] = useState(0);

  const [branches, setBranches] = useState([]);
  const [categories, setCategories] = useState([]);

  const [filterBranch, setFilterBranch] = useState('');
  const [filterCategory, setFilterCategory] = useState('');
  const [from, setFrom] = useState(monthStart());
  const [to, setTo] = useState(today());

  const [showCategories, setShowCategories] = useState(false);
  const [newCategory, setNewCategory] = useState('');

  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    setLoading(true);
    return getOperationalExpenses({
      branch_id: filterBranch || undefined,
      category_id: filterCategory || undefined,
      from, to,
    })
      .then(r => {
        setRows(r.data?.expenses || []);
        setByCategory(r.data?.by_category || []);
        setTotal(r.data?.total || 0);
      })
      .catch(() => setError('Gagal memuat beban operasional'))
      .finally(() => setLoading(false));
  }, [filterBranch, filterCategory, from, to]);

  useEffect(() => { load(); }, [load]);

  const loadCategories = useCallback(() =>
    getOperationalExpenseCategories()
      .then(r => setCategories(r.data || []))
      .catch(() => {}),
  []);

  useEffect(() => {
    loadCategories();
    getBranches().then(b => setBranches(b.data || [])).catch(() => {});
  }, [loadCategories]);

  const filterCategories = useMemo(
    () => categories.filter(c => !filterBranch || c.branch_id === filterBranch),
    [categories, filterBranch],
  );

  const cancel = async (row) => {
    const reason = prompt(`Alasan pembatalan ${row.number}?`);
    if (!reason?.trim()) return;
    try {
      await cancelOperationalExpense(row.id, reason.trim());
      await load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal membatalkan');
    }
  };

  const addCategory = async (e) => {
    e.preventDefault();
    if (!newCategory.trim() || !filterBranch) {
      setError('Pilih cabang dan isi nama kategori');
      return;
    }
    try {
      await createOperationalExpenseCategory({ branch_id: filterBranch, name: newCategory.trim() });
      setNewCategory('');
      await loadCategories();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menambah kategori');
    }
  };

  const removeCategory = async (c) => {
    if (!confirm(`Hapus kategori "${c.name}" beserta akunnya?`)) return;
    try {
      await deleteOperationalExpenseCategory(c.id);
      await loadCategories();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menghapus kategori');
    }
  };

  const maxCategoryTotal = Math.max(1, ...byCategory.map(c => Number(c.total || 0)));

  return (
    <>
      <div className="page-header">
        <h1>Beban Operasional</h1>
        <div style={{ display: 'flex', gap: '0.6rem', alignItems: 'center' }}>
          <Link to="/accounts" className="btn btn-secondary btn-sm">Akun</Link>
          {isAdmin && (
            <button className="btn btn-secondary" onClick={() => setShowCategories(s => !s)}>
              {showCategories ? 'Tutup Kategori' : 'Kelola Kategori'}
            </button>
          )}
        </div>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      {/* The way in. Four tiles rather than a form, because recording these is a
          routine somebody sits down to do for one kind of bill at a time —
          "bayar listrik semua cabang" — and the kind is the first thing they
          know. Carrying it into the form means that screen can be about the two
          things that actually vary, the branch and the month. */}
      <div style={{
        display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(190px,1fr))',
        gap: '0.85rem', marginBottom: '1.5rem',
      }}>
        {OP_KINDS.map(kind => (
          <Link
            key={kind.key}
            to={`/operational-expenses/new?kind=${kind.key}`}
            className="op-kind-tile"
            style={{ borderRadius: '12px', border: '1px solid #e9edf3', background: '#fff',
                     padding: '1rem', display: 'flex', gap: '0.85rem', alignItems: 'center',
                     textDecoration: 'none', color: 'inherit' }}
          >
            <span style={{
              width: '46px', height: '46px', borderRadius: '12px', flexShrink: 0,
              display: 'grid', placeItems: 'center', background: kind.tint, color: kind.color,
            }}>
              <Icon name={kind.icon} size={26} />
            </span>
            <span style={{ minWidth: 0 }}>
              <span style={{ display: 'block', fontWeight: 700, fontSize: '0.98rem', color: '#1f2430' }}>
                {kind.label}
              </span>
              <span style={{ display: 'block', fontSize: '0.74rem', color: '#8a93a3', lineHeight: 1.35, marginTop: '0.1rem' }}>
                {kind.hint}
              </span>
            </span>
          </Link>
        ))}
      </div>

      {showCategories && (
        <div className="card" style={{ marginBottom: '1.5rem' }}>
          <div className="card-header" style={{ marginBottom: '0.75rem' }}>
            <h2 style={{ fontSize: '1rem', margin: 0 }}>Kategori Beban Operasional</h2>
            <span style={{ fontSize: '0.78rem', color: '#888' }}>
              {filterBranch ? branches.find(b => b.id === filterBranch)?.name : 'semua cabang'}
            </span>
          </div>

          <p style={{ fontSize: '0.8rem', color: '#888', lineHeight: 1.5, marginTop: 0 }}>
            Setiap kategori adalah satu akun nyata di bawah <strong>Beban – &lt;cabang&gt; / Operasional</strong>.
            Kategori bawaan ada di semua cabang dan tidak dapat dihapus — itulah yang membuat
            perbandingan listrik antar cabang selalu setara. Kategori yang sudah dipakai juga tidak
            dapat dihapus, karena akunnya sudah menyimpan riwayat jurnal.
          </p>

          {isAdmin && (
            <form onSubmit={addCategory} style={{ display: 'flex', gap: '0.5rem', margin: '0.75rem 0 1rem', flexWrap: 'wrap' }}>
              <select value={filterBranch} onChange={e => setFilterBranch(e.target.value)} style={{ ...fieldStyle, width: 'auto' }}>
                <option value="">— pilih cabang —</option>
                {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
              </select>
              <input value={newCategory} placeholder="nama kategori baru" style={{ ...fieldStyle, width: 'auto' }}
                     onChange={e => setNewCategory(e.target.value)} />
              <button type="submit" className="btn btn-secondary btn-sm" disabled={!filterBranch || !newCategory.trim()}>
                Tambah
              </button>
            </form>
          )}

          <table>
            <thead>
              <tr>
                <th>Cabang</th><th>Kategori</th><th>Akun</th>
                <th style={{ textAlign: 'right' }}>Saldo akun</th><th></th>
              </tr>
            </thead>
            <tbody>
              {filterCategories.map(c => (
                <tr key={c.id}>
                  <td>{c.branch_name}</td>
                  <td style={{ fontWeight: 600 }}>
                    {c.name}
                    {c.is_system && (
                      <span style={{ marginLeft: '0.4rem', fontSize: '0.68rem', padding: '0.1rem 0.35rem',
                                     borderRadius: '3px', background: '#eef2ff', color: '#3949ab', fontWeight: 600 }}>
                        bawaan
                      </span>
                    )}
                  </td>
                  <td style={{ fontFamily: 'monospace', fontSize: '0.8rem', color: '#4f8ef7' }}>{c.account_number}</td>
                  <td style={{ textAlign: 'right' }}>{rp(c.balance)}</td>
                  <td>
                    {isAdmin && !c.is_system && Number(c.balance) === 0 && (
                      <button className="btn btn-danger btn-sm" onClick={() => removeCategory(c)}>Hapus</button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0,2fr) minmax(260px,1fr)', gap: '1.5rem', alignItems: 'start' }}>
        <div className="card">
          <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap', alignItems: 'center', marginBottom: '1rem' }}>
            <select value={filterBranch} onChange={e => { setFilterBranch(e.target.value); setFilterCategory(''); }} style={{ ...fieldStyle, width: 'auto' }}>
              <option value="">Semua cabang</option>
              {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
            </select>
            <select value={filterCategory} onChange={e => setFilterCategory(e.target.value)} style={{ ...fieldStyle, width: 'auto' }}>
              <option value="">Semua jenis</option>
              {filterCategories.map(c => (
                <option key={c.id} value={c.id}>{c.name}{filterBranch ? '' : ` · ${c.branch_name}`}</option>
              ))}
            </select>
            <input type="date" value={from} onChange={e => setFrom(e.target.value)} style={{ ...fieldStyle, width: 'auto' }} />
            <span style={{ color: '#999' }}>s/d</span>
            <input type="date" value={to} onChange={e => setTo(e.target.value)} style={{ ...fieldStyle, width: 'auto' }} />
            <span style={{ marginLeft: 'auto', fontWeight: 700 }}>Total: {rp(total)}</span>
          </div>

          <table>
            <thead>
              <tr>
                <th>Nomor</th><th>Bulan</th><th>Tgl. Bayar</th><th>Cabang</th><th>Jenis</th>
                <th>Dibayar dari</th><th style={{ textAlign: 'right' }}>Jumlah</th>
                <th>Referensi</th><th></th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr><td colSpan={9} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Memuat…</td></tr>
              ) : rows.length === 0 ? (
                <tr><td colSpan={9} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                  Belum ada beban operasional pada rentang ini
                </td></tr>
              ) : rows.map(r => (
                <tr key={r.id} style={r.status === 'cancelled' ? { opacity: 0.55, textDecoration: 'line-through' } : undefined}>
                  <td style={{ fontFamily: 'monospace' }}>{r.number}</td>
                  {/* The month billed comes first: it is what the row is about.
                      The payment date answers a different question and is easy
                      to mistake for it, so the two are labelled apart. */}
                  <td style={{ fontWeight: 600, whiteSpace: 'nowrap' }}>{monthLabel(r.period_month)}</td>
                  <td style={{ color: '#8a93a3', fontSize: '0.85rem' }}>{String(r.date).slice(0, 10)}</td>
                  <td>{r.branch_name}</td>
                  <td style={{ fontWeight: 600 }}>{r.category_name}</td>
                  <td style={{ fontSize: '0.85rem', color: '#666' }}>{r.credit_account_name}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{rp(r.amount)}</td>
                  <td style={{ color: '#777', fontSize: '0.85rem' }}>
                    {r.reference || '—'}
                    {r.vendor_name && <div style={{ color: '#999' }}>{r.vendor_name}</div>}
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

        {/* The composition, which is the whole point of splitting the account up.
            Bars rather than a table of numbers: the question is which bill is the
            big one, and relative length answers that before you read a figure. */}
        <div className="card">
          <div className="card-header" style={{ marginBottom: '1rem' }}>
            <h2 style={{ fontSize: '1rem', margin: 0 }}>Rincian per Jenis</h2>
            <span style={{ fontSize: '0.75rem', color: '#aaa' }}>{from} s/d {to}</span>
          </div>

          {byCategory.length === 0 ? (
            <p style={{ color: '#bbb', fontSize: '0.88rem', padding: '1rem 0' }}>Belum ada data.</p>
          ) : byCategory.map(c => {
            const value = Number(c.total || 0);
            return (
              <div key={`${c.category_id}`} style={{ marginBottom: '0.85rem' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: '0.5rem' }}>
                  <span style={{ fontSize: '0.85rem', fontWeight: 600 }}>
                    {c.category_name}
                    {!filterBranch && <span style={{ color: '#aaa', fontWeight: 400 }}> · {c.branch_name}</span>}
                  </span>
                  <span style={{ fontSize: '0.85rem', fontWeight: 700, whiteSpace: 'nowrap' }}>{rp(value)}</span>
                </div>
                <div style={{ height: '7px', background: '#f1f3f7', borderRadius: '4px', marginTop: '0.3rem', overflow: 'hidden' }}>
                  <div style={{ width: `${(value / maxCategoryTotal) * 100}%`, height: '100%', background: '#e67e22', borderRadius: '4px' }} />
                </div>
                <div style={{ fontSize: '0.72rem', color: '#aaa', marginTop: '0.15rem' }}>
                  {c.entries} catatan · {total > 0 ? Math.round((value / total) * 100) : 0}% dari total
                </div>
              </div>
            );
          })}
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
