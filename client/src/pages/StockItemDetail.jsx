import { useEffect, useMemo, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getItemStockDetail, getStockHistory } from '../api';
import { typeLabel, typeStyle, sourcePath } from '../stockMovements';
import ItemPriceBreakdown from '../components/ItemPriceBreakdown';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(v) || 0);
const num     = (v) => Number(v ?? 0).toLocaleString('id-ID', { maximumFractionDigits: 3 });
const fmt     = (d) => (d ? new Date(d).toLocaleDateString('id-ID', { dateStyle: 'medium' }) : '—');
const fmtTime = (d) => (d ? new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' }) : '—');
const monthLabel = (m) => {
  if (!m) return '—';
  const [y, mo] = m.split('-');
  return new Date(Number(y), Number(mo) - 1, 1).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' });
};

const PAYMENT_LABEL = { unpaid: 'Belum Bayar', partial: 'Sebagian', paid: 'Lunas' };
const PAYMENT_STYLE = {
  unpaid:  { background: '#fdecea', color: '#e74c3c' },
  partial: { background: '#fef9e7', color: '#e67e22' },
  paid:    { background: '#e6f9f0', color: '#27ae60' },
};

const DISPATCH_STATUS_LABEL = { active: 'Aktif', cancelled: 'Dibatalkan' };
const DISPATCH_STATUS_STYLE = {
  active:    { background: '#f3e8ff', color: '#8b5cf6' },
  cancelled: { background: '#eee',    color: '#777' },
};

const PAGE_SIZE = 50;

const TABS = [
  ['ringkasan',  'Ringkasan'],
  ['pembelian',  'Pembelian'],
  ['harga',      'Riwayat Harga'],
  ['pemakaian',  'Pemakaian'],
  ['pergerakan', 'Pergerakan Stok'],
];

const Empty = ({ children, cols }) => (
  <tr><td colSpan={cols} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>{children}</td></tr>
);

function Card({ title, right, children }) {
  return (
    <div className="card" style={{ marginBottom: '1.25rem' }}>
      <div className="card-header">
        <h2>{title}</h2>
        {right}
      </div>
      {children}
    </div>
  );
}

function Stat({ label, value, sub, color }) {
  return (
    <div className="card" style={{ padding: '1.15rem' }}>
      <div style={{ fontSize: '0.72rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.35rem' }}>
        {label}
      </div>
      <div style={{ fontSize: '1.25rem', fontWeight: 700, color: color ?? '#333' }}>{value}</div>
      {sub && <div style={{ fontSize: '0.78rem', color: '#aaa', marginTop: '0.2rem' }}>{sub}</div>}
    </div>
  );
}

function Badge({ label, style }) {
  return (
    <span style={{ display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px', fontSize: '0.75rem', fontWeight: 600, ...style }}>
      {label}
    </span>
  );
}

const dash = <span style={{ color: '#ccc' }}>—</span>;

export default function StockItemDetail() {
  const { id } = useParams();
  const [detail, setDetail]       = useState(null);
  const [movements, setMovements] = useState([]);
  const [loading, setLoading]     = useState(true);
  const [error, setError]         = useState('');
  const [tab, setTab]             = useState('ringkasan');
  const [mvFilter, setMvFilter]   = useState({ warehouse_id: '', type: '', direction: '' });
  const [mvPage, setMvPage]       = useState(1);

  useEffect(() => {
    setLoading(true);
    setError('');
    setMvPage(1);
    Promise.all([getItemStockDetail(id), getStockHistory(id, {})])
      .then(([d, m]) => {
        setDetail(d.data);
        setMovements(m.data);
      })
      .catch((err) => setError(err.response?.data?.error || 'Gagal memuat data barang.'))
      .finally(() => setLoading(false));
  }, [id]);

  const item       = detail?.item;
  const stock      = detail?.stock_by_warehouse ?? [];
  const purchases  = detail?.purchases ?? [];
  const dispatches = detail?.dispatches ?? [];
  const usage      = detail?.usage_by_destination ?? [];
  const monthly    = detail?.monthly_flow ?? [];
  const byType     = detail?.flow_by_type ?? [];

  const baseUnit = item?.units?.[0]?.name ?? '';

  const totals = useMemo(() => {
    const sum = (rows, field) => (rows ?? []).reduce((s, r) => s + Number(r[field] ?? 0), 0);
    return {
      onHandQty:   sum(detail?.stock_by_warehouse, 'quantity'),
      onHandValue: sum(detail?.stock_by_warehouse, 'value'),
      spend:         sum(detail?.purchases, 'line_total'),
      dispatchValue: sum(detail?.dispatches, 'value'),
      qtyIn:       sum(detail?.monthly_flow, 'qty_in'),
      qtyOut:      sum(detail?.monthly_flow, 'qty_out'),
      valueOut:    sum(detail?.monthly_flow, 'value_out'),
    };
  }, [detail]);

  const warehouseOptions = useMemo(() => {
    const map = new Map();
    for (const m of movements) {
      if (m.warehouse_id && !map.has(m.warehouse_id)) map.set(m.warehouse_id, m.warehouse_name ?? '—');
    }
    return [...map.entries()];
  }, [movements]);

  const typeOptions = useMemo(
    () => [...new Set(movements.map((m) => m.type))],
    [movements],
  );

  const filteredMovements = useMemo(
    () => movements.filter((m) =>
      (!mvFilter.warehouse_id || m.warehouse_id === mvFilter.warehouse_id) &&
      (!mvFilter.type || m.type === mvFilter.type) &&
      (!mvFilter.direction ||
        (mvFilter.direction === 'in'  && Number(m.quantity_change) > 0) ||
        (mvFilter.direction === 'out' && Number(m.quantity_change) < 0))),
    [movements, mvFilter],
  );

  const setFilter = (patch) => {
    setMvFilter((f) => ({ ...f, ...patch }));
    setMvPage(1);
  };

  const pageCount   = Math.max(1, Math.ceil(filteredMovements.length / PAGE_SIZE));
  const currentPage = Math.min(mvPage, pageCount);
  const pagedMovements = useMemo(
    () => filteredMovements.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE),
    [filteredMovements, currentPage],
  );

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Memuat…</div>;
  if (error)   return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>{error}</div>;
  if (!item)   return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>Barang tidak ditemukan.</div>;

  return (
    <>
      <div className="page-header">
        <div>
          <h1 style={{ marginBottom: '0.2rem' }}>{item.name}</h1>
          <div style={{ fontSize: '0.85rem', color: '#888' }}>
            {item.code} &nbsp;·&nbsp;
            <span style={{ background: '#e8f5e9', color: '#388e3c', fontWeight: 600, fontSize: '0.78rem', padding: '0.1rem 0.45rem', borderRadius: '4px' }}>
              Stok
            </span>
            &nbsp;·&nbsp; {item.units?.map((u) => u.name).join(' → ')}
          </div>
        </div>
        <div style={{ display: 'flex', gap: '0.6rem' }}>
          <Link to={`/inventory/history/${id}`} className="btn btn-secondary">Riwayat Stok Lengkap</Link>
          <Link to="/items" className="btn btn-secondary">← Kembali ke Barang</Link>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', gap: '1rem', marginBottom: '1.25rem' }}>
        <Stat label="Stok Saat Ini" value={`${num(totals.onHandQty)} ${baseUnit}`} sub={`${stock.length} gudang`} />
        <Stat label="Nilai Persediaan" value={idr(totals.onHandValue)} color="#27ae60" />
        <Stat label="Total Pembelian" value={idr(totals.spend)} sub={`${purchases.length} baris invoice`} color="#e74c3c" />
        <Stat label="Total Masuk" value={`${num(totals.qtyIn)} ${baseUnit}`} color="#27ae60" />
        <Stat label="Total Keluar" value={`${num(totals.qtyOut)} ${baseUnit}`} sub={idr(totals.valueOut)} color="#8b5cf6" />
      </div>

      <div style={{ display: 'flex', gap: '0.4rem', marginBottom: '1rem', flexWrap: 'wrap' }}>
        {TABS.map(([key, label]) => (
          <button
            key={key}
            type="button"
            onClick={() => setTab(key)}
            className={tab === key ? 'btn btn-primary btn-sm' : 'btn btn-secondary btn-sm'}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === 'ringkasan' && (
        <>
          <Card title={`Stok per Gudang (${stock.length})`}>
            <table>
              <thead>
                <tr>
                  <th>Gudang</th>
                  <th style={{ textAlign: 'right' }}>Qty ({baseUnit})</th>
                  <th style={{ textAlign: 'right' }}>Nilai</th>
                  <th style={{ textAlign: 'right' }}>Harga Rata-rata</th>
                  <th style={{ textAlign: 'right' }}>Lot</th>
                  <th>Lot Tertua</th>
                </tr>
              </thead>
              <tbody>
                {stock.length === 0 ? (
                  <Empty cols={6}>Tidak ada stok tersisa di gudang mana pun.</Empty>
                ) : stock.map((r) => {
                  const qty = Number(r.quantity ?? 0);
                  return (
                    <tr key={r.warehouse_id}>
                      <td><span className="badge">{r.warehouse_name}</span></td>
                      <td style={{ textAlign: 'right', fontWeight: 600 }}>{num(qty)}</td>
                      <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(r.value)}</td>
                      <td style={{ textAlign: 'right', color: '#555' }}>{qty > 0 ? idr(Number(r.value) / qty) : dash}</td>
                      <td style={{ textAlign: 'right', color: '#888' }}>{r.lot_count}</td>
                      <td style={{ color: '#888', fontSize: '0.85rem' }}>{fmt(r.oldest_lot_date)}</td>
                    </tr>
                  );
                })}
              </tbody>
              {stock.length > 0 && (
                <tfoot>
                  <tr>
                    <td style={{ fontWeight: 600, color: '#555' }}>Total</td>
                    <td style={{ textAlign: 'right', fontWeight: 700 }}>{num(totals.onHandQty)}</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: '#27ae60' }}>{idr(totals.onHandValue)}</td>
                    <td colSpan={3}></td>
                  </tr>
                </tfoot>
              )}
            </table>
          </Card>

          <Card title="Ringkasan Bulanan">
            <table>
              <thead>
                <tr>
                  <th>Bulan</th>
                  <th style={{ textAlign: 'right' }}>Masuk</th>
                  <th style={{ textAlign: 'right' }}>Keluar</th>
                  <th style={{ textAlign: 'right' }}>Netto</th>
                  <th style={{ textAlign: 'right' }}>Nilai Masuk</th>
                  <th style={{ textAlign: 'right' }}>Nilai Keluar</th>
                  <th style={{ textAlign: 'right' }}>Pergerakan</th>
                </tr>
              </thead>
              <tbody>
                {monthly.length === 0 ? (
                  <Empty cols={7}>Belum ada pergerakan stok tercatat.</Empty>
                ) : monthly.map((r) => {
                  const net = Number(r.qty_in ?? 0) - Number(r.qty_out ?? 0);
                  return (
                    <tr key={r.month}>
                      <td style={{ fontWeight: 500 }}>{monthLabel(r.month)}</td>
                      <td style={{ textAlign: 'right', color: '#27ae60', fontWeight: 600 }}>{num(r.qty_in)}</td>
                      <td style={{ textAlign: 'right', color: '#e74c3c', fontWeight: 600 }}>{num(r.qty_out)}</td>
                      <td style={{ textAlign: 'right', fontWeight: 700, color: net >= 0 ? '#27ae60' : '#e74c3c' }}>
                        {net > 0 ? '+' : ''}{num(net)}
                      </td>
                      <td style={{ textAlign: 'right', color: '#555' }}>{idr(r.value_in)}</td>
                      <td style={{ textAlign: 'right', color: '#555' }}>{idr(r.value_out)}</td>
                      <td style={{ textAlign: 'right', color: '#888' }}>{r.movement_count}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </Card>

          <Card title="Ringkasan per Jenis Transaksi">
            <table>
              <thead>
                <tr>
                  <th>Jenis</th>
                  <th style={{ textAlign: 'right' }}>Masuk</th>
                  <th style={{ textAlign: 'right' }}>Keluar</th>
                  <th style={{ textAlign: 'right' }}>Nilai Netto</th>
                  <th style={{ textAlign: 'right' }}>Pergerakan</th>
                </tr>
              </thead>
              <tbody>
                {byType.length === 0 ? (
                  <Empty cols={5}>Belum ada pergerakan stok tercatat.</Empty>
                ) : byType.map((r) => (
                  <tr key={r.type}>
                    <td><Badge label={typeLabel(r.type)} style={typeStyle(r.type)} /></td>
                    <td style={{ textAlign: 'right', color: '#27ae60', fontWeight: 600 }}>{num(r.qty_in)}</td>
                    <td style={{ textAlign: 'right', color: '#e74c3c', fontWeight: 600 }}>{num(r.qty_out)}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600, color: Number(r.value_net) >= 0 ? '#27ae60' : '#e74c3c' }}>
                      {idr(r.value_net)}
                    </td>
                    <td style={{ textAlign: 'right', color: '#888' }}>{r.movement_count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        </>
      )}

      {tab === 'pembelian' && (
        <Card title={`Riwayat Pembelian (${purchases.length})`}>
          <table>
            <thead>
              <tr>
                <th>Tanggal</th>
                <th>Invoice</th>
                <th>Vendor</th>
                <th>Gudang</th>
                <th style={{ textAlign: 'right' }}>Qty</th>
                <th>Satuan</th>
                <th style={{ textAlign: 'right' }}>Harga</th>
                <th style={{ textAlign: 'right' }}>Total</th>
                <th>Pembayaran</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {purchases.length === 0 ? (
                <Empty cols={10}>Belum ada pembelian tercatat untuk barang ini.</Empty>
              ) : purchases.map((r) => (
                <tr key={r.id}>
                  <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(r.date)}</td>
                  <td style={{ fontWeight: 600 }}>{r.invoice_number}</td>
                  <td style={{ color: '#555' }}>{r.vendor_name ?? dash}</td>
                  <td style={{ color: '#555' }}>{r.warehouse_name ?? dash}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{num(r.quantity)}</td>
                  <td style={{ color: '#666' }}>{r.unit_name ?? dash}</td>
                  <td style={{ textAlign: 'right', color: '#555' }}>{idr(r.price)}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(r.line_total)}</td>
                  <td>
                    <Badge
                      label={PAYMENT_LABEL[r.payment_status] ?? r.payment_status}
                      style={PAYMENT_STYLE[r.payment_status] ?? { background: '#eee', color: '#555' }}
                    />
                  </td>
                  <td>
                    <Link to={`/invoices/view/${r.invoice_id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                  </td>
                </tr>
              ))}
            </tbody>
            {purchases.length > 0 && (
              <tfoot>
                <tr>
                  <td colSpan={7} style={{ textAlign: 'right', fontWeight: 600, color: '#555' }}>Total Pembelian:</td>
                  <td style={{ textAlign: 'right', fontWeight: 700, color: '#e74c3c' }}>{idr(totals.spend)}</td>
                  <td colSpan={2}></td>
                </tr>
              </tfoot>
            )}
          </table>
        </Card>
      )}

      {tab === 'harga' && <ItemPriceBreakdown itemId={id} />}

      {tab === 'pemakaian' && (
        <>
          <Card title={`Pemakaian per Cabang / Divisi (${usage.length})`}>
            <table>
              <thead>
                <tr>
                  <th>Cabang</th>
                  <th>Divisi</th>
                  <th style={{ textAlign: 'right' }}>Qty</th>
                  <th>Satuan</th>
                  <th style={{ textAlign: 'right' }}>Jumlah Pengeluaran</th>
                  <th>Terakhir</th>
                </tr>
              </thead>
              <tbody>
                {usage.length === 0 ? (
                  <Empty cols={6}>Belum ada pengeluaran barang ini ke cabang/divisi.</Empty>
                ) : usage.map((r, i) => (
                  <tr key={`${r.branch_name}-${r.division_name}-${r.unit_name}-${i}`}>
                    <td style={{ fontWeight: 500 }}>{r.branch_name ?? dash}</td>
                    <td style={{ color: '#555' }}>{r.division_name ?? dash}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{num(r.quantity)}</td>
                    <td style={{ color: '#666' }}>{r.unit_name}</td>
                    <td style={{ textAlign: 'right', color: '#888' }}>{r.dispatch_count}</td>
                    <td style={{ color: '#888', fontSize: '0.85rem' }}>{fmt(r.last_dispatched_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>

          {/* Per-dispatch detail. These lines used to sit on the Pembelian tab,
              because a dispatch books its cost onto an auto-created expense
              invoice — they belong here, next to the usage they explain. */}
          <Card title={`Riwayat Pengeluaran (${dispatches.length})`}>
            <table>
              <thead>
                <tr>
                  <th>Tanggal</th>
                  <th>Gudang Asal</th>
                  <th>Cabang</th>
                  <th>Divisi</th>
                  <th style={{ textAlign: 'right' }}>Qty</th>
                  <th>Satuan</th>
                  <th style={{ textAlign: 'right' }}>Nilai (HPP)</th>
                  <th>Catatan</th>
                  <th>Status</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {dispatches.length === 0 ? (
                  <Empty cols={10}>Belum ada pengeluaran tercatat untuk barang ini.</Empty>
                ) : dispatches.map((r, i) => (
                  <tr key={`${r.dispatch_id}-${i}`}>
                    <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(r.dispatched_at)}</td>
                    <td><span className="badge">{r.warehouse_name ?? '—'}</span></td>
                    <td style={{ fontWeight: 500 }}>{r.branch_name ?? dash}</td>
                    <td style={{ color: '#555' }}>{r.division_name ?? dash}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600 }}>{num(r.quantity)}</td>
                    <td style={{ color: '#666' }}>{r.unit_name}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600, color: '#8b5cf6' }}>{idr(r.value)}</td>
                    <td style={{ color: '#888', fontSize: '0.85rem' }}>{r.notes || dash}</td>
                    <td>
                      <Badge
                        label={DISPATCH_STATUS_LABEL[r.status] ?? r.status}
                        style={DISPATCH_STATUS_STYLE[r.status] ?? { background: '#eee', color: '#555' }}
                      />
                    </td>
                    <td>
                      <Link to={`/dispatches/${r.dispatch_id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                    </td>
                  </tr>
                ))}
              </tbody>
              {dispatches.length > 0 && (
                <tfoot>
                  <tr>
                    <td colSpan={6} style={{ textAlign: 'right', fontWeight: 600, color: '#555' }}>Total Nilai Pengeluaran:</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: '#8b5cf6' }}>{idr(totals.dispatchValue)}</td>
                    <td colSpan={3}></td>
                  </tr>
                </tfoot>
              )}
            </table>
          </Card>
        </>
      )}

      {tab === 'pergerakan' && (
        <Card
          title={`${filteredMovements.length} pergerakan`}
          right={
            <div className="filters">
              <select value={mvFilter.warehouse_id} onChange={(e) => setFilter({ warehouse_id: e.target.value })}>
                <option value="">Semua Gudang</option>
                {warehouseOptions.map(([wid, name]) => <option key={wid} value={wid}>{name}</option>)}
              </select>
              <select value={mvFilter.type} onChange={(e) => setFilter({ type: e.target.value })}>
                <option value="">Semua Jenis</option>
                {typeOptions.map((t) => <option key={t} value={t}>{typeLabel(t)}</option>)}
              </select>
              <select value={mvFilter.direction} onChange={(e) => setFilter({ direction: e.target.value })}>
                <option value="">Masuk & Keluar</option>
                <option value="in">Hanya Masuk</option>
                <option value="out">Hanya Keluar</option>
              </select>
            </div>
          }
        >
          <table>
            <thead>
              <tr>
                <th>Tanggal</th>
                <th>Dicatat</th>
                <th>Jenis</th>
                <th style={{ textAlign: 'right' }}>Perubahan</th>
                <th style={{ textAlign: 'right' }}>Saldo</th>
                <th>Satuan</th>
                <th style={{ textAlign: 'right' }}>Nilai</th>
                <th>Gudang</th>
                <th>Tujuan</th>
                <th>Vendor</th>
                <th>Referensi</th>
              </tr>
            </thead>
            <tbody>
              {pagedMovements.length === 0 ? (
                <Empty cols={11}>Tidak ada pergerakan ditemukan.</Empty>
              ) : pagedMovements.map((m) => {
                const qty   = Number(m.quantity_change);
                const val   = m.value == null ? null : Number(m.value);
                const path  = sourcePath(m.source_type, m.source_id);
                return (
                  <tr key={m.id}>
                    <td style={{ fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(m.date)}</td>
                    <td style={{ color: '#aaa', fontSize: '0.78rem', whiteSpace: 'nowrap' }}>{fmtTime(m.created_at)}</td>
                    <td><Badge label={typeLabel(m.type)} style={typeStyle(m.type)} /></td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: qty >= 0 ? '#27ae60' : '#e74c3c', whiteSpace: 'nowrap' }}>
                      {qty > 0 ? '+' : ''}{num(qty)}
                    </td>
                    <td style={{ textAlign: 'right', fontWeight: 600, color: '#555', whiteSpace: 'nowrap' }}>
                      {m.balance_after == null ? dash : num(m.balance_after)}
                    </td>
                    <td style={{ color: '#666' }}>{m.unit_name}</td>
                    <td style={{ textAlign: 'right', fontWeight: 600, whiteSpace: 'nowrap', color: val == null ? '#ccc' : val >= 0 ? '#27ae60' : '#e74c3c' }}>
                      {val == null ? '—' : (val >= 0 ? '+' : '-') + idr(Math.abs(val))}
                    </td>
                    <td><span className="badge">{m.warehouse_name}</span></td>
                    <td style={{ color: '#555', fontSize: '0.85rem' }}>{m.destination ?? dash}</td>
                    <td style={{ color: '#555' }}>{m.vendor ?? dash}</td>
                    <td style={{ fontSize: '0.85rem' }}>
                      {path
                        ? <Link to={path} style={{ color: '#4f8ef7', textDecoration: 'none', fontWeight: 500 }}>{m.reference ?? 'Lihat'}</Link>
                        : <span style={{ color: m.reference ? '#888' : '#ccc' }}>{m.reference ?? '—'}</span>}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          {filteredMovements.length > PAGE_SIZE && (
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '1rem', padding: '0.85rem 1rem', flexWrap: 'wrap' }}>
              <span style={{ color: '#888', fontSize: '0.85rem' }}>
                Menampilkan {(currentPage - 1) * PAGE_SIZE + 1}–{Math.min(currentPage * PAGE_SIZE, filteredMovements.length)} dari {filteredMovements.length}
              </span>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <button type="button" className="btn btn-secondary btn-sm" disabled={currentPage === 1} onClick={() => setMvPage(1)}>«</button>
                <button type="button" className="btn btn-secondary btn-sm" disabled={currentPage === 1} onClick={() => setMvPage((p) => Math.max(1, p - 1))}>‹ Sebelumnya</button>
                <span style={{ color: '#555', fontSize: '0.85rem' }}>Hal. {currentPage} / {pageCount}</span>
                <button type="button" className="btn btn-secondary btn-sm" disabled={currentPage === pageCount} onClick={() => setMvPage((p) => Math.min(pageCount, p + 1))}>Berikutnya ›</button>
                <button type="button" className="btn btn-secondary btn-sm" disabled={currentPage === pageCount} onClick={() => setMvPage(pageCount)}>»</button>
              </div>
            </div>
          )}
        </Card>
      )}
    </>
  );
}
