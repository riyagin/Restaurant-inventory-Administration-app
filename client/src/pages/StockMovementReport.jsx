import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import * as XLSX from 'xlsx';
import { getStockMovementReport, getWarehouses } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Math.round(v || 0));
const num = (v, d = 2) =>
  new Intl.NumberFormat('id-ID', { maximumFractionDigits: d }).format(v || 0);
const signed = (v, fmt) => (v > 0 ? `+${fmt(v)}` : fmt(v));
const fmtDate = (d) => (d ? new Date(d + 'T00:00:00').toLocaleDateString('id-ID', { day: '2-digit', month: 'short' }) : '—');
const fmtDow = (d) => new Date(d + 'T00:00:00').toLocaleDateString('id-ID', { weekday: 'short' });

function compactIdr(v) {
  const a = Math.abs(v);
  if (a === 0) return '0';
  if (a >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(1).replace('.0', '')}M`;
  if (a >= 1_000_000) return `${(v / 1_000_000).toFixed(1).replace('.0', '')}jt`;
  if (a >= 1_000) return `${(v / 1_000).toFixed(0)}rb`;
  return String(Math.round(v));
}

function ymd(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
const todayStr = () => ymd(new Date());
function nDaysAgo(n) {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return ymd(d);
}

const GAIN_COLOR = '#16a34a';
const LOSS_COLOR = '#dc2626';
const DISPATCH_COLOR = '#6366f1';

const MODES = [
  { key: 'opname', label: 'Stok Opname saja', hint: 'Selisih yang ditemukan saat stok dihitung' },
  { key: 'opname_dispatch', label: 'Opname + Pemakaian (Pengiriman)', hint: 'Selisih opname ditambah barang yang dikirim ke cabang' },
];

const PRESETS = [
  { label: '7 Hari', days: 6 },
  { label: '14 Hari', days: 13 },
  { label: '30 Hari', days: 29 },
  { label: '90 Hari', days: 89 },
];

// ─── daily diverging chart: stock up above the axis, stock down below ─────────
function DailyChart({ days, withDispatch }) {
  if (!days || days.length === 0) return null;
  const hasAny = days.some(d => d.opname_gain_value || d.opname_loss_value || d.dispatch_value);
  if (!hasAny) {
    return <p style={{ color: '#999', fontSize: '0.85rem', textAlign: 'center', padding: '2rem 0' }}>Tidak ada pergerakan stok pada rentang ini.</p>;
  }

  const W = 760, H = 280;
  const padL = 56, padR = 12, padT = 14, padB = 34;
  const plotW = W - padL - padR;
  const plotH = H - padT - padB;

  // dispatch and opname loss stack downward from the axis
  const up = (d) => d.opname_gain_value;
  const down = (d) => -d.opname_loss_value + (withDispatch ? -d.dispatch_value : 0);
  const maxUp = Math.max(...days.map(up), 0);
  const maxDown = Math.max(...days.map(down), 0);
  const span = (maxUp + maxDown) * 1.08 || 1;
  const zeroY = padT + (maxUp * 1.04 / span) * plotH;
  const scale = (v) => (v / span) * plotH;

  const slotW = plotW / days.length;
  const barW = Math.max(slotW * 0.68, 1.5);
  const isCompact = days.length > 20;
  const labelEvery = isCompact ? Math.ceil(days.length / 20) : 1;

  const tip = (d) => [
    `${fmtDate(d.date)} (${fmtDow(d.date)})`,
    `Opname lebih: ${idr(d.opname_gain_value)}`,
    `Opname kurang: ${idr(d.opname_loss_value)}`,
    ...(withDispatch ? [`Pengiriman: ${idr(d.dispatch_value)}`] : []),
    `Bersih: ${idr(d.opname_gain_value + d.opname_loss_value + (withDispatch ? d.dispatch_value : 0))}`,
    `Barang bergerak: ${d.items_moved}`,
  ].join('\n');

  return (
    <div style={{ width: '100%', overflowX: 'auto' }}>
      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', minWidth: '340px', height: 'auto', display: 'block' }}>
        {maxUp > 0 && (
          <text x={padL - 6} y={zeroY - scale(maxUp) + 4} textAnchor="end" fontSize={10} fill="#aaa">+{compactIdr(maxUp)}</text>
        )}
        {maxDown > 0 && (
          <>
            <line x1={padL} y1={zeroY + scale(maxDown)} x2={padL + plotW} y2={zeroY + scale(maxDown)} stroke="#f1f1f4" />
            <text x={padL - 6} y={zeroY + scale(maxDown) + 4} textAnchor="end" fontSize={10} fill="#aaa">−{compactIdr(maxDown)}</text>
          </>
        )}
        <text x={padL - 6} y={zeroY + 4} textAnchor="end" fontSize={10} fill="#aaa">0</text>

        {days.map((d, i) => {
          const x = padL + i * slotW + (slotW - barW) / 2;
          const gain = Math.max(d.opname_gain_value, 0);
          const loss = Math.max(-d.opname_loss_value, 0);
          const disp = withDispatch ? Math.max(-d.dispatch_value, 0) : 0;
          return (
            <g key={d.date}>
              <title>{tip(d)}</title>
              <rect x={padL + i * slotW} y={padT} width={slotW} height={plotH} fill="transparent" />
              {gain > 0 && <rect x={x} y={zeroY - scale(gain)} width={barW} height={Math.max(scale(gain), 1)} fill={GAIN_COLOR} rx={1.5} />}
              {disp > 0 && <rect x={x} y={zeroY} width={barW} height={Math.max(scale(disp), 1)} fill={DISPATCH_COLOR} opacity={0.85} rx={1.5} />}
              {loss > 0 && <rect x={x} y={zeroY + scale(disp)} width={barW} height={Math.max(scale(loss), 1)} fill={LOSS_COLOR} rx={1.5} />}
              {i % labelEvery === 0 && (
                <text x={x + barW / 2} y={H - padB + 16} textAnchor="middle" fontSize={isCompact ? 8 : 9} fill="#999">
                  {isCompact ? new Date(d.date + 'T00:00:00').getDate() : fmtDate(d.date)}
                </text>
              )}
            </g>
          );
        })}

        <line x1={padL} y1={zeroY} x2={padL + plotW} y2={zeroY} stroke="#cfd3da" strokeWidth={1} />
      </svg>

      <div style={{ display: 'flex', gap: '1.25rem', justifyContent: 'center', marginTop: '0.25rem', flexWrap: 'wrap' }}>
        {[
          ['Opname lebih (stok bertambah)', GAIN_COLOR],
          ['Opname kurang (stok hilang)', LOSS_COLOR],
          ...(withDispatch ? [['Pemakaian (pengiriman ke cabang)', DISPATCH_COLOR]] : []),
        ].map(([label, color]) => (
          <span key={label} style={{ fontSize: '0.8rem', color: '#555', display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
            <span style={{ width: 12, height: 12, background: color, borderRadius: 2, display: 'inline-block' }} />
            {label}
          </span>
        ))}
      </div>
    </div>
  );
}

function StatCard({ label, value, sub, color }) {
  return (
    <div className="card" style={{ padding: '1.1rem 1.25rem' }}>
      <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>{label}</div>
      <div style={{ fontSize: '1.2rem', fontWeight: 700, color }}>{value}</div>
      {sub && <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>{sub}</div>}
    </div>
  );
}

// Sticky first column of the matrix: needs its own background or the day
// columns show through it while scrolling sideways.
const stickyCell = { position: 'sticky', left: 0, background: 'var(--surface)', zIndex: 1, minWidth: 200, maxWidth: 260 };

export default function StockMovementReport() {
  const [mode, setMode] = useState('opname');
  const [range, setRange] = useState({ date_from: nDaysAgo(29), date_to: todayStr() });
  const [draft, setDraft] = useState({ date_from: nDaysAgo(29), date_to: todayStr() });
  const [preset, setPreset] = useState('30 Hari');
  const [warehouseId, setWarehouseId] = useState('');
  const [warehouses, setWarehouses] = useState([]);

  const [metric, setMetric] = useState('quantity'); // quantity | value
  const [allDates, setAllDates] = useState(false);
  const [sortBy, setSortBy] = useState('value'); // value | loss | name
  const [search, setSearch] = useState('');

  useEffect(() => {
    getWarehouses().then(r => setWarehouses(r.data || [])).catch(() => {});
  }, []);

  // The result remembers which request produced it: loading is "the latest
  // request has not answered yet", and a slower answer to an older request
  // (toggling the mode quickly) is dropped instead of overwriting a newer one.
  const params = useMemo(() => {
    const p = { ...range, mode };
    if (warehouseId) p.warehouse_id = warehouseId;
    return p;
  }, [range, mode, warehouseId]);
  const reqKey = JSON.stringify(params);
  const [result, setResult] = useState({ key: null, data: null, error: '' });

  useEffect(() => {
    let alive = true;
    getStockMovementReport(params)
      .then(r => { if (alive) setResult({ key: reqKey, data: r.data, error: '' }); })
      .catch(e => {
        if (alive) setResult(prev => ({ key: reqKey, data: prev.data, error: e?.response?.data?.error || 'Gagal memuat laporan pergerakan stok' }));
      });
    return () => { alive = false; };
  }, [params, reqKey]);

  const data = result.data;
  const error = result.error;
  const loading = result.key !== reqKey;

  const applyPreset = (p) => {
    const next = { date_from: nDaysAgo(p.days), date_to: todayStr() };
    setPreset(p.label);
    setDraft(next);
    setRange(next);
  };

  // Render from the mode the data was fetched with, not the toggle, so a
  // half-loaded switch never shows dispatch columns over opname-only numbers.
  const withDispatch = data?.mode === 'opname_dispatch';
  const summary = data?.summary;
  const isQty = metric === 'quantity';

  // Opname sessions per day, for the column headers.
  const opnamesByDay = useMemo(() => {
    const m = {};
    for (const o of data?.opnames || []) (m[o.date] ||= []).push(o);
    return m;
  }, [data]);

  const columns = useMemo(() => {
    const days = data?.days || [];
    return allDates ? days.map(d => d.date) : days.filter(d => d.items_moved > 0).map(d => d.date);
  }, [data, allDates]);

  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    const list = (data?.items || []).filter(it => !q || `${it.item_name} ${it.item_code}`.toLowerCase().includes(q));
    if (sortBy === 'name') return [...list].sort((a, b) => a.item_name.localeCompare(b.item_name, 'id'));
    if (sortBy === 'loss') return [...list].sort((a, b) => a.opname_value - b.opname_value);
    return list; // server order: largest value moved first
  }, [data, search, sortBy]);

  const cellOf = (it, date) => {
    const c = it.days[date];
    if (!c) return null;
    const qty = c.opname_quantity + (withDispatch ? c.dispatch_quantity : 0);
    const value = c.opname_value + (withDispatch ? c.dispatch_value : 0);
    return { ...c, qty, value, hasOpname: c.opname_quantity !== 0 };
  };

  const cellTip = (it, date, c) => {
    const lines = [`${it.item_name} — ${fmtDate(date)}`];
    if (c.opname_quantity !== 0) lines.push(`Opname: ${signed(c.opname_quantity, num)} ${it.unit_name} (${idr(c.opname_value)})`);
    if (withDispatch && c.dispatch_quantity !== 0) lines.push(`Pengiriman: ${signed(c.dispatch_quantity, num)} ${it.unit_name} (${idr(c.dispatch_value)})`);
    return lines.join('\n');
  };

  const totalOf = (it) => (isQty
    ? it.opname_quantity + (withDispatch ? it.dispatch_quantity : 0)
    : it.opname_value + (withDispatch ? it.dispatch_value : 0));

  const fmtCell = (v) => (isQty ? signed(v, (x) => num(x)) : signed(v, compactIdr));
  const colorOf = (v) => (v > 0 ? GAIN_COLOR : v < 0 ? LOSS_COLOR : '#999');

  const downloadExcel = () => {
    if (!data) return;
    const wb = XLSX.utils.book_new();
    const modeLabel = MODES.find(m => m.key === data.mode)?.label || data.mode;
    const whName = warehouses.find(w => w.id === data.warehouse_id)?.name || 'Semua gudang';
    const head = [
      ['Laporan Pergerakan Stok'],
      [`Periode: ${data.date_from} s/d ${data.date_to}`],
      [`Tampilan: ${modeLabel} · Gudang: ${whName}`],
      [],
    ];

    // One matrix per metric so both survive the export regardless of the toggle.
    const matrix = (useQty) => {
      const rows = [...head, ['Barang', 'Kode', 'Satuan', ...columns, 'Total']];
      for (const it of shown) {
        rows.push([
          it.item_name, it.item_code, it.unit_name,
          ...columns.map(d => {
            const c = cellOf(it, d);
            if (!c) return '';
            return useQty ? c.qty : c.value;
          }),
          useQty
            ? it.opname_quantity + (withDispatch ? it.dispatch_quantity : 0)
            : it.opname_value + (withDispatch ? it.dispatch_value : 0),
        ]);
      }
      const ws = XLSX.utils.aoa_to_sheet(rows);
      ws['!cols'] = [{ wch: 30 }, { wch: 12 }, { wch: 10 }, ...columns.map(() => ({ wch: useQty ? 9 : 13 })), { wch: useQty ? 11 : 15 }];
      return ws;
    };
    XLSX.utils.book_append_sheet(wb, matrix(true), 'Kuantitas');
    XLSX.utils.book_append_sheet(wb, matrix(false), 'Nilai (Rp)');

    const detail = [['Barang', 'Kode', 'Satuan', 'Opname Qty', 'Opname Nilai', ...(withDispatch ? ['Pengiriman Qty', 'Pengiriman Nilai'] : []), 'Total Qty', 'Total Nilai', 'Hari Bergerak']];
    for (const it of shown) {
      detail.push([
        it.item_name, it.item_code, it.unit_name,
        it.opname_quantity, it.opname_value,
        ...(withDispatch ? [it.dispatch_quantity, it.dispatch_value] : []),
        it.total_quantity, it.total_value, it.active_days,
      ]);
    }
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet(detail), 'Ringkasan Barang');

    const dayRows = [['Tanggal', 'Opname Lebih', 'Opname Kurang', ...(withDispatch ? ['Pengiriman'] : []), 'Bersih', 'Barang Bergerak']];
    for (const d of data.days) {
      dayRows.push([d.date, d.opname_gain_value, d.opname_loss_value, ...(withDispatch ? [d.dispatch_value] : []), d.net_value, d.items_moved]);
    }
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet(dayRows), 'Harian');

    XLSX.writeFile(wb, `pergerakan-stok-${data.mode}-${data.date_from}_${data.date_to}.xlsx`);
  };

  return (
    <>
      <div className="page-header">
        <h1>Pergerakan Stok</h1>
        {data && shown.length > 0 && (
          <button onClick={downloadExcel} className="btn btn-secondary">⬇ Download Excel</button>
        )}
      </div>

      <div className="card" style={{ marginBottom: '1.25rem' }}>
        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginBottom: '1rem' }}>
          {MODES.map(m => (
            <button
              key={m.key}
              className={`btn ${mode === m.key ? 'btn-primary' : 'btn-secondary'}`}
              onClick={() => setMode(m.key)}
              title={m.hint}
            >{m.label}</button>
          ))}
        </div>
        <div className="card-header" style={{ flexWrap: 'wrap', gap: '0.75rem', marginBottom: 0 }}>
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
            <select value={warehouseId} onChange={e => setWarehouseId(e.target.value)} title="Gudang">
              <option value="">Semua gudang</option>
              {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
            </select>
            <input type="date" value={draft.date_from} onChange={e => setDraft(d => ({ ...d, date_from: e.target.value }))} title="Dari tanggal" />
            <input type="date" value={draft.date_to} onChange={e => setDraft(d => ({ ...d, date_to: e.target.value }))} title="Sampai tanggal" />
            <button className="btn btn-sm btn-primary" onClick={() => { setPreset(''); setRange(draft); }}>Terapkan</button>
          </div>
        </div>
      </div>

      {error && <div className="alert alert-error" style={{ marginBottom: '1rem' }}>{error}</div>}

      {summary && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '1rem', marginBottom: '1.5rem' }}>
          <StatCard label="Opname Kurang" value={idr(summary.opname_loss_value)} color={LOSS_COLOR} sub="stok hilang saat dihitung" />
          <StatCard label="Opname Lebih" value={idr(summary.opname_gain_value)} color={GAIN_COLOR} sub="stok ditemukan lebih" />
          <StatCard
            label="Selisih Opname Bersih"
            value={idr(summary.opname_net_value)}
            color={colorOf(summary.opname_net_value)}
            sub={`${summary.opname_days} hari ada opname · ${data.opnames.length} sesi`}
          />
          {withDispatch && (
            <>
              <StatCard label="Pemakaian (Pengiriman)" value={idr(summary.dispatch_value)} color={DISPATCH_COLOR} sub="nilai FIFO barang keluar" />
              <StatCard label="Total Perubahan" value={idr(summary.net_value)} color={colorOf(summary.net_value)} sub="opname + pengiriman" />
            </>
          )}
          <StatCard label="Barang Bergerak" value={summary.item_count} sub={`dalam ${summary.day_count} hari`} />
        </div>
      )}

      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div className="card-header">
          <h2>{loading ? 'Memuat…' : 'Perubahan Nilai Stok per Hari'}</h2>
        </div>
        <DailyChart days={data?.days} withDispatch={withDispatch} />
      </div>

      <div className="card">
        <div className="card-header" style={{ flexWrap: 'wrap', gap: '0.6rem' }}>
          <h2>{loading ? 'Memuat…' : `${shown.length} barang`}</h2>
          <div className="filters" style={{ flexWrap: 'wrap', alignItems: 'center' }}>
            <input placeholder="Cari barang…" value={search} onChange={e => setSearch(e.target.value)} />
            <select value={sortBy} onChange={e => setSortBy(e.target.value)} title="Urutkan">
              <option value="value">Nilai bergerak terbesar</option>
              <option value="loss">Selisih opname terbesar</option>
              <option value="name">Nama barang</option>
            </select>
            <div style={{ display: 'flex', gap: '0.4rem' }}>
              <button className={`btn btn-sm ${isQty ? 'btn-primary' : 'btn-secondary'}`} onClick={() => setMetric('quantity')}>Kuantitas</button>
              <button className={`btn btn-sm ${!isQty ? 'btn-primary' : 'btn-secondary'}`} onClick={() => setMetric('value')}>Nilai (Rp)</button>
            </div>
            <label style={{ fontSize: '0.85rem', color: '#555', display: 'flex', alignItems: 'center', gap: '0.35rem', cursor: 'pointer' }}>
              <input type="checkbox" checked={allDates} onChange={e => setAllDates(e.target.checked)} />
              Tampilkan semua tanggal
            </label>
          </div>
        </div>

        <div style={{ overflowX: 'auto' }}>
          <table style={{ fontSize: '0.82rem' }}>
            <thead>
              <tr>
                <th style={{ ...stickyCell, zIndex: 2 }}>Barang</th>
                {columns.map(date => {
                  const ops = opnamesByDay[date];
                  return (
                    <th key={date} style={{ textAlign: 'right', padding: '0.5rem 0.55rem' }}>
                      <div>{fmtDate(date)}</div>
                      <div style={{ fontWeight: 400, textTransform: 'none', color: '#aaa' }}>
                        {ops ? (
                          ops.length === 1
                            ? <Link to={`/stock-opname/${ops[0].id}`} title={`Stok opname ${ops[0].warehouse_name} · ${ops[0].item_count} barang`} style={{ color: LOSS_COLOR, fontWeight: 600 }}>SO</Link>
                            : <span title={ops.map(o => `${o.warehouse_name} · ${o.item_count} barang`).join('\n')} style={{ color: LOSS_COLOR, fontWeight: 600 }}>SO×{ops.length}</span>
                        ) : fmtDow(date)}
                      </div>
                    </th>
                  );
                })}
                <th style={{ textAlign: 'right' }}>Total</th>
              </tr>
            </thead>
            <tbody>
              {shown.length === 0 ? (
                <tr><td colSpan={columns.length + 2} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                  {loading ? 'Memuat…' : 'Tidak ada pergerakan stok pada rentang ini'}
                </td></tr>
              ) : shown.map(it => {
                const total = totalOf(it);
                return (
                  <tr key={it.item_id}>
                    <td style={stickyCell}>
                      <Link to={`/items/stock/${it.item_id}`} style={{ fontWeight: 600, color: 'inherit' }}>{it.item_name}</Link>
                      <div style={{ fontSize: '0.72rem', color: '#aaa' }}>
                        {it.item_code || '—'} · {it.unit_name || '—'} · {it.active_days} hari
                      </div>
                    </td>
                    {columns.map(date => {
                      const c = cellOf(it, date);
                      if (!c || (c.qty === 0 && c.value === 0)) {
                        return <td key={date} style={{ textAlign: 'right', color: '#ddd', padding: '0.5rem 0.55rem' }}>·</td>;
                      }
                      const v = isQty ? c.qty : c.value;
                      // In the combined view, mark the cells an opname touched so
                      // a count-day correction stands out from routine usage.
                      const mark = withDispatch && c.hasOpname;
                      return (
                        <td
                          key={date}
                          title={cellTip(it, date, c)}
                          style={{
                            textAlign: 'right', whiteSpace: 'nowrap', padding: '0.5rem 0.55rem',
                            color: colorOf(v), fontVariantNumeric: 'tabular-nums',
                            background: mark ? 'rgba(220, 38, 38, 0.07)' : undefined,
                            fontWeight: mark ? 600 : 400,
                          }}
                        >{fmtCell(v)}</td>
                      );
                    })}
                    <td style={{ textAlign: 'right', whiteSpace: 'nowrap', fontWeight: 700, color: colorOf(total), fontVariantNumeric: 'tabular-nums' }}>
                      {isQty ? `${signed(total, (x) => num(x))} ${it.unit_name}` : signed(total, idr)}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <p style={{ fontSize: '0.78rem', color: '#999', padding: '0.75rem 1rem 0', margin: 0 }}>
          Angka negatif berarti stok berkurang, positif berarti bertambah. Kuantitas memakai satuan terkecil barang.
          {withDispatch
            ? ' Sel berlatar merah muda memuat koreksi stok opname; arahkan kursor untuk melihat rincian opname dan pengiriman.'
            : ' Hanya selisih stok opname (termasuk koreksinya) yang dihitung — pembelian, transfer dan pengiriman tidak ikut.'}
          {' '}Pembatalan dan edit pengiriman ikut mengurangi/menambah pada tanggal dicatat. Klik “SO” di kepala kolom untuk membuka sesi opname.
        </p>
      </div>
    </>
  );
}
