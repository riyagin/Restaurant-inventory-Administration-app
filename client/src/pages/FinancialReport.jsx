import { useEffect, useState, useMemo, useCallback } from 'react';
import { Link } from 'react-router-dom';
import * as XLSX from 'xlsx';
import { getFinancialReport, getProfitLossByBranch } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

function todayStr() {
  return new Date().toISOString().slice(0, 10);
}

function firstOfMonthStr() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`;
}

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

// A parent's total is its own postings plus its children's.
//
// The children used to be the whole story, because the only parents that existed
// were pure groupings that never held a balance (Utang Usaha 20100, the type
// roots). Expense categories broke that: a division's expense account now has
// category children AND its own direct postings — dispatch usage debits the
// parent, since consumed stock belongs to no purchase category. Summing only the
// children would drop that spending out of the report entirely.
//
// Adding the parent's own balance is a no-op for the pure groupings, which carry
// zero by design.
function effectiveBalance(node) {
  return node.children.reduce((s, c) => s + effectiveBalance(c), Number(node.balance));
}

// ── Per-branch P&L ────────────────────────────────────────────────────────────
//
// Same rule as effectiveBalance, applied one column at a time: a parent's figure
// in a branch column is its own postings there plus its children's. An expense
// category child and its division parent belong to the same branch, so in
// practice a subtree lands in a single column — but nothing relies on that, and
// an account re-parented across branches still adds up.
function effectiveAmounts(node, columns) {
  const out = {};
  for (const col of columns) out[col.id] = Number(node.amounts?.[col.id] || 0);
  for (const child of node.children) {
    const sub = effectiveAmounts(child, columns);
    for (const col of columns) out[col.id] += sub[col.id];
  }
  return out;
}

function sumAmounts(rows, columns) {
  const out = {};
  for (const col of columns) out[col.id] = 0;
  for (const row of rows) for (const col of columns) out[col.id] += row[col.id];
  return out;
}

function totalOfAmounts(amounts, columns) {
  return columns.reduce((s, col) => s + amounts[col.id], 0);
}

// `showTotal` exists because the same row renders in two places: the all-branches
// comparison, where a Total column is the point, and a single branch's tab, where
// it would repeat the one column beside it.
function BranchAccountRow({ node, columns, depth = 0, showTotal = true }) {
  const [open, setOpen] = useState(depth < 2);
  const amounts = effectiveAmounts(node, columns);
  const total = totalOfAmounts(amounts, columns);
  const hasChildren = node.children.length > 0;
  const isRoot = depth === 0;

  if (total === 0 && !hasChildren && !isRoot) return null;
  // In a single-branch tab, a subtree belonging to another branch is all zeros —
  // and a page of zero rows is worse than a short page.
  if (!showTotal && total === 0 && !isRoot) return null;

  return (
    <>
      <tr
        style={{
          background: isRoot ? '#f0f4ff' : depth === 1 ? '#f8f9ff' : undefined,
          cursor: hasChildren ? 'pointer' : undefined,
        }}
        onClick={hasChildren ? () => setOpen(o => !o) : undefined}
      >
        <td style={{ paddingLeft: `${depth * 1.25 + 0.75}rem`, paddingTop: '0.4rem', paddingBottom: '0.4rem', position: 'sticky', left: 0, background: isRoot ? '#f0f4ff' : depth === 1 ? '#f8f9ff' : '#fff' }}>
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}>
            {hasChildren && (
              <span style={{ fontSize: '0.65rem', color: '#aaa', userSelect: 'none' }}>{open ? '▼' : '▶'}</span>
            )}
            {node.account_number && (
              <span style={{ fontFamily: 'monospace', fontSize: '0.75rem', color: '#4f8ef7', minWidth: '3rem' }}>
                {node.account_number}
              </span>
            )}
            <span style={{ fontWeight: isRoot ? 700 : depth === 1 ? 600 : 400, fontSize: '0.85rem' }}>
              {node.name}
            </span>
          </span>
        </td>
        {columns.map(col => (
          <td key={col.id} style={{ textAlign: 'right', paddingRight: '0.75rem', fontSize: '0.82rem', fontWeight: isRoot || hasChildren ? 600 : 400, color: amounts[col.id] < 0 ? '#e74c3c' : undefined }}>
            {amounts[col.id] !== 0 ? idr(amounts[col.id]) : <span style={{ color: '#ddd' }}>—</span>}
          </td>
        ))}
        {showTotal && (
          <td style={{ textAlign: 'right', paddingRight: '1rem', fontSize: '0.82rem', fontWeight: 700, borderLeft: '1px solid #eee', color: total < 0 ? '#e74c3c' : undefined }}>
            {total !== 0 ? idr(total) : <span style={{ color: '#ddd' }}>—</span>}
          </td>
        )}
      </tr>
      {open && node.children.map(child => (
        <BranchAccountRow key={child.id} node={child} columns={columns} depth={depth + 1} showTotal={showTotal} />
      ))}
    </>
  );
}

function BranchSectionRows({ title, nodes, columns, color, showTotal = true }) {
  const totals = sumAmounts(nodes.map(n => effectiveAmounts(n, columns)), columns);
  return (
    <>
      <tr style={{ background: color }}>
        <td style={{ fontWeight: 700, fontSize: '0.92rem', padding: '0.55rem 0.75rem', position: 'sticky', left: 0, background: color }}>
          {title}
        </td>
        {columns.map(col => (
          <td key={col.id} style={{ textAlign: 'right', paddingRight: '0.75rem', fontWeight: 700, fontSize: '0.85rem' }}>
            {idr(totals[col.id])}
          </td>
        ))}
        {showTotal && (
          <td style={{ textAlign: 'right', paddingRight: '1rem', fontWeight: 700, fontSize: '0.85rem', borderLeft: '1px solid #eee' }}>
            {idr(totalOfAmounts(totals, columns))}
          </td>
        )}
      </tr>
      {nodes.map(n => <BranchAccountRow key={n.id} node={n} columns={columns} depth={0} showTotal={showTotal} />)}
    </>
  );
}

function TabButton({ active, muted, onClick, children }) {
  return (
    <button
      role="tab"
      aria-selected={active}
      onClick={onClick}
      style={{
        border: 'none', background: 'none', cursor: 'pointer',
        padding: '0.5rem 0.85rem', fontSize: '0.88rem',
        fontWeight: active ? 700 : 500,
        color: active ? '#2c6fc2' : muted ? '#b0863a' : '#6b7484',
        borderBottom: `2px solid ${active ? '#2c6fc2' : 'transparent'}`,
        marginBottom: '-1px',
      }}
    >
      {children}
    </button>
  );
}

// Revenue above expense on a shared scale — the gap between the two bars is the
// profit, read without doing the subtraction.
function ProportionBar({ revenue, expense, scale }) {
  const width = (v) => `${scale > 0 ? Math.min(100, (Math.max(0, v) / scale) * 100) : 0}%`;
  return (
    <div title={`Pendapatan ${idr(revenue)} · Beban ${idr(expense)}`}>
      <div style={{ height: '6px', background: '#eef1f5', borderRadius: '3px', overflow: 'hidden' }}>
        <div style={{ width: width(revenue), height: '100%', background: '#1f9d68', borderRadius: '3px' }} />
      </div>
      <div style={{ height: '6px', background: '#eef1f5', borderRadius: '3px', overflow: 'hidden', marginTop: '2px' }}>
        <div style={{ width: width(expense), height: '100%', background: '#d98324', borderRadius: '3px' }} />
      </div>
    </div>
  );
}

function Figure({ label, value, color, hint }) {
  return (
    <div style={{ border: '1px solid #e9edf3', borderRadius: '8px', padding: '0.7rem 0.9rem' }}>
      <div style={{ fontSize: '0.76rem', color: '#8a93a3' }}>{label}</div>
      <div style={{ fontSize: '1.15rem', fontWeight: 700, color, marginTop: '0.15rem' }}>{idr(value)}</div>
      {hint && <div style={{ fontSize: '0.72rem', color: '#aab1bd', marginTop: '0.1rem' }}>{hint}</div>}
    </div>
  );
}

function AccountRow({ node, depth = 0 }) {
  const [open, setOpen] = useState(depth < 2);
  const bal = effectiveBalance(node);
  const hasChildren = node.children.length > 0;
  const isRoot = depth === 0;

  if (bal === 0 && !hasChildren && !isRoot) return null;

  return (
    <>
      <tr
        style={{
          background: isRoot ? '#f0f4ff' : depth === 1 ? '#f8f9ff' : undefined,
          cursor: hasChildren ? 'pointer' : undefined,
        }}
        onClick={hasChildren ? () => setOpen(o => !o) : undefined}
      >
        <td style={{ paddingLeft: `${depth * 1.25 + 0.75}rem`, paddingTop: '0.45rem', paddingBottom: '0.45rem' }}>
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}>
            {hasChildren && (
              <span style={{ fontSize: '0.65rem', color: '#aaa', userSelect: 'none' }}>{open ? '▼' : '▶'}</span>
            )}
            {node.account_number && (
              <span style={{ fontFamily: 'monospace', fontSize: '0.78rem', color: '#4f8ef7', minWidth: '3rem' }}>
                {node.account_number}
              </span>
            )}
            <span style={{ fontWeight: isRoot ? 700 : depth === 1 ? 600 : 400, fontSize: isRoot ? '0.95rem' : '0.88rem' }}>
              {node.name}
            </span>
          </span>
        </td>
        <td style={{ textAlign: 'right', paddingRight: '1rem', fontWeight: isRoot || hasChildren ? 600 : 400, fontSize: '0.88rem', color: bal < 0 ? '#e74c3c' : undefined }}>
          {bal !== 0 ? idr(bal) : <span style={{ color: '#ddd' }}>—</span>}
        </td>
        {node.total_adjustments !== 0 && !hasChildren ? (
          <td style={{ textAlign: 'right', paddingRight: '0.75rem', fontSize: '0.78rem', color: node.total_adjustments > 0 ? '#27ae60' : '#e74c3c' }}>
            {node.total_adjustments > 0 ? '+' : ''}{idr(node.total_adjustments)}
          </td>
        ) : <td />}
      </tr>
      {open && node.children.map(child => (
        <AccountRow key={child.id} node={child} depth={depth + 1} />
      ))}
    </>
  );
}

function Section({ title, nodes, color }) {
  const total = nodes.reduce((s, n) => s + effectiveBalance(n), 0);
  return (
    <div style={{ marginBottom: '0.25rem' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '0.6rem 1rem', background: color, borderRadius: '6px 6px 0 0', marginBottom: '1px' }}>
        <span style={{ fontWeight: 700, fontSize: '1rem' }}>{title}</span>
        <span style={{ fontWeight: 700, fontSize: '1rem' }}>{idr(total)}</span>
      </div>
      <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: '0.75rem' }}>
        <tbody>
          {nodes.map(n => <AccountRow key={n.id} node={n} depth={0} />)}
        </tbody>
      </table>
    </div>
  );
}

// ── Excel export ──────────────────────────────────────────────────────────────

function flattenTree(nodes, depth = 0, rows = []) {
  for (const node of nodes) {
    const bal = effectiveBalance(node);
    rows.push({
      indent: depth,
      account_number: node.account_number || '',
      name: node.name,
      balance: bal,
      is_group: node.children.length > 0,
    });
    if (node.children.length > 0) flattenTree(node.children, depth + 1, rows);
  }
  return rows;
}

function buildExcel({ trees, startDate, endDate, isPeriod }) {
  const wb = XLSX.utils.book_new();
  const dateLabel = isPeriod
    ? `${startDate} s/d ${endDate}`
    : 'Semua waktu';

  const totalRevenue = (trees.revenue || []).reduce((s, n) => s + effectiveBalance(n), 0);
  const totalExpense = (trees.expense || []).reduce((s, n) => s + effectiveBalance(n), 0);
  const netIncome    = totalRevenue - totalExpense;

  // ── P&L sheet ──
  const plRows = [
    ['Laporan Laba Rugi', '', dateLabel],
    [],
    ['No. Akun', 'Akun', 'Jumlah'],
  ];

  const addSection = (label, nodes) => {
    const total = nodes.reduce((s, n) => s + effectiveBalance(n), 0);
    plRows.push([label.toUpperCase(), '', '']);
    for (const row of flattenTree(nodes)) {
      plRows.push([
        row.account_number,
        '  '.repeat(row.indent) + row.name,
        row.balance !== 0 ? row.balance : '',
      ]);
    }
    plRows.push(['', `Total ${label}`, total]);
    plRows.push([]);
  };

  addSection('Pendapatan', trees.revenue || []);
  addSection('Beban', trees.expense || []);
  plRows.push(['', netIncome >= 0 ? 'LABA BERSIH' : 'RUGI BERSIH', netIncome]);

  const plWs = XLSX.utils.aoa_to_sheet(plRows);
  plWs['!cols'] = [{ wch: 12 }, { wch: 40 }, { wch: 20 }];
  XLSX.utils.book_append_sheet(wb, plWs, 'Laba Rugi');

  // ── Balance Sheet sheet ──
  const totalAsset  = (trees.asset || []).reduce((s, n) => s + effectiveBalance(n), 0);
  const totalLiab   = (trees.liability || []).reduce((s, n) => s + effectiveBalance(n), 0);
  const totalEquity = (trees.equity || []).reduce((s, n) => s + effectiveBalance(n), 0);

  const bsRows = [
    ['Neraca', '', isPeriod ? `Per: ${endDate}` : 'Saldo Saat Ini'],
    [],
    ['No. Akun', 'Akun', 'Jumlah'],
  ];

  const addBsSection = (label, nodes) => {
    const total = nodes.reduce((s, n) => s + effectiveBalance(n), 0);
    bsRows.push([label.toUpperCase(), '', '']);
    for (const row of flattenTree(nodes)) {
      bsRows.push([
        row.account_number,
        '  '.repeat(row.indent) + row.name,
        row.balance !== 0 ? row.balance : '',
      ]);
    }
    bsRows.push(['', `Total ${label}`, total]);
    bsRows.push([]);
  };

  addBsSection('Aset', trees.asset || []);
  addBsSection('Kewajiban', trees.liability || []);
  addBsSection('Ekuitas', trees.equity || []);
  bsRows.push(['', 'Total Kewajiban + Ekuitas + Laba', totalLiab + totalEquity + netIncome]);
  bsRows.push(['', 'Total Aset', totalAsset]);

  const bsWs = XLSX.utils.aoa_to_sheet(bsRows);
  bsWs['!cols'] = [{ wch: 12 }, { wch: 40 }, { wch: 20 }];
  XLSX.utils.book_append_sheet(wb, bsWs, 'Neraca');

  const filename = isPeriod
    ? `laporan-keuangan_${startDate}_${endDate}.xlsx`
    : 'laporan-keuangan.xlsx';
  XLSX.writeFile(wb, filename);
}

function flattenBranchTree(nodes, columns, depth = 0, rows = []) {
  for (const node of nodes) {
    const amounts = effectiveAmounts(node, columns);
    const total = totalOfAmounts(amounts, columns);
    if (total !== 0 || node.children.length > 0 || depth === 0) {
      rows.push({ indent: depth, account_number: node.account_number || '', name: node.name, amounts, total });
    }
    if (node.children.length > 0) flattenBranchTree(node.children, columns, depth + 1, rows);
  }
  return rows;
}

function buildBranchExcel({ branchTrees, columns, startDate, endDate, isPeriod }) {
  const wb = XLSX.utils.book_new();
  const dateLabel = isPeriod ? `${startDate} s/d ${endDate}` : 'Semua waktu';

  const rows = [
    ['Laporan Laba Rugi per Cabang', '', dateLabel],
    [],
    ['No. Akun', 'Akun', ...columns.map(c => c.name), 'Total'],
  ];

  const sectionTotals = {};
  const addSection = (label, nodes) => {
    const totals = sumAmounts(nodes.map(n => effectiveAmounts(n, columns)), columns);
    sectionTotals[label] = totals;
    rows.push([label.toUpperCase(), '', ...columns.map(() => ''), '']);
    for (const row of flattenBranchTree(nodes, columns)) {
      rows.push([
        row.account_number,
        '  '.repeat(row.indent) + row.name,
        ...columns.map(c => (row.amounts[c.id] !== 0 ? row.amounts[c.id] : '')),
        row.total !== 0 ? row.total : '',
      ]);
    }
    rows.push(['', `Total ${label}`, ...columns.map(c => totals[c.id]), totalOfAmounts(totals, columns)]);
    rows.push([]);
  };

  addSection('Pendapatan', branchTrees.revenue || []);
  addSection('Beban', branchTrees.expense || []);

  const rev = sectionTotals['Pendapatan'];
  const exp = sectionTotals['Beban'];
  const net = columns.map(c => rev[c.id] - exp[c.id]);
  rows.push(['', 'LABA / RUGI BERSIH', ...net, net.reduce((s, v) => s + v, 0)]);

  const ws = XLSX.utils.aoa_to_sheet(rows);
  ws['!cols'] = [{ wch: 12 }, { wch: 40 }, ...columns.map(() => ({ wch: 18 })), { wch: 20 }];
  XLSX.utils.book_append_sheet(wb, ws, 'Laba Rugi per Cabang');

  XLSX.writeFile(wb, isPeriod
    ? `laba-rugi-per-cabang_${startDate}_${endDate}.xlsx`
    : 'laba-rugi-per-cabang.xlsx');
}

// ── Main component ─────────────────────────────────────────────────────────────

export default function FinancialReport() {
  const [accounts, setAccounts] = useState([]);
  const [loading, setLoading]   = useState(true);
  const [startDate, setStartDate] = useState(firstOfMonthStr());
  const [endDate, setEndDate]     = useState(todayStr());
  const [isPeriod, setIsPeriod]   = useState(false);
  const [byBranch, setByBranch]   = useState(false);
  // The report is stored with the filter key it was fetched for, so "still
  // loading" is derived rather than tracked: no second state to keep in sync, and
  // a response that arrives after the filters moved on is visibly stale instead
  // of silently overwriting the current one.
  const [branchReport, setBranchReport] = useState(null);
  // Which branch's tab is open; '' is the all-branches comparison.
  const [branchTab, setBranchTab] = useState('');

  const fetchReport = useCallback(() => {
    setLoading(true);
    const params = isPeriod ? { start_date: startDate, end_date: endDate } : {};
    getFinancialReport(params)
      .then(r => setAccounts(r.data))
      .finally(() => setLoading(false));
  }, [isPeriod, startDate, endDate]);

  useEffect(() => { fetchReport(); }, [fetchReport]);

  const branchKey = isPeriod ? `${startDate}..${endDate}` : 'all';

  useEffect(() => {
    if (!byBranch) return undefined;
    let cancelled = false;
    const params = isPeriod ? { start_date: startDate, end_date: endDate } : {};
    getProfitLossByBranch(params)
      .then(r => { if (!cancelled) setBranchReport({ key: branchKey, data: r.data }); })
      .catch(() => { if (!cancelled) setBranchReport({ key: branchKey, data: null }); });
    return () => { cancelled = true; };
  }, [byBranch, isPeriod, startDate, endDate, branchKey]);

  const branchData = branchReport?.key === branchKey ? branchReport.data : null;
  const branchLoading = byBranch && branchData === null;

  const { trees } = useMemo(() => {
    const byType = { asset: [], liability: [], equity: [], revenue: [], expense: [] };
    accounts.forEach(a => { if (byType[a.account_type]) byType[a.account_type].push(a); });
    const trees = {};
    for (const type of Object.keys(byType)) trees[type] = buildTree(byType[type]);
    return { trees };
  }, [accounts]);

  const branchColumns = useMemo(() => branchData?.columns ?? [], [branchData]);
  const branchTrees = useMemo(() => {
    const byType = { revenue: [], expense: [] };
    (branchData?.accounts ?? []).forEach(a => { if (byType[a.account_type]) byType[a.account_type].push(a); });
    return { revenue: buildTree(byType.revenue), expense: buildTree(byType.expense) };
  }, [branchData]);

  const branchSectionTotals = useMemo(() => {
    if (branchColumns.length === 0) return { revenue: {}, expense: {} };
    return {
      revenue: sumAmounts((branchTrees.revenue || []).map(n => effectiveAmounts(n, branchColumns)), branchColumns),
      expense: sumAmounts((branchTrees.expense || []).map(n => effectiveAmounts(n, branchColumns)), branchColumns),
    };
  }, [branchTrees, branchColumns]);

  const branchNet = useMemo(() => {
    const out = {};
    for (const col of branchColumns) {
      out[col.id] = (branchSectionTotals.revenue[col.id] || 0) - (branchSectionTotals.expense[col.id] || 0);
    }
    return out;
  }, [branchSectionTotals, branchColumns]);

  // One scale across every proportion bar, so a bar's length means the same
  // thing on every row. Per-row scaling would draw the smallest branch as
  // confidently as the largest.
  const branchScale = useMemo(() => Math.max(
    0,
    ...branchColumns.flatMap(col => [
      branchSectionTotals.revenue[col.id] || 0,
      branchSectionTotals.expense[col.id] || 0,
    ]),
  ), [branchSectionTotals, branchColumns]);

  // A tab pointing at a branch the current period no longer returns falls back
  // to the comparison rather than rendering an empty card.
  const activeBranchTab = branchColumns.some(c => c.id === branchTab) ? branchTab : '';

  const totalRevenue = trees.revenue?.reduce((s, n) => s + effectiveBalance(n), 0) ?? 0;
  const totalExpense = trees.expense?.reduce((s, n) => s + effectiveBalance(n), 0) ?? 0;
  const netIncome    = totalRevenue - totalExpense;
  const totalAsset   = trees.asset?.reduce((s, n) => s + effectiveBalance(n), 0) ?? 0;
  const totalLiab    = trees.liability?.reduce((s, n) => s + effectiveBalance(n), 0) ?? 0;
  const totalEquity  = trees.equity?.reduce((s, n) => s + effectiveBalance(n), 0) ?? 0;

  return (
    <>
      <div className="page-header">
        <h1>Laporan Keuangan</h1>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
          <Link to="/account-adjustments" className="btn btn-secondary">Jurnal Manual</Link>
          <Link to="/reports/profit-loss" className="btn btn-secondary">Perbandingan Periode</Link>
          {/* The printable statement is this report in document form, so it is
              reached from here and inherits whatever period is on screen —
              rather than sitting in the menu with its own date pickers that
              start from a different default. */}
          <Link
            to={isPeriod
              ? `/reports/statement?start_date=${startDate}&end_date=${endDate}`
              : '/reports/statement'}
            className="btn btn-secondary"
          >
            Dokumen / Cetak
          </Link>
          {byBranch && (
            <button
              className="btn btn-secondary"
              onClick={() => buildBranchExcel({ branchTrees, columns: branchColumns, startDate, endDate, isPeriod })}
              disabled={branchLoading || branchColumns.length === 0}
            >
              Unduh Excel (per Cabang)
            </button>
          )}
          <button
            className="btn btn-primary"
            onClick={() => buildExcel({ trees, startDate, endDate, isPeriod })}
            disabled={loading}
          >
            Unduh Excel
          </button>
        </div>
      </div>

      {/* Date range + branch filter */}
      <div className="card" style={{ marginBottom: '1.25rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer', userSelect: 'none' }}>
            <input
              type="checkbox"
              checked={isPeriod}
              onChange={e => setIsPeriod(e.target.checked)}
            />
            <span style={{ fontWeight: 600, fontSize: '0.88rem' }}>Filter periode</span>
          </label>

          {isPeriod && (
            <>
              <div className="form-group" style={{ margin: 0 }}>
                <label style={{ fontSize: '0.78rem', color: '#888', marginBottom: '0.2rem', display: 'block' }}>Dari</label>
                <input
                  type="date"
                  className="form-control"
                  value={startDate}
                  onChange={e => setStartDate(e.target.value)}
                  style={{ width: 'auto' }}
                />
              </div>
              <div className="form-group" style={{ margin: 0 }}>
                <label style={{ fontSize: '0.78rem', color: '#888', marginBottom: '0.2rem', display: 'block' }}>Sampai</label>
                <input
                  type="date"
                  className="form-control"
                  value={endDate}
                  onChange={e => setEndDate(e.target.value)}
                  style={{ width: 'auto' }}
                />
              </div>

            </>
          )}

          <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer', userSelect: 'none', alignSelf: 'flex-end', paddingBottom: '0.25rem' }}>
            <input
              type="checkbox"
              checked={byBranch}
              onChange={e => setByBranch(e.target.checked)}
            />
            <span style={{ fontWeight: 600, fontSize: '0.88rem' }}>Rincian per cabang</span>
          </label>

          {isPeriod && (
            <span style={{ fontSize: '0.82rem', color: '#888', alignSelf: 'flex-end', paddingBottom: '0.25rem' }}>
              Pendapatan &amp; beban dihitung dari transaksi dalam periode ini. Neraca menggunakan saldo saat ini.
            </span>
          )}
        </div>
      </div>

      {/* ── Per-branch P&L ── */}
      {byBranch && (
        <div className="card" style={{ marginBottom: '1.5rem' }}>
          <div className="card-header" style={{ marginBottom: '1rem' }}>
            <h2>Laba Rugi per Cabang</h2>
            <span style={{ fontSize: '0.78rem', color: '#888' }}>
              {isPeriod ? `${startDate} s/d ${endDate}` : 'Semua waktu'} · dari jurnal
            </span>
          </div>

          {branchLoading ? (
            <p style={{ padding: '1.5rem', color: '#999' }}>Memuat...</p>
          ) : branchColumns.length === 0 ? (
            <p style={{ padding: '1.5rem', color: '#999' }}>Belum ada cabang.</p>
          ) : (
            <>
              {/* Tabs rather than one wide table.
                  The n-column grid put every branch on screen at once, which is
                  the right shape for comparing a single line across branches and
                  the wrong one for the question people actually bring here —
                  "how did this branch do" — where it means reading down a column
                  while four others compete for the eye, on a table that scrolls
                  sideways past its own account names. So the comparison keeps a
                  tab of its own, reduced to the three figures that carry it, and
                  each branch gets a tab where its P&L is just a P&L. */}
              <div role="tablist" aria-label="Cabang" style={{ display: 'flex', gap: '0.25rem', flexWrap: 'wrap', borderBottom: '1px solid #eceff4', marginBottom: '1rem' }}>
                <TabButton active={activeBranchTab === ''} onClick={() => setBranchTab('')}>Perbandingan</TabButton>
                {branchColumns.map(col => (
                  <TabButton
                    key={col.id}
                    active={activeBranchTab === col.id}
                    onClick={() => setBranchTab(col.id)}
                    muted={col.id === 'unallocated'}
                  >
                    {col.name}
                  </TabButton>
                ))}
              </div>

              {activeBranchTab === '' ? (
                <>
                  <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <thead>
                      <tr style={{ borderBottom: '2px solid #eee' }}>
                        <th style={{ textAlign: 'left', padding: '0.5rem 0.75rem', fontSize: '0.8rem', color: '#888' }}>Cabang</th>
                        <th style={{ textAlign: 'right', paddingRight: '0.75rem', fontSize: '0.8rem', color: '#888' }}>Pendapatan</th>
                        <th style={{ textAlign: 'right', paddingRight: '0.75rem', fontSize: '0.8rem', color: '#888' }}>Beban</th>
                        <th style={{ textAlign: 'right', paddingRight: '1rem', fontSize: '0.8rem', color: '#555' }}>Laba / Rugi</th>
                        <th style={{ width: '32%', paddingLeft: '1rem', fontSize: '0.8rem', color: '#888', textAlign: 'left' }}>Proporsi</th>
                      </tr>
                    </thead>
                    <tbody>
                      {branchColumns.map(col => {
                        const rev = branchSectionTotals.revenue[col.id] || 0;
                        const exp = branchSectionTotals.expense[col.id] || 0;
                        const net = branchNet[col.id] || 0;
                        return (
                          <tr key={col.id} style={{ borderBottom: '1px solid #f4f6f9' }}>
                            <td style={{ padding: '0.55rem 0.75rem', fontWeight: 600, fontSize: '0.9rem', color: col.id === 'unallocated' ? '#b0863a' : '#1f2430' }}>
                              <button
                                onClick={() => setBranchTab(col.id)}
                                style={{ background: 'none', border: 'none', padding: 0, font: 'inherit', color: 'inherit', cursor: 'pointer', textDecoration: 'underline', textDecorationColor: '#dde3ec' }}
                              >
                                {col.name}
                              </button>
                            </td>
                            <td style={{ textAlign: 'right', paddingRight: '0.75rem', fontSize: '0.88rem', fontWeight: 600, color: '#1f9d68' }}>{idr(rev)}</td>
                            <td style={{ textAlign: 'right', paddingRight: '0.75rem', fontSize: '0.88rem', fontWeight: 600, color: '#d98324' }}>{idr(exp)}</td>
                            <td style={{ textAlign: 'right', paddingRight: '1rem', fontSize: '0.9rem', fontWeight: 700, color: net >= 0 ? '#1b5e45' : '#c0392b' }}>{idr(net)}</td>
                            <td style={{ paddingLeft: '1rem' }}>
                              <ProportionBar revenue={rev} expense={exp} scale={branchScale} />
                            </td>
                          </tr>
                        );
                      })}
                      <tr style={{ background: '#f0f4ff' }}>
                        <td style={{ padding: '0.6rem 0.75rem', fontWeight: 700, fontSize: '0.9rem' }}>Total</td>
                        <td style={{ textAlign: 'right', paddingRight: '0.75rem', fontWeight: 700, fontSize: '0.88rem' }}>
                          {idr(totalOfAmounts(branchSectionTotals.revenue, branchColumns))}
                        </td>
                        <td style={{ textAlign: 'right', paddingRight: '0.75rem', fontWeight: 700, fontSize: '0.88rem' }}>
                          {idr(totalOfAmounts(branchSectionTotals.expense, branchColumns))}
                        </td>
                        <td style={{ textAlign: 'right', paddingRight: '1rem', fontWeight: 700, fontSize: '0.92rem', color: totalOfAmounts(branchNet, branchColumns) >= 0 ? '#1b5e45' : '#c0392b' }}>
                          {idr(totalOfAmounts(branchNet, branchColumns))}
                        </td>
                        <td />
                      </tr>
                    </tbody>
                  </table>

                  <p style={{ fontSize: '0.78rem', color: '#888', marginTop: '0.75rem', lineHeight: 1.5 }}>
                    Setiap akun pendapatan/beban dipetakan ke cabang pemiliknya — akun cabang, akun divisinya,
                    dan akun turunannya (kategori beban, beban operasional, beban gaji). Tab <strong>Umum</strong> memuat
                    akun yang tidak dimiliki cabang mana pun, sehingga laba tiap cabang hanya menanggung biayanya sendiri.
                    Angka diambil dari jurnal, jadi termasuk pemakaian dispatch, gaji, selisih opname dan jurnal manual.
                  </p>
                </>
              ) : (
                (() => {
                  const col = branchColumns.find(c => c.id === activeBranchTab);
                  if (!col) return null;
                  const one = [col];
                  const rev = branchSectionTotals.revenue[col.id] || 0;
                  const exp = branchSectionTotals.expense[col.id] || 0;
                  const net = branchNet[col.id] || 0;
                  return (
                    <>
                      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(160px,1fr))', gap: '0.75rem', marginBottom: '1.25rem' }}>
                        <Figure label="Pendapatan" value={rev} color="#1f9d68" />
                        <Figure label="Beban" value={exp} color="#d98324" />
                        <Figure
                          label={net >= 0 ? 'Laba Bersih' : 'Rugi Bersih'}
                          value={Math.abs(net)}
                          color={net >= 0 ? '#1b5e45' : '#c0392b'}
                          hint={rev > 0 ? `margin ${Math.round((net / rev) * 100)}%` : 'belum ada pendapatan'}
                        />
                      </div>

                      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                        <thead>
                          <tr style={{ borderBottom: '2px solid #eee' }}>
                            <th style={{ textAlign: 'left', padding: '0.5rem 0.75rem', fontSize: '0.8rem', color: '#888' }}>Akun</th>
                            <th style={{ textAlign: 'right', paddingRight: '0.75rem', fontSize: '0.8rem', color: '#888' }}>{col.name}</th>
                          </tr>
                        </thead>
                        <tbody>
                          <BranchSectionRows title="Pendapatan" nodes={branchTrees.revenue} columns={one} color="#e6f9f0" showTotal={false} />
                          <BranchSectionRows title="Beban" nodes={branchTrees.expense} columns={one} color="#fff3e0" showTotal={false} />
                          <tr style={{ background: '#f0f4ff', borderTop: '2px solid #dde4ff' }}>
                            <td style={{ fontWeight: 700, fontSize: '0.95rem', padding: '0.65rem 0.75rem' }}>Laba / Rugi Bersih</td>
                            <td style={{ textAlign: 'right', paddingRight: '0.75rem', fontWeight: 700, fontSize: '0.9rem', color: net >= 0 ? '#1b5e45' : '#c0392b' }}>
                              {idr(net)}
                            </td>
                          </tr>
                        </tbody>
                      </table>
                    </>
                  );
                })()
              )}
            </>
          )}
        </div>
      )}

      {loading ? (
        <p style={{ padding: '2rem', color: '#999' }}>Memuat...</p>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem' }}>

          {/* ── P&L ── */}
          <div className="card">
            <div className="card-header" style={{ marginBottom: '1rem' }}>
              <h2>Laporan Laba Rugi</h2>
              <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '0.2rem' }}>
                {isPeriod && (
                  <span style={{ fontSize: '0.78rem', color: '#888' }}>{startDate} s/d {endDate}</span>
                )}
                <span style={{ fontSize: '0.75rem', color: '#aaa' }}>Seluruh Organisasi</span>
              </div>
            </div>

            <Section title="Pendapatan" nodes={trees.revenue || []} color="#e6f9f0" />
            <Section title="Beban" nodes={trees.expense || []} color="#fff3e0" />

            <div style={{
              display: 'flex', justifyContent: 'space-between', alignItems: 'center',
              padding: '0.75rem 1rem', marginTop: '0.5rem',
              background: netIncome >= 0 ? '#e6f9f0' : '#fdecea',
              borderRadius: '6px', border: `1px solid ${netIncome >= 0 ? '#b2dfdb' : '#f5c6cb'}`,
            }}>
              <span style={{ fontWeight: 700, fontSize: '1.05rem' }}>
                {netIncome >= 0 ? 'Laba Bersih' : 'Rugi Bersih'}
              </span>
              <span style={{ fontWeight: 700, fontSize: '1.15rem', color: netIncome >= 0 ? '#1b5e45' : '#c0392b' }}>
                {idr(Math.abs(netIncome))}
              </span>
            </div>
          </div>

          {/* ── Balance Sheet ── */}
          <div className="card">
            <div className="card-header" style={{ marginBottom: '1rem' }}>
              <h2>Neraca</h2>
              {isPeriod && (
                <span style={{ fontSize: '0.78rem', color: '#888' }}>Saldo saat ini</span>
              )}
            </div>

            <Section title="Aset" nodes={trees.asset || []} color="#e8f5e9" />
            <Section title="Kewajiban" nodes={trees.liability || []} color="#fce4ec" />
            <Section title="Ekuitas" nodes={trees.equity || []} color="#e8eaf6" />

            <div style={{ padding: '0.5rem 1rem', fontSize: '0.8rem', color: '#888', borderTop: '1px solid #f0f0f0', marginTop: '0.25rem' }}>
              Ekuitas {idr(totalEquity)} + Laba bersih {idr(netIncome)} = {idr(totalEquity + netIncome)}
            </div>

            <div style={{
              display: 'flex', justifyContent: 'space-between', alignItems: 'center',
              padding: '0.75rem 1rem', marginTop: '0.25rem',
              background: '#f0f4ff', borderRadius: '6px',
            }}>
              <span style={{ fontWeight: 700, fontSize: '1.05rem' }}>Total Aset</span>
              <span style={{ fontWeight: 700, fontSize: '1.1rem' }}>{idr(totalAsset)}</span>
            </div>
            <div style={{
              display: 'flex', justifyContent: 'space-between', alignItems: 'center',
              padding: '0.5rem 1rem', fontSize: '0.8rem', color: '#888',
            }}>
              <span>Kewajiban + Ekuitas + Laba</span>
              <span style={{ fontWeight: 600, color: Math.abs(totalAsset - (totalLiab + totalEquity + netIncome)) < 1 ? '#27ae60' : '#e74c3c' }}>
                {idr(totalLiab + totalEquity + netIncome)}
                {Math.abs(totalAsset - (totalLiab + totalEquity + netIncome)) < 1
                  ? ' ✓'
                  : ' ✗ tidak seimbang'}
              </span>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
