import { useEffect, useState, useCallback, useRef } from 'react';
import { Link } from 'react-router-dom';
import * as XLSX from 'xlsx';
import { getItems, deleteItem, createItem } from '../api';

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

const SAMPLE_ROWS = [
  ['Nama Barang', 'Kode', 'Tipe', 'Satuan 1', 'Satuan 2', 'Isi per Satuan 1', 'Satuan 3', 'Isi per Satuan 2'],
  ['Cat Tembok Putih', 'CTW-001', 'Stok', 'Kaleng', 'Liter', 4, '', ''],
  ['Semen Portland', 'SEM-001', 'Stok', 'Sak', '', '', '', ''],
  ['Kuas Cat 4"', 'KCS-004', 'Stok', 'Lusin', 'Pcs', 12, '', ''],
  ['Amplas Halus', 'AMP-H', 'Stok', 'Roll', 'Lembar', 20, '', ''],
  ['Tiner A Special', 'TNR-AS', 'Stok', 'Drum', 'Galon', 5, 'Liter', 4],
  ['Sarung Tangan', 'SGT-001', 'Non-Stok', 'Kotak', 'Pcs', 10, '', ''],
];

const HEADER_STYLE = {
  font: { bold: true },
  fill: { fgColor: { rgb: 'D9E1F2' }, patternType: 'solid' },
  alignment: { horizontal: 'center', vertical: 'center' },
  border: { top: { style: 'thin' }, bottom: { style: 'thin' }, left: { style: 'thin' }, right: { style: 'thin' } },
};
const CELL_STYLE = {
  border: { top: { style: 'thin' }, bottom: { style: 'thin' }, left: { style: 'thin' }, right: { style: 'thin' } },
};

function downloadSampleExcel() {
  const ws = XLSX.utils.aoa_to_sheet(SAMPLE_ROWS);
  ws['!cols'] = [34, 14, 10, 14, 14, 16, 14, 16].map(wch => ({ wch }));
  ws['!rows'] = [{ hpt: 22 }, ...Array(SAMPLE_ROWS.length - 1).fill({ hpt: 20 })];

  SAMPLE_ROWS[0].forEach((_, c) => {
    const addr = XLSX.utils.encode_cell({ r: 0, c });
    if (ws[addr]) ws[addr].s = HEADER_STYLE;
  });
  for (let r = 1; r < SAMPLE_ROWS.length; r++) {
    SAMPLE_ROWS[0].forEach((_, c) => {
      const addr = XLSX.utils.encode_cell({ r, c });
      if (ws[addr]) ws[addr].s = CELL_STYLE;
    });
  }

  const wb = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(wb, ws, 'Barang');
  XLSX.writeFile(wb, 'template-import-barang.xlsx', { cellStyles: true });
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

  return { item: { name, code, units, is_stock: tipe !== 'non-stok' } };
}

// ---------------------------------------------------------------------------
// Import modal
// ---------------------------------------------------------------------------

function ImportModal({ onClose, onDone }) {
  const fileRef = useRef();
  const [rows, setRows] = useState([]);       // { item, error, status: 'pending'|'ok'|'error', msg }
  const [importing, setImporting] = useState(false);
  const [done, setDone] = useState(false);

  const handleFile = (e) => {
    const file = e.target.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (ev) => {
      const wb = XLSX.read(ev.target.result, { type: 'array' });
      const ws = wb.Sheets[wb.SheetNames[0]];
      const data = XLSX.utils.sheet_to_json(ws, { defval: '' });
      const parsed = data.map((row, i) => {
        const result = parseRow(row, i + 2);
        return result.error
          ? { error: result.error, status: 'error', msg: result.error }
          : { item: result.item, status: 'pending', msg: '' };
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
      try {
        await createItem(updated[i].item);
        updated[i] = { ...updated[i], status: 'ok', msg: 'Berhasil' };
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
            <input ref={fileRef} type="file" accept=".xlsx,.xls" onChange={handleFile} style={{ display: 'none' }} />
          </label>
          <button onClick={downloadSampleExcel} className="btn btn-secondary" style={{ fontSize: '0.9rem' }}>
            ⬇ Download Template
          </button>
          <span style={{ fontSize: '0.82rem', color: '#888' }}>Format: .xlsx atau .xls</span>
        </div>

        {rows.length > 0 && (
          <>
            <div style={{ fontSize: '0.85rem', color: '#555', display: 'flex', gap: '1rem' }}>
              <span>{rows.length} baris terdeteksi</span>
              {okCount > 0     && <span style={{ color: '#27ae60', fontWeight: 600 }}>✓ {okCount} berhasil</span>}
              {errorCount > 0  && <span style={{ color: '#e74c3c', fontWeight: 600 }}>✗ {errorCount} gagal</span>}
              {pendingCount > 0 && !done && <span style={{ color: '#888' }}>{pendingCount} menunggu</span>}
            </div>

            <div style={{ overflowY: 'auto', flex: 1, border: '1px solid #eee', borderRadius: '6px' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.83rem' }}>
                <thead style={{ position: 'sticky', top: 0, background: '#f5f7fa' }}>
                  <tr>
                    {['#', 'Nama', 'Kode', 'Tipe', 'Satuan', 'Status'].map(h => (
                      <th key={h} style={{ padding: '0.45rem 0.6rem', textAlign: 'left', borderBottom: '1px solid #e0e0e0', fontWeight: 600, color: '#555' }}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r, i) => (
                    <tr key={i} style={{ background: r.status === 'ok' ? '#f0faf4' : r.status === 'error' ? '#fff5f5' : 'transparent' }}>
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
                      <td style={{ padding: '0.35rem 0.6rem' }}>
                        {r.status === 'ok'      && <span style={{ color: '#27ae60', fontWeight: 600 }}>✓ Berhasil</span>}
                        {r.status === 'error'   && <span style={{ color: '#e74c3c', fontSize: '0.8rem' }}>{r.msg}</span>}
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
                  {importing ? `Mengimpor… (${okCount + errorCount}/${rows.length})` : `Import ${pendingCount} Barang`}
                </button>
              )}
            </div>
          </>
        )}

        {rows.length === 0 && (
          <p style={{ color: '#aaa', fontSize: '0.88rem', margin: '0.5rem 0' }}>
            Pilih file Excel untuk melihat pratinjau data sebelum mengimpor.
            Gunakan tombol <strong>Download Template</strong> untuk format yang benar.
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

  const load = useCallback(() => {
    const params = { search };
    if (typeFilter !== '') params.is_stock = typeFilter;
    getItems(params).then(r => setItems(r.data));
  }, [search, typeFilter]);

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
          <button onClick={() => setShowImport(true)} className="btn btn-secondary">⬆ Import Excel</button>
          <Link to="/items/new" className="btn btn-primary">+ Tambah Barang</Link>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>{items.length} item{items.length !== 1 ? 's' : ''}</h2>
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
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>Nama</th>
              <th>Kode</th>
              <th>Tipe</th>
              <th>Satuan</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr><td colSpan={5} style={{textAlign:'center',color:'#999',padding:'2rem'}}>Tidak ada data</td></tr>
            ) : items.map(item => (
              <tr key={item.id}>
                <td style={{fontWeight: 500}}>
                  {item.is_stock === false ? (
                    <Link to={`/items/history/${item.id}`} style={{color:'#4f8ef7',textDecoration:'none'}}>{item.name}</Link>
                  ) : item.name}
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
                <td>
                  <div className="actions">
                    {item.is_stock === false && (
                      <Link to={`/items/history/${item.id}`} className="btn btn-secondary btn-sm">Riwayat</Link>
                    )}
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
