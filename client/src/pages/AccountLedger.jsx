import { useCallback, useEffect, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { getAccountLedger, getBranches } from '../api';

// Buku Besar Akun — every posting that ever touched one account.
//
// The chart of accounts could always show you a balance and never how it got
// there: the journal had no reader anywhere in the app, so "why is Kas at this
// number" ended at querying the database by hand. This is that reader.
//
// Each line is tagged with the branch it can be traced back to — from the source
// document where it has one, otherwise from the branch-owned accounts the entry
// touches. Lines that cannot be placed say so rather than being hidden or guessed
// at, and they are filterable in their own right: on a shared account like Kas,
// the unplaceable remainder is usually the most interesting thing on the page.

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v ?? 0);

const TYPE_LABEL = {
  asset: 'Aset', liability: 'Kewajiban', equity: 'Ekuitas',
  revenue: 'Pendapatan', expense: 'Beban',
};

// Where a posting came from, and where to go to look at it. `path` is null for
// the sources that have no page of their own — an opname line links nowhere, and
// a dead link is worse than plain text.
const SOURCE = {
  invoice:             { label: 'Invoice',              path: (id) => `/invoices/view/${id}` },
  invoice_payment:     { label: 'Pembayaran invoice',   path: (id) => `/invoices/view/${id}` },
  dispatch:            { label: 'Pengiriman',           path: (id) => `/dispatches/${id}` },
  daily_purchase:      { label: 'Pembelanjaan harian',  path: null },
  operational_expense: { label: 'Beban operasional',    path: null },
  cash_deposit:        { label: 'Setoran',              path: null },
  stock_transfer:      { label: 'Transfer gudang',      path: (id) => `/transfers/group/${id}` },
  stock_opname:        { label: 'Stok opname',          path: (id) => `/stock-opname/detail/${id}` },
  pos_import:          { label: 'Import POS',           path: null },
  sale:                { label: 'Penjualan',            path: null },
  account_adjustment:  { label: 'Jurnal manual',        path: null },
  payroll:             { label: 'Penggajian',           path: null },
  thr:                 { label: 'THR',                  path: null },
  kasbon:              { label: 'Kasbon',               path: null },
  inventory:           { label: 'Stok manual',          path: null },
  opening_balance:     { label: 'Saldo awal',           path: null },
  capital_injection:   { label: 'Setoran modal',        path: null },
};

const sourceLabel = (t) => SOURCE[t]?.label || t;

