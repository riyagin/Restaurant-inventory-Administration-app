import { Fragment, useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { getInventory, getInventoryCount, getWarehouses, deleteInventoryRecord, getLotHistory } from '../api';

const PAGE_SIZES = [25, 50, 100, 200];

const SOURCE_PATH = {
  invoice:  (id) => `/invoices/view/${id}`,
  transfer: (id) => `/transfers/group/${id}`,
  dispatch: (id) => `/dispatches/${id}`,
  opname:   (id) => `/stock-opname/detail/${id}`,
};

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID') : '—';

const TYPE_LABEL = {
  invoice: 'Invoice',
  manual_in: 'Manual In',
  manual_out: 'Manual Out',
  manual_adjustment: 'Adjustment',
  pemakaian: 'Pemakaian',
  SO: 'SO',
};

const TYPE_STYLE = {
  invoice:           { background: '#e8f0fe', color: '#4f8ef7' },
  manual_in:         { background: '#e6f9f0', color: '#27ae60' },
  manual_out:        { background: '#fdecea', color: '#e74c3c' },
  manual_adjustment: { background: '#fef9e7', color: '#e67e22' },
  pemakaian:         { background: '#f3e8ff', color: '#8b5cf6' },
  SO:                { background: '#fff3e0', color: '#f57c00' },
};

const SOURCE_LABEL = {
  dispatch: 'Pengiriman',
  stock_transfer: 'Transfer gudang',
  production: 'Produksi',
  stock_opname: 'Stok opname',
  enumeration: 'Pencacahan',
  invoice: 'Koreksi faktur',
  daily_purchase: 'Koreksi pembelanjaan',
};

// One lot's life, not the item's.
//
// The old panel showed every movement of the item in that warehouse, so the
// deliveries either side of this one were mixed into the same list and you could
// not tell which usage came out of which batch. This shows the arrival, the
// usages that actually consumed *this* lot, and whatever is still on the shelf.
function LotHistoryPanel({ lotId }) {
  const [data, setData] = useState(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let alive = true;
    getLotHistory(lotId)
      .then(r => { if (alive) setData(r.data); })
      .catch(() => { if (alive) setFailed(true); });
    return () => { alive = false; };
  }, [lotId]);

  const wrap = (children) => (
    <td colSpan={7} style={{padding:'0.75rem 1.5rem 1rem',background:'#f8f9ff',borderTop:'none'}}>
      {children}
    </td>
  );

  if (failed) return wrap(<span style={{color:'#e74c3c',fontSize:'0.85rem'}}>Gagal memuat riwayat lot</span>);
  if (!data) return wrap(<span style={{color:'#999',fontSize:'0.85rem'}}>Memuat riwayat…</span>);

  const { lot, consumptions, traced } = data;
  const opened = Number(lot.quantity) + Number(lot.consumed_quantity);

  return wrap(
    <>
      <div style={{display:'flex',gap:'1.5rem',flexWrap:'wrap',marginBottom:'0.75rem',fontSize:'0.83rem'}}>
        <span><strong>Masuk</strong> {fmt(lot.date)}
          {lot.opening_vendor && <span style={{color:'#777'}}> · {lot.opening_vendor}</span>}
          {lot.opening_reference && <span style={{color:'#777'}}> · {lot.opening_reference}</span>}
        </span>
        <span style={{color:'#555'}}>Jumlah awal <strong>{opened.toLocaleString('id-ID')}</strong></span>
        <span style={{color:'#555'}}>Terpakai <strong>{Number(lot.consumed_quantity).toLocaleString('id-ID')}</strong></span>
        <span style={{color:'#555'}}>Sisa <strong>{Number(lot.quantity).toLocaleString('id-ID')}</strong></span>
        {lot.depleted_at
          ? <span style={{color:'#e74c3c',fontWeight:600}}>Habis {fmt(lot.depleted_at)}</span>
          : <span style={{color:'#27ae60',fontWeight:600}}>Masih ada</span>}
      </div>

      {!traced ? (
        // Lots consumed before per-lot tracing existed were deleted outright, so
        // their usage genuinely cannot be reconstructed. Say that, rather than
        // showing an empty table that reads as "never used".
        <div style={{fontSize:'0.83rem',color:'#888',fontStyle:'italic'}}>
          Lot ini habis sebelum pelacakan per-lot aktif, jadi rincian pemakaiannya tidak tersimpan.
          Riwayat tingkat barang masih lengkap di halaman barang.
        </div>
      ) : consumptions.length === 0 ? (
        <div style={{fontSize:'0.83rem',color:'#999'}}>Belum ada pemakaian dari lot ini.</div>
      ) : (
        <table style={{width:'100%',borderCollapse:'collapse',fontSize:'0.82rem'}}>
          <thead>
            <tr>
              {['Tanggal','Dipakai untuk','Tujuan','Jumlah','Nilai','Referensi'].map(h => (
                <th key={h} style={{textAlign: h === 'Nilai' ? 'right' : 'left',padding:'0.3rem 0.6rem',color:'#888',fontWeight:600,borderBottom:'1px solid #e8e8e8',whiteSpace:'nowrap'}}>
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {consumptions.map(c => {
              const path = c.source_type && c.source_id ? SOURCE_PATH[c.source_type]?.(c.source_id) : null;
              return (
                <tr key={c.id}>
                  <td style={{padding:'0.3rem 0.6rem',color:'#555'}}>{fmt(c.date)}</td>
                  <td style={{padding:'0.3rem 0.6rem'}}>
                    <span style={{display:'inline-block',padding:'0.1rem 0.45rem',borderRadius:'4px',fontSize:'0.75rem',fontWeight:600,
                                  ...(TYPE_STYLE[c.source_type] ?? { background:'#f3e8ff', color:'#8b5cf6' })}}>
                      {SOURCE_LABEL[c.source_type] ?? c.source_type ?? 'Penyesuaian'}
                    </span>
                  </td>
                  <td style={{padding:'0.3rem 0.6rem',color:'#555'}}>{c.destination || '—'}</td>
                  <td style={{padding:'0.3rem 0.6rem',fontWeight:600,color:'#e74c3c'}}>
                    −{Number(c.quantity).toLocaleString('id-ID')}
                  </td>
                  <td style={{padding:'0.3rem 0.6rem',textAlign:'right',fontWeight:600,whiteSpace:'nowrap',color:'#e74c3c'}}>
                    {idr(c.value)}
                  </td>
                  <td style={{padding:'0.3rem 0.6rem'}}>
                    {path ? (
                      <Link to={path} style={{color:'#4f8ef7',textDecoration:'none',fontWeight:500}}>
                        {c.reference || 'Lihat'}
                      </Link>
                    ) : (
                      <span style={{color:'#ccc',fontStyle:'italic'}}>{c.reference || '—'}</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </>
  );
}

// A clickable column heading. Clicking the active column flips direction rather
// than resetting it, so a second click means "same column, other end" — which is
// what people expect and saves a round trip through ascending.
function SortableTh({ label, field, sort, dir, onSort, align }) {
  const active = sort === field;
  return (
    <th
      onClick={() => onSort(field)}
      title={`Urutkan menurut ${label.toLowerCase()}`}
      style={{ cursor:'pointer', userSelect:'none', whiteSpace:'nowrap', textAlign: align || 'left',
               color: active ? '#4f8ef7' : undefined }}
    >
      {label}
      <span style={{ marginLeft:'0.3rem', fontSize:'0.7rem', opacity: active ? 1 : 0.25 }}>
        {active && dir === 'desc' ? '▼' : '▲'}
      </span>
    </th>
  );
}

export default function Inventory() {
  const [records, setRecords] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [search, setSearch] = useState('');
  const [warehouseId, setWarehouseId] = useState('all');
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo, setDateTo] = useState('');
  const [expandedId, setExpandedId] = useState(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZES[0]);
  const [totalLots, setTotalLots] = useState(0);
  // Zero-stock rows are shown by default: the item you are about to run out of
  // is the one worth seeing, and hiding it was the wrong default to have had.
  const [includeEmpty, setIncludeEmpty] = useState(true);
  const [sort, setSort] = useState('item');
  const [dir, setDir] = useState('asc');

  const filterParams = {
    search,
    warehouse_id: warehouseId,
    date_from: dateFrom || undefined,
    date_to: dateTo || undefined,
    include_empty: includeEmpty ? undefined : 'false',
  };

  const load = useCallback(() => {
    getInventory({ ...filterParams, sort, dir, page, limit: pageSize })
      .then(r => setRecords(r.data));
  }, [search, warehouseId, dateFrom, dateTo, includeEmpty, sort, dir, page, pageSize]);

  const loadCount = useCallback(() => {
    getInventoryCount(filterParams).then(r => setTotalLots(r.data.count));
  }, [search, warehouseId, dateFrom, dateTo, includeEmpty]);

  // Clicking the active column flips direction; a different column starts
  // ascending. Either way we return to page 1 — staying on page 7 of a list that
  // was just reordered shows an arbitrary slice.
  const applySort = (field) => {
    if (field === sort) setDir(d => (d === 'asc' ? 'desc' : 'asc'));
    else { setSort(field); setDir('asc'); }
    setPage(1);
  };

  useEffect(() => {
    getWarehouses().then(r => setWarehouses(r.data));
  }, []);

  useEffect(() => { load(); }, [load]);
  useEffect(() => { loadCount(); }, [loadCount]);

  // Filter setters that also reset pagination back to page 1.
  const updateSearch = (v) => { setSearch(v); setPage(1); };
  const updateWarehouseId = (v) => { setWarehouseId(v); setPage(1); };
  const updateDateFrom = (v) => { setDateFrom(v); setPage(1); };
  const updateDateTo = (v) => { setDateTo(v); setPage(1); };
  const updatePageSize = (v) => { setPageSize(v); setPage(1); };

  const clearDates = () => { setDateFrom(''); setDateTo(''); setPage(1); };

  const isFiltered = !!(search || warehouseId !== 'all' || dateFrom || dateTo);
  const totalPages = Math.max(1, Math.ceil(totalLots / pageSize));

  const handleDelete = async (e, id) => {
    e.stopPropagation();
    if (!confirm('Yakin hapus catatan inventaris ini?')) return;
    await deleteInventoryRecord(id);
    setExpandedId(null);
    load();
    loadCount();
  };

  const toggleRow = (id) => setExpandedId(prev => prev === id ? null : id);

  return (
    <>
      <div className="page-header">
        <h1>Inventaris</h1>
        <Link to="/inventory/new" className="btn btn-primary">+ Tambah Catatan</Link>
      </div>

      <div className="card">
        <div className="card-header">
          <div>
            <h2 style={{ marginBottom: '0.2rem' }}>
              {totalLots.toLocaleString('id-ID')} baris{isFiltered ? ' (difilter)' : ''}
            </h2>
            {(dateFrom || dateTo) && (
              <div style={{ fontSize: '0.8rem', color: '#888' }}>
                Menampilkan lot inventaris
                {dateFrom && <> dari <strong>{new Date(dateFrom).toLocaleDateString('id-ID')}</strong></>}
                {dateTo   && <> s/d <strong>{new Date(dateTo).toLocaleDateString('id-ID')}</strong></>}
              </div>
            )}
          </div>
          <div className="filters">
            <input
              placeholder="Cari barang..."
              value={search}
              onChange={e => updateSearch(e.target.value)}
            />
            <select value={warehouseId} onChange={e => updateWarehouseId(e.target.value)}>
              <option value="all">Semua Gudang</option>
              {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
            </select>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
              <label style={{ fontSize: '0.8rem', color: '#888', whiteSpace: 'nowrap' }}>Dari</label>
              <input type="date" value={dateFrom} onChange={e => updateDateFrom(e.target.value)} />
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
              <label style={{ fontSize: '0.8rem', color: '#888', whiteSpace: 'nowrap' }}>s/d</label>
              <input type="date" value={dateTo} onChange={e => updateDateTo(e.target.value)} />
            </div>
            {(dateFrom || dateTo) && (
              <button type="button" onClick={clearDates} className="btn btn-secondary btn-sm">Hapus filter tanggal</button>
            )}
            <label style={{ display:'flex', alignItems:'center', gap:'0.4rem', fontSize:'0.85rem', whiteSpace:'nowrap' }}>
              <input
                type="checkbox"
                checked={includeEmpty}
                onChange={e => { setIncludeEmpty(e.target.checked); setPage(1); }}
              />
              Tampilkan stok kosong
            </label>
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th></th>
              <SortableTh label="Barang"  field="item"      sort={sort} dir={dir} onSort={applySort} />
              <SortableTh label="Kode"    field="code"      sort={sort} dir={dir} onSort={applySort} />
              <SortableTh label="Jumlah"  field="quantity"  sort={sort} dir={dir} onSort={applySort} />
              <SortableTh label="Gudang"  field="warehouse" sort={sort} dir={dir} onSort={applySort} />
              <SortableTh label="Tanggal" field="date"      sort={sort} dir={dir} onSort={applySort} />
              <th></th>
            </tr>
          </thead>
          <tbody>
            {records.length === 0 ? (
              <tr><td colSpan={7} style={{textAlign:'center',color:'#999',padding:'2rem'}}>Tidak ada data</td></tr>
            ) : records.map(rec => {
              // A row with no lot id is an item we stock and have none of. There
              // is nothing to expand, edit, delete or trace — the row exists to
              // report the absence.
              const isEmptyItem = !rec.id;
              const isDepleted = !isEmptyItem && Number(rec.quantity) <= 0;
              const rowKey = rec.id || `empty-${rec.item_id}`;
              return (
                <Fragment key={rowKey}>
                  <tr
                    onClick={() => !isEmptyItem && toggleRow(rec.id)}
                    style={{cursor: isEmptyItem ? 'default' : 'pointer', opacity: isEmptyItem ? 0.68 : 1}}
                    className={expandedId === rec.id ? 'row-expanded' : ''}
                  >
                    <td style={{width:'28px',color:'#aaa',fontSize:'0.75rem',userSelect:'none'}}>
                      {isEmptyItem ? '' : expandedId === rec.id ? '▼' : '▶'}
                    </td>
                    <td>{rec.item_name}</td>
                    <td style={{color:'#888',fontSize:'0.85rem'}}>{rec.item_code}</td>
                    <td>
                      {isEmptyItem ? (
                        <span className="badge" style={{background:'#f2f2f2',color:'#999'}}>Stok kosong</span>
                      ) : (
                        <span className="badge" style={isDepleted ? {background:'#fdecea',color:'#e74c3c'} : undefined}>
                          {Number(rec.quantity).toLocaleString('id-ID')} {rec.unit_name}
                          {isDepleted && ' · habis'}
                        </span>
                      )}
                    </td>
                    <td>
                      {rec.warehouse_name
                        ? <span className="badge">{rec.warehouse_name}</span>
                        : <span style={{color:'#bbb',fontSize:'0.85rem'}}>—</span>}
                    </td>
                    <td style={{color:'#888',fontSize:'0.85rem'}}>{fmt(rec.date)}</td>
                    <td onClick={e => e.stopPropagation()}>
                      <div className="actions">
                        {isEmptyItem ? (
                          <Link to={`/items/stock/${rec.item_id}`} className="btn btn-secondary btn-sm">Lihat Barang</Link>
                        ) : (
                          <>
                            <Link to={`/inventory/history/${rec.item_id}`} className="btn btn-secondary btn-sm">Riwayat Barang</Link>
                            <Link to={`/inventory/edit/${rec.id}`} className="btn btn-secondary btn-sm">Edit</Link>
                            <button onClick={(e) => handleDelete(e, rec.id)} className="btn btn-danger btn-sm">Hapus</button>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                  {!isEmptyItem && expandedId === rec.id && (
                    <tr style={{background:'#f8f9ff'}}>
                      <LotHistoryPanel lotId={rec.id} />
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>

        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '0.75rem 0 0', flexWrap: 'wrap', gap: '0.6rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
            <label style={{ fontSize: '0.8rem', color: '#888' }}>Tampilkan</label>
            <select value={pageSize} onChange={e => updatePageSize(Number(e.target.value))}>
              {PAGE_SIZES.map(size => <option key={size} value={size}>{size}</option>)}
            </select>
            <span style={{ fontSize: '0.8rem', color: '#888' }}>per halaman</span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              disabled={page <= 1}
              onClick={() => setPage(p => Math.max(1, p - 1))}
            >
              Sebelumnya
            </button>
            <span style={{ fontSize: '0.8rem', color: '#888' }}>Halaman {page} dari {totalPages}</span>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              disabled={page >= totalPages}
              onClick={() => setPage(p => Math.min(totalPages, p + 1))}
            >
              Berikutnya
            </button>
          </div>
        </div>
      </div>
    </>
  );
}
