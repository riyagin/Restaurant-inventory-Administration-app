import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import * as XLSX from 'xlsx';
import { getProfitLossPeriodic } from '../api';

// Laba rugi side by side, one column per month or per year.
//
// The dashboard chart answers "how are we doing"; this answers "compared with
// what". So the unit here is a period column and the rows go all the way down to
// the account — reading a month as worse than the last one is only useful if you
// can open it and see which account moved.
//
// Scope is a filter rather than a column dimension. A branch's divisions carry
// the same names across branches ("Dapur", "Bar"), so picking a division name
// compares that division across every branch that has one; a branch without it
// is dropped from the comparison entirely and named underneath, because a column
// of zeros for a division that does not exist reads exactly like a bad month.

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v || 0);

// Columns are narrow and there are up to twelve of them; full rupiah would wrap.
const compact = (v) => {
  const n = Number(v) || 0;
  const abs = Math.abs(n);
  const sign = n < 0 ? '-' : '';
  if (abs >= 1e9) return `${sign}${(abs / 1e9).toFixed(abs >= 1e10 ? 0 : 1)} M`;
  if (abs >= 1e6) return `${sign}${(abs / 1e6).toFixed(abs >= 1e7 ? 0 : 1)} jt`;
  if (abs >= 1e3) return `${sign}${Math.round(abs / 1e3)} rb`;
  return `${sign}${abs}`;
};

const MONTH_RANGES = [
  ['6m', '6 bulan terakhir'],
  ['ytd', 'Tahun berjalan'],
];

function buildTree(accounts) {
  const map = {};
  accounts.forEach(a => { map[a.id] = { ...a, children: [] }; });
  const roots = [];
  accounts.forEach(a => {
    if (a.parent_id && map[a.parent_id]) map[a.parent_id].children.push(map[a.id]);
    else roots.push(map[a.id]);
  });
  return roots;
}

// A parent's figure in a column is its own postings there plus its children's —
// the same rule as the other financial views. A division expense account holds
// both: dispatch usage debits it directly, purchases debit its category
// children, so summing only the children would drop the dispatch spending.
function amountsOf(node, columns) {
  const out = {};
  for (const c of columns) out[c.key] = Number(node.amounts?.[c.key] || 0);
  for (const child of node.children) {
    const sub = amountsOf(child, columns);
    for (const c of columns) out[c.key] += sub[c.key];
  }
  return out;
}

const sumRows = (rows, columns) => {
  const out = {};
  for (const c of columns) out[c.key] = rows.reduce((s, r) => s + (r[c.key] || 0), 0);
  return out;
};
const rowTotal = (amounts, columns) => columns.reduce((s, c) => s + (amounts[c.key] || 0), 0);

// Percentage change against the previous column, which is the whole point of
// putting the periods side by side. Null when there is nothing to compare with
// or the base is zero — "+∞%" is not a finding.
function deltaPct(amounts, columns, i) {
  if (i === 0) return null;
  const prev = amounts[columns[i - 1].key] || 0;
  const curr = amounts[columns[i].key] || 0;
  if (prev === 0) return null;
  return ((curr - prev) / Math.abs(prev)) * 100;
}

function AccountRow({ node, columns, depth }) {
  const [open, setOpen] = useState(depth < 1);
  const amounts = amountsOf(node, columns);
  const total = rowTotal(amounts, columns);
  const hasChildren = node.children.length > 0;

  if (total === 0 && !hasChildren) return null;

  return (
    <>
      <tr
        className={`plc-row${hasChildren ? ' group' : ''}`}
        onClick={hasChildren ? () => setOpen(o => !o) : undefined}
      >
        <th scope="row" className="plc-label" style={{ paddingLeft: `${depth * 1 + 1.6}rem` }}>
          {hasChildren && <span className="plc-caret">{open ? '▾' : '▸'}</span>}
          {node.account_number && <span className="plc-accno">{node.account_number}</span>}
          {node.name}
        </th>
        {columns.map(c => (
          <td key={c.key} className="plc-num">
            {amounts[c.key] ? compact(amounts[c.key]) : <span className="plc-zero">—</span>}
          </td>
        ))}
        <td className="plc-num plc-total">{total ? compact(total) : <span className="plc-zero">—</span>}</td>
      </tr>
      {open && node.children.map(ch => (
        <AccountRow key={ch.id} node={ch} columns={columns} depth={depth + 1} />
      ))}
    </>
  );
}

