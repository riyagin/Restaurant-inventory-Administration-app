import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { getStats, getDailySalesByBranch, getStockFlow, getBranches } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const fmtDate = (d) =>
  d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '—';

const fmtDateShort = (d) => {
  if (!d) return '';
  const dt = new Date(d + 'T00:00:00');
  return dt.toLocaleDateString('id-ID', { day: '2-digit', month: 'short' });
};

const todayStr = new Date().toISOString().split('T')[0];

function offsetDate(dateStr, days) {
  const d = new Date(dateStr);
  d.setDate(d.getDate() + days);
  return d.toISOString().split('T')[0];
}

function nDaysAgo(n) {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return d.toISOString().split('T')[0];
}

const PERIODS = [
  { key: 'daily',   label: 'Harian' },
  { key: 'weekly',  label: 'Mingguan' },
  { key: 'monthly', label: 'Bulanan' },
  { key: 'yearly',  label: 'Tahunan' },
];

const PERIOD_LABELS = {
  daily:   'Hari Ini',
  weekly:  '7 Hari Terakhir',
  monthly: 'Bulan Ini',
  yearly:  'Tahun Ini',
};

// ─── compact number formatter for chart y-axis ───────────────────────────────
function compactIdr(v) {
  if (v === 0) return '0';
  if (v >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(1).replace('.0', '')}M`;
  if (v >= 1_000_000)     return `${(v / 1_000_000).toFixed(1).replace('.0', '')}jt`;
  if (v >= 1_000)         return `${(v / 1_000).toFixed(0)}rb`;
  return String(v);
}

// ─── SVG bar chart ────────────────────────────────────────────────────────────
function StockFlowChart({ data }) {
  if (!data || data.length === 0) {
    return <p style={{ color: '#999', fontSize: '0.85rem', textAlign: 'center', padding: '2rem 0' }}>Tidak ada data untuk rentang ini.</p>;
  }

  const W = 700, H = 240;
  const padL = 58, padR = 12, padT = 14, padB = 42;
  const plotW = W - padL - padR;
  const plotH = H - padT - padB;

  const maxVal = Math.max(...data.flatMap(d => [d.revenue, d.spend]), 1);
  const yMax   = maxVal * 1.12;

  const groupW = plotW / data.length;
  const barPad = Math.max(groupW * 0.12, 2);
  const barW   = Math.max((groupW - barPad * 2 - 2) / 2, 3);

  const py = (val) => padT + plotH - (val / yMax) * plotH;
  const bh = (val) => Math.max((val / yMax) * plotH, 0);

  const yTicks = [0, 0.25, 0.5, 0.75, 1].map(f => yMax * f);

  return (
    <div style={{ width: '100%', overflowX: 'auto' }}>
      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', minWidth: '320px', height: 'auto', display: 'block' }}>
        {yTicks.map((tick, i) => (
          <g key={i}>
            <line x1={padL} y1={py(tick)} x2={padL + plotW} y2={py(tick)} stroke="#f0f0f0" strokeWidth={1} />
            <text x={padL - 5} y={py(tick) + 4} textAnchor="end" fontSize={10} fill="#aaa">
              {compactIdr(tick)}
            </text>
          </g>
        ))}

        {data.map((d, i) => {
          const gx = padL + i * groupW + barPad;
          return (
            <g key={d.date}>
              <rect x={gx} y={py(d.spend)} width={barW} height={bh(d.spend)}
                fill="#f97316" rx={2} opacity={0.85}>
                <title>Pengeluaran: {idr(d.spend)}</title>
              </rect>
              <rect x={gx + barW + 2} y={py(d.revenue)} width={barW} height={bh(d.revenue)}
                fill="#22c55e" rx={2} opacity={0.85}>
                <title>Pendapatan: {idr(d.revenue)}</title>
              </rect>
              <text
                x={gx + barW + 1} y={H - padB + 14}
                textAnchor="middle" fontSize={data.length > 20 ? 7 : 9} fill="#999"
              >
                {fmtDateShort(d.date)}
              </text>
            </g>
          );
        })}

        <line x1={padL} y1={padT} x2={padL} y2={padT + plotH} stroke="#e0e0e0" strokeWidth={1} />
        <line x1={padL} y1={padT + plotH} x2={padL + plotW} y2={padT + plotH} stroke="#e0e0e0" strokeWidth={1} />
      </svg>

      <div style={{ display: 'flex', gap: '1.25rem', justifyContent: 'center', marginTop: '0.25rem' }}>
        <span style={{ fontSize: '0.8rem', color: '#555', display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
          <span style={{ width: 12, height: 12, background: '#22c55e', borderRadius: 2, display: 'inline-block' }} />
          Pendapatan
        </span>
        <span style={{ fontSize: '0.8rem', color: '#555', display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
          <span style={{ width: 12, height: 12, background: '#f97316', borderRadius: 2, display: 'inline-block' }} />
          Pengeluaran
        </span>
      </div>
    </div>
  );
}

// ─── stock flow card ──────────────────────────────────────────────────────────
const QUICK_RANGES = [
  { label: '7 Hari',    days: 6  },
  { label: '14 Hari',   days: 13 },
  { label: '30 Hari',   days: 29 },
  { label: 'Bulan Ini', days: null }, // special: 1st of month → today
];

function getMonthStart() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`;
}

