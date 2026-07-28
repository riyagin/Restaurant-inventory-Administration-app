import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { getItemPriceHistory } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(v) || 0);
const num = (v) => Number(v ?? 0).toLocaleString('id-ID', { maximumFractionDigits: 3 });
const fmt = (d) => (d ? new Date(d).toLocaleDateString('id-ID', { dateStyle: 'medium' }) : '—');
const monthLabel = (m) => {
  if (!m) return '—';
  const [y, mo] = m.split('-');
  return new Date(Number(y), Number(mo) - 1, 1).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' });
};

const dash = <span style={{ color: '#ccc' }}>—</span>;
const unitKey = (r) => (r.unit_index ?? 0) + '|' + (r.unit_name ?? '—');

const avgPrice = (r) => {
  const qty = Number(r.total_quantity ?? 0);
  return qty > 0 ? Number(r.total_spend ?? 0) / qty : null;
};

// Percentage move between two prices, rendered as a coloured chip. Cheaper is
// good news here — these are purchase prices, not sales.
function Delta({ from, to }) {
  if (!from || !to || from === to) return dash;
  const pct = ((to - from) / from) * 100;
  const up = pct > 0;
  return (
    <span style={{
      display: 'inline-block', padding: '0.1rem 0.4rem', borderRadius: '4px', fontSize: '0.75rem', fontWeight: 700,
      background: up ? '#fdecea' : '#e6f9f0', color: up ? '#e74c3c' : '#27ae60',
    }}>
      {up ? '▲' : '▼'} {Math.abs(pct).toFixed(1)}%
    </span>
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

const Empty = ({ children, cols }) => (
  <tr><td colSpan={cols} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>{children}</td></tr>
);

export default function ItemPriceBreakdown({ itemId }) {
  const [data, setData]       = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError]     = useState('');
  const [unit, setUnit]       = useState('');

  useEffect(() => {
    setLoading(true);
    setError('');
    getItemPriceHistory(itemId)
      .then((r) => setData(r.data))
      .catch((err) => setError(err.response?.data?.error || 'Gagal memuat riwayat harga.'))
      .finally(() => setLoading(false));
  }, [itemId]);

  const byUnit = data?.by_unit ?? [];

  // Prices of different units are not comparable, so the whole panel is scoped
  // to one unit at a time. Default to the unit with the largest spend.
  const activeUnit = unit || (byUnit.length ? unitKey(byUnit[0]) : '');
  const summary    = byUnit.find((r) => unitKey(r) === activeUnit) ?? null;

  const vendors = useMemo(
    () => (data?.by_vendor ?? []).filter((r) => unitKey(r) === activeUnit),
    [data, activeUnit],
  );

  const months = useMemo(
    () => (data?.monthly ?? []).filter((r) => unitKey(r) === activeUnit),
    [data, activeUnit],
  );

  // Every purchase line for this unit, oldest first, so each row can be shown
  // against the price actually paid the time before.
  const lines = useMemo(() => {
    const rows = (data?.purchases ?? [])
      .filter((r) => (r.unit_index ?? 0) + '|' + (r.unit_name ?? '—') === activeUnit)
      .slice()
      .reverse();
    return rows.map((r, i) => ({
      ...r,
      prev_price:  i > 0 ? Number(rows[i - 1].price) : null,
      prev_vendor: i > 0 ? (rows[i - 1].vendor_name ?? null) : null,
    })).reverse();
  }, [data, activeUnit]);

  const cheapest = useMemo(() => {
    const withPrice = vendors.filter((v) => Number(v.last_price) > 0);
    if (!withPrice.length) return null;
    return withPrice.reduce((best, v) => (Number(v.last_price) < Number(best.last_price) ? v : best));
  }, [vendors]);

  if (loading) return <div className="card" style={{ padding: '2rem', color: '#999' }}>Memuat riwayat harga…</div>;
  if (error)   return <div className="card" style={{ padding: '2rem', color: '#e74c3c' }}>{error}</div>;
  if (!summary) {
    return <div className="card" style={{ padding: '2rem', color: '#999', textAlign: 'center' }}>Belum ada pembelian tercatat untuk barang ini.</div>;
  }

  const unitName = summary.unit_name ?? 'satuan';
  const avg      = avgPrice(summary);
  const last     = Number(summary.last_price);
  const first    = Number(summary.first_price);

  return (
    <>
      {byUnit.length > 1 && (
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '1rem', flexWrap: 'wrap' }}>
          <span style={{ fontSize: '0.8rem', color: '#888' }}>Satuan pembelian:</span>
          {byUnit.map((u) => (
            <button
              key={unitKey(u)}
              type="button"
              onClick={() => setUnit(unitKey(u))}
              className={unitKey(u) === activeUnit ? 'btn btn-primary btn-sm' : 'btn btn-secondary btn-sm'}
            >
              {u.unit_name ?? '—'} ({u.purchase_count})
            </button>
          ))}
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', gap: '1rem', marginBottom: '1.25rem' }}>
        <Stat
          label={`Harga Terakhir / ${unitName}`}
          value={idr(last)}
          sub={fmt(summary.last_purchase_date)}
          color="#333"
        />
        <Stat
          label={`Harga Rata-rata / ${unitName}`}
          value={avg == null ? '—' : idr(avg)}
          sub={`${summary.purchase_count} pembelian · ${num(summary.total_quantity)} ${unitName}`}
          color="#4f8ef7"
        />
        <Stat label="Harga Terendah" value={idr(summary.min_price)} color="#27ae60" />
        <Stat label="Harga Tertinggi" value={idr(summary.max_price)} color="#e74c3c" />
        <Stat
          label="Perubahan Sejak Awal"
          value={first > 0 && last > 0 ? <Delta from={first} to={last} /> : '—'}
          sub={first > 0 ? `dari ${idr(first)} (${fmt(summary.first_purchase_date)})` : undefined}
        />
        <Stat label="Total Pembelian" value={idr(summary.total_spend)} sub={`${summary.vendor_count} vendor`} color="#8b5cf6" />
      </div>

      <Card
        title={`Harga per Vendor (${vendors.length})`}
        right={cheapest && vendors.length > 1 && (
          <span style={{ fontSize: '0.8rem', color: '#27ae60', fontWeight: 600 }}>
            Termurah terakhir: {cheapest.vendor_name ?? 'Tanpa Vendor'} · {idr(cheapest.last_price)}
          </span>
        )}
      >
        <table>
          <thead>
            <tr>
              <th>Vendor</th>
              <th style={{ textAlign: 'right' }}>Harga Terakhir</th>
              <th>Tgl Terakhir</th>
              <th style={{ textAlign: 'right' }}>Harga Rata-rata</th>
              <th style={{ textAlign: 'right' }}>Terendah</th>
              <th style={{ textAlign: 'right' }}>Tertinggi</th>
              <th style={{ textAlign: 'right' }}>Qty</th>
              <th style={{ textAlign: 'right' }}>Total</th>
              <th style={{ textAlign: 'right' }}>Beli</th>
            </tr>
          </thead>
          <tbody>
            {vendors.length === 0 ? (
              <Empty cols={9}>Belum ada pembelian pada satuan ini.</Empty>
            ) : vendors.map((v) => {
              const vAvg    = avgPrice(v);
              const isBest  = cheapest && v === cheapest && vendors.length > 1;
              return (
                <tr key={(v.vendor_id ?? 'none') + unitKey(v)} style={isBest ? { background: '#f4fdf8' } : undefined}>
                  <td style={{ fontWeight: 600 }}>
                    {v.vendor_id
                      ? <Link to={`/vendors/${v.vendor_id}/history`} style={{ color: '#4f8ef7', textDecoration: 'none' }}>{v.vendor_name}</Link>
                      : <span style={{ color: '#aaa', fontStyle: 'italic' }}>Tanpa Vendor</span>}
                    {isBest && (
                      <span style={{ marginLeft: '0.4rem', fontSize: '0.68rem', background: '#e6f9f0', color: '#27ae60', borderRadius: '3px', padding: '0.05rem 0.3rem', fontWeight: 700 }}>
                        TERMURAH
                      </span>
                    )}
                  </td>
                  <td style={{ textAlign: 'right', fontWeight: 700 }}>{idr(v.last_price)}</td>
                  <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(v.last_purchase_date)}</td>
                  <td style={{ textAlign: 'right', color: '#555' }}>{vAvg == null ? dash : idr(vAvg)}</td>
                  <td style={{ textAlign: 'right', color: '#27ae60' }}>{idr(v.min_price)}</td>
                  <td style={{ textAlign: 'right', color: '#e74c3c' }}>{idr(v.max_price)}</td>
                  <td style={{ textAlign: 'right' }}>{num(v.total_quantity)}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(v.total_spend)}</td>
                  <td style={{ textAlign: 'right', color: '#888' }}>{v.purchase_count}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </Card>

      <Card title={`Tren Harga Bulanan (${months.length})`}>
        <table>
          <thead>
            <tr>
              <th>Bulan</th>
              <th style={{ textAlign: 'right' }}>Harga Rata-rata</th>
              <th style={{ textAlign: 'right' }}>vs Bulan Sebelumnya</th>
              <th style={{ textAlign: 'right' }}>Terendah</th>
              <th style={{ textAlign: 'right' }}>Tertinggi</th>
              <th style={{ textAlign: 'right' }}>Qty</th>
              <th style={{ textAlign: 'right' }}>Nilai</th>
              <th style={{ textAlign: 'right' }}>Beli</th>
            </tr>
          </thead>
          <tbody>
            {months.length === 0 ? (
              <Empty cols={8}>Belum ada pembelian pada satuan ini.</Empty>
            ) : months.map((m, i) => {
              const mAvg  = avgPrice(m);
              // Rows come newest first, so the previous month is the next row.
              const prev  = months[i + 1] ? avgPrice(months[i + 1]) : null;
              return (
                <tr key={m.month}>
                  <td style={{ fontWeight: 500 }}>{monthLabel(m.month)}</td>
                  <td style={{ textAlign: 'right', fontWeight: 700 }}>{mAvg == null ? dash : idr(mAvg)}</td>
                  <td style={{ textAlign: 'right' }}>{prev && mAvg ? <Delta from={prev} to={mAvg} /> : dash}</td>
                  <td style={{ textAlign: 'right', color: '#27ae60' }}>{idr(m.min_price)}</td>
                  <td style={{ textAlign: 'right', color: '#e74c3c' }}>{idr(m.max_price)}</td>
                  <td style={{ textAlign: 'right' }}>{num(m.total_quantity)}</td>
                  <td style={{ textAlign: 'right', color: '#555' }}>{idr(m.total_spend)}</td>
                  <td style={{ textAlign: 'right', color: '#888' }}>{m.purchase_count}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </Card>

      <Card title={`Riwayat Harga per Pembelian (${lines.length})`}>
        <table>
          <thead>
            <tr>
              <th>Tanggal</th>
              <th>Invoice</th>
              <th>Vendor</th>
              <th style={{ textAlign: 'right' }}>Harga / {unitName}</th>
              <th style={{ textAlign: 'right' }}>vs Pembelian Sebelumnya</th>
              <th style={{ textAlign: 'right' }}>Qty</th>
              <th style={{ textAlign: 'right' }}>Total</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {lines.length === 0 ? (
              <Empty cols={8}>Belum ada pembelian pada satuan ini.</Empty>
            ) : lines.map((r) => {
              const switched = r.prev_vendor && (r.vendor_name ?? null) !== r.prev_vendor;
              return (
                <tr key={r.id}>
                  <td style={{ color: '#888', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{fmt(r.date)}</td>
                  <td style={{ fontWeight: 600 }}>{r.invoice_number}</td>
                  <td style={{ color: '#555' }}>
                    {r.vendor_name ?? dash}
                    {switched && (
                      <span style={{ marginLeft: '0.4rem', fontSize: '0.68rem', background: '#fff3e0', color: '#f57c00', borderRadius: '3px', padding: '0.05rem 0.3rem', fontWeight: 700 }}>
                        GANTI VENDOR
                      </span>
                    )}
                  </td>
                  <td style={{ textAlign: 'right', fontWeight: 700 }}>{idr(r.price)}</td>
                  <td style={{ textAlign: 'right' }}>
                    {r.prev_price ? <Delta from={r.prev_price} to={Number(r.price)} /> : dash}
                  </td>
                  <td style={{ textAlign: 'right' }}>{num(r.quantity)}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{idr(r.line_total)}</td>
                  <td>
                    <Link to={`/invoices/view/${r.invoice_id}`} className="btn btn-secondary btn-sm">Lihat</Link>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </Card>
    </>
  );
}
