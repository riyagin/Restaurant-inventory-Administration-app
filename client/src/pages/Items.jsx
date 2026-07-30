import { useEffect, useState, useCallback, useRef } from 'react';
import { Link } from 'react-router-dom';
import * as XLSX from 'xlsx';
import { getItems, deleteItem, createItem, updateItem } from '../api';

// Stock and non-stock items have different history pages: stock items get
// warehouse balances + movements, non-stock items only ever have invoice lines.
const historyPath = (item) =>
  item.is_stock === false ? `/items/history/${item.id}` : `/items/stock/${item.id}`;

function UnitChain({ units }) {
  return (
    <span style={{fontSize:'0.85rem'}}>
      {units.map((u, i) => (
        <span key={i}>
          {i > 0 && (
            <span style={{color:'#aaa',margin:'0 4px'}}>
              → <span style={{color:'#888',fontSize:'0.75rem'}}>×{u.perPrev}</span>
            </span>
          )}
          <span className="badge">{u.name}</span>
        </span>
      ))}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Excel helpers
// ---------------------------------------------------------------------------

// One header row shared by the template, the export and the import parser, so a
// list exported here can be edited and fed straight back in. The two trailing
// "(info)" columns are written by the export for context — the parser ignores
// them: the smallest unit is derived from the Satuan columns and current stock
// is not something an import may set.
const HEADERS = [
  'Nama Barang', 'Kode', 'Tipe',
  'Satuan 1', 'Satuan 2', 'Isi per Satuan 1', 'Satuan 3', 'Isi per Satuan 2',
  'Stok Minimum',
];
const INFO_HEADERS = ['Satuan Terkecil (info)', 'Stok Saat Ini (info)'];

const SAMPLE_ROWS = [
  HEADERS,
  ['Cat Tembok Putih', 'CTW-001', 'Stok', 'Kaleng', 'Liter', 4, '', '', 20],
  ['Semen Portland', 'SEM-001', 'Stok', 'Sak', '', '', '', '', 50],
  ['Kuas Cat 4"', 'KCS-004', 'Stok', 'Lusin', 'Pcs', 12, '', '', 0],
  ['Amplas Halus', 'AMP-H', 'Stok', 'Roll', 'Lembar', 20, '', '', 100],
  ['Tiner A Special', 'TNR-AS', 'Stok', 'Drum', 'Galon', 5, 'Liter', 4, 40],
  ['Sarung Tangan', 'SGT-001', 'Non-Stok', 'Kotak', 'Pcs', 10, '', '', ''],
];

const qty = (n) => Number(n || 0).toLocaleString('id-ID', { maximumFractionDigits: 4 });

const baseUnitName = (units) => units?.[units.length - 1]?.name || '';

// Identity of a unit chain, for telling "the sheet re-states what we already
// have" apart from "the sheet changes the units". Names are compared
// case-insensitively; a ratio change is a real change.
const unitsSignature = (units) =>
  (units || [])
    .map((u, i) => `${String(u.name || '').trim().toLowerCase()}:${i === 0 ? '' : Number(u.perPrev) || ''}`)
    .join(' > ');

const HEADER_STYLE = {
  font: { bold: true },
  fill: { fgColor: { rgb: 'D9E1F2' }, patternType: 'solid' },
  alignment: { horizontal: 'center', vertical: 'center' },
  border: { top: { style: 'thin' }, bottom: { style: 'thin' }, left: { style: 'thin' }, right: { style: 'thin' } },
};
const CELL_STYLE = {
  border: { top: { style: 'thin' }, bottom: { style: 'thin' }, left: { style: 'thin' }, right: { style: 'thin' } },
};

const COL_WIDTHS = [34, 14, 10, 14, 14, 16, 14, 16, 14, 18, 16];

function writeSheet(rows, filename) {
  const ws = XLSX.utils.aoa_to_sheet(rows);
  ws['!cols'] = COL_WIDTHS.slice(0, rows[0].length).map(wch => ({ wch }));
  ws['!rows'] = [{ hpt: 22 }, ...Array(Math.max(rows.length - 1, 0)).fill({ hpt: 20 })];
  ws['!freeze'] = { xSplit: 0, ySplit: 1 };

  for (let r = 0; r < rows.length; r++) {
    rows[0].forEach((_, c) => {
      const addr = XLSX.utils.encode_cell({ r, c });
      if (ws[addr]) ws[addr].s = r === 0 ? HEADER_STYLE : CELL_STYLE;
    });
  }

  const wb = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(wb, ws, 'Barang');
  XLSX.writeFile(wb, filename, { cellStyles: true });
}

function downloadSampleExcel() {
  writeSheet(SAMPLE_ROWS, 'template-import-barang.xlsx');
}

// Export the current list in exactly the shape the import reads back, so
// setting minimum stock in bulk is: export → fill the "Stok Minimum" column →
// import. Stock items only carry a threshold, so non-stock rows leave it blank.
function exportItemsExcel(items) {
  const rows = [
    [...HEADERS, ...INFO_HEADERS],
    ...items.map(it => {
      const u = it.units || [];
      return [
        it.name,
        it.code,
        it.is_stock === false ? 'Non-Stok' : 'Stok',
        u[0]?.name ?? '',
        u[1]?.name ?? '',
        u[1] ? Number(u[1].perPrev) || '' : '',
        u[2]?.name ?? '',
        u[2] ? Number(u[2].perPrev) || '' : '',
        it.is_stock === false ? '' : Number(it.min_stock) || 0,
        it.is_stock === false ? '' : baseUnitName(u),
        it.is_stock === false ? '' : Number(it.stock_quantity) || 0,
      ];
    }),
  ];
  const stamp = new Date().toISOString().slice(0, 10);
  writeSheet(rows, `daftar-barang-${stamp}.xlsx`);
}

// Parse a worksheet row into an item payload, or return an error string
function parseRow(row, rowNum) {
  const name    = String(row['Nama Barang'] ?? '').trim();
  const code    = String(row['Kode'] ?? '').trim();
  const tipe    = String(row['Tipe'] ?? 'Stok').trim().toLowerCase();
  const unit1   = String(row['Satuan 1'] ?? '').trim();
  const unit2   = String(row['Satuan 2'] ?? '').trim();
  const perS1   = row['Isi per Satuan 1'];
  const unit3   = String(row['Satuan 3'] ?? '').trim();
  const perS2   = row['Isi per Satuan 2'];
  const minRaw  = row['Stok Minimum'];

  if (!name) return { error: `Baris ${rowNum}: Nama Barang kosong` };
  if (!code) return { error: `Baris ${rowNum}: Kode kosong` };
  if (!unit1) return { error: `Baris ${rowNum}: Satuan 1 kosong` };

  const units = [{ name: unit1, perPrev: null }];
  if (unit2) {
    const ratio = Number(perS1);
    if (!perS1 || isNaN(ratio) || ratio <= 0)
      return { error: `Baris ${rowNum}: "Isi per Satuan 1" harus angka positif jika Satuan 2 diisi` };
    units.push({ name: unit2, perPrev: ratio });
  }
  if (unit3) {
    if (!unit2) return { error: `Baris ${rowNum}: Satuan 2 harus diisi sebelum Satuan 3` };
    const ratio = Number(perS2);
    if (!perS2 || isNaN(ratio) || ratio <= 0)
      return { error: `Baris ${rowNum}: "Isi per Satuan 2" harus angka positif jika Satuan 3 diisi` };
    units.push({ name: unit3, perPrev: ratio });
  }

  const isStock = tipe !== 'non-stok';
  let minStock = 0;
  if (minRaw !== '' && minRaw != null) {
    const parsed = Number(String(minRaw).replace(/\./g, '').replace(',', '.'));
    if (isNaN(parsed) || parsed < 0)
      return { error: `Baris ${rowNum}: "Stok Minimum" harus angka tidak negatif` };
    minStock = parsed;
  }
  // A non-stock item is never counted in inventory, so a threshold on it would
  // never be checked against anything.
  if (!isStock) minStock = 0;

  return { item: { name, code, units, is_stock: isStock, min_stock: minStock } };
}

// What an imported row would change on the item it matched, in words. Empty
// means the sheet restates what is already stored, so the row is skipped rather
// than pushed through an update that would only churn the activity log.
function diffAgainst(existing, incoming) {
  const changes = [];
  if (existing.name !== incoming.name) changes.push('nama');
  if ((existing.is_stock !== false) !== incoming.is_stock) changes.push('tipe');
  if (unitsSignature(existing.units) !== unitsSignature(incoming.units)) changes.push('satuan');
  if ((Number(existing.min_stock) || 0) !== incoming.min_stock) changes.push('stok minimum');
  return changes;
}

// ---------------------------------------------------------------------------
// Import modal
// ---------------------------------------------------------------------------

function ImportModal({ onClose, onDone }) {
  const fileRef = useRef();
  // { item, existing, action: 'create'|'update'|'skip', changes, status: 'pending'|'ok'|'error'|'skip', msg }
  const [rows, setRows] = useState([]);
  const [catalog, setCatalog] = useState(null);   // every item, for matching by code
  const [importing, setImporting] = useState(false);
  const [done, setDone] = useState(false);

  // Matching is done against the whole catalogue, not the filtered list behind
  // the modal — otherwise a search in the page would silently turn updates into
  // duplicate-code creates.
  useEffect(() => { getItems({}).then(r => setCatalog(r.data)); }, []);

  const handleFile = (e) => {
    const file = e.target.files[0];
    if (!file) return;
    const byCode = new Map((catalog || []).map(it => [it.code.trim().toLowerCase(), it]));
    const reader = new FileReader();
    reader.onload = (ev) => {
      const wb = XLSX.read(ev.target.result, { type: 'array' });
      const ws = wb.Sheets[wb.SheetNames[0]];
      const data = XLSX.utils.sheet_to_json(ws, { defval: '' });
      const parsed = data.map((row, i) => {
        const result = parseRow(row, i + 2);
        if (result.error) return { error: result.error, status: 'error', msg: result.error };

        const existing = byCode.get(result.item.code.toLowerCase());
        if (!existing) return { item: result.item, action: 'create', status: 'pending', msg: '' };

        const changes = diffAgainst(existing, result.item);
        return changes.length
          ? { item: result.item, existing, action: 'update', changes, status: 'pending', msg: '' }
          : { item: result.item, existing, action: 'skip', changes: [], status: 'skip', msg: 'Tidak ada perubahan' };
      });
      setRows(parsed);
      setDone(false);
    };
    reader.readAsArrayBuffer(file);
  };

  const handleImport = async () => {
    setImporting(true);
    const updated = [...rows];
    for (let i = 0; i < updated.length; i++) {
      if (updated[i].status !== 'pending') continue;
      const { item, existing, action } = updated[i];
      try {
        if (action === 'update') await updateItem(existing.id, item);
        else await createItem(item);
        updated[i] = { ...updated[i], status: 'ok', msg: action === 'update' ? 'Diperbarui' : 'Ditambahkan' };
      } catch (err) {
        updated[i] = { ...updated[i], status: 'error', msg: err.response?.data?.error || 'Gagal' };
      }
      setRows([...updated]);
    }
    setImporting(false);
    setDone(true);
    onDone();
  };

  const pendingCount  = rows.filter(r => r.status === 'pending').length;
  const okCount       = rows.filter(r => r.status === 'ok').length;
  const errorCount    = rows.filter(r => r.status === 'error').length;
  const skipCount     = rows.filter(r => r.status === 'skip').length;
  const createCount   = rows.filter(r => r.status === 'pending' && r.action === 'create').length;
  const updateCount   = rows.filter(r => r.status === 'pending' && r.action === 'update').length;

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div style={{ background: '#fff', borderRadius: '10px', padding: '1.75rem 2rem', width: '720px', maxWidth: '95vw', maxHeight: '85vh', display: 'flex', flexDirection: 'column', gap: '1rem' }}>

        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <h2 style={{ margin: 0 }}>Import Barang dari Excel</h2>
          <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: '1.4rem', cursor: 'pointer', color: '#888', lineHeight: 1 }}>×</button>
        </div>

        <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', flexWrap: 'wrap' }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer', padding: '0.45rem 1rem', border: '1px solid #ddd', borderRadius: '6px', fontSize: '0.9rem', background: '#f8f9ff' }}>
            📂 Pilih File Excel
            <input ref={fileRef} type="file" accept=".xlsx,.xls" onChange={handleFile} disabled={!catalog} style={{ display: 'none' }} />
          </label>
          <button onClick={downloadSampleExcel} className="btn btn-secondary" style={{ fontSize: '0.9rem' }}>
            ⬇ Download Template
          </button>
          <span style={{ fontSize: '0.82rem', color: '#888' }}>
            {catalog ? 'Format: .xlsx atau .xls' : 'Memuat daftar barang…'}
          </span>
        </div>

        {rows.length > 0 && (
          <>
            <div style={{ fontSize: '0.85rem', color: '#555', display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
              <span>{rows.length} baris terdeteksi</span>
              {!done && createCount > 0 && <span style={{ color: '#4f8ef7', fontWeight: 600 }}>+ {createCount} baru</span>}
              {!done && updateCount > 0 && <span style={{ color: '#e67e22', fontWeight: 600 }}>↻ {updateCount} diperbarui</span>}
              {skipCount > 0   && <span style={{ color: '#888' }}>— {skipCount} tanpa perubahan</span>}
              {okCount > 0     && <span style={{ color: '#27ae60', fontWeight: 600 }}>✓ {okCount} berhasil</span>}
              {errorCount > 0  && <span style={{ color: '#e74c3c', fontWeight: 600 }}>✗ {errorCount} gagal</span>}
              {pendingCount > 0 && !done && <span style={{ color: '#888' }}>{pendingCount} menunggu</span>}
            </div>

            <div style={{ overflowY: 'auto', flex: 1, border: '1px solid #eee', borderRadius: '6px' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.83rem' }}>
                <thead style={{ position: 'sticky', top: 0, background: '#f5f7fa' }}>
                  <tr>
                    {['#', 'Nama', 'Kode', 'Tipe', 'Satuan', 'Stok Min', 'Aksi', 'Status'].map(h => (
                      <th key={h} style={{ padding: '0.45rem 0.6rem', textAlign: 'left', borderBottom: '1px solid #e0e0e0', fontWeight: 600, color: '#555' }}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r, i) => (
                    <tr key={i} style={{ background: r.status === 'ok' ? '#f0faf4' : r.status === 'error' ? '#fff5f5' : r.status === 'skip' ? '#fafafa' : 'transparent' }}>
                      <td style={{ padding: '0.35rem 0.6rem', color: '#aaa' }}>{i + 1}</td>
                      <td style={{ padding: '0.35rem 0.6rem', fontWeight: 500 }}>{r.item?.name ?? <span style={{ color: '#e74c3c', fontSize: '0.8rem' }}>—</span>}</td>
                      <td style={{ padding: '0.35rem 0.6rem', color: '#777' }}>{r.item?.code ?? '—'}</td>
                      <td style={{ padding: '0.35rem 0.6rem' }}>
                        {r.item && (
                          <span className="badge" style={{ background: r.item.is_stock ? '#e8f5e9' : '#fff3e0', color: r.item.is_stock ? '#388e3c' : '#f57c00', fontSize: '0.75rem' }}>
                            {r.item.is_stock ? 'Stok' : 'Non-Stok'}
                          </span>
                        )}
                      </td>
                      <td style={{ padding: '0.35rem 0.6rem', color: '#555' }}>
                        {r.item?.units.map((u, ui) => (
                          <span key={ui}>
                            {ui > 0 && <span style={{ color: '#bbb', margin: '0 3px' }}>→ ×{u.perPrev}</span>}
                            <span className="badge" style={{ fontSize: '0.72rem' }}>{u.name}</span>
                          </span>
                        ))}
                      </td>
                      <td style={{ padding: '0.35rem 0.6rem', whiteSpace: 'nowrap', color: '#555' }}>
                        {r.item && r.item.is_stock ? (
                          <>
                            {r.existing && (Number(r.existing.min_stock) || 0) !== r.item.min_stock && (
                              <span style={{ color: '#bbb', textDecoration: 'line-through', marginRight: '0.3rem' }}>
                                {qty(r.existing.min_stock)}
                              </span>
                            )}
                            <span style={{ fontWeight: r.item.min_stock > 0 ? 600 : 400, color: r.item.min_stock > 0 ? '#333' : '#bbb' }}>
                              {qty(r.item.min_stock)}
                            </span>
                          </>
                        ) : <span style={{ color: '#ddd' }}>—</span>}
                      </td>
                      <td style={{ padding: '0.35rem 0.6rem', fontSize: '0.78rem' }}>
                        {r.action === 'create' && <span style={{ color: '#4f8ef7', fontWeight: 600 }}>Baru</span>}
                        {r.action === 'update' && (
                          <span style={{ color: '#e67e22', fontWeight: 600 }}>
                            Perbarui <span style={{ color: '#999', fontWeight: 400 }}>({r.changes.join(', ')})</span>
                          </span>
                        )}
                        {r.action === 'skip' && <span style={{ color: '#aaa' }}>Sama</span>}
                      </td>
                      <td style={{ padding: '0.35rem 0.6rem' }}>
                        {r.status === 'ok'      && <span style={{ color: '#27ae60', fontWeight: 600 }}>✓ {r.msg}</span>}
                        {r.status === 'error'   && <span style={{ color: '#e74c3c', fontSize: '0.8rem' }}>{r.msg}</span>}
                        {r.status === 'skip'    && <span style={{ color: '#bbb', fontSize: '0.8rem' }}>Dilewati</span>}
                        {r.status === 'pending' && <span style={{ color: '#aaa' }}>Menunggu</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
              <button onClick={onClose} className="btn btn-secondary">
                {done ? 'Tutup' : 'Batal'}
              </button>
              {!done && pendingCount > 0 && (
                <button
                  onClick={handleImport}
                  className="btn btn-primary"
                  disabled={importing}
                >
                  {importing ? `Memproses… (${okCount + errorCount}/${pendingCount})` : `Terapkan ${pendingCount} Baris`}
                </button>
              )}
            </div>
          </>
        )}

        {!done && rows.some(r => r.status === 'pending' && r.changes?.includes('satuan')) && (
          <div className="notice-msg" style={{ fontSize: '0.83rem' }}>
            Ada baris yang mengubah satuan barang. Stok yang sudah ada akan dikonversi
            otomatis ke satuan terkecil yang baru, begitu juga stok minimumnya.
          </div>
        )}

        {rows.length === 0 && (
          <p style={{ color: '#aaa', fontSize: '0.88rem', margin: '0.5rem 0' }}>
            Pilih file Excel untuk melihat pratinjau data sebelum mengimpor. Barang dengan
            <strong> kode yang sudah ada akan diperbarui</strong> (termasuk stok minimumnya),
            sisanya ditambahkan sebagai barang baru. Cara termudah mengatur stok minimum massal:
            <strong> Export Excel</strong> dari daftar barang, isi kolom <em>Stok Minimum</em>, lalu import kembali.
          </p>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------

export default function Items() {
  const [items, setItems] = useState([]);
  const [search, setSearch] = useState('');
  const [typeFilter, setTypeFilter] = useState('');
  const [showImport, setShowImport] = useState(false);
  const [lowOnly, setLowOnly] = useState(false);

  const load = useCallback(() => {
    const params = { search };
    if (typeFilter !== '') params.is_stock = typeFilter;
    if (lowOnly) params.low_stock = 'true';
    getItems(params).then(r => setItems(r.data));
  }, [search, typeFilter, lowOnly]);

  useEffect(() => { load(); }, [load]);

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus barang ini? Semua catatan inventaris terkait juga akan dihapus.')) return;
    try {
      await deleteItem(id);
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus barang.');
    }
  };

  const lowCount = items.filter(i => i.is_low_stock).length;

  return (
    <>
      {showImport && (
        <ImportModal
          onClose={() => setShowImport(false)}
          onDone={load}
        />
      )}

      <div className="page-header">
        <h1>Barang</h1>
        <div style={{ display: 'flex', gap: '0.6rem' }}>
          <button
            onClick={() => exportItemsExcel(items)}
            className="btn btn-secondary"
            disabled={items.length === 0}
            title="Unduh daftar barang yang sedang tampil, dalam format yang bisa diimpor kembali"
          >
            ⬇ Export Excel
          </button>
          <button onClick={() => setShowImport(true)} className="btn btn-secondary">⬆ Import Excel</button>
          <Link to="/items/new" className="btn btn-primary">+ Tambah Barang</Link>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>
            {items.length} item{items.length !== 1 ? 's' : ''}
            {lowCount > 0 && !lowOnly && (
              <span style={{marginLeft:'0.6rem',fontSize:'0.82rem',fontWeight:600,color:'#e74c3c'}}>
                • {lowCount} stok menipis
              </span>
            )}
          </h2>
          <div className="filters">
            <input
              placeholder="Cari nama atau kode..."
              value={search}
              onChange={e => setSearch(e.target.value)}
            />
            <select value={typeFilter} onChange={e => setTypeFilter(e.target.value)}>
              <option value="">Semua Tipe</option>
              <option value="true">Barang Stok</option>
              <option value="false">Barang Non-Stok</option>
            </select>
            <label style={{display:'flex',alignItems:'center',gap:'0.35rem',fontSize:'0.85rem',color:'#555',whiteSpace:'nowrap'}}>
              <input type="checkbox" checked={lowOnly} onChange={e => setLowOnly(e.target.checked)} />
              Stok menipis saja
            </label>
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>Nama</th>
              <th>Kode</th>
              <th>Tipe</th>
              <th>Satuan</th>
              <th style={{textAlign:'right'}}>Stok / Minimum</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr><td colSpan={6} style={{textAlign:'center',color:'#999',padding:'2rem'}}>Tidak ada data</td></tr>
            ) : items.map(item => (
              <tr key={item.id}>
                <td style={{fontWeight: 500}}>
                  <Link to={historyPath(item)} style={{color:'#4f8ef7',textDecoration:'none'}}>{item.name}</Link>
                </td>
                <td style={{color:'#888',fontSize:'0.85rem'}}>{item.code}</td>
                <td>
                  {item.is_stock === false ? (
                    <span className="badge" style={{background:'#fff3e0',color:'#f57c00'}}>Non-Stok</span>
                  ) : (
                    <span className="badge" style={{background:'#e8f5e9',color:'#388e3c'}}>Stok</span>
                  )}
                </td>
                <td><UnitChain units={item.units} /></td>
                <td style={{textAlign:'right',whiteSpace:'nowrap',fontSize:'0.85rem'}}>
                  {item.is_stock === false ? (
                    <span style={{color:'#ddd'}}>—</span>
                  ) : (
                    <>
                      <span style={{fontWeight:600,color: item.is_low_stock ? '#e74c3c' : '#333'}}>
                        {qty(item.stock_quantity)}
                      </span>
                      <span style={{color:'#bbb'}}> / </span>
                      <span style={{color: Number(item.min_stock) > 0 ? '#666' : '#ccc'}}>
                        {Number(item.min_stock) > 0 ? qty(item.min_stock) : '—'}
                      </span>
                      <span style={{color:'#aaa',marginLeft:'0.3rem',fontSize:'0.78rem'}}>
                        {baseUnitName(item.units)}
                      </span>
                      {item.is_low_stock && (
                        <span className="badge" style={{background:'#fdecea',color:'#e74c3c',marginLeft:'0.4rem',fontSize:'0.72rem'}}>
                          Menipis
                        </span>
                      )}
                    </>
                  )}
                </td>
                <td>
                  <div className="actions">
                    <Link to={historyPath(item)} className="btn btn-secondary btn-sm">Riwayat</Link>
                    <Link to={`/items/edit/${item.id}`} className="btn btn-secondary btn-sm">Edit</Link>
                    <button onClick={() => handleDelete(item.id)} className="btn btn-danger btn-sm">Hapus</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