// One branch. Collapsed it is three lines — revenue, expense, profit — which is
// the level most comparisons are read at; expanded it is the full account tree.
function GroupBlock({ group, columns, defaultOpen }) {
  const [open, setOpen] = useState(defaultOpen);

  const { revenue, expense, net } = useMemo(() => {
    const byType = { revenue: [], expense: [] };
    for (const a of group.accounts) if (byType[a.account_type]) byType[a.account_type].push(a);
    const rev = buildTree(byType.revenue);
    const exp = buildTree(byType.expense);
    const revSum = sumRows(rev.map(n => amountsOf(n, columns)), columns);
    const expSum = sumRows(exp.map(n => amountsOf(n, columns)), columns);
    const netSum = {};
    for (const c of columns) netSum[c.key] = revSum[c.key] - expSum[c.key];
    return {
      revenue: { nodes: rev, sum: revSum },
      expense: { nodes: exp, sum: expSum },
      net: netSum,
    };
  }, [group, columns]);

  return (
    <tbody className="plc-group">
      <tr className="plc-group-head">
        <th scope="row" className="plc-label">
          <button type="button" className="plc-group-toggle" onClick={() => setOpen(o => !o)}
                  aria-expanded={open}>
            <span className="plc-caret">{open ? '▾' : '▸'}</span>{group.name}
          </button>
        </th>
        {columns.map((c, i) => {
          const d = deltaPct(net, columns, i);
          return (
            <td key={c.key} className="plc-num">
              <span className={net[c.key] < 0 ? 'plc-neg' : undefined}>{compact(net[c.key])}</span>
              {d !== null && (
                <span className={`plc-delta ${d >= 0 ? 'up' : 'down'}`}>
                  {d >= 0 ? '▲' : '▼'}{Math.abs(d).toFixed(0)}%
                </span>
              )}
            </td>
          );
        })}
        <td className="plc-num plc-total">{compact(rowTotal(net, columns))}</td>
      </tr>

      {open && (
        <>
          <tr className="plc-section rev">
            <th scope="row" className="plc-label">Pendapatan</th>
            {columns.map(c => <td key={c.key} className="plc-num">{compact(revenue.sum[c.key])}</td>)}
            <td className="plc-num plc-total">{compact(rowTotal(revenue.sum, columns))}</td>
          </tr>
          {revenue.nodes.map(n => <AccountRow key={n.id} node={n} columns={columns} depth={0} />)}

          <tr className="plc-section exp">
            <th scope="row" className="plc-label">Beban</th>
            {columns.map(c => <td key={c.key} className="plc-num">{compact(expense.sum[c.key])}</td>)}
            <td className="plc-num plc-total">{compact(rowTotal(expense.sum, columns))}</td>
          </tr>
          {expense.nodes.map(n => <AccountRow key={n.id} node={n} columns={columns} depth={0} />)}
        </>
      )}
    </tbody>
  );
}

function groupNet(group, columns) {
  const byType = { revenue: [], expense: [] };
  for (const a of group.accounts) if (byType[a.account_type]) byType[a.account_type].push(a);
  const rev = sumRows(buildTree(byType.revenue).map(n => amountsOf(n, columns)), columns);
  const exp = sumRows(buildTree(byType.expense).map(n => amountsOf(n, columns)), columns);
  const out = {};
  for (const c of columns) out[c.key] = rev[c.key] - exp[c.key];
  return { rev, exp, net: out };
}

