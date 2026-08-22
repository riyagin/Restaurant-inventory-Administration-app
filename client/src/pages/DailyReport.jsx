import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { getDailyReport, getBranches } from '../api';

// Laporan Harian — the day at a glance, with the paperwork underneath it.
//
// This page used to open with five stat tiles and then five long tables of
// individual documents. Everything was there and nothing was answered: "how did
// we do today" needed you to read every row and add up in your head, and the
// per-branch and per-division split — the two cuts anybody actually asks about —
// were not derivable from the page at all.
//
// So the landing view is now the overview: what each branch earned and spent,
// and which divisions carried the revenue. The document tables are still here,
// unchanged, one collapse away — they are what you open when the overview shows
// something surprising, which is the order those two questions come in.
//
// The overview figures come from the journal (see handler.dailyPerformance), not
// from summing the tables below. The tables are source rows, and summing them
// would miss payroll, Pembelanjaan Harian and opname write-offs while
// double-counting every dispatch against its mirror invoice.

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v ?? 0);

const fmtTime = (d) =>
  d ? new Date(d).toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit' }) : '—';

const todayISO = () => new Date().toISOString().split('T')[0];

// One hue per measure, held constant across every chart on the page: revenue is
// always this green and expense always this amber, so a bar's meaning never
// depends on which card you are looking at.
const REVENUE = '#1f9d68';
const EXPENSE = '#d98324';
const INK = '#1f2430';
const MUTED = '#8a93a3';
const TRACK = '#eef1f5';

function SectionHeader({ title, count, total, totalColor }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.75rem' }}>
      <h2 style={{ margin: 0, fontSize: '1rem' }}>
        {title}
        {count != null && (
          <span style={{ marginLeft: '0.5rem', fontSize: '0.78rem', color: '#999', fontWeight: 400 }}>
            ({count})
          </span>
        )}
      </h2>
      {total != null && total > 0 && (
        <span style={{ fontWeight: 700, color: totalColor || '#333', fontSize: '1rem' }}>{idr(total)}</span>
      )}
    </div>
  );
}

function EmptyRow({ cols, label }) {
  return (
    <tr>
      <td colSpan={cols} style={{ textAlign: 'center', color: '#ccc', padding: '1rem', fontSize: '0.85rem' }}>
        {label}
      </td>
    </tr>
  );
}

// A branch's day. Revenue and expense are drawn on one shared scale across every
// card, so bar length is comparable between branches rather than only within a
// card — the comparison the row exists to make.
function BranchCard({ branch, scale }) {
  const net = Number(branch.net || 0);
  const revenue = Number(branch.revenue || 0);
  const expense = Number(branch.expense || 0);
  const idle = revenue === 0 && expense === 0;
  const width = (v) => `${scale > 0 ? Math.min(100, (v / scale) * 100) : 0}%`;

  return (
    <div style={{
      border: '1px solid #e9edf3', borderRadius: '10px', padding: '0.9rem 1rem',
      background: idle ? '#fbfcfd' : '#fff',
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: '0.5rem' }}>
        <span style={{ fontWeight: 700, fontSize: '0.95rem', color: idle ? MUTED : INK }}>{branch.name}</span>
        <span style={{
          fontWeight: 700, fontSize: '0.95rem', whiteSpace: 'nowrap',
          color: idle ? '#c8cdd6' : net >= 0 ? REVENUE : '#c0392b',
        }}>
          {idle ? '—' : idr(net)}
        </span>
      </div>
      <div style={{ fontSize: '0.72rem', color: MUTED, marginTop: '0.1rem' }}>
        {idle ? 'tidak ada aktivitas' : net >= 0 ? 'laba hari ini' : 'rugi hari ini'}
      </div>

      <div style={{ marginTop: '0.7rem' }}>
        <Bar label="Pendapatan" value={revenue} width={width(revenue)} color={REVENUE} />
        <Bar label="Beban" value={expense} width={width(expense)} color={EXPENSE} />
      </div>
    </div>
  );
}

