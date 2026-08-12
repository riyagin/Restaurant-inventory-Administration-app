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

function BranchAccountRow({ node, columns, depth = 0 }) {
  const [open, setOpen] = useState(depth < 2);
  const amounts = effectiveAmounts(node, columns);
  const total = totalOfAmounts(amounts, columns);
  const hasChildren = node.children.length > 0;
  const isRoot = depth === 0;

  if (total === 0 && !hasChildren && !isRoot) return null;

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
        <td style={{ textAlign: 'right', paddingRight: '1rem', fontSize: '0.82rem', fontWeight: 700, borderLeft: '1px solid #eee', color: total < 0 ? '#e74c3c' : undefined }}>
          {total !== 0 ? idr(total) : <span style={{ color: '#ddd' }}>—</span>}
        </td>
      </tr>
      {open && node.children.map(child => (
        <BranchAccountRow key={child.id} node={child} columns={columns} depth={depth + 1} />
      ))}
    </>
  );
}

function BranchSectionRows({ title, nodes, columns, color }) {
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
        <td style={{ textAlign: 'right', paddingRight: '1rem', fontWeight: 700, fontSize: '0.85rem', borderLeft: '1px solid #eee' }}>
          {idr(totalOfAmounts(totals, columns))}
        </td>
      </tr>
      {nodes.map(n => <BranchAccountRow key={n.id} node={n} columns={columns} depth={0} />)}
    </>
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

  const branchNet = useMemo(() => {
    if (branchColumns.length === 0) return {};
    const rev = sumAmounts((branchTrees.revenue || []).map(n => effectiveAmounts(n, branchColumns)), branchColumns);
    const exp = sumAmounts((branchTrees.expense || []).map(n => effectiveAmounts(n, branchColumns)), branchColumns);
    const out = {};
    for (const col of branchColumns) out[col.id] = rev[col.id] - exp[col.id];
    return out;
  }, [branchTrees, branchColumns]);

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
              <div style={{ overflowX: 'auto' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', minWidth: `${28 + branchColumns.length * 9}rem` }}>
                  <thead>
                    <tr style={{ borderBottom: '2px solid #eee' }}>
                      <th style={{ textAlign: 'left', padding: '0.5rem 0.75rem', fontSize: '0.8rem', color: '#888', position: 'sticky', left: 0, background: '#fff' }}>Akun</th>
                      {branchColumns.map(col => (
                        <th key={col.id} style={{ textAlign: 'right', paddingRight: '0.75rem', fontSize: '0.8rem', color: col.id === 'unallocated' ? '#b0863a' : '#888' }}>
                          {col.name}
                        </th>
                      ))}
                      <th style={{ textAlign: 'right', paddingRight: '1rem', fontSize: '0.8rem', color: '#555', borderLeft: '1px solid #eee' }}>Total</th>
                    </tr>
                  </thead>
                  <tbody>
                    <BranchSectionRows title="Pendapatan" nodes={branchTrees.revenue} columns={branchColumns} color="#e6f9f0" />
                    <BranchSectionRows title="Beban" nodes={branchTrees.expense} columns={branchColumns} color="#fff3e0" />
                    <tr style={{ background: '#f0f4ff', borderTop: '2px solid #dde4ff' }}>
                      <td style={{ fontWeight: 700, fontSize: '0.95rem', padding: '0.65rem 0.75rem', position: 'sticky', left: 0, background: '#f0f4ff' }}>
                        Laba / Rugi Bersih
                      </td>
                      {branchColumns.map(col => (
                        <td key={col.id} style={{ textAlign: 'right', paddingRight: '0.75rem', fontWeight: 700, fontSize: '0.88rem', color: branchNet[col.id] >= 0 ? '#1b5e45' : '#c0392b' }}>
                          {idr(branchNet[col.id])}
                        </td>
                      ))}
                      <td style={{ textAlign: 'right', paddingRight: '1rem', fontWeight: 700, fontSize: '0.92rem', borderLeft: '1px solid #eee', color: totalOfAmounts(branchNet, branchColumns) >= 0 ? '#1b5e45' : '#c0392b' }}>
                        {idr(totalOfAmounts(branchNet, branchColumns))}
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>

              <p style={{ fontSize: '0.78rem', color: '#888', marginTop: '0.75rem', lineHeight: 1.5 }}>
                Setiap akun pendapatan/beban dipetakan ke cabang pemiliknya — akun cabang, akun divisinya,
                dan akun turunannya (kategori beban, beban gaji). Kolom <strong>Umum</strong> memuat akun
                yang tidak dimiliki cabang mana pun, sehingga laba tiap cabang hanya menanggung biayanya sendiri.
                Angka diambil dari jurnal, jadi termasuk pemakaian dispatch, gaji, selisih opname dan jurnal manual.
              </p>
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