export default function AccountLedger() {
  const { id } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();

  const [data, setData] = useState(null);
  const [branches, setBranches] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // The filters live in the URL so a ledger view is linkable — "look at
  // Cimanggu's share of Kas in July" is a thing worth sending someone.
  const branchId = searchParams.get('branch_id') || '';
  const unplaced = searchParams.get('unplaced') === 'true';
  const from = searchParams.get('from') || '';
  const to = searchParams.get('to') || '';

  const setParam = (key, value) => {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(key, value); else next.delete(key);
    // Branch and "unplaced" are two answers to the same question, so choosing
    // one clears the other rather than silently returning nothing.
    if (key === 'branch_id' && value) next.delete('unplaced');
    if (key === 'unplaced' && value) next.delete('branch_id');
    setSearchParams(next, { replace: true });
  };

  useEffect(() => { getBranches().then(r => setBranches(r.data)).catch(() => {}); }, []);

  const load = useCallback(() => {
    setLoading(true);
    setError('');
    return getAccountLedger(id, {
      branch_id: branchId || undefined,
      unplaced: unplaced ? 'true' : undefined,
      from: from || undefined,
      to: to || undefined,
    })
      .then(r => setData(r.data))
      .catch(e => setError(e.response?.data?.error || 'Gagal memuat buku besar'))
      .finally(() => setLoading(false));
  }, [id, branchId, unplaced, from, to]);

  useEffect(() => { load(); }, [load]);

  const account = data?.account;
  const lines = data?.lines || [];
  const split = data?.branch_split || [];
  const filtered = branchId || unplaced || from || to;
  const shownTotal = lines.reduce((s, l) => s + Number(l.delta || 0), 0);
  const maxSplit = Math.max(1, ...split.map(s => Math.abs(Number(s.amount || 0))));

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.15rem' }}>
            {account ? account.name : 'Buku Besar'}
          </h1>
          {account && (
            <div style={{ fontSize: '0.82rem', color: '#8a93a3' }}>
              <span style={{ fontFamily: 'monospace', color: '#4f8ef7' }}>{account.account_number ?? '—'}</span>
              {' · '}{TYPE_LABEL[account.account_type] || account.account_type}
              {' · saldo '}<strong style={{ color: '#1f2430' }}>{idr(account.balance)}</strong>
            </div>
          )}
        </div>
        <Link to="/accounts" className="btn btn-secondary">← Daftar Akun</Link>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      {/* Per-branch split of the whole account — always the unfiltered picture,
          so it stays the map you navigate by rather than a reflection of where
          you already are. */}
      {split.length > 0 && (
        <div className="card" style={{ marginBottom: '1.25rem' }}>
          <div className="card-header" style={{ marginBottom: '0.9rem' }}>
            <h2 style={{ fontSize: '1rem', margin: 0 }}>Rincian per Cabang</h2>
            <span style={{ fontSize: '0.75rem', color: '#aaa' }}>seluruh riwayat akun ini</span>
          </div>
          {split.map(s => {
            const amount = Number(s.amount || 0);
            const isUnplaced = !s.branch_id;
            const active = isUnplaced ? unplaced : branchId === s.branch_id;
            return (
              <button
                key={s.branch_id || 'unplaced'}
                onClick={() => (isUnplaced
                  ? setParam('unplaced', unplaced ? '' : 'true')
                  : setParam('branch_id', branchId === s.branch_id ? '' : s.branch_id))}
                style={{
                  display: 'block', width: '100%', textAlign: 'left', cursor: 'pointer',
                  background: active ? '#f0f4ff' : 'none', border: 'none',
                  borderRadius: '6px', padding: '0.4rem 0.5rem', marginBottom: '0.3rem', font: 'inherit',
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: '0.5rem' }}>
                  <span style={{ fontSize: '0.86rem', fontWeight: active ? 700 : 600, color: isUnplaced ? '#b0863a' : '#1f2430' }}>
                    {s.branch_name}
                  </span>
                  <span style={{ fontSize: '0.86rem', fontWeight: 700, whiteSpace: 'nowrap', color: amount < 0 ? '#c0392b' : '#1f2430' }}>
                    {idr(amount)}
                  </span>
                </div>
                <div style={{ height: '6px', background: '#eef1f5', borderRadius: '3px', marginTop: '0.25rem', overflow: 'hidden' }}>
                  <div style={{
                    width: `${(Math.abs(amount) / maxSplit) * 100}%`, height: '100%',
                    background: isUnplaced ? '#c9a227' : amount < 0 ? '#c0392b' : '#2c6fc2', borderRadius: '3px',
                  }} />
                </div>
                <div style={{ fontSize: '0.72rem', color: '#aab1bd', marginTop: '0.1rem' }}>
                  {s.lines} baris jurnal{active ? ' · sedang ditampilkan' : ''}
                </div>
              </button>
            );
          })}
          <p style={{ fontSize: '0.75rem', color: '#8a93a3', marginTop: '0.6rem', lineHeight: 1.5 }}>
            Cabang ditelusuri dari dokumen sumber tiap jurnal (invoice, pengiriman, pembelanjaan,
            setoran) atau dari akun milik cabang yang disentuh jurnal tersebut. Jurnal yang
            menyentuh lebih dari satu cabang — misalnya penggajian satu periode — sengaja tidak
            dibagi dan masuk ke <strong>Tidak dapat ditelusuri</strong>.
          </p>
        </div>
      )}

      <div className="card">
        <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap', alignItems: 'center', marginBottom: '1rem' }}>
          <select value={unplaced ? '__unplaced' : branchId} style={fieldStyle}
                  onChange={e => (e.target.value === '__unplaced'
                    ? setParam('unplaced', 'true')
                    : setParam('branch_id', e.target.value))}>
            <option value="">Semua cabang</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
            <option value="__unplaced">Tidak dapat ditelusuri</option>
          </select>
          <input type="date" value={from} onChange={e => setParam('from', e.target.value)} style={fieldStyle} />
          <span style={{ color: '#999' }}>s/d</span>
          <input type="date" value={to} onChange={e => setParam('to', e.target.value)} style={fieldStyle} />
          {filtered && (
            <button className="btn btn-secondary btn-sm" onClick={() => setSearchParams({}, { replace: true })}>
              Hapus filter
            </button>
          )}
          <span style={{ marginLeft: 'auto', fontSize: '0.85rem', color: '#8a93a3' }}>
            {lines.length} baris · jumlah {idr(shownTotal)}
          </span>
        </div>

        {data?.truncated && (
          <div style={{ fontSize: '0.8rem', color: '#b0863a', background: '#fff8e6', border: '1px solid #f0e0b0',
                        borderRadius: '6px', padding: '0.5rem 0.75rem', marginBottom: '0.9rem' }}>
            Hanya {lines.length} baris terbaru yang ditampilkan. Persempit rentang tanggal untuk melihat sisanya.
          </div>
        )}

        <table>
          <thead>
            <tr>
              <th style={{ width: '95px' }}>Tanggal</th>
              <th>Keterangan</th>
              <th style={{ width: '150px' }}>Sumber</th>
              <th style={{ width: '130px' }}>Cabang</th>
              <th style={{ textAlign: 'right', width: '140px' }}>Mutasi</th>
              <th style={{ textAlign: 'right', width: '150px' }}>Saldo</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={6} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>Memuat…</td></tr>
            ) : lines.length === 0 ? (
              <tr><td colSpan={6} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                {filtered ? 'Tidak ada mutasi yang cocok dengan filter ini.' : 'Akun ini belum pernah menerima jurnal.'}
              </td></tr>
            ) : lines.map((l, i) => {
              const delta = Number(l.delta || 0);
              const src = SOURCE[l.source_type];
              const href = src?.path && l.source_id ? src.path(l.source_id) : null;
              return (
                <tr key={`${l.entry_id}-${i}`}>
                  <td style={{ color: '#8a93a3', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>
                    {String(l.entry_date).slice(0, 10)}
                  </td>
                  <td style={{ fontSize: '0.86rem' }}>
                    {l.description}
                    {l.memo && <div style={{ color: '#aab1bd', fontSize: '0.76rem' }}>{l.memo}</div>}
                  </td>
                  <td style={{ fontSize: '0.8rem' }}>
                    {href
                      ? <Link to={href} style={{ color: '#2c6fc2' }}>{sourceLabel(l.source_type)}</Link>
                      : <span style={{ color: '#8a93a3' }}>{sourceLabel(l.source_type)}</span>}
                    {l.created_by_name && (
                      <div style={{ color: '#c3c9d3', fontSize: '0.72rem' }}>{l.created_by_name}</div>
                    )}
                  </td>
                  <td style={{ fontSize: '0.82rem' }}>
                    {l.branch_name
                      ? <span style={{ color: '#1f2430' }}>{l.branch_name}</span>
                      : <span style={{ color: '#c9a227' }}>tidak dapat ditelusuri</span>}
                  </td>
                  <td style={{ textAlign: 'right', fontWeight: 600, whiteSpace: 'nowrap',
                               color: delta < 0 ? '#c0392b' : '#1f9d68' }}>
                    {delta > 0 ? '+' : ''}{idr(delta)}
                  </td>
                  <td style={{ textAlign: 'right', color: '#8a93a3', fontSize: '0.84rem', whiteSpace: 'nowrap' }}>
                    {idr(l.running_balance)}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>

        <p style={{ fontSize: '0.75rem', color: '#8a93a3', marginTop: '0.75rem', lineHeight: 1.5 }}>
          Kolom <strong>Saldo</strong> adalah saldo akun ini setelah tiap jurnal, dihitung atas seluruh
          mutasi akun — bukan atas baris yang sedang difilter. Filter mempersempit baris yang
          ditampilkan, bukan isi akunnya.
        </p>
      </div>
    </>
  );
}

const fieldStyle = {
  padding: '0.45rem 0.6rem', border: '1px solid #ddd',
  borderRadius: '6px', fontSize: '0.9rem',
};