function StockFlowCard({ branchId }) {
  const defaultStart = nDaysAgo(6);

  const [rangeStart, setRangeStart] = useState(defaultStart);
  const [rangeEnd,   setRangeEnd]   = useState(todayStr);
  const [draft, setDraft] = useState({ start: defaultStart, end: todayStr });
  const [activePreset, setActivePreset] = useState('7 Hari');

  const [data,    setData]    = useState(null);
  const [loading, setLoading] = useState(true);

  const fetchRange = useCallback((s, e, bid) => {
    setLoading(true);
    const params = { start: s, end: e };
    if (bid) params.branch_id = bid;
    getStockFlow(params)
      .then(r  => { setData(r.data);  setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  useEffect(() => { fetchRange(rangeStart, rangeEnd, branchId); }, [branchId]); // eslint-disable-line

  function applyPreset(opt) {
    const s = opt.days === null ? getMonthStart() : nDaysAgo(opt.days);
    const e = todayStr;
    setRangeStart(s); setRangeEnd(e);
    setDraft({ start: s, end: e });
    setActivePreset(opt.label);
    fetchRange(s, e, branchId);
  }

  function applyCustom() {
    if (!draft.start || !draft.end || draft.start > draft.end) return;
    setRangeStart(draft.start); setRangeEnd(draft.end);
    setActivePreset(null);
    fetchRange(draft.start, draft.end, branchId);
  }

  const s = data?.summary;

  return (
    <div className="card" style={{ marginBottom: '1.5rem' }}>
      <div className="card-header">
        <h2 style={{ margin: 0 }}>Pendapatan vs Pengeluaran</h2>
      </div>

      {/* summary tiles */}
      {loading ? (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '0.75rem', marginBottom: '1.25rem' }}>
          {[0, 1, 2].map(i => (
            <div key={i} style={tileStyle('#f5f5f5', '#ddd')}>
              <div style={tileLabelStyle}>—</div>
              <div style={{ ...tileValueStyle, color: '#ccc' }}>—</div>
            </div>
          ))}
        </div>
      ) : !s ? (
        <p style={{ color: '#e74c3c', fontSize: '0.88rem' }}>Gagal memuat data.</p>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '0.75rem', marginBottom: '1.25rem' }}>
          <div style={tileStyle('#f0fdf4', '#15803d')}>
            <div style={tileLabelStyle}>Pendapatan</div>
            <div style={tileValueStyle}>{idr(s.revenue)}</div>
          </div>
          <div style={tileStyle('#fff7ed', '#c2410c')}>
            <div style={tileLabelStyle}>Pengeluaran</div>
            <div style={tileValueStyle}>{idr(s.spend)}</div>
          </div>
          <div style={tileStyle(s.margin >= 0 ? '#f0fdf4' : '#fef2f2', s.margin >= 0 ? '#15803d' : '#b91c1c')}>
            <div style={tileLabelStyle}>Selisih (Margin)</div>
            <div style={{ ...tileValueStyle, color: s.margin >= 0 ? '#15803d' : '#b91c1c' }}>
              {s.margin >= 0 ? '+' : ''}{idr(s.margin)}
            </div>
          </div>
        </div>
      )}

      {/* range controls — sit directly above the chart */}
      <div style={{ borderTop: '1px solid #f0f0f0', paddingTop: '0.75rem', marginBottom: '0.75rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', flexWrap: 'wrap' }}>
          {QUICK_RANGES.map(opt => (
            <button
              key={opt.label}
              className="btn btn-secondary btn-sm"
              style={{ background: activePreset === opt.label ? '#4f8ef7' : undefined, color: activePreset === opt.label ? '#fff' : undefined }}
              onClick={() => applyPreset(opt)}
            >
              {opt.label}
            </button>
          ))}

          <span style={{ color: '#ddd', margin: '0 0.15rem' }}>|</span>

          <input
            type="date"
            value={draft.start}
            max={draft.end}
            onChange={e => { setDraft(d => ({ ...d, start: e.target.value })); setActivePreset(null); }}
            style={dateInputStyle}
          />
          <span style={{ color: '#aaa', fontSize: '0.8rem' }}>–</span>
          <input
            type="date"
            value={draft.end}
            min={draft.start}
            max={todayStr}
            onChange={e => { setDraft(d => ({ ...d, end: e.target.value })); setActivePreset(null); }}
            style={dateInputStyle}
          />
          <button className="btn btn-primary btn-sm" onClick={applyCustom}>Terapkan</button>
        </div>
      </div>

      {loading ? (
        <p style={{ color: '#999', fontSize: '0.85rem', textAlign: 'center', padding: '1.5rem 0' }}>Memuat...</p>
      ) : (
        <StockFlowChart data={data?.chart} />
      )}
    </div>
  );
}