function Bar({ label, value, width, color, title }) {
  return (
    <div style={{ marginBottom: '0.4rem' }} title={title || `${label}: ${idr(value)}`}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.5rem', fontSize: '0.76rem' }}>
        <span style={{ color: MUTED }}>{label}</span>
        <span style={{ color: INK, fontWeight: 600, whiteSpace: 'nowrap' }}>{idr(value)}</span>
      </div>
      <div style={{ height: '6px', background: TRACK, borderRadius: '3px', marginTop: '0.2rem', overflow: 'hidden' }}>
        <div style={{ width, height: '100%', background: color, borderRadius: '3px' }} />
      </div>
    </div>
  );
}

export default function DailyReport() {
  const [date, setDate]         = useState(todayISO());
  const [branchId, setBranchId] = useState('');
  const [branches, setBranches] = useState([]);
  const [data, setData]         = useState(null);
  const [loading, setLoading]   = useState(false);
  const [error, setError]       = useState('');
  // Collapsed by default: the documents are the follow-up question, and opening
  // with five tables is exactly what buried the overview before.
  const [showDetail, setShowDetail] = useState(false);

  useEffect(() => {
    getBranches().then(r => setBranches(r.data)).catch(() => {});
  }, []);

  useEffect(() => {
    if (!date) return;
    setLoading(true);
    setError('');
    const params = { date };
    if (branchId) params.branch_id = branchId;
    getDailyReport(params)
      .then(r => setData(r.data))
      .catch(e => setError(e.response?.data?.error || 'Gagal memuat laporan'))
      .finally(() => setLoading(false));
  }, [date, branchId]);

  const fmtDate = (d) => d
    ? new Date(d + 'T00:00:00').toLocaleDateString('id-ID', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })
    : '—';

  const summary = data?.summary || {};
  const branchPerf = useMemo(() => data?.branches ?? [], [data]);

  // Divisions ranked by what they brought in — the chart answers "who made the
  // money today", so it is sorted by revenue and nothing else.
  const divisionPerf = useMemo(
    () => [...(data?.divisions ?? [])].sort((a, b) => Number(b.revenue) - Number(a.revenue)),
    [data],
  );

  const totals = useMemo(() => branchPerf.reduce(
    (acc, b) => ({
      revenue: acc.revenue + Number(b.revenue || 0),
      expense: acc.expense + Number(b.expense || 0),
    }),
    { revenue: 0, expense: 0 },
  ), [branchPerf]);
  const net = totals.revenue - totals.expense;

  const branchScale = useMemo(
    () => Math.max(0, ...branchPerf.flatMap(b => [Number(b.revenue || 0), Number(b.expense || 0)])),
    [branchPerf],
  );
  const divisionScale = useMemo(
    () => Math.max(0, ...divisionPerf.map(d => Number(d.revenue || 0))),
    [divisionPerf],
  );
  const divisionRevenueTotal = divisionPerf.reduce((s, d) => s + Number(d.revenue || 0), 0);

  const detailCount = data
    ? data.pos_imports.length + data.sales.length + data.invoices.length +
      data.dispatches.length + data.transfers.length + data.opnames.length
    : 0;

  const statusBadge = (status) => {
    const cfg = {
      paid:       { bg: '#e6f9f0', color: '#1b5e45', label: 'Lunas' },
      partial:    { bg: '#fff3e0', color: '#e67e22', label: 'Sebagian' },
      unpaid:     { bg: '#fdecea', color: '#c0392b', label: 'Belum' },
      dispatched: { bg: '#e8eaf6', color: '#3949ab', label: 'Pengiriman' },
    }[status] || { bg: '#f5f5f5', color: '#888', label: status };
    return (
      <span style={{ fontSize: '0.72rem', padding: '0.1rem 0.4rem', borderRadius: '3px', fontWeight: 600, background: cfg.bg, color: cfg.color }}>
        {cfg.label}
      </span>
    );
  };

  return (
    <>
      <div className="page-header">
        <h1>Laporan Harian</h1>
        <Link to="/reports/financial" className="btn btn-secondary">← Laporan Keuangan</Link>
      </div>

      {/* Date + branch filter */}
      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
          <label style={{ fontWeight: 600, fontSize: '0.9rem', color: '#444' }}>Tanggal</label>
          <input
            type="date"
            value={date}
            onChange={e => setDate(e.target.value)}
            style={{ padding: '0.4rem 0.75rem', border: '1px solid #ddd', borderRadius: '6px', fontSize: '0.95rem', fontWeight: 600 }}
          />
          <button className="btn btn-secondary btn-sm" onClick={() => setDate(todayISO())}>Hari Ini</button>
          {date && <span style={{ color: '#888', fontSize: '0.88rem' }}>{fmtDate(date)}</span>}

          {branches.length > 0 && (
            <>
              <span style={{ color: '#ddd' }}>|</span>
              <label style={{ fontWeight: 600, fontSize: '0.9rem', color: '#444' }}>Cabang</label>
              <select
                value={branchId}
                onChange={e => setBranchId(e.target.value)}
                style={{ padding: '0.4rem 0.6rem', border: '1px solid #ddd', borderRadius: '6px', fontSize: '0.88rem' }}
              >
                <option value="">Semua Cabang</option>
                {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
              </select>
            </>
          )}
        </div>
      </div>

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
      {loading && <p style={{ color: '#999', padding: '1rem 0' }}>Memuat...</p>}

      {data && !loading && (
        <>
          {/* ── The day in three numbers ── */}
          <div className="stats-grid" style={{ marginBottom: '1.5rem' }}>
            <div className="stat-card">
              <div className="label">Pendapatan</div>
              <div className="value" style={{ fontSize: '1.35rem', color: totals.revenue > 0 ? REVENUE : '#ccc' }}>
                {idr(totals.revenue)}
              </div>
              <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>
                {data.pos_imports.length} import POS
                {summary.manual_sales > 0 ? ` · ${idr(summary.manual_sales)} manual` : ''}
              </div>
            </div>
            <div className="stat-card">
              <div className="label">Beban</div>
              <div className="value" style={{ fontSize: '1.35rem', color: totals.expense > 0 ? EXPENSE : '#ccc' }}>
                {idr(totals.expense)}
              </div>
              <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>
                termasuk pemakaian dispatch &amp; gaji
              </div>
            </div>
            <div className="stat-card">
              <div className="label">{net >= 0 ? 'Laba Hari Ini' : 'Rugi Hari Ini'}</div>
              <div className="value" style={{ fontSize: '1.35rem', color: net >= 0 ? REVENUE : '#c0392b' }}>
                {idr(Math.abs(net))}
              </div>
              <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>
                {totals.revenue > 0 ? `margin ${Math.round((net / totals.revenue) * 100)}%` : 'belum ada pendapatan'}
              </div>
            </div>
            <div className="stat-card">
              <div className="label">Dokumen Hari Ini</div>
              <div className="value" style={{ fontSize: '1.35rem', color: detailCount > 0 ? INK : '#ccc' }}>{detailCount}</div>
              <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>
                invoice, pengiriman, opname, transfer
              </div>
            </div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) minmax(320px,1fr)', gap: '1.5rem', marginBottom: '1.5rem', alignItems: 'start' }}>
            {/* ── Per-branch performance ── */}
            <div className="card">
              <div className="card-header" style={{ marginBottom: '1rem' }}>
                <h2 style={{ fontSize: '1rem', margin: 0 }}>Performa per Cabang</h2>
                <span style={{ fontSize: '0.75rem', color: '#aaa' }}>dari jurnal</span>
              </div>

              {branchPerf.length === 0 ? (
                <p style={{ color: '#bbb', padding: '1rem 0' }}>Belum ada cabang.</p>
              ) : (
                <div style={{ display: 'grid', gap: '0.75rem' }}>
                  {branchPerf.map(b => <BranchCard key={b.id} branch={b} scale={branchScale} />)}
                </div>
              )}

              <p style={{ fontSize: '0.75rem', color: MUTED, marginTop: '1rem', lineHeight: 1.5 }}>
                Setiap akun pendapatan/beban dihitung ke cabang pemiliknya. Akun yang tidak
                dimiliki cabang mana pun tidak masuk ke sini, sehingga laba tiap cabang hanya
                menanggung biayanya sendiri.
              </p>
            </div>

            {/* ── Which divisions made the money ──
                One measure, one hue, ranked: the question is who is biggest, and
                bar length answers it before any figure is read. Sorted rather than
                alphabetical, since a ranking that is not ordered is a table. */}
            <div className="card">
              <div className="card-header" style={{ marginBottom: '1rem' }}>
                <h2 style={{ fontSize: '1rem', margin: 0 }}>Pendapatan per Divisi</h2>
                <span style={{ fontSize: '0.75rem', color: '#aaa' }}>{idr(divisionRevenueTotal)}</span>
              </div>

              {divisionPerf.length === 0 ? (
                <p style={{ color: '#bbb', padding: '1rem 0' }}>
                  Tidak ada divisi yang mencatat aktivitas pada tanggal ini.
                </p>
              ) : divisionPerf.map(d => {
                const revenue = Number(d.revenue || 0);
                const share = divisionRevenueTotal > 0 ? Math.round((revenue / divisionRevenueTotal) * 100) : 0;
                return (
                  <div
                    key={d.id}
                    style={{ marginBottom: '0.8rem' }}
                    title={`${d.branch_name} / ${d.name}\nPendapatan ${idr(revenue)}\nBeban ${idr(d.expense)}\nSelisih ${idr(d.net)}`}
                  >
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: '0.5rem' }}>
                      <span style={{ fontSize: '0.85rem', fontWeight: 600, color: INK }}>
                        {d.name}
                        {!branchId && <span style={{ color: MUTED, fontWeight: 400 }}> · {d.branch_name}</span>}
                      </span>
                      <span style={{ fontSize: '0.85rem', fontWeight: 700, whiteSpace: 'nowrap', color: INK }}>
                        {idr(revenue)}
                      </span>
                    </div>
                    <div style={{ height: '8px', background: TRACK, borderRadius: '4px', marginTop: '0.3rem', overflow: 'hidden' }}>
                      <div style={{
                        width: `${divisionScale > 0 ? Math.max(revenue > 0 ? 2 : 0, (revenue / divisionScale) * 100) : 0}%`,
                        height: '100%', background: REVENUE, borderRadius: '4px',
                      }} />
                    </div>
                    <div style={{ fontSize: '0.72rem', color: MUTED, marginTop: '0.15rem' }}>
                      {share}% dari pendapatan · beban {idr(d.expense)}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          {/* ── The paperwork ── */}
          <div className="card" style={{ marginBottom: showDetail ? '1.5rem' : 0 }}>
            <button
              onClick={() => setShowDetail(s => !s)}
              style={{
                display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%',
                background: 'none', border: 'none', padding: 0, cursor: 'pointer', font: 'inherit', textAlign: 'left',
              }}
            >
              <span style={{ fontWeight: 700, fontSize: '1rem', color: INK }}>
                Rincian Dokumen
                <span style={{ marginLeft: '0.5rem', fontSize: '0.78rem', color: '#999', fontWeight: 400 }}>
                  ({detailCount})
                </span>
              </span>
              <span style={{ fontSize: '0.82rem', color: '#4f8ef7', fontWeight: 600 }}>
                {showDetail ? 'Sembunyikan ▲' : 'Tampilkan ▼'}
              </span>
            </button>
          </div>

          {showDetail && (
            <>
              {/* POS Imports */}
              <div className="card" style={{ marginBottom: '1.5rem' }}>
                <SectionHeader title="Import POS" count={data.pos_imports.length} total={summary.pos_revenue} totalColor={REVENUE} />
                <table>
                  <thead>
                    <tr>
                      <th>Deskripsi</th>
                      <th>File</th>
                      <th style={{ textAlign: 'right' }}>Revenue</th>
                      <th style={{ textAlign: 'right' }}>Diskon</th>
                      <th style={{ textAlign: 'right' }}>Kas Diterima</th>
                      <th>Dicatat oleh</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.pos_imports.length === 0
                      ? <EmptyRow cols={6} label="Tidak ada import POS pada tanggal ini" />
                      : data.pos_imports.map(imp => {
                          const lines = imp.lines || [];
                          const revTotal  = lines.filter(l => l.line_type === 'revenue').reduce((s, l) => s + Number(l.amount), 0);
                          const discTotal = lines.filter(l => l.line_type === 'discount').reduce((s, l) => s + Number(l.amount), 0);
                          const cashTotal = lines.filter(l => l.line_type === 'cash').reduce((s, l) => s + Number(l.amount), 0);
                          return (
                            <tr key={imp.id}>
                              <td style={{ fontWeight: 500 }}>{imp.description}</td>
                              <td style={{ color: '#888', fontSize: '0.82rem' }}>{imp.source_file || '—'}</td>
                              <td style={{ textAlign: 'right', fontWeight: 700, color: REVENUE, whiteSpace: 'nowrap' }}>{idr(revTotal)}</td>
                              <td style={{ textAlign: 'right', color: discTotal < 0 ? '#e74c3c' : '#ccc', whiteSpace: 'nowrap' }}>
                                {discTotal < 0 ? idr(discTotal) : '—'}
                              </td>
                              <td style={{ textAlign: 'right', fontWeight: 600, color: '#2c6fc2', whiteSpace: 'nowrap' }}>{idr(cashTotal)}</td>
                              <td style={{ color: '#888', fontSize: '0.82rem' }}>{imp.created_by_name || '—'}</td>
                            </tr>
                          );
                        })}
                  </tbody>
                </table>
              </div>

              {/* Manual Sales */}
              {data.sales.length > 0 && (
                <div className="card" style={{ marginBottom: '1.5rem' }}>
                  <SectionHeader title="Penjualan Manual" count={data.sales.length} total={summary.manual_sales} totalColor={REVENUE} />
                  <table>
                    <thead>
                      <tr>
                        <th>Keterangan</th>
                        <th>Cabang / Divisi</th>
                        <th style={{ textAlign: 'right' }}>Jumlah</th>
                        <th>Dicatat oleh</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.sales.map(s => (
                        <tr key={s.id}>
                          <td style={{ color: '#555', fontSize: '0.88rem' }}>{s.description || '—'}</td>
                          <td style={{ fontSize: '0.85rem', color: '#666' }}>
                            {s.branch_name || '—'}{s.division_name ? ` / ${s.division_name}` : ''}
                          </td>
                          <td style={{ textAlign: 'right', fontWeight: 700, color: REVENUE, whiteSpace: 'nowrap' }}>{idr(s.amount)}</td>
                          <td style={{ color: '#888', fontSize: '0.82rem' }}>{s.created_by_name || '—'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {/* Invoices */}
              <div className="card" style={{ marginBottom: '1.5rem' }}>
                <SectionHeader
                  title="Invoice Pembelian & Beban"
                  count={data.invoices.length}
                  total={summary.purchases + summary.expenses}
                  totalColor="#2c6fc2"
                />
                <table>
                  <thead>
                    <tr>
                      <th>No. Invoice</th>
                      <th>Vendor</th>
                      <th>Jenis</th>
                      <th>Cabang / Divisi</th>
                      <th style={{ textAlign: 'right' }}>Total</th>
                      <th>Bayar</th>
                      <th></th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.invoices.length === 0
                      ? <EmptyRow cols={7} label="Tidak ada invoice pada tanggal ini" />
                      : data.invoices.map(inv => (
                          <tr key={inv.id}>
                            <td style={{ fontWeight: 600, fontSize: '0.88rem' }}>{inv.invoice_number}</td>
                            <td style={{ color: '#666', fontSize: '0.88rem' }}>{inv.vendor_name || '—'}</td>
                            <td>
                              <span style={{
                                fontSize: '0.72rem', padding: '0.1rem 0.4rem', borderRadius: '3px', fontWeight: 600,
                                background: inv.invoice_type === 'expense' ? '#fff3e0' : '#e8f4fd',
                                color: inv.invoice_type === 'expense' ? '#e67e22' : '#2c6fc2',
                              }}>
                                {inv.invoice_type === 'expense' ? 'Beban' : 'Pembelian'}
                              </span>
                            </td>
                            <td style={{ fontSize: '0.85rem', color: '#666' }}>
                              {inv.branch_name || '—'}{inv.division_name ? ` / ${inv.division_name}` : ''}
                            </td>
                            <td style={{ textAlign: 'right', fontWeight: 700, whiteSpace: 'nowrap' }}>{idr(inv.total)}</td>
                            <td>{statusBadge(inv.payment_status)}</td>
                            <td>
                              <Link to={`/invoices/view/${inv.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                            </td>
                          </tr>
                        ))}
                  </tbody>
                </table>
              </div>

              {/* Dispatches */}
              <div className="card" style={{ marginBottom: '1.5rem' }}>
                <SectionHeader title="Pengiriman ke Cabang" count={data.dispatches.length} />
                <table>
                  <thead>
                    <tr>
                      <th>Waktu</th>
                      <th>Gudang</th>
                      <th>Cabang / Divisi</th>
                      <th>Catatan</th>
                      <th style={{ textAlign: 'right' }}>Jml Item</th>
                      <th>Oleh</th>
                      <th></th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.dispatches.length === 0
                      ? <EmptyRow cols={7} label="Tidak ada pengiriman ke cabang pada tanggal ini" />
                      : data.dispatches.map(d => (
                          <tr key={d.id}>
                            <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmtTime(d.dispatched_at)}</td>
                            <td style={{ fontSize: '0.88rem' }}>{d.warehouse_name}</td>
                            <td style={{ fontSize: '0.85rem', color: '#666' }}>{d.branch_name} / {d.division_name}</td>
                            <td style={{ color: '#888', fontSize: '0.82rem' }}>{d.notes || '—'}</td>
                            <td style={{ textAlign: 'right', fontSize: '0.85rem' }}>{d.item_count} SKU</td>
                            <td style={{ color: '#888', fontSize: '0.82rem' }}>{d.dispatched_by_name || '—'}</td>
                            <td>
                              <Link to={`/dispatches/${d.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                            </td>
                          </tr>
                        ))}
                  </tbody>
                </table>
              </div>

              {/* Stock Transfers */}
              {data.transfers.length > 0 && (
                <div className="card" style={{ marginBottom: '1.5rem' }}>
                  <SectionHeader title="Transfer Gudang" count={data.transfers.length} />
                  <table>
                    <thead>
                      <tr>
                        <th>Waktu</th>
                        <th>Dari Gudang</th>
                        <th>Ke Gudang</th>
                        <th style={{ textAlign: 'right' }}>Jml Item</th>
                        <th>Oleh</th>
                        <th></th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.transfers.map(t => (
                        <tr key={t.group_id}>
                          <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmtTime(t.transferred_at)}</td>
                          <td style={{ fontSize: '0.88rem' }}>{t.from_warehouse}</td>
                          <td style={{ fontSize: '0.88rem' }}>{t.to_warehouse}</td>
                          <td style={{ textAlign: 'right', fontSize: '0.85rem' }}>{t.distinct_items} SKU ({t.item_count} baris)</td>
                          <td style={{ color: '#888', fontSize: '0.82rem' }}>{t.transferred_by_name || '—'}</td>
                          <td>
                            <Link to={`/transfers/group/${t.group_id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {/* Stock Opname */}
              {data.opnames.length > 0 && (
                <div className="card" style={{ marginBottom: '1.5rem' }}>
                  <SectionHeader title="Stok Opname" count={data.opnames.length} />
                  <table>
                    <thead>
                      <tr>
                        <th>Waktu</th>
                        <th>Gudang</th>
                        <th>Operator</th>
                        <th style={{ textAlign: 'right' }}>Jml Item</th>
                        <th style={{ textAlign: 'right' }}>Total Selisih</th>
                        <th>Catatan</th>
                        <th></th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.opnames.map(op => (
                        <tr key={op.id}>
                          <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmtTime(op.performed_at)}</td>
                          <td style={{ fontSize: '0.88rem' }}>{op.warehouse_name}</td>
                          <td style={{ fontSize: '0.85rem', color: '#666' }}>{op.operator_name || op.performed_by_name || '—'}</td>
                          <td style={{ textAlign: 'right', fontSize: '0.85rem' }}>{op.item_count}</td>
                          <td style={{ textAlign: 'right', fontWeight: 600, color: op.total_diff > 0 ? '#e74c3c' : '#ccc' }}>
                            {op.total_diff > 0 ? op.total_diff : '—'}
                          </td>
                          <td style={{ color: '#888', fontSize: '0.82rem' }}>{op.notes || '—'}</td>
                          <td>
                            <Link to={`/stock-opname/detail/${op.id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {detailCount === 0 && (
                <div style={{ textAlign: 'center', color: '#bbb', padding: '3rem', fontSize: '0.95rem' }}>
                  Tidak ada dokumen pada tanggal ini.
                </div>
              )}
            </>
          )}
        </>
      )}
    </>
  );
}
