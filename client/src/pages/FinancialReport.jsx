import { useEffect, useState, useMemo, useCallback } from 'react';
import { Link } from 'react-router-dom';
import * as XLSX from 'xlsx';
import { getFinancialReport } from '../api';

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

function effectiveBalance(node) {
  if (node.children.length === 0) return node.balance;
  return node.children.reduce((s, c) => s + effectiveBalance(c), 0);
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

// ── Main component ─────────────────────────────────────────────────────────────

export default function FinancialReport() {
  const [accounts, setAccounts] = useState([]);
  const [loading, setLoading] = useState(true);
  const [startDate, setStartDate] = useState(firstOfMonthStr());
  const [endDate, setEndDate]     = useState(todayStr());
  const [isPeriod, setIsPeriod]   = useState(false);

  const fetchReport = useCallback(() => {
    setLoading(true);
    const params = isPeriod ? { start_date: startDate, end_date: endDate } : {};
    getFinancialReport(params)
      .then(r => setAccounts(r.data))
      .finally(() => setLoading(false));
  }, [isPeriod, startDate, endDate]);

  useEffect(() => { fetchReport(); }, [fetchReport]);

  const { trees } = useMemo(() => {
    const byType = { asset: [], liability: [], equity: [], revenue: [], expense: [] };
    accounts.forEach(a => { if (byType[a.account_type]) byType[a.account_type].push(a); });
    const trees = {};
    for (const type of Object.keys(byType)) trees[type] = buildTree(byType[type]);
    return { trees };
  }, [accounts]);

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
          <button
            className="btn btn-primary"
            onClick={() => buildExcel({ trees, startDate, endDate, isPeriod })}
            disabled={loading}
          >
            Unduh Excel
          </button>
        </div>
      </div>

      {/* Date range filter */}
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

          {isPeriod && (
            <span style={{ fontSize: '0.82rem', color: '#888', alignSelf: 'flex-end', paddingBottom: '0.25rem' }}>
              Pendapatan &amp; beban dihitung dari transaksi dalam periode ini. Neraca menggunakan saldo saat ini.
            </span>
          )}
        </div>
      </div>

      {loading ? (
        <p style={{ padding: '2rem', color: '#999' }}>Memuat...</p>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem' }}>

          {/* ── P&L ── */}
          <div className="card">
            <div className="card-header" style={{ marginBottom: '1rem' }}>
              <h2>Laporan Laba Rugi</h2>
              {isPeriod && (
                <span style={{ fontSize: '0.78rem', color: '#888' }}>{startDate} s/d {endDate}</span>
              )}
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