function buildExcel({ data, scopeLabel }) {
  const { columns, groups } = data;
  const rows = [
    ['Perbandingan Laba Rugi', scopeLabel],
    [],
    ['Cabang', 'Akun', ...columns.map(c => c.label), 'Total'],
  ];

  const flatten = (nodes, depth = 0) => {
    for (const node of nodes) {
      const amounts = amountsOf(node, columns);
      const total = rowTotal(amounts, columns);
      if (total !== 0 || node.children.length > 0) {
        rows.push(['', '  '.repeat(depth) + node.name,
          ...columns.map(c => amounts[c.key] || ''), total || '']);
      }
      if (node.children.length) flatten(node.children, depth + 1);
    }
  };

  for (const g of groups) {
    const { rev, exp, net } = groupNet(g, columns);
    const byType = { revenue: [], expense: [] };
    for (const a of g.accounts) if (byType[a.account_type]) byType[a.account_type].push(a);

    rows.push([g.name, 'LABA / RUGI', ...columns.map(c => net[c.key]), rowTotal(net, columns)]);
    rows.push(['', 'Pendapatan', ...columns.map(c => rev[c.key]), rowTotal(rev, columns)]);
    flatten(buildTree(byType.revenue));
    rows.push(['', 'Beban', ...columns.map(c => exp[c.key]), rowTotal(exp, columns)]);
    flatten(buildTree(byType.expense));
    rows.push([]);
  }

  const ws = XLSX.utils.aoa_to_sheet(rows);
  ws['!cols'] = [{ wch: 22 }, { wch: 38 }, ...columns.map(() => ({ wch: 16 })), { wch: 18 }];
  const wb = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(wb, ws, 'Perbandingan');
  XLSX.writeFile(wb, `perbandingan-laba-rugi_${columns[0].key}_${columns[columns.length - 1].key}.xlsx`);
}

