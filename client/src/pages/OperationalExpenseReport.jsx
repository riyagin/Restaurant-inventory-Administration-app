import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { getOperationalExpenseReport, getBranches } from '../api';
import Icon from '../components/Icon';

// Statistik Beban Operasional — the standing bills over time, per branch.
//
// The question this page exists for is "did the water bill jump", and answering
// it needs the month a bill *covers*, not the day it was paid. Those differ by
// however long settlement took, so the figures come from `operational_expenses`
// keyed on `period_month` rather than from the journal, which is keyed on
// payment date. The P&L is right to disagree with this page in any month where a
// bill was paid late; they are answering different questions. The note at the
// bottom says so, because a reader who spots the difference deserves the reason
// rather than a bug report.
//
// Listrik, Air and Gas are charted because they are metered: they move with what
// the branch actually consumed, which is what makes a jump worth investigating.
// The contract-priced recurring bills (sewa, internet, telepon, kebersihan,
// keamanan) are table rows — a flat line is not worth 200px — and everything
// ad-hoc collapses into one Lainnya row, since a repair in March predicts
// nothing about April.

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v ?? 0);

// Axis labels only. Full rupiah everywhere a figure is actually read.
const compact = (v) => {
  const n = Number(v || 0);
  if (Math.abs(n) >= 1e9) return (n / 1e9).toFixed(1).replace('.0', '') + ' M';
  if (Math.abs(n) >= 1e6) return (n / 1e6).toFixed(1).replace('.0', '') + ' jt';
  if (Math.abs(n) >= 1e3) return Math.round(n / 1e3) + ' rb';
  return String(n);
};

// Categorical hues in fixed order, assigned per branch and never cycled, so a
// branch keeps its colour when the filter changes the series count. Validated
// for CVD separation on the adjacent pairlist; the ones below 3:1 against white
// are why every chart here ships a legend and a table.
const BRANCH_COLORS = [
  '#2a78d6', '#eb6834', '#1baf7a', '#eda100',
  '#e87ba4', '#008300', '#4a3aa7', '#e34948',
];
const OVERFLOW_COLOR = '#8a93a3';

const UTILITY_ICON = { Listrik: 'zap', Air: 'droplet', Gas: 'zap' };

const INK = '#1f2430';
const MUTED = '#8a93a3';
const GRID = '#eef1f5';