const tileStyle = (bg, color) => ({
  background: bg, borderRadius: 8, padding: '0.85rem 1rem',
  borderLeft: `3px solid ${color}`,
});
const tileLabelStyle = { fontSize: '0.75rem', color: '#666', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.35rem' };
const tileValueStyle  = { fontSize: '1.1rem', fontWeight: 700, color: '#1a1a2e' };
const dateInputStyle  = { fontSize: '0.8rem', padding: '0.25rem 0.4rem', borderRadius: 5, border: '1px solid #ddd' };

// ─── daily sales card ─────────────────────────────────────────────────────────
function DailySalesCard() {
  const [date, setDate]   = useState(todayStr);
  const [data, setData]   = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    getDailySalesByBranch(date)
      .then(r => { setData(r.data); setLoading(false); })
      .catch(() => setLoading(false));
  }, [date]);

  const isToday    = date === todayStr;
  const grandTotal = data ? data.branches.reduce((s, b) => s + b.total, 0) : 0;

  return (
    <div className="card" style={{ marginBottom: '1.5rem' }}>
      <div className="card-header" style={{ alignItems: 'center' }}>
        <h2 style={{ margin: 0 }}>Penjualan Harian per Cabang</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <button className="btn btn-secondary btn-sm"
            onClick={() => setDate(d => offsetDate(d, -1))}
            style={{ padding: '0.25rem 0.6rem', fontSize: '1rem', lineHeight: 1 }}>‹</button>
          <span style={{ fontWeight: 600, fontSize: '0.9rem', minWidth: '120px', textAlign: 'center' }}>
            {fmtDate(date)}
            {isToday && <span style={{ marginLeft: '0.35rem', fontSize: '0.72rem', background: '#e8f5e9', color: '#2e7d32', borderRadius: '4px', padding: '0.05rem 0.35rem', fontWeight: 700 }}>Hari ini</span>}
          </span>
          <button className="btn btn-secondary btn-sm"
            onClick={() => setDate(d => offsetDate(d, 1))}
            disabled={isToday}
            style={{ padding: '0.25rem 0.6rem', fontSize: '1rem', lineHeight: 1 }}>›</button>
        </div>
      </div>

      {loading ? (
        <p style={{ color: '#999', fontSize: '0.88rem', padding: '0.5rem 0' }}>Memuat...</p>
      ) : !data ? (
        <p style={{ color: '#e74c3c', fontSize: '0.88rem' }}>Gagal memuat data.</p>
      ) : (
        <>
          <table className="daily-sales-table">
            <thead>
              <tr>
                <th>Cabang</th>
                <th style={{ textAlign: 'right' }}>POS Import</th>
                <th style={{ textAlign: 'right' }}>Manual</th>
                <th style={{ textAlign: 'right' }}>Total</th>
              </tr>
            </thead>
            <tbody>
              {data.branches.map(b => (
                <tr key={b.branch_id}>
                  <td style={{ fontWeight: 500 }}>{b.branch_name}</td>
                  <td style={{ textAlign: 'right', color: b.pos_revenue > 0 ? '#2c6fc2' : '#bbb', fontWeight: b.pos_revenue > 0 ? 600 : 'normal', fontSize: '0.88rem' }}>
                    {b.pos_revenue > 0 ? idr(b.pos_revenue) : '—'}
                    {b.pos_import_count > 0 && <span style={{ marginLeft: '0.3rem', fontSize: '0.75rem', color: '#888' }}>({b.pos_import_count}x)</span>}
                  </td>
                  <td style={{ textAlign: 'right', color: b.manual_sales > 0 ? '#555' : '#bbb', fontSize: '0.88rem' }}>
                    {b.manual_sales > 0 ? idr(b.manual_sales) : '—'}
                    {b.sale_count > 0 && <span style={{ marginLeft: '0.3rem', fontSize: '0.75rem', color: '#888' }}>({b.sale_count}x)</span>}
                  </td>
                  <td style={{ textAlign: 'right', fontWeight: b.total > 0 ? 700 : 'normal', color: b.total > 0 ? '#1a6632' : '#bbb' }}>
                    {idr(b.total)}
                  </td>
                </tr>
              ))}
              {data.branches.length > 0 && (
                <tr style={{ borderTop: '2px solid #e0e0e0', fontWeight: 700 }}>
                  <td>Total</td>
                  <td style={{ textAlign: 'right', color: '#2c6fc2' }}>{idr(data.branches.reduce((s, b) => s + b.pos_revenue, 0))}</td>
                  <td style={{ textAlign: 'right', color: '#555' }}>{idr(data.branches.reduce((s, b) => s + b.manual_sales, 0))}</td>
                  <td style={{ textAlign: 'right', color: grandTotal > 0 ? '#1a6632' : '#bbb' }}>{idr(grandTotal)}</td>
                </tr>
              )}
            </tbody>
          </table>
          {data.branches.length === 0 && (
            <p style={{ color: '#999', fontSize: '0.88rem', margin: '0.5rem 0 0' }}>Tidak ada cabang terdaftar.</p>
          )}
        </>
      )}
    </div>
  );
}