export default function ProfitLossComparison() {
  const [granularity, setGranularity] = useState('month');
  const [range, setRange] = useState('6m');
  const [years, setYears] = useState(3);
  const [branchId, setBranchId] = useState('');
  const [division, setDivision] = useState('');

  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let alive = true;
    setLoading(true);
    const params = { granularity };
    if (granularity === 'year') params.years = years;
    else params.range = range;
    if (branchId) params.branch_id = branchId;
    if (division) params.division = division;

    getProfitLossPeriodic(params)
      .then(r => { if (alive) { setData(r.data); setError(''); } })
      .catch(err => { if (alive) setError(err.response?.data?.error || 'Gagal memuat laporan'); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [granularity, range, years, branchId, division]);

  const columns = data?.columns ?? [];
  const groups = data?.groups ?? [];
  const excluded = data?.excluded_branches ?? [];

  // The combined row is the sum of the groups shown — so under a division filter
  // it is that division across the branches that have it, not the whole company.
  const combined = useMemo(() => {
    const out = {};
    for (const c of columns) out[c.key] = 0;
    for (const g of groups) {
      const { net } = groupNet(g, columns);
      for (const c of columns) out[c.key] += net[c.key];
    }
    return out;
  }, [groups, columns]);

  const scopeLabel = [
    branchId ? (data?.branches ?? []).find(b => b.id === branchId)?.name : 'Semua cabang',
    division || 'Semua divisi',
    granularity === 'year' ? `${years} tahun terakhir` : (MONTH_RANGES.find(([k]) => k === range)?.[1] ?? ''),
  ].filter(Boolean).join(' · ');

  return (
    <>
      <div className="page-header">
        <h1>Perbandingan Laba Rugi</h1>
        <div style={{ display: 'flex', gap: '0.6rem', flexWrap: 'wrap' }}>
          <Link to="/reports/financial" className="btn btn-secondary">Laporan Keuangan</Link>
          <button className="btn btn-primary" disabled={loading || !data || groups.length === 0}
                  onClick={() => buildExcel({ data, scopeLabel })}>
            Unduh Excel
          </button>
        </div>
      </div>

      <div className="card plc-filters">
        <div className="plc-filter">
          <span className="plc-filter-label">Rincian</span>
          <div className="plc-seg">
            <button type="button" className={granularity === 'month' ? 'on' : ''}
                    onClick={() => setGranularity('month')}>Bulanan</button>
            <button type="button" className={granularity === 'year' ? 'on' : ''}
                    onClick={() => setGranularity('year')}>Tahunan</button>
          </div>
        </div>

        <div className="plc-filter">
          <span className="plc-filter-label">Periode</span>
          {granularity === 'month' ? (
            <div className="plc-seg">
              {MONTH_RANGES.map(([key, label]) => (
                <button key={key} type="button" className={range === key ? 'on' : ''}
                        onClick={() => setRange(key)}>{label}</button>
              ))}
            </div>
          ) : (
            <select value={years} onChange={e => setYears(Number(e.target.value))} className="plc-select">
              {[2, 3, 4, 5].map(n => <option key={n} value={n}>{n} tahun terakhir</option>)}
            </select>
          )}
        </div>

        <div className="plc-filter">
          <span className="plc-filter-label">Cabang</span>
          <select value={branchId} onChange={e => setBranchId(e.target.value)} className="plc-select">
            <option value="">Semua cabang</option>
            {(data?.branches ?? []).map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
        </div>

        <div className="plc-filter">
          <span className="plc-filter-label">Divisi</span>
          <select value={division} onChange={e => setDivision(e.target.value)} className="plc-select">
            <option value="">Semua divisi</option>
            {(data?.division_names ?? []).map(n => <option key={n} value={n}>{n}</option>)}
          </select>
        </div>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      {division && excluded.length > 0 && (
        <p className="plc-note">
          <strong>{excluded.map(b => b.name).join(', ')}</strong>{' '}
          {excluded.length > 1 ? 'tidak memiliki' : 'tidak memiliki'} divisi “{division}”,
          sehingga dikeluarkan dari perbandingan.
        </p>
      )}

      {loading ? (
        <div className="card" style={{ padding: '2rem', color: 'var(--ink-3)' }}>Memuat…</div>
      ) : groups.length === 0 ? (
        <div className="card" style={{ padding: '2rem', color: 'var(--ink-3)' }}>
          Tidak ada data laba rugi untuk filter ini.
        </div>
      ) : (
        <div className="card">
          <div className="plc-scroll">
            <table className="plc-table">
              <thead>
                <tr>
                  <th className="plc-label">{division ? `Divisi ${division} · per cabang` : 'Cabang / Akun'}</th>
                  {columns.map(c => <th key={c.key} className="plc-num">{c.label}</th>)}
                  <th className="plc-num plc-total">Total</th>
                </tr>
              </thead>

              {groups.map(g => (
                <GroupBlock key={g.id} group={g} columns={columns} defaultOpen={groups.length === 1} />
              ))}

              <tfoot>
                <tr className="plc-combined">
                  <th scope="row" className="plc-label">Laba / Rugi Gabungan</th>
                  {columns.map((c, i) => {
                    const d = deltaPct(combined, columns, i);
                    return (
                      <td key={c.key} className="plc-num">
                        <span className={combined[c.key] < 0 ? 'plc-neg' : undefined}>{compact(combined[c.key])}</span>
                        {d !== null && (
                          <span className={`plc-delta ${d >= 0 ? 'up' : 'down'}`}>
                            {d >= 0 ? '▲' : '▼'}{Math.abs(d).toFixed(0)}%
                          </span>
                        )}
                      </td>
                    );
                  })}
                  <td className="plc-num plc-total">{compact(rowTotal(combined, columns))}</td>
                </tr>
              </tfoot>
            </table>
          </div>

          <p className="plc-footnote">
            Angka dalam ribuan (rb) / juta (jt) / miliar (M) rupiah — {idr(combined[columns[0]?.key] || 0)} ditulis{' '}
            {compact(combined[columns[0]?.key] || 0)}. Klik nama cabang untuk membuka rincian akun; persentase
            membandingkan dengan periode sebelumnya. Angka diambil dari jurnal, jadi termasuk pemakaian dispatch,
            gaji, selisih opname dan jurnal manual.
          </p>
        </div>
      )}
    </>
  );
}
