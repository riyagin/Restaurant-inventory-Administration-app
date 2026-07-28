import { useCallback, useEffect, useMemo, useState } from 'react';
import * as XLSX from 'xlsx';
import { getPriceChangesReport } from '../api';

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

function todayStr() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
function nDaysAgo(n) {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

// Percentage pill — green when prices fell (cheaper is good), red when they rose.
function Pct({ value, invert = false, size = '0.85rem' }) {
  if (value === null || value === undefined) {
    return <span style={{ color: '#bbb', fontSize: size }}>—</span>;
  }
  const up = value > 0;
  const good = invert ? up : !up;
  const color = Math.abs(value) < 0.05 ? '#888' : good ? '#16a34a' : '#dc2626';
  const arrow = Math.abs(value) < 0.05 ? '' : up ? '▲' : '▼';
  return (
    <span style={{ color, fontWeight: 600, fontSize: size, whiteSpace: 'nowrap' }}>
      {arrow} {value > 0 ? '+' : ''}{value.toFixed(1)}%
    </span>
  );
}

// ─── weekly price index chart (line + area, 100 = harga awal periode) ─────────
function IndexChart({ weeks }) {
  if (!weeks || weeks.length === 0) {
    return <p style={{ color: '#999', fontSize: '0.85rem', textAlign: 'center', padding: '2rem 0' }}>Tidak ada pembelian pada rentang ini.</p>;
  }
  const pts = weeks.filter(w => w.index_pct !== null);
  if (pts.length === 0) {
    return <p style={{ color: '#999', fontSize: '0.85rem', textAlign: 'center', padding: '2rem 0' }}>Belum cukup data untuk membentuk indeks harga.</p>;
  }

  const W = 720, H = 260;
  const padL = 46, padR = 14, padT = 16, padB = 46;
  const plotW = W - padL - padR;
  const plotH = H - padT - padB;

  const vals = pts.map(p => p.index_pct);
  let lo = Math.min(100, ...vals), hi = Math.max(100, ...vals);
  const span = Math.max(hi - lo, 2);
  lo -= span * 0.15; hi += span * 0.15;

  const px = (i) => padL + (pts.length === 1 ? plotW / 2 : (i / (pts.length - 1)) * plotW);
  const py = (v) => padT + plotH - ((v - lo) / (hi - lo)) * plotH;

  const line = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${px(i)},${py(p.index_pct)}`).join(' ');
  const area = `${line} L${px(pts.length - 1)},${py(lo)} L${px(0)},${py(lo)} Z`;
  const ticks = [0, 0.25, 0.5, 0.75, 1].map(f => lo + (hi - lo) * f);
  const last = pts[pts.length - 1].index_pct;
  const rising = last >= 100;
  const stroke = rising ? '#dc2626' : '#16a34a';

  return (
    <div style={{ width: '100%', overflowX: 'auto' }}>
      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', minWidth: '340px', height: 'auto', display: 'block' }}>
        <defs>
          <linearGradient id="idxfill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={stroke} stopOpacity="0.22" />
            <stop offset="100%" stopColor={stroke} stopOpacity="0.02" />
          </linearGradient>
        </defs>

        {ticks.map((t, i) => (
          <g key={i}>
            <line x1={padL} y1={py(t)} x2={padL + plotW} y2={py(t)} stroke="#f1f1f4" strokeWidth={1} />
            <text x={padL - 6} y={py(t) + 4} textAnchor="end" fontSize={10} fill="#aaa">{t.toFixed(0)}</text>
          </g>
        ))}

        {/* baseline: harga di awal periode */}
        <line x1={padL} y1={py(100)} x2={padL + plotW} y2={py(100)} stroke="#94a3b8" strokeWidth={1} strokeDasharray="4 3" />
        <text x={padL + plotW} y={py(100) - 5} textAnchor="end" fontSize={9} fill="#94a3b8">basis 100</text>

        <path d={area} fill="url(#idxfill)" />
        <path d={line} fill="none" stroke={stroke} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />

        {pts.map((p, i) => (
          <g key={p.week_start}>
            <circle cx={px(i)} cy={py(p.index_pct)} r={3.5} fill="#fff" stroke={stroke} strokeWidth={2}>
              <title>
                {`${fmtDate(p.week_start)} – ${fmtDate(p.week_end)}\nIndeks: ${p.index_pct.toFixed(1)} (${p.change_pct > 0 ? '+' : ''}${p.change_pct.toFixed(1)}%)\nBelanja: ${idr(p.spend)}\nBarang dibeli: ${p.item_count}\nKeranjang ter-update: ${p.priced_pct.toFixed(0)}%`}
              </title>
            </circle>
            <text x={px(i)} y={H - padB + 16} textAnchor="middle" fontSize={9} fill="#999">
              {fmtDate(p.week_start)}
            </text>
            <text x={px(i)} y={H - padB + 28} textAnchor="middle" fontSize={8} fill="#c0c0c8">
              {p.change_pct > 0 ? '+' : ''}{p.change_pct.toFixed(1)}%
            </text>
          </g>
        ))}

        <line x1={padL} y1={padT} x2={padL} y2={padT + plotH} stroke="#e5e5ea" strokeWidth={1} />
        <line x1={padL} y1={padT + plotH} x2={padL + plotW} y2={padT + plotH} stroke="#e5e5ea" strokeWidth={1} />
      </svg>
      <p style={{ textAlign: 'center', fontSize: '0.78rem', color: '#999', margin: '0.35rem 0 0' }}>
        Indeks harga keranjang tetap per minggu — 100 = harga pada awal periode. Barang yang tidak dibeli pada suatu
        minggu memakai harga terakhirnya.
      </p>
    </div>
  );
}

const PRESETS = [
  { label: '30 Hari', days: 29 },
  { label: '90 Hari', days: 89 },
  { label: '6 Bulan', days: 179 },
  { label: '1 Tahun', days: 364 },
];

export default function PriceChangeReport() {
  const [range, setRange] = useState({ date_from: nDaysAgo(89), date_to: todayStr() });
  const [draft, setDraft] = useState({ date_from: nDaysAgo(89), date_to: todayStr() });
  const [preset, setPreset] = useState('90 Hari');
  const [stockFilter, setStockFilter] = useState('');   // '' | 'true' | 'false'
  const [dirFilter, setDirFilter] = useState('all');    // all | up | down
  const [search, setSearch] = useState('');

  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    setLoading(true);
    setError('');
    const params = { ...range };
    if (stockFilter) params.is_stock = stockFilter;
    getPriceChangesReport(params)
      .then(r => setData(r.data))
      .catch(e => setError(e?.response?.data?.error || 'Gagal memuat laporan perubahan harga'))
      .finally(() => setLoading(false));
  }, [range, stockFilter]);

  useEffect(() => { load(); }, [load]);

  const applyPreset = (p) => {
    const next = { date_from: nDaysAgo(p.days), date_to: todayStr() };
    setPreset(p.label);
    setDraft(next);
    setRange(next);
  };

  const summary = data?.summary;
  const items = data?.items || [];

  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    return items.filter(it => {
      if (dirFilter === 'up' && !(it.in_basket && it.change_pct > 0)) return false;
      if (dirFilter === 'down' && !(it.in_basket && it.change_pct < 0)) return false;
      if (q && !`${it.item_name} ${it.item_code}`.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [items, dirFilter, search]);

  const downloadExcel = () => {
    if (!data) return;
    const wb = XLSX.utils.book_new();
    const period = `${range.date_from} s/d ${range.date_to}`;

    const rows = [
      ['Laporan Perubahan Harga Pembelian'],
      [`Periode: ${period}`],
      [`Perubahan keseluruhan: ${summary.change_pct === null ? '—' : summary.change_pct.toFixed(2) + '%'}`],
      [],
      ['Barang', 'Kode', 'Jenis', 'Satuan', 'Qty Dibeli', 'Total Belanja', 'Harga Awal', 'Harga Akhir', 'Perubahan %', 'Dampak (Rp)', 'Pembelian', 'Tgl Awal', 'Tgl Akhir'],
    ];
    for (const it of shown) {
      rows.push([
        it.item_name, it.item_code, it.is_stock ? 'Stok' : 'Non-stok', it.unit_name,
        it.quantity, it.spend, it.first_price, it.in_basket ? it.last_price : '',
        it.change_pct === null || it.change_pct === undefined ? '' : Number(it.change_pct.toFixed(2)),
        it.in_basket ? Math.round(it.impact) : '',
        it.purchase_count, it.first_date, it.last_date,
      ]);
    }
    const ws1 = XLSX.utils.aoa_to_sheet(rows);
    ws1['!cols'] = [{ wch: 30 }, { wch: 12 }, { wch: 10 }, { wch: 12 }, { wch: 12 }, { wch: 16 }, { wch: 14 }, { wch: 14 }, { wch: 12 }, { wch: 16 }, { wch: 10 }, { wch: 12 }, { wch: 12 }];
    XLSX.utils.book_append_sheet(wb, ws1, 'Perubahan Harga');

    const weekRows = [['Minggu Mulai', 'Minggu Selesai', 'Indeks (100=awal)', 'Perubahan %', 'Belanja', 'Jumlah Barang']];
    for (const w of data.weeks) {
      weekRows.push([
        w.week_start, w.week_end,
        w.index_pct === null ? '' : Number(w.index_pct.toFixed(2)),
        w.change_pct === null ? '' : Number(w.change_pct.toFixed(2)),
        w.spend, w.item_count,
      ]);
    }
    const ws2 = XLSX.utils.aoa_to_sheet(weekRows);
    ws2['!cols'] = [{ wch: 14 }, { wch: 14 }, { wch: 18 }, { wch: 13 }, { wch: 16 }, { wch: 14 }];
    XLSX.utils.book_append_sheet(wb, ws2, 'Indeks Mingguan');

    XLSX.writeFile(wb, `perubahan-harga-${range.date_from}_${range.date_to}.xlsx`);
  };

  return (
    <>
      <div className="page-header">
        <h1>Perubahan Harga Pembelian</h1>
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
            <select value={stockFilter} onChange={e => setStockFilter(e.target.value)} title="Jenis barang">
              <option value="">Semua barang</option>
              <option value="true">Barang stok</option>
              <option value="false">Barang non-stok</option>
            </select>
          </div>
        </div>
      </div>

      {error && <div className="alert alert-error" style={{ marginBottom: '1rem' }}>{error}</div>}

      {summary && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))', gap: '1rem', marginBottom: '1.5rem' }}>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Perubahan Harga Keseluruhan</div>
            <div style={{ fontSize: '1.6rem', fontWeight: 700 }}>
              <Pct value={summary.change_pct} size="1.6rem" />
            </div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>
              {fmtDate(data.date_from)} → {fmtDate(data.date_to)}
            </div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Nilai Keranjang Awal</div>
            <div style={{ fontSize: '1.2rem', fontWeight: 700 }}>{idr(summary.base_cost)}</div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>harga awal × qty periode</div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Nilai Keranjang Akhir</div>
            <div style={{ fontSize: '1.2rem', fontWeight: 700, color: summary.end_cost > summary.base_cost ? '#dc2626' : '#16a34a' }}>
              {idr(summary.end_cost)}
            </div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>
              selisih {idr(summary.end_cost - summary.base_cost)}
            </div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Barang Dibandingkan</div>
            <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>{summary.basket_item_count}</div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>dari {summary.tracked_items} barang dibeli</div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Naik / Turun</div>
            <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>
              <span style={{ color: '#dc2626' }}>{summary.items_up}</span>
              <span style={{ color: '#ccc' }}> / </span>
              <span style={{ color: '#16a34a' }}>{summary.items_down}</span>
            </div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>{summary.items_flat} harga tetap</div>
          </div>
          <div className="card" style={{ padding: '1.25rem' }}>
            <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.4rem' }}>Total Belanja Periode</div>
            <div style={{ fontSize: '1.2rem', fontWeight: 700, color: '#e67e22' }}>{idr(summary.total_spend)}</div>
            <div style={{ fontSize: '0.75rem', color: '#aaa', marginTop: '0.25rem' }}>{summary.week_count} minggu</div>
          </div>
        </div>
      )}

      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div className="card-header"><h2>{loading ? 'Memuat…' : 'Tren Harga Mingguan'}</h2></div>
        <div style={{ padding: '0.5rem 1rem 1rem' }}>
          <IndexChart weeks={data?.weeks} />
        </div>
      </div>

      <div className="card">
        <div className="card-header" style={{ flexWrap: 'wrap', gap: '0.6rem' }}>
          <h2>{loading ? 'Memuat…' : `${shown.length} barang`}</h2>
          <div className="filters" style={{ flexWrap: 'wrap' }}>
            <input placeholder="Cari barang…" value={search} onChange={e => setSearch(e.target.value)} />
            <select value={dirFilter} onChange={e => setDirFilter(e.target.value)}>
              <option value="all">Semua perubahan</option>
              <option value="up">Hanya naik</option>
              <option value="down">Hanya turun</option>
            </select>
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>Barang</th>
              <th>Satuan</th>
              <th style={{ textAlign: 'right' }}>Qty Dibeli</th>
              <th style={{ textAlign: 'right' }}>Harga Awal</th>
              <th style={{ textAlign: 'right' }}>Harga Akhir</th>
              <th style={{ textAlign: 'right' }}>Perubahan</th>
              <th style={{ textAlign: 'right' }}>Dampak Rp</th>
              <th style={{ textAlign: 'right' }}>Belanja</th>
            </tr>
          </thead>
          <tbody>
            {shown.length === 0 ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                {loading ? 'Memuat…' : 'Tidak ada pembelian pada rentang ini'}
              </td></tr>
            ) : shown.map(it => (
              <tr key={`${it.item_id}-${it.unit_index}`}>
                <td>
                  <div style={{ fontWeight: 600 }}>{it.item_name}</div>
                  <div style={{ fontSize: '0.75rem', color: '#aaa' }}>
                    {it.item_code || '—'} · {it.is_stock ? 'stok' : 'non-stok'} · {it.purchase_count}× beli
                    {!it.in_basket && ' · hanya 1 minggu'}
                  </div>
                </td>
                <td style={{ fontSize: '0.85rem', color: '#666' }}>{it.unit_name || '—'}</td>
                <td style={{ textAlign: 'right' }}>{num(it.quantity)}</td>
                <td style={{ textAlign: 'right' }}>{idr(it.first_price)}</td>
                <td style={{ textAlign: 'right', fontWeight: 600 }}>{it.in_basket ? idr(it.last_price) : '—'}</td>
                <td style={{ textAlign: 'right' }}><Pct value={it.in_basket ? it.change_pct : null} /></td>
                <td style={{ textAlign: 'right', color: it.impact > 0 ? '#dc2626' : it.impact < 0 ? '#16a34a' : '#888', fontWeight: 600 }}>
                  {it.in_basket ? `${it.impact > 0 ? '+' : ''}${idr(it.impact)}` : '—'}
                </td>
                <td style={{ textAlign: 'right', color: '#e67e22' }}>{compactIdr(it.spend)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p style={{ fontSize: '0.78rem', color: '#999', padding: '0.75rem 1rem 0', margin: 0 }}>
          Harga awal/akhir adalah rata-rata tertimbang pada minggu pertama dan terakhir barang tersebut dibeli dalam
          periode. Barang yang hanya dibeli pada satu minggu tidak punya pembanding sehingga tidak masuk perhitungan
          keseluruhan. Dampak Rp = qty periode × selisih harga.
        </p>
      </div>
    </>
  );
}