const monthsAgo = (n) => {
  const d = new Date();
  d.setDate(1);
  d.setMonth(d.getMonth() - n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
};

function ChangeBadge({ pct }) {
  if (pct == null) {
    return <span style={{ color: '#c8cdd6', fontSize: '0.78rem' }}>—</span>;
  }
  const up = pct > 0;
  const flat = Math.abs(pct) < 0.5;
  const color = flat ? MUTED : up ? '#c0392b' : '#1f9d68';
  return (
    <span style={{ color, fontWeight: 600, fontSize: '0.8rem', whiteSpace: 'nowrap' }}>
      {flat ? '±' : up ? '▲' : '▼'} {Math.abs(pct).toFixed(0)}%
    </span>
  );
}

// One measure, many branches, over months. A line chart because the reading is
// "how did this move", and lines are what carry a trend across a shared x-axis.
function MonthlyLineChart({ months, series, height = 200 }) {
  const [hover, setHover] = useState(null);

  const padL = 62, padR = 14, padT = 14, padB = 26;
  const width = 720;
  const plotW = width - padL - padR;
  const plotH = height - padT - padB;

  const values = series.flatMap(s => months.map(m => Number(s.amounts[m.key] || 0)));
  const max = Math.max(1, ...values);
  // A y-axis anchored at zero: the question is how big the bill is, and a
  // truncated axis exaggerates every wobble into a spike.
  const y = (v) => padT + plotH - (Number(v || 0) / max) * plotH;
  const x = (i) => months.length === 1
    ? padL + plotW / 2
    : padL + (i / (months.length - 1)) * plotW;

  const ticks = [0, 0.25, 0.5, 0.75, 1].map(f => max * f);
  // Thin the x labels so they never collide; every month still has a gridline.
  const labelEvery = Math.ceil(months.length / 8);

  const anyData = values.some(v => v > 0);

  return (
    <div style={{ position: 'relative' }}>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        style={{ width: '100%', height: 'auto', display: 'block', overflow: 'visible' }}
        role="img"
        onMouseLeave={() => setHover(null)}
      >
        {ticks.map((t, i) => (
          <g key={i}>
            <line x1={padL} x2={width - padR} y1={y(t)} y2={y(t)} stroke={GRID} strokeWidth="1" />
            <text x={padL - 8} y={y(t) + 4} textAnchor="end" fontSize="10" fill={MUTED}>{compact(t)}</text>
          </g>
        ))}

        {months.map((m, i) => (
          i % labelEvery === 0 && (
            <text key={m.key} x={x(i)} y={height - 8} textAnchor="middle" fontSize="10" fill={MUTED}>
              {m.label}
            </text>
          )
        ))}

        {hover != null && (
          <line x1={x(hover)} x2={x(hover)} y1={padT} y2={padT + plotH} stroke="#c8cdd6" strokeWidth="1" strokeDasharray="3 3" />
        )}

        {series.map(s => {
          // Months with no bill recorded break the line rather than being drawn
          // as zero — a gap is "nobody recorded this", which is a different
          // statement from "it cost nothing", and the one worth noticing.
          const segments = [];
          let current = [];
          months.forEach((m, i) => {
            if (s.entries[m.key] > 0) current.push([x(i), y(s.amounts[m.key])]);
            else if (current.length) { segments.push(current); current = []; }
          });
          if (current.length) segments.push(current);

          return (
            <g key={s.id}>
              {segments.map((seg, si) => (
                <polyline
                  key={si}
                  points={seg.map(p => p.join(',')).join(' ')}
                  fill="none" stroke={s.color} strokeWidth="2"
                  strokeLinecap="round" strokeLinejoin="round"
                />
              ))}
              {months.map((m, i) => s.entries[m.key] > 0 && (
                <circle
                  key={m.key} cx={x(i)} cy={y(s.amounts[m.key])} r={hover === i ? 5 : 4}
                  fill={s.color} stroke="#fff" strokeWidth="2"
                />
              ))}
            </g>
          );
        })}

        {/* Hit targets wider than the marks, one band per month. */}
        {months.map((m, i) => (
          <rect
            key={m.key}
            x={x(i) - plotW / Math.max(1, months.length * 2) - 6}
            y={padT} width={plotW / Math.max(1, months.length) + 12} height={plotH}
            fill="transparent"
            onMouseEnter={() => setHover(i)}
          />
        ))}

        {!anyData && (
          <text x={padL + plotW / 2} y={padT + plotH / 2} textAnchor="middle" fontSize="12" fill="#c8cdd6">
            Belum ada data pada rentang ini
          </text>
        )}
      </svg>

      {hover != null && (
        <div style={{
          position: 'absolute', top: 0, right: 0, background: '#fff', border: '1px solid #e3e8ef',
          borderRadius: '8px', boxShadow: '0 4px 14px rgba(31,36,48,0.10)', padding: '0.5rem 0.7rem',
          fontSize: '0.78rem', pointerEvents: 'none', minWidth: '150px',
        }}>
          <div style={{ fontWeight: 700, color: INK, marginBottom: '0.3rem' }}>{months[hover].label}</div>
          {series.map(s => (
            <div key={s.id} style={{ display: 'flex', justifyContent: 'space-between', gap: '0.75rem' }}>
              <span style={{ color: MUTED, display: 'inline-flex', alignItems: 'center', gap: '0.3rem' }}>
                <span style={{ width: '8px', height: '8px', borderRadius: '2px', background: s.color, display: 'inline-block' }} />
                {s.name}
              </span>
              <span style={{ color: INK, fontWeight: 600 }}>
                {s.entries[months[hover].key] > 0 ? idr(s.amounts[months[hover].key]) : '—'}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function Legend({ series }) {
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.75rem', marginTop: '0.5rem' }}>
      {series.map(s => (
        <span key={s.id} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem', fontSize: '0.8rem', color: INK }}>
          <span style={{ width: '10px', height: '10px', borderRadius: '3px', background: s.color, display: 'inline-block' }} />
          {s.name}
        </span>
      ))}
    </div>
  );
}

export default function OperationalExpenseReport() {
  const [data, setData] = useState(null);
  const [branches, setBranches] = useState([]);
  const [months, setMonths] = useState(12);
  const [end, setEnd] = useState(monthsAgo(0));
  const [branchId, setBranchId] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => { getBranches().then(r => setBranches(r.data)).catch(() => {}); }, []);

  const load = useCallback(() => {
    setLoading(true);
    setError('');
    return getOperationalExpenseReport({ months, end, branch_id: branchId || undefined })
      .then(r => setData(r.data))
      .catch(e => setError(e.response?.data?.error || 'Gagal memuat laporan'))
      .finally(() => setLoading(false));
  }, [months, end, branchId]);

  useEffect(() => { load(); }, [load]);

  const cols = useMemo(() => data?.months ?? [], [data]);
  const blocks = useMemo(() => data?.branches ?? [], [data]);
  const utilities = useMemo(() => data?.utilities ?? [], [data]);

  // Colour follows the branch, fixed by its position in the full branch list —
  // not by its position in whatever subset a filter leaves — so filtering never
  // repaints the survivors.
  const colorOf = useCallback((id) => {
    const i = branches.findIndex(b => b.id === id);
    return i >= 0 && i < BRANCH_COLORS.length ? BRANCH_COLORS[i] : OVERFLOW_COLOR;
  }, [branches]);

  const seriesFor = useCallback((category) => blocks.map(block => {
    const row = block.categories.find(c => c.category === category);
    return {
      id: block.id,
      name: block.name,
      color: colorOf(block.id),
      amounts: row?.amounts ?? {},
      entries: row?.entries ?? {},
    };
  }), [blocks, colorOf]);

  const orgTotal = blocks.reduce((s, b) => s + Number(b.total || 0), 0);

  // The headline per utility: what it cost last month across everything on
  // screen, and how that compares with the month before.
  const utilitySummary = useMemo(() => utilities.map(name => {
    let latest = 0, previous = 0, total = 0;
    for (const block of blocks) {
      const row = block.categories.find(c => c.category === name);
      if (!row) continue;
      latest += Number(row.latest || 0);
      previous += Number(row.previous || 0);
      total += Number(row.total || 0);
    }
    const pct = previous !== 0 ? ((latest - previous) / previous) * 100 : null;
    return { name, latest, previous, total, pct };
  }), [utilities, blocks]);

  return (
    <>
      <div className="page-header">
        <h1>Statistik Beban Operasional</h1>
        <div style={{ display: 'flex', gap: '0.6rem', alignItems: 'center' }}>
          <Link to="/operational-expenses" className="btn btn-secondary">Catat Beban</Link>
          <Link to="/reports/financial" className="btn btn-secondary">Laporan Keuangan</Link>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap', alignItems: 'center' }}>
          <label style={{ fontSize: '0.85rem', color: '#444', fontWeight: 600 }}>Rentang</label>
          <select value={months} onChange={e => setMonths(Number(e.target.value))} style={fieldStyle}>
            <option value={6}>6 bulan</option>
            <option value={12}>12 bulan</option>
            <option value={24}>24 bulan</option>
          </select>
          <label style={{ fontSize: '0.85rem', color: '#444', fontWeight: 600 }}>Sampai bulan</label>
          <input type="month" value={end} onChange={e => setEnd(e.target.value)} style={fieldStyle} />
          <label style={{ fontSize: '0.85rem', color: '#444', fontWeight: 600 }}>Cabang</label>
          <select value={branchId} onChange={e => setBranchId(e.target.value)} style={fieldStyle}>
            <option value="">Semua cabang</option>
            {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
          <span style={{ marginLeft: 'auto', fontSize: '0.85rem', color: MUTED }}>
            Total periode: <strong style={{ color: INK }}>{idr(orgTotal)}</strong>
          </span>
        </div>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
      {loading && <p style={{ color: '#999', padding: '1rem 0' }}>Memuat…</p>}

      {data && !loading && (
        <>
          {/* Headline per metered bill. */}
          <div className="stats-grid" style={{ marginBottom: '1.5rem' }}>
            {utilitySummary.map(u => (
              <div className="stat-card" key={u.name}>
                <div className="label" style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem' }}>
                  <Icon name={UTILITY_ICON[u.name] || 'grid'} size={14} />
                  {u.name} — bulan terakhir
                </div>
                <div className="value" style={{ fontSize: '1.25rem', color: u.latest > 0 ? INK : '#ccc' }}>
                  {idr(u.latest)}
                </div>
                <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem', display: 'flex', gap: '0.4rem', alignItems: 'center' }}>
                  <ChangeBadge pct={u.pct} />
                  <span>vs bulan sebelumnya</span>
                </div>
              </div>
            ))}
            <div className="stat-card">
              <div className="label">Total {cols.length} bulan</div>
              <div className="value" style={{ fontSize: '1.25rem', color: INK }}>{idr(orgTotal)}</div>
              <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>
                seluruh beban operasional
              </div>
            </div>
          </div>

          {/* One chart per metered bill, one line per branch. */}
          {utilities.map(name => {
            const series = seriesFor(name);
            return (
              <div className="card" key={name} style={{ marginBottom: '1.25rem' }}>
                <div className="card-header" style={{ marginBottom: '0.75rem' }}>
                  <h2 style={{ fontSize: '1rem', margin: 0, display: 'inline-flex', alignItems: 'center', gap: '0.45rem' }}>
                    <Icon name={UTILITY_ICON[name] || 'grid'} size={18} />
                    {name} per Bulan
                  </h2>
                  <span style={{ fontSize: '0.75rem', color: '#aaa' }}>
                    menurut bulan tagihan · {cols.length} bulan
                  </span>
                </div>
                <MonthlyLineChart months={cols} series={series} />
                <Legend series={series} />
              </div>
            );
          })}

          {/* The full picture per branch: every recurring bill on its own line,
              everything ad-hoc on one. This is also the table view that the
              charts' lighter hues oblige. */}
          {blocks.map(block => (
            <div className="card" key={block.id} style={{ marginBottom: '1.25rem' }}>
              <div className="card-header" style={{ marginBottom: '0.9rem' }}>
                <h2 style={{ fontSize: '1rem', margin: 0, display: 'inline-flex', alignItems: 'center', gap: '0.45rem' }}>
                  <span style={{ width: '10px', height: '10px', borderRadius: '3px', background: colorOf(block.id), display: 'inline-block' }} />
                  {block.name}
                </h2>
                <span style={{ fontSize: '0.8rem', color: MUTED }}>
                  total {idr(block.total)}
                </span>
              </div>

              <div style={{ overflowX: 'auto' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', minWidth: `${18 + cols.length * 5.5}rem` }}>
                  <thead>
                    <tr style={{ borderBottom: '2px solid #eee' }}>
                      <th style={{ textAlign: 'left', padding: '0.4rem 0.6rem', fontSize: '0.78rem', color: MUTED, position: 'sticky', left: 0, background: '#fff' }}>
                        Jenis
                      </th>
                      {cols.map(m => (
                        <th key={m.key} style={{ textAlign: 'right', paddingRight: '0.6rem', fontSize: '0.75rem', color: MUTED, whiteSpace: 'nowrap' }}>
                          {m.label}
                        </th>
                      ))}
                      <th style={{ textAlign: 'right', paddingRight: '0.6rem', fontSize: '0.78rem', color: '#555', borderLeft: '1px solid #eee', whiteSpace: 'nowrap' }}>Rata-rata</th>
                      <th style={{ textAlign: 'right', paddingRight: '0.6rem', fontSize: '0.78rem', color: '#555', whiteSpace: 'nowrap' }}>Total</th>
                      <th style={{ textAlign: 'right', paddingRight: '0.6rem', fontSize: '0.78rem', color: '#555', whiteSpace: 'nowrap' }}>Δ</th>
                    </tr>
                  </thead>
                  <tbody>
                    {block.categories.map(row => {
                      const isOther = !row.recurring;
                      const missing = row.recurring && row.months_recorded < cols.length;
                      return (
                        <tr key={row.category} style={{
                          borderTop: isOther ? '2px solid #e9edf3' : '1px solid #f4f6f9',
                          background: row.utility ? '#fcfdff' : undefined,
                        }}>
                          <td style={{
                            padding: '0.4rem 0.6rem', fontWeight: row.utility ? 700 : isOther ? 600 : 500,
                            fontSize: '0.85rem', position: 'sticky', left: 0,
                            background: row.utility ? '#fcfdff' : '#fff',
                            color: isOther ? MUTED : INK, whiteSpace: 'nowrap',
                          }}>
                            {row.category}
                            {missing && (
                              <span title={`${cols.length - row.months_recorded} bulan tanpa catatan`}
                                    style={{ marginLeft: '0.35rem', color: '#c9a227', fontSize: '0.72rem' }}>
                                ({row.months_recorded}/{cols.length})
                              </span>
                            )}
                          </td>
                          {cols.map(m => (
                            <td key={m.key} style={{
                              textAlign: 'right', paddingRight: '0.6rem', fontSize: '0.8rem', whiteSpace: 'nowrap',
                              color: row.entries[m.key] > 0 ? INK : '#dfe3ea',
                            }}>
                              {row.entries[m.key] > 0 ? idr(row.amounts[m.key]) : '—'}
                            </td>
                          ))}
                          <td style={{ textAlign: 'right', paddingRight: '0.6rem', fontSize: '0.8rem', color: MUTED, borderLeft: '1px solid #eee', whiteSpace: 'nowrap' }}>
                            {row.months_recorded > 0 ? idr(row.average) : '—'}
                          </td>
                          <td style={{ textAlign: 'right', paddingRight: '0.6rem', fontSize: '0.82rem', fontWeight: 700, whiteSpace: 'nowrap' }}>
                            {row.total !== 0 ? idr(row.total) : '—'}
                          </td>
                          <td style={{ textAlign: 'right', paddingRight: '0.6rem' }}>
                            <ChangeBadge pct={row.change_pct} />
                          </td>
                        </tr>
                      );
                    })}
                    <tr style={{ background: '#f0f4ff', borderTop: '2px solid #dde4ff' }}>
                      <td style={{ padding: '0.5rem 0.6rem', fontWeight: 700, fontSize: '0.85rem', position: 'sticky', left: 0, background: '#f0f4ff' }}>
                        Total
                      </td>
                      {cols.map(m => (
                        <td key={m.key} style={{ textAlign: 'right', paddingRight: '0.6rem', fontWeight: 700, fontSize: '0.8rem', whiteSpace: 'nowrap' }}>
                          {block.total_by_month[m.key] ? idr(block.total_by_month[m.key]) : '—'}
                        </td>
                      ))}
                      <td style={{ borderLeft: '1px solid #eee' }} />
                      <td style={{ textAlign: 'right', paddingRight: '0.6rem', fontWeight: 700, fontSize: '0.82rem', whiteSpace: 'nowrap' }}>
                        {idr(block.total)}
                      </td>
                      <td />
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          ))}

          <p style={{ fontSize: '0.78rem', color: MUTED, lineHeight: 1.6, padding: '0 0.25rem 1rem' }}>
            Angka dikelompokkan menurut <strong>bulan tagihan</strong>, bukan tanggal pembayaran —
            listrik bulan Juli yang dibayar bulan Agustus tetap dihitung sebagai Juli di sini.
            Karena itu halaman ini sengaja bisa berbeda dari Laporan Laba Rugi, yang dihitung dari
            jurnal menurut tanggal uang keluar. Tanda <span style={{ color: '#c9a227' }}>(3/12)</span> pada
            sebuah jenis berarti hanya 3 dari 12 bulan yang punya catatan — garis yang terputus pada
            grafik menandai hal yang sama. Beban yang dibatalkan tidak dihitung.
          </p>
        </>
      )}
    </>
  );
}

const fieldStyle = {
  padding: '0.45rem 0.6rem', border: '1px solid #ddd',
  borderRadius: '6px', fontSize: '0.88rem',
};