// ─── main dashboard ───────────────────────────────────────────────────────────
export default function Dashboard() {
  const [period,   setPeriod]   = useState('daily');
  const [branchId, setBranchId] = useState('');
  const [branches, setBranches] = useState([]);
  const [stats,    setStats]    = useState(null);
  const [error,    setError]    = useState('');

  useEffect(() => {
    getBranches().then(r => setBranches(r.data)).catch(() => {});
  }, []);

  useEffect(() => {
    setStats(null);
    const params = { period };
    if (branchId) params.branch_id = branchId;
    getStats(params)
      .then(r => setStats(r.data))
      .catch(err => setError(err.response?.data?.error || err.message || 'Gagal memuat dasbor'));
  }, [period, branchId]);

  if (error) return <p style={{ padding: '2rem', color: '#e74c3c' }}>Error: {error}</p>;

  const outstanding  = stats?.outstandingInvoices || [];
  const overdueCount = outstanding.filter(inv => inv.due_date && inv.due_date < todayStr).length;

  return (
    <>
      <div className="page-header" style={{ alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
        <h1 style={{ margin: 0 }}>Dasbor</h1>
        {/* period filter */}
        <div style={{ display: 'flex', gap: '0.35rem', background: '#f3f4f6', borderRadius: 8, padding: '0.25rem' }}>
          {PERIODS.map(p => (
            <button
              key={p.key}
              onClick={() => setPeriod(p.key)}
              style={{
                border: 'none', cursor: 'pointer', borderRadius: 6,
                padding: '0.3rem 0.75rem', fontSize: '0.82rem', fontWeight: 600,
                background: period === p.key ? '#fff' : 'transparent',
                color:      period === p.key ? '#1a1a2e' : '#888',
                boxShadow:  period === p.key ? '0 1px 3px rgba(0,0,0,0.12)' : 'none',
                transition: 'all 0.15s',
              }}
            >
              {p.label}
            </button>
          ))}
        </div>
        {/* branch filter */}
        {branches.length > 0 && (
          <select
            value={branchId}
            onChange={e => setBranchId(e.target.value)}
            style={{ padding: '0.35rem 0.6rem', border: '1px solid #ddd', borderRadius: 6, fontSize: '0.82rem', fontWeight: 600, color: branchId ? '#1a1a2e' : '#888' }}
          >
            <option value="">Semua Cabang</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
        )}
      </div>

      {/* stat cards */}
      {!stats ? (
        <div className="stats-grid" style={{ marginBottom: '1.5rem' }}>
          {[0,1,2,3].map(i => (
            <div key={i} className="stat-card" style={{ opacity: 0.4 }}>
              <div className="label">—</div>
              <div className="value" style={{ fontSize: '1.5rem', color: '#ccc' }}>—</div>
            </div>
          ))}
        </div>
      ) : (
        <div className="stats-grid" style={{ marginBottom: '1.5rem' }}>
          <div className="stat-card">
            <div className="label">Total Produk</div>
            <div className="value">{stats.totalItems}</div>
            <div style={{ fontSize: '0.8rem', color: '#aaa', marginTop: '0.25rem' }}>
              {stats.totalInventoryRecords} catatan inventaris
            </div>
          </div>

          <div className="stat-card">
            <div className="label">Nilai Inventaris Global</div>
            <div className="value" style={{ fontSize: '1.25rem' }}>{idr(stats.totalInventoryValue)}</div>
          </div>

          <div className="stat-card">
            <div className="label">Pembelian — {PERIOD_LABELS[period]}</div>
            <div className="value" style={{ fontSize: '1.25rem', color: stats.purchasesTotal > 0 ? '#2c6fc2' : '#ccc' }}>
              {idr(stats.purchasesTotal)}
            </div>
            {stats.purchasesCount > 0 && (
              <div style={{ fontSize: '0.8rem', color: '#aaa', marginTop: '0.25rem' }}>
                {stats.purchasesCount} invoice
              </div>
            )}
          </div>

          <Link to="/invoices?status=unpaid" className="stat-card"
            style={{ textDecoration: 'none', cursor: 'pointer',
              background: outstanding.length > 0 ? '#fff8e1' : undefined,
              borderColor: outstanding.length > 0 ? '#ffe082' : undefined }}>
            <div className="label" style={{ color: outstanding.length > 0 ? '#b45309' : undefined }}>Invoice Belum Lunas</div>
            <div className="value" style={{ color: outstanding.length > 0 ? '#b45309' : '#ccc' }}>
              {outstanding.length}
            </div>
            {overdueCount > 0 && (
              <div style={{ fontSize: '0.8rem', color: '#e74c3c', fontWeight: 600, marginTop: '0.25rem' }}>
                {overdueCount} lewat jatuh tempo
              </div>
            )}
          </Link>
        </div>
      )}

      {/* stock flow card */}
      <StockFlowCard branchId={branchId} />

      {/* daily sales by branch */}
      <DailySalesCard />

      {/* outstanding invoices */}
      {outstanding.length > 0 && (
        <div className="card" style={{ marginBottom: '1.5rem' }}>
          <div className="card-header">
            <h2>Invoice Belum Lunas ({outstanding.length})</h2>
            <Link to="/invoices" className="btn btn-secondary btn-sm">Lihat Semua Invoice</Link>
          </div>

          {/* desktop table */}
          <table className="invoice-desktop-table">
            <thead>
              <tr>
                <th>No. Invoice</th>
                <th>Vendor</th>
                <th>Tanggal</th>
                <th>Jatuh Tempo</th>
                <th style={{ textAlign: 'right' }}>Total</th>
                <th style={{ textAlign: 'right' }}>Sisa</th>
                <th>Status</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {outstanding.map(inv => {
                const remaining = Number(inv.total) - Number(inv.amount_paid);
                const isOverdue = inv.due_date && inv.due_date < todayStr;
                return (
                  <tr key={inv.id} style={{ background: isOverdue ? '#fff5f5' : undefined }}>
                    <td style={{ fontWeight: 600 }}>{inv.invoice_number}</td>
                    <td style={{ color: '#666', fontSize: '0.88rem' }}>{inv.vendor_name || '—'}</td>
                    <td style={{ color: '#888', fontSize: '0.85rem' }}>{fmtDate(inv.date)}</td>
                    <td style={{ fontSize: '0.85rem', fontWeight: isOverdue ? 700 : 'normal', color: isOverdue ? '#e74c3c' : '#555', whiteSpace: 'nowrap' }}>
                      {inv.due_date ? fmtDate(inv.due_date) : '—'}
                      {isOverdue && (
                        <span style={{ marginLeft: '0.35rem', fontSize: '0.7rem', background: '#fdecea', color: '#e74c3c', borderRadius: '3px', padding: '0.05rem 0.3rem', fontWeight: 700 }}>LEWAT</span>
                      )}
                    </td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(inv.total)}</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: '#e67e22' }}>{idr(remaining)}</td>
                    <td>
                      <span style={{
                        display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px',
                        fontSize: '0.75rem', fontWeight: 600,
                        background: inv.payment_status === 'partial' ? '#fff3e0' : '#fdecea',
                        color:      inv.payment_status === 'partial' ? '#e67e22' : '#c0392b',
                      }}>
                        {inv.payment_status === 'partial' ? 'Sebagian' : 'Belum Dibayar'}
                      </span>
                    </td>
                    <td>
                      <Link to={`/invoices/view/${inv.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          {/* mobile cards */}
          <div className="invoice-card-list">
            {outstanding.map(inv => {
              const remaining = Number(inv.total) - Number(inv.amount_paid);
              const isOverdue = inv.due_date && inv.due_date < todayStr;
              return (
                <div key={inv.id} className={`inv-card${isOverdue ? ' overdue' : ''}`}>
                  <div className="inv-card-top">
                    <div>
                      <div className="inv-card-num">{inv.invoice_number}</div>
                      <div className="inv-card-vendor">{inv.vendor_name || '—'}</div>
                    </div>
                    <span style={{
                      display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px',
                      fontSize: '0.75rem', fontWeight: 600,
                      background: inv.payment_status === 'partial' ? '#fff3e0' : '#fdecea',
                      color:      inv.payment_status === 'partial' ? '#e67e22' : '#c0392b',
                    }}>
                      {inv.payment_status === 'partial' ? 'Sebagian' : 'Belum Dibayar'}
                    </span>
                  </div>
                  <div className="inv-card-row">
                    <span className="lbl">Tanggal</span>
                    <span>{fmtDate(inv.date)}</span>
                  </div>
                  <div className="inv-card-row">
                    <span className="lbl">Jatuh Tempo</span>
                    <span style={{ fontWeight: isOverdue ? 700 : 'normal', color: isOverdue ? '#e74c3c' : undefined }}>
                      {inv.due_date ? fmtDate(inv.due_date) : '—'}
                      {isOverdue && <span style={{ marginLeft: '0.3rem', fontSize: '0.7rem', background: '#fdecea', color: '#e74c3c', borderRadius: '3px', padding: '0.05rem 0.3rem', fontWeight: 700 }}>LEWAT</span>}
                    </span>
                  </div>
                  <div className="inv-card-row">
                    <span className="lbl">Total</span>
                    <span style={{ fontWeight: 600 }}>{idr(inv.total)}</span>
                  </div>
                  <div className="inv-card-footer">
                    <span style={{ fontWeight: 700, color: '#e67e22', fontSize: '0.95rem' }}>Sisa: {idr(remaining)}</span>
                    <Link to={`/invoices/view/${inv.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {outstanding.length === 0 && stats && (
        <div style={{ background: '#e6f9f0', border: '1px solid #b2dfdb', borderRadius: '8px', padding: '1rem 1.5rem', color: '#1b5e45', fontWeight: 500, fontSize: '0.9rem', marginBottom: '1.5rem' }}>
          Semua invoice sudah lunas.
        </div>
      )}

      {/* recent activity */}
      {(stats?.recentActivity || []).length > 0 && (
        <div className="card">
          <div className="card-header" style={{ marginBottom: '0.75rem' }}>
            <h2>Aktivitas Terakhir</h2>
          </div>
          <table className="activity-table">
            <thead>
              <tr>
                <th>Waktu</th>
                <th>Pengguna</th>
                <th>Aksi</th>
                <th>Keterangan</th>
              </tr>
            </thead>
            <tbody>
              {stats.recentActivity.map(a => (
                <tr key={a.id}>
                  <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>
                    {new Date(a.created_at).toLocaleString('id-ID')}
                  </td>
                  <td style={{ fontWeight: 500, fontSize: '0.88rem' }}>{a.username || '—'}</td>
                  <td>
                    <span style={{
                      display: 'inline-block', padding: '0.1rem 0.45rem', borderRadius: '4px', fontSize: '0.75rem', fontWeight: 600,
                      background: a.action === 'create' ? '#e8f5e9' : a.action === 'delete' ? '#fdecea' : '#e8f0fe',
                      color:      a.action === 'create' ? '#2e7d32' : a.action === 'delete' ? '#c0392b' : '#3949ab',
                    }}>
                      {a.action}
                    </span>
                  </td>
                  <td style={{ color: '#555', fontSize: '0.85rem' }}>{a.description}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
