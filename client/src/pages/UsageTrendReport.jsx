import { useCallback, useEffect, useMemo, useState } from 'react';
import * as XLSX from 'xlsx';
import { getUsageTrendReport } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Math.round(v || 0));
const num = (v, d = 2) =>
  new Intl.NumberFormat('id-ID', { maximumFractionDigits: d }).format(v || 0);
const fmtDate = (d) => (d ? new Date(d + 'T00:00:00').toLocaleDateString('id-ID', { day: '2-digit', month: 'short' }) : '—');

function compactIdr(v) {
  const a = Math.abs(v);
  if (a === 0) return '0';
  if (a >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(1).replace('.0', '')}M`;
  if (a >= 1_000_000) return `${(v / 1_000_000).toFixed(1).replace('.0', '')}jt`;
  if (a >= 1_000) return `${(v / 1_000).toFixed(0)}rb`;
  return String(Math.round(v));
}
function compactNum(v) {
  const a = Math.abs(v);
  if (a >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}jt`;
  if (a >= 1_000) return `${(v / 1_000).toFixed(1)}rb`;
  return num(v, a < 10 ? 1 : 0);
}

function todayStr() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
function nDaysAgo(n) {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function Pct({ value, size = '0.85rem' }) {
  if (value === null || value === undefined) {
    return <span style={{ color: '#bbb', fontSize: size }}>—</span>;
  }
  const up = value > 0;
  const color = Math.abs(value) < 0.05 ? '#888' : up ? '#2563eb' : '#f97316';
  const arrow = Math.abs(value) < 0.05 ? '' : up ? '▲' : '▼';
  return (
    <span style={{ color, fontWeight: 600, fontSize: size, whiteSpace: 'nowrap' }}>
      {arrow} {value > 0 ? '+' : ''}{value.toFixed(1)}%
    </span>
  );
}

const STOCK_COLOR = '#6366f1';
const NONSTOCK_COLOR = '#14b8a6';

// ─── daily stacked bar chart (stok vs non-stok) ──────────────────────────────
function DailyChart({ days, metric }) {
  if (!days || days.length === 0) {
    return <p style={{ color: '#999', fontSize: '0.85rem', textAlign: 'center', padding: '2rem 0' }}>Tidak ada pemakaian pada rentang ini.</p>;
  }
  const isValue = metric === 'value';
  const stockOf = (d) => (isValue ? d.stock_value : d.stock_quantity);
  const nonOf = (d) => (isValue ? d.nonstock_value : d.nonstock_quantity);
  const totalOf = (d) => Math.max(stockOf(d) + nonOf(d), 0);
  const fmt = isValue ? compactIdr : compactNum;
  const fmtFull = isValue ? idr : ((v) => num(v));

  const W = 760, H = 260;
  const padL = 52, padR = 12, padT = 16, padB = 42;
  const plotW = W - padL - padR;
  const plotH = H - padT - padB;

  const maxVal = Math.max(...days.map(totalOf), 1);
  const yMax = maxVal * 1.12;
  const slotW = plotW / days.length;
  const barW = Math.max(slotW * 0.68, 1.5);
  const isCompact = days.length > 20;

  const py = (v) => padT + plotH - (v / yMax) * plotH;
  const bh = (v) => Math.max((v / yMax) * plotH, v > 0 ? 1 : 0);
  const ticks = [0, 0.25, 0.5, 0.75, 1].map(f => yMax * f);

  // trend line over the daily totals so direction is readable at a glance
  const line = days.map((d, i) => `${i === 0 ? 'M' : 'L'}${padL + i * slotW + slotW / 2},${py(totalOf(d))}`).join(' ');

  return (
    <div style={{ width: '100%', overflowX: 'auto' }}>
      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', minWidth: '340px', height: 'auto', display: 'block' }}>
        {ticks.map((t, i) => (
          <g key={i}>
            <line x1={padL} y1={py(t)} x2={padL + plotW} y2={py(t)} stroke="#f1f1f4" strokeWidth={1} />
            <text x={padL - 6} y={py(t) + 4} textAnchor="end" fontSize={10} fill="#aaa">{fmt(t)}</text>
          </g>
        ))}

        {days.map((d, i) => {
          const x = padL + i * slotW + (slotW - barW) / 2;
          const s = Math.max(stockOf(d), 0);
          const n = Math.max(nonOf(d), 0);
          const dayNum = new Date(d.date + 'T00:00:00').getDate();
          const showLabel = !isCompact || i % Math.ceil(days.length / 20) === 0;
          return (
            <g key={d.date}>
              <rect x={x} y={py(s)} width={barW} height={bh(s)} fill={STOCK_COLOR} rx={1.5} opacity={0.9}>
                <title>{`${fmtDate(d.date)}\nBarang stok: ${fmtFull(stockOf(d))}\nNon-stok: ${fmtFull(nonOf(d))}\nTotal: ${fmtFull(totalOf(d))}\nBarang aktif: ${d.active_items}`}</title>
              </rect>
              <rect x={x} y={py(s + n)} width={barW} height={bh(n)} fill={NONSTOCK_COLOR} rx={1.5} opacity={0.9}>
                <title>{`${fmtDate(d.date)}\nBarang stok: ${fmtFull(stockOf(d))}\nNon-stok: ${fmtFull(nonOf(d))}\nTotal: ${fmtFull(totalOf(d))}\nBarang aktif: ${d.active_items}`}</title>
              </rect>
              {showLabel && (
                <text x={x + barW / 2} y={H - padB + 14} textAnchor="middle" fontSize={isCompact ? 8 : 9} fill="#999">
                  {isCompact ? dayNum : fmtDate(d.date)}
                </text>
              )}
            </g>
          );
        })}

        <path d={line} fill="none" stroke="#f59e0b" strokeWidth={1.5} strokeOpacity={0.85} strokeLinejoin="round" />

        <line x1={padL} y1={padT} x2={padL} y2={padT + plotH} stroke="#e5e5ea" strokeWidth={1} />
        <line x1={padL} y1={padT + plotH} x2={padL + plotW} y2={padT + plotH} stroke="#e5e5ea" strokeWidth={1} />
      </svg>

      <div style={{ display: 'flex', gap: '1.25rem', justifyContent: 'center', marginTop: '0.25rem', flexWrap: 'wrap' }}>
        {[['Barang stok (pengiriman)', STOCK_COLOR], ['Barang non-stok (pembelian)', NONSTOCK_COLOR], ['Total harian', '#f59e0b']].map(([label, color]) => (
          <span key={label} style={{ fontSize: '0.8rem', color: '#555', display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
            <span style={{ width: 12, height: 12, background: color, borderRadius: 2, display: 'inline-block' }} />
            {label}
          </span>
        ))}
      </div>
    </div>
  );
}

const PRESETS = [
  { label: '7 Hari', days: 6 },
  { label: '14 Hari', days: 13 },
  { label: '30 Hari', days: 29 },
  { label: '90 Hari', days: 89 },
];

export default function UsageTrendReport() {
  const [range, setRange] = useState({ date_from: nDaysAgo(29), date_to: todayStr() });
  const [draft, setDraft] = useState({ date_from: nDaysAgo(29), date_to: todayStr() });
  const [preset, setPreset] = useState('30 Hari');
  const [metric, setMetric] = useState('value');       // value | quantity
  const [compareMode, setCompareMode] = useState('endpoint'); // endpoint | active
  const [typeFilter, setTypeFilter] = useState('all'); // all | stock | nonstock
  const [search, setSearch] = useState('');

  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    setLoading(true);
    setError('');
    getUsageTrendReport(range)
      .then(r => setData(r.data))
      .catch(e => setError(e?.response?.data?.error || 'Gagal memuat laporan pemakaian barang'))
      .finally(() => setLoading(false));
  }, [range]);

  useEffect(() => { load(); }, [load]);

  const applyPreset = (p) => {
    const next = { date_from: nDaysAgo(p.days), date_to: todayStr() };
    setPreset(p.label);
    setDraft(next);
    setRange(next);
  };

  const summary = data?.summary;
  const items = data?.items || [];
  const byEndpoint = compareMode === 'endpoint';

  // One accessor pair so the table, the cards and the export all read the same
  // numbers regardless of which comparison the user picked.
  const pick = useCallback((it) => (byEndpoint
    ? {
      startQty: it.start_quantity, startVal: it.start_value,
      endQty: it.end_quantity, endVal: it.end_value,
      qtyPct: it.qty_change_pct, valPct: it.value_change_pct,
      startLabel: data?.date_from, endLabel: data?.date_to,
    }
    : {
      startQty: it.first_active_quantity, startVal: it.first_active_value,
      endQty: it.last_active_quantity, endVal: it.last_active_value,
      qtyPct: it.active_qty_change_pct, valPct: it.active_value_change_pct,
      startLabel: it.first_active_date, endLabel: it.last_active_date,
    }), [byEndpoint, data]);

  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    return items.filter(it => {
      if (typeFilter === 'stock' && !it.is_stock) return false;
      if (typeFilter === 'nonstock' && it.is_stock) return false;
      if (q && !`${it.item_name} ${it.item_code}`.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [items, typeFilter, search]);

  const downloadExcel = () => {
    if (!data) return;
    const wb = XLSX.utils.book_new();
    const modeLabel = byEndpoint ? 'tanggal awal vs akhir' : 'hari aktif pertama vs terakhir';

    const rows = [
      ['Laporan Perubahan Pemakaian Barang'],
      [`Periode: ${data.date_from} s/d ${data.date_to}`],
      [`Pembanding: ${modeLabel}`],
      [],
      ['Barang', 'Kode', 'Jenis', 'Satuan', 'Tgl Awal', 'Qty Awal', 'Nilai Awal', 'Tgl Akhir', 'Qty Akhir', 'Nilai Akhir', 'Δ Qty %', 'Δ Nilai %', 'Total Qty', 'Total Nilai', 'Hari Aktif'],
    ];
    for (const it of shown) {
      const p = pick(it);
      rows.push([
        it.item_name, it.item_code, it.is_stock ? 'Stok' : 'Non-stok', it.unit_name,
        p.startLabel || '', p.startQty, p.startVal,
        p.endLabel || '', p.endQty, p.endVal,
        p.qtyPct === null || p.qtyPct === undefined ? '' : Number(p.qtyPct.toFixed(2)),
        p.valPct === null || p.valPct === undefined ? '' : Number(p.valPct.toFixed(2)),
        it.total_quantity, it.total_value, it.active_days,
      ]);
    }
    const ws1 = XLSX.utils.aoa_to_sheet(rows);
    ws1['!cols'] = [{ wch: 30 }, { wch: 12 }, { wch: 10 }, { wch: 12 }, { wch: 12 }, { wch: 11 }, { wch: 15 }, { wch: 12 }, { wch: 11 }, { wch: 15 }, { wch: 10 }, { wch: 10 }, { wch: 12 }, { wch: 16 }, { wch: 10 }];
    XLSX.utils.book_append_sheet(wb, ws1, 'Per Barang');

    const dayRows = [['Tanggal', 'Qty Stok', 'Nilai Stok', 'Qty Non-stok', 'Nilai Non-stok', 'Total Qty', 'Total Nilai', 'Barang Aktif']];
    for (const d of data.days) {
      dayRows.push([d.date, d.stock_quantity, d.stock_value, d.nonstock_quantity, d.nonstock_value, d.total_quantity, d.total_value, d.active_items]);
    }
    const ws2 = XLSX.utils.aoa_to_sheet(dayRows);
    ws2['!cols'] = [{ wch: 12 }, { wch: 12 }, { wch: 16 }, { wch: 14 }, { wch: 16 }, { wch: 12 }, { wch: 16 }, { wch: 13 }];
    XLSX.utils.book_append_sheet(wb, ws2, 'Harian');

    XLSX.writeFile(wb, `pemakaian-barang-${data.date_from}_${data.date_to}.xlsx`);
  };

  return (
    <>
      <div className="page-header">
        <h1>Perubahan Pemakaian Barang</h1>
        {data && items.length > 0 && (
          <button onClick={downloadExcel} className="btn btn-secondary">⬇ Download Excel</button>
        )}
      </div>

      <div className="card" style={{ marginBottom: '1.25rem' }}>
        <div className="card-header" style={{ flexWrap: 'wrap', gap: '0.75rem' }}>
          <div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap' }}>
            {PRESETS.map(p => (
              <button
                key={p.label}
                className={`btn btn-sm ${preset === p.label ? 'btn-primary' : 'btn-secondary'}`}
                onClick={() => applyPreset(p)}
              >{p.label}</button>
            ))}
          </div>
          <div className="filters" style={{ flexWrap: 'wrap' }}>
            <input type="date" value={draft.date_from} onChange={e => setDraft(d => ({ ...d, date_from: e.target.value }))} title="Dari tanggal" />
            <input type="date" value={draft.date_to} onChange={e => setDraft(d => ({ ...d, date_to: e.target.value }))} title="Sampai tanggal" />
            <button className="btn btn-sm btn-primary" onClick={() => { setPreset(''); setRange(draft); }}>Terapkan</button>
          </div>
        </div>
      </div>

      {error && <div className="alert alert-error" style={{ marginBottom: '1rem' }}>{error}</div>}

      {summary && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))', gap: '1rem', marginBottom: '1.5rem' }}>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Pemakaian {fmtDate(data.date_from)}</div>
            <div style={{ fontSize: '1.2rem', fontWeight: 700 }}>{idr(summary.start_value)}</div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>{num(summary.start_quantity)} unit</div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Pemakaian {fmtDate(data.date_to)}</div>
            <div style={{ fontSize: '1.2rem', fontWeight: 700 }}>{idr(summary.end_value)}</div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>{num(summary.end_quantity)} unit</div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Perubahan Nilai</div>
            <div style={{ fontSize: '1.5rem', fontWeight: 700 }}><Pct value={summary.value_change_pct} size="1.5rem" /></div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>kuantitas <Pct value={summary.qty_change_pct} size="0.75rem" /></div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>
              Rata-rata {summary.window_days} Hari Awal → Akhir
            </div>
            <div style={{ fontSize: '1.2rem', fontWeight: 700 }}><Pct value={summary.window_value_pct} size="1.2rem" /></div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>
              {compactIdr(summary.window_start_value)} → {compactIdr(summary.window_end_value)}
            </div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Total Pemakaian Periode</div>
            <div style={{ fontSize: '1.2rem', fontWeight: 700, color: '#e67e22' }}>{idr(summary.total_value)}</div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>{summary.item_count} barang · {summary.day_count} hari</div>
          </div>
        </div>
      )}

      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div className="card-header" style={{ flexWrap: 'wrap', gap: '0.6rem' }}>
          <h2>{loading ? 'Memuat…' : 'Pemakaian Harian'}</h2>
          <div style={{ display: 'flex', gap: '0.4rem' }}>
            <button className={`btn btn-sm ${metric === 'value' ? 'btn-primary' : 'btn-secondary'}`} onClick={() => setMetric('value')}>Nilai (Rp)</button>
            <button className={`btn btn-sm ${metric === 'quantity' ? 'btn-primary' : 'btn-secondary'}`} onClick={() => setMetric('quantity')}>Kuantitas</button>
          </div>
        </div>
        <div style={{ padding: '0.5rem 1rem 1rem' }}>
          <DailyChart days={data?.days} metric={metric} />
        </div>
      </div>

      <div className="card">
        <div className="card-header" style={{ flexWrap: 'wrap', gap: '0.6rem' }}>
          <h2>{loading ? 'Memuat…' : `${shown.length} barang`}</h2>
          <div className="filters" style={{ flexWrap: 'wrap' }}>
            <input placeholder="Cari barang…" value={search} onChange={e => setSearch(e.target.value)} />
            <select value={typeFilter} onChange={e => setTypeFilter(e.target.value)}>
              <option value="all">Semua jenis</option>
              <option value="stock">Barang stok</option>
              <option value="nonstock">Barang non-stok</option>
            </select>
            <select value={compareMode} onChange={e => setCompareMode(e.target.value)} title="Titik pembanding">
              <option value="endpoint">Tanggal awal vs akhir</option>
              <option value="active">Hari aktif pertama vs terakhir</option>
            </select>
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>Barang</th>
              <th>Satuan</th>
              <th style={{ textAlign: 'right' }}>Awal (Qty / Rp)</th>
              <th style={{ textAlign: 'right' }}>Akhir (Qty / Rp)</th>
              <th style={{ textAlign: 'right' }}>Δ Qty</th>
              <th style={{ textAlign: 'right' }}>Δ Nilai</th>
              <th style={{ textAlign: 'right' }}>Total Periode</th>
            </tr>
          </thead>
          <tbody>
            {shown.length === 0 ? (
              <tr><td colSpan={7} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                {loading ? 'Memuat…' : 'Tidak ada pemakaian barang pada rentang ini'}
              </td></tr>
            ) : shown.map(it => {
              const p = pick(it);
              return (
                <tr key={it.item_id}>
                  <td>
                    <div style={{ fontWeight: 600 }}>{it.item_name}</div>
                    <div style={{ fontSize: '0.75rem', color: '#aaa' }}>
                      {it.item_code || '—'} · {it.is_stock ? 'stok' : 'non-stok'} · aktif {it.active_days} hari
                    </div>
                  </td>
                  <td style={{ fontSize: '0.85rem', color: '#666' }}>{it.unit_name || '—'}</td>
                  <td style={{ textAlign: 'right' }}>
                    <div>{num(p.startQty)}</div>
                    <div style={{ fontSize: '0.75rem', color: '#aaa' }}>
                      {compactIdr(p.startVal)}{!byEndpoint && p.startLabel ? ` · ${fmtDate(p.startLabel)}` : ''}
                    </div>
                  </td>
                  <td style={{ textAlign: 'right' }}>
                    <div style={{ fontWeight: 600 }}>{num(p.endQty)}</div>
                    <div style={{ fontSize: '0.75rem', color: '#aaa' }}>
                      {compactIdr(p.endVal)}{!byEndpoint && p.endLabel ? ` · ${fmtDate(p.endLabel)}` : ''}
                    </div>
                  </td>
                  <td style={{ textAlign: 'right' }}><Pct value={p.qtyPct} /></td>
                  <td style={{ textAlign: 'right' }}><Pct value={p.valPct} /></td>
                  <td style={{ textAlign: 'right' }}>
                    <div style={{ fontWeight: 600, color: '#e67e22' }}>{idr(it.total_value)}</div>
                    <div style={{ fontSize: '0.75rem', color: '#aaa' }}>{num(it.total_quantity)} unit</div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        <p style={{ fontSize: '0.78rem', color: '#999', padding: '0.75rem 1rem 0', margin: 0 }}>
          Pemakaian barang stok dihitung dari pengiriman ke cabang (koreksi dan pembatalan ikut mengurangi). Barang
          non-stok dihitung dari invoice, karena barang tersebut langsung terpakai saat dibeli. Bila tanggal awal atau
          akhir kebetulan tidak ada transaksi, gunakan pembanding “hari aktif pertama vs terakhir”.
        </p>
      </div>
    </>
  );
}
