import { useEffect, useState, useMemo, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { getFinancialReport, getCashSummaryReport, getHRSettings } from '../api';

const SERVER = 'http://localhost:5000';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v ?? 0);

function todayStr() {
  return new Date().toISOString().slice(0, 10);
}

function firstOfMonthStr() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`;
}

function fmtLongDate(str) {
  if (!str) return '';
  const d = new Date(`${str}T00:00:00`);
  return d.toLocaleDateString('id-ID', { day: 'numeric', month: 'long', year: 'numeric' });
}

// ── P&L tree helpers (leaf accounts only, for a clean statement) ────────────────

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

// Flatten to leaf accounts with a non-zero balance, preserving account order.
function leafLines(nodes, out = []) {
  for (const node of nodes) {
    if (node.children.length === 0) {
      if (node.balance !== 0) out.push({ name: node.name, amount: node.balance });
    } else {
      leafLines(node.children, out);
    }
  }
  return out;
}

// ── Presentation blocks ─────────────────────────────────────────────────────────

function LineRow({ label, amount, indent = false, muted = false }) {
  return (
    <tr>
      <td style={{ padding: '0.28rem 0.5rem 0.28rem 0', paddingLeft: indent ? '1.5rem' : 0, color: muted ? '#666' : '#111' }}>
        {label}
      </td>
      <td style={{ padding: '0.28rem 0', textAlign: 'right', fontVariantNumeric: 'tabular-nums', color: amount < 0 ? '#c0392b' : (muted ? '#666' : '#111') }}>
        {idr(amount)}
      </td>
    </tr>
  );
}

function SubtotalRow({ label, amount }) {
  return (
    <tr>
      <td style={{ padding: '0.35rem 0.5rem 0.35rem 0', fontWeight: 600, borderTop: '1px solid #ccc' }}>{label}</td>
      <td style={{ padding: '0.35rem 0', textAlign: 'right', fontWeight: 600, fontVariantNumeric: 'tabular-nums', borderTop: '1px solid #ccc' }}>
        {idr(amount)}
      </td>
    </tr>
  );
}

function TotalRow({ label, amount, negativeLabel }) {
  const neg = amount < 0;
  return (
    <tr>
      <td style={{ padding: '0.5rem 0.5rem 0.5rem 0', fontWeight: 700, borderTop: '2px solid #333', borderBottom: '2px solid #333' }}>
        {neg && negativeLabel ? negativeLabel : label}
      </td>
      <td style={{ padding: '0.5rem 0', textAlign: 'right', fontWeight: 700, fontVariantNumeric: 'tabular-nums', borderTop: '2px solid #333', borderBottom: '2px solid #333', color: neg ? '#c0392b' : '#111' }}>
        {idr(Math.abs(amount))}
      </td>
    </tr>
  );
}

function StatementSection({ title, children }) {
  return (
    <div style={{ marginTop: '1.75rem' }}>
      <h3 style={{ margin: '0 0 0.5rem', fontSize: '0.95rem', letterSpacing: '0.03em', textTransform: 'uppercase', borderBottom: '1px solid #333', paddingBottom: '0.3rem' }}>
        {title}
      </h3>
      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.88rem' }}>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

// ── Main component ─────────────────────────────────────────────────────────────

export default function FinancialStatement() {
  const [startDate, setStartDate] = useState(firstOfMonthStr());
  const [endDate, setEndDate]     = useState(todayStr());
  const [accounts, setAccounts]   = useState([]);
  const [cash, setCash]           = useState(null);
  const [settings, setSettings]   = useState(null);
  const [loading, setLoading]     = useState(true);
  const [error, setError]         = useState('');

  useEffect(() => {
    getHRSettings().then(r => setSettings(r.data)).catch(() => {});
  }, []);

  const fetchData = useCallback(() => {
    if (!startDate || !endDate) return;
    setLoading(true);
    setError('');
    const params = { start_date: startDate, end_date: endDate };
    Promise.all([getFinancialReport(params), getCashSummaryReport(params)])
      .then(([fin, csh]) => {
        setAccounts(fin.data);
        setCash(csh.data);
      })
      .catch(() => setError('Gagal memuat data laporan.'))
      .finally(() => setLoading(false));
  }, [startDate, endDate]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const pl = useMemo(() => {
    const revenue = [], expense = [];
    accounts.forEach(a => {
      if (a.account_type === 'revenue') revenue.push(a);
      else if (a.account_type === 'expense') expense.push(a);
    });
    const revLines = leafLines(buildTree(revenue));
    const expLines = leafLines(buildTree(expense));
    const totalRevenue = revLines.reduce((s, l) => s + l.amount, 0);
    const totalExpense = expLines.reduce((s, l) => s + l.amount, 0);
    return { revLines, expLines, totalRevenue, totalExpense, netIncome: totalRevenue - totalExpense };
  }, [accounts]);

  const companyName = settings?.company_name?.trim() || 'Perusahaan';
  const address = settings?.address?.trim() || '';
  const logoPath = settings?.logo_path?.trim();
  const footer = settings?.payslip_footer?.trim();

  return (
    <>
      {/* Print rules: hide the app chrome and controls, show only the sheet. */}
      <style>{`
        @media print {
          .navbar { display: none !important; }
          .main-content { padding: 0 !important; margin: 0 !important; max-width: none !important; }
          .fs-no-print { display: none !important; }
          .fs-sheet { box-shadow: none !important; margin: 0 !important; border: none !important; width: 100% !important; max-width: none !important; }
          @page { size: A4; margin: 16mm; }
        }
      `}</style>

      {/* Toolbar (not printed) */}
      <div className="fs-no-print page-header">
        <h1>Dokumen Laporan Keuangan</h1>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
          <Link to="/reports/financial" className="btn btn-secondary">Versi Interaktif</Link>
          <button className="btn btn-primary" onClick={() => window.print()} disabled={loading}>
            Cetak / Simpan PDF
          </button>
        </div>
      </div>

      <div className="fs-no-print card" style={{ marginBottom: '1.25rem' }}>
        <div style={{ display: 'flex', alignItems: 'flex-end', gap: '1rem', flexWrap: 'wrap' }}>
          <div className="form-group" style={{ margin: 0 }}>
            <label style={{ fontSize: '0.78rem', color: '#888', marginBottom: '0.2rem', display: 'block' }}>Dari</label>
            <input type="date" className="form-control" value={startDate} onChange={e => setStartDate(e.target.value)} style={{ width: 'auto' }} />
          </div>
          <div className="form-group" style={{ margin: 0 }}>
            <label style={{ fontSize: '0.78rem', color: '#888', marginBottom: '0.2rem', display: 'block' }}>Sampai</label>
            <input type="date" className="form-control" value={endDate} onChange={e => setEndDate(e.target.value)} style={{ width: 'auto' }} />
          </div>
          <span style={{ fontSize: '0.82rem', color: '#888', paddingBottom: '0.4rem' }}>
            Laba rugi &amp; arus kas dihitung dari transaksi dalam periode ini.
          </span>
        </div>
      </div>

      {error && <div className="fs-no-print card" style={{ color: '#c0392b', marginBottom: '1rem' }}>{error}</div>}

      {loading ? (
        <p className="fs-no-print" style={{ padding: '2rem', color: '#999' }}>Memuat...</p>
      ) : (
        <div
          className="fs-sheet"
          style={{
            background: '#fff', color: '#111', maxWidth: 800, margin: '0 auto',
            padding: '2.5rem 3rem', boxShadow: '0 1px 8px rgba(0,0,0,0.12)', borderRadius: 4,
          }}
        >
          {/* Document header */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '1.25rem', borderBottom: '2px solid #333', paddingBottom: '1rem' }}>
            {logoPath && (
              <img src={`${SERVER}/uploads/${logoPath}`} alt="Logo" style={{ width: 64, height: 64, objectFit: 'contain' }} />
            )}
            <div>
              <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>{companyName}</div>
              {address && <div style={{ fontSize: '0.85rem', color: '#555', marginTop: '0.15rem' }}>{address}</div>}
            </div>
          </div>

          {/* Title + period */}
          <div style={{ textAlign: 'center', margin: '1.5rem 0 0.25rem' }}>
            <h2 style={{ margin: 0, fontSize: '1.15rem', letterSpacing: '0.05em' }}>LAPORAN KEUANGAN</h2>
            <div style={{ fontSize: '0.9rem', color: '#555', marginTop: '0.3rem' }}>
              Periode {fmtLongDate(startDate)} s/d {fmtLongDate(endDate)}
            </div>
          </div>

          {/* Laba Rugi */}
          <StatementSection title="Laporan Laba Rugi">
            <tr><td colSpan={2} style={{ paddingTop: '0.4rem', fontWeight: 600 }}>Pendapatan</td></tr>
            {pl.revLines.length === 0
              ? <tr><td colSpan={2} style={{ paddingLeft: '1.5rem', color: '#999' }}>Tidak ada pendapatan</td></tr>
              : pl.revLines.map((l, i) => <LineRow key={i} label={l.name} amount={l.amount} indent />)}
            <SubtotalRow label="Total Pendapatan" amount={pl.totalRevenue} />

            <tr><td colSpan={2} style={{ paddingTop: '0.75rem', fontWeight: 600 }}>Beban</td></tr>
            {pl.expLines.length === 0
              ? <tr><td colSpan={2} style={{ paddingLeft: '1.5rem', color: '#999' }}>Tidak ada beban</td></tr>
              : pl.expLines.map((l, i) => <LineRow key={i} label={l.name} amount={l.amount} indent />)}
            <SubtotalRow label="Total Beban" amount={pl.totalExpense} />

            <tr><td colSpan={2} style={{ height: '0.5rem' }} /></tr>
            <TotalRow label="Laba Bersih" negativeLabel="Rugi Bersih" amount={pl.netIncome} />
          </StatementSection>

          {/* Arus Kas */}
          {cash && (
            <StatementSection title="Ringkasan Arus Kas">
              <tr><td colSpan={2} style={{ paddingTop: '0.4rem', fontWeight: 600 }}>Kas Masuk</td></tr>
              {cash.inflows.map((l, i) => <LineRow key={i} label={l.label} amount={l.amount} indent />)}
              <SubtotalRow label="Total Kas Masuk" amount={cash.total_inflow} />

              <tr><td colSpan={2} style={{ paddingTop: '0.75rem', fontWeight: 600 }}>Kas Keluar</td></tr>
              {cash.outflows.map((l, i) => <LineRow key={i} label={l.label} amount={l.amount} indent />)}
              <SubtotalRow label="Total Kas Keluar" amount={cash.total_outflow} />

              <tr><td colSpan={2} style={{ height: '0.5rem' }} /></tr>
              <TotalRow label="Arus Kas Bersih" negativeLabel="Arus Kas Bersih (Defisit)" amount={cash.net_cash_flow} />
            </StatementSection>
          )}

          {footer && (
            <p style={{ marginTop: '1.75rem', fontSize: '0.8rem', color: '#666', fontStyle: 'italic' }}>{footer}</p>
          )}

          {/* Signatures */}
          <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: '3rem', fontSize: '0.88rem' }}>
            <div style={{ textAlign: 'center', minWidth: 180 }}>
              <div style={{ color: '#555' }}>Disiapkan oleh,</div>
              <div style={{ height: '4rem' }} />
              <div style={{ borderTop: '1px solid #333', paddingTop: '0.3rem' }}>(_____________________)</div>
            </div>
            <div style={{ textAlign: 'center', minWidth: 180 }}>
              <div style={{ color: '#555' }}>Disetujui oleh,</div>
              <div style={{ height: '4rem' }} />
              <div style={{ borderTop: '1px solid #333', paddingTop: '0.3rem' }}>(_____________________)</div>
            </div>
          </div>

          <div style={{ marginTop: '1.5rem', fontSize: '0.75rem', color: '#999', textAlign: 'right' }}>
            Dicetak pada {fmtLongDate(todayStr())}
          </div>
        </div>
      )}
    </>
  );
}
