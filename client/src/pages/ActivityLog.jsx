import { useEffect, useState, useCallback } from 'react';
import { getActivityLog, deleteActivityLog, exportActivityLog } from '../api';

const ACTION_STYLE = {
  create:   { background: '#e6f9f0', color: '#27ae60' },
  update:   { background: '#e8f0fe', color: '#4f8ef7' },
  delete:   { background: '#fdecea', color: '#e74c3c' },
  transfer: { background: '#fef9e7', color: '#e67e22' },
};

const ENTITY_LABEL = {
  invoice:   'Invoice',
  inventory: 'Inventaris',
  item:      'Barang',
  transfer:  'Transfer',
};

const PAGE_SIZE = 50;

function getUser() {
  try { return JSON.parse(localStorage.getItem('user')); } catch { return null; }
}

export default function ActivityLog() {
  const currentUser = getUser();
  const isAdmin = currentUser?.role === 'admin';

  const [logs, setLogs]       = useState([]);
  const [total, setTotal]     = useState(0);
  const [page, setPage]       = useState(1);
  const [loading, setLoading] = useState(false);

  const [search,     setSearch]     = useState('');
  const [entityType, setEntityType] = useState('all');
  const [action,     setAction]     = useState('all');
  const [dateFrom,   setDateFrom]   = useState('');
  const [dateTo,     setDateTo]     = useState('');

  // Clear modal state
  const [showClear, setShowClear]     = useState(false);
  const [clearBefore, setClearBefore] = useState('');
  const [clearing, setClearing]       = useState(false);
  const [clearError, setClearError]   = useState('');

  const buildParams = useCallback(() => {
    const params = { page, limit: PAGE_SIZE };
    if (search)               params.search      = search;
    if (entityType !== 'all') params.entity_type = entityType;
    if (action !== 'all')     params.action      = action;
    if (dateFrom)             params.date_from   = dateFrom;
    if (dateTo)               params.date_to     = dateTo;
    return params;
  }, [page, search, entityType, action, dateFrom, dateTo]);

  const load = useCallback(() => {
    setLoading(true);
    getActivityLog(buildParams())
      .then(r => { setLogs(r.data.rows); setTotal(r.data.total); })
      .finally(() => setLoading(false));
  }, [buildParams]);

  useEffect(() => { load(); }, [load]);

  const setFilter = (setter) => (e) => { setter(e.target.value); setPage(1); };

  const clearFilters = () => {
    setSearch(''); setEntityType('all'); setAction('all');
    setDateFrom(''); setDateTo(''); setPage(1);
  };

  const handleExport = async () => {
    const params = buildParams();
    delete params.page;
    delete params.limit;
    try {
      const response = await exportActivityLog(params);
      const url = URL.createObjectURL(new Blob([response.data], { type: 'text/csv;charset=utf-8;' }));
      const a = document.createElement('a');
      a.href = url;
      a.download = `activity-log-${new Date().toISOString().split('T')[0]}.csv`;
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      alert('Gagal mengekspor data.');
    }
  };

  const handleClear = async (e) => {
    e.preventDefault();
    setClearError('');
    setClearing(true);
    try {
      const r = await deleteActivityLog(clearBefore);
      setShowClear(false);
      setClearBefore('');
      load();
      alert(`${r.data.deleted} entri berhasil dihapus.`);
    } catch (err) {
      setClearError(err.response?.data?.error || 'Terjadi kesalahan');
    } finally {
      setClearing(false);
    }
  };

  const hasFilters  = search || entityType !== 'all' || action !== 'all' || dateFrom || dateTo;
  const totalPages  = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const pageStart   = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1;
  const pageEnd     = Math.min(page * PAGE_SIZE, total);

  const fmt = (d) => new Date(d).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' });

  return (
    <>
      <div className="page-header">
        <h1>Log Aktivitas</h1>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <button onClick={handleExport} className="btn btn-secondary">⬇ Export CSV</button>
          {isAdmin && (
            <button onClick={() => { setShowClear(true); setClearBefore(''); setClearError(''); }} className="btn btn-danger">
              Hapus Entri Lama
            </button>
          )}
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>
            {loading ? 'Memuat…' : total === 0 ? 'Tidak ada entri' : `${pageStart}–${pageEnd} dari ${total} entri`}
          </h2>
          <div className="filters">
            <input
              placeholder="Cari pengguna / deskripsi…"
              value={search}
              onChange={setFilter(setSearch)}
              style={{ minWidth: '200px' }}
            />
            <select value={entityType} onChange={setFilter(setEntityType)}>
              <option value="all">Semua Tipe</option>
              <option value="invoice">Invoice</option>
              <option value="inventory">Inventory</option>
              <option value="item">Item</option>
              <option value="transfer">Transfer</option>
            </select>
            <select value={action} onChange={setFilter(setAction)}>
              <option value="all">Semua Aksi</option>
              <option value="create">Buat</option>
              <option value="update">Perbarui</option>
              <option value="delete">Hapus</option>
              <option value="transfer">Transfer</option>
            </select>
            <input type="date" value={dateFrom} onChange={setFilter(setDateFrom)} title="Dari tanggal" />
            <input type="date" value={dateTo}   onChange={setFilter(setDateTo)}   title="Sampai tanggal" />
            {hasFilters && (
              <button type="button" onClick={clearFilters} className="btn btn-secondary btn-sm">Bersihkan</button>
            )}
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>Waktu</th>
              <th>Pengguna</th>
              <th>Aksi</th>
              <th>Tipe</th>
              <th>Deskripsi</th>
            </tr>
          </thead>
          <tbody>
            {logs.length === 0 ? (
              <tr><td colSpan={5} style={{ textAlign: 'center', color: '#999', padding: '2rem' }}>
                {loading ? 'Memuat…' : 'Tidak ada entri ditemukan'}
              </td></tr>
            ) : logs.map(log => (
              <tr key={log.id}>
                <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap' }}>{fmt(log.created_at)}</td>
                <td style={{ fontWeight: 500, fontSize: '0.88rem' }}>{log.username}</td>
                <td>
                  <span style={{
                    display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px', fontSize: '0.75rem', fontWeight: 600,
                    ...(ACTION_STYLE[log.action] ?? { background: '#eee', color: '#555' }),
                  }}>
                    {log.action}
                  </span>
                </td>
                <td>
                  <span className="badge">{ENTITY_LABEL[log.entity_type] ?? log.entity_type}</span>
                </td>
                <td style={{ fontSize: '0.88rem', color: '#444' }}>{log.description}</td>
              </tr>
            ))}
          </tbody>
        </table>

        {totalPages > 1 && (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0.75rem 1.5rem', borderTop: '1px solid #f0f0f0' }}>
            <span style={{ fontSize: '0.85rem', color: '#888' }}>Halaman {page} dari {totalPages}</span>
            <div style={{ display: 'flex', gap: '0.35rem' }}>
              <button className="btn btn-secondary btn-sm" onClick={() => setPage(1)} disabled={page === 1}>«</button>
              <button className="btn btn-secondary btn-sm" onClick={() => setPage(p => p - 1)} disabled={page === 1}>‹ Sebelumnya</button>
              {Array.from({ length: totalPages }, (_, i) => i + 1)
                .filter(p => p === 1 || p === totalPages || Math.abs(p - page) <= 2)
                .reduce((acc, p, idx, arr) => {
                  if (idx > 0 && p - arr[idx - 1] > 1) acc.push('…');
                  acc.push(p);
                  return acc;
                }, [])
                .map((p, idx) =>
                  p === '…'
                    ? <span key={`el-${idx}`} style={{ padding: '0 0.3rem', color: '#aaa', lineHeight: '2' }}>…</span>
                    : <button
                        key={p}
                        className="btn btn-sm"
                        style={{ background: p === page ? '#4f8ef7' : undefined, color: p === page ? '#fff' : undefined, border: p === page ? 'none' : undefined }}
                        onClick={() => setPage(p)}
                      >{p}</button>
                )
              }
              <button className="btn btn-secondary btn-sm" onClick={() => setPage(p => p + 1)} disabled={page === totalPages}>Berikutnya ›</button>
              <button className="btn btn-secondary btn-sm" onClick={() => setPage(totalPages)} disabled={page === totalPages}>»</button>
            </div>
          </div>
        )}
      </div>

      {/* Clear modal */}
      {showClear && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '100%', maxWidth: '420px', padding: '2rem', margin: '1rem' }}>
            <h2 style={{ marginBottom: '0.5rem', fontSize: '1.1rem', color: '#e74c3c' }}>Hapus Entri Log</h2>
            <p style={{ fontSize: '0.88rem', color: '#555', marginBottom: '1.25rem' }}>
              Semua entri pada tanggal yang dipilih dan sebelumnya akan dihapus permanen dari database.
              Pastikan sudah mengekspor data terlebih dahulu.
            </p>
            {clearError && <div className="error-msg" style={{ marginBottom: '1rem' }}>{clearError}</div>}
            <form onSubmit={handleClear}>
              <div className="form-group">
                <label>Hapus entri sebelum dan pada tanggal</label>
                <input
                  type="date"
                  value={clearBefore}
                  onChange={e => setClearBefore(e.target.value)}
                  required
                />
              </div>
              <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1.5rem' }}>
                <button
                  type="submit"
                  className="btn btn-danger"
                  disabled={clearing || !clearBefore}
                  style={{ flex: 1, justifyContent: 'center' }}
                >
                  {clearing ? 'Menghapus...' : 'Hapus Permanen'}
                </button>
                <button type="button" onClick={() => setShowClear(false)} className="btn btn-secondary">Batal</button>
              </div>
            </form>
          </div>
        </div>
      )}
    </>
  );
}
