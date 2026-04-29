import { useEffect, useState } from 'react';
import * as XLSX from 'xlsx';
import { getAccounts, createAccount, updateAccount, deleteAccount } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const TYPE_LABEL = {
  asset:     'Aset',
  liability: 'Kewajiban',
  equity:    'Ekuitas',
  revenue:   'Pendapatan',
  expense:   'Beban',
};

const TYPE_COLOR = {
  asset:     '#27ae60',
  liability: '#e74c3c',
  equity:    '#8b5cf6',
  revenue:   '#2c6fc2',
  expense:   '#e67e22',
};

function typeFromNumber(n) {
  const num = Number(n);
  if (num < 20000) return 'asset';
  if (num < 30000) return 'liability';
  if (num < 40000) return 'equity';
  if (num < 50000) return 'revenue';
  return 'expense';
}

const emptyForm = { name: '', balance: '', account_number: '', parent_id: '' };

export default function Accounts() {
  const [accounts, setAccounts] = useState([]);
  const [form, setForm]         = useState(emptyForm);
  const [editId, setEditId]     = useState(null);
  const [editForm, setEditForm] = useState(emptyForm);
  const [error, setError]       = useState('');
  const [collapsed, setCollapsed] = useState({});

  const load = () => getAccounts().then(r => setAccounts(r.data));
  useEffect(() => { load(); }, []);

  const toggle = (id) => setCollapsed(s => ({ ...s, [id]: !s[id] }));

  // Recursive balance: leaf node's own balance, parent = sum of children
  const totalOf = (a) => {
    const children = accounts.filter(c => c.parent_id === a.id);
    if (!children.length) return Number(a.balance);
    return children.reduce((s, c) => s + totalOf(c), 0);
  };

  const sectionTotal = (type) =>
    accounts.filter(a => a.account_type === type && !a.parent_id)
            .reduce((s, a) => s + totalOf(a), 0);

  const totalRevenue = sectionTotal('revenue');
  const totalExpense = sectionTotal('expense');
  const totalAsset   = sectionTotal('asset');
  const netIncome    = totalRevenue - totalExpense;

  // ── Account management ──────────────────────────────────────────────────────

  const handleAdd = async (e) => {
    e.preventDefault();
    setError('');
    try {
      await createAccount({
        name:           form.name,
        balance:        Number(form.balance) || 0,
        account_number: form.account_number ? Number(form.account_number) : undefined,
        parent_id:      form.parent_id || undefined,
      });
      setForm(emptyForm);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    }
  };

  const handleEdit = async (id) => {
    setError('');
    try {
      await updateAccount(id, {
        name:           editForm.name,
        balance:        Number(editForm.balance) || 0,
        account_number: editForm.account_number ? Number(editForm.account_number) : undefined,
        parent_id:      editForm.parent_id || undefined,
      });
      setEditId(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus akun ini?')) return;
    setError('');
    try {
      await deleteAccount(id);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Tidak bisa dihapus');
    }
  };

  const startEdit = (a) => {
    setEditId(a.id);
    setEditForm({
      name:           a.name,
      balance:        String(a.balance),
      account_number: a.account_number != null ? String(a.account_number) : '',
      parent_id:      a.parent_id ?? '',
    });
  };

  const previewType = form.account_number.length === 5
    ? TYPE_LABEL[typeFromNumber(form.account_number)] ?? ''
    : '';

  // ── Row renderer ────────────────────────────────────────────────────────────

  const renderRow = (a, depth = 0) => {
    const children  = accounts.filter(c => c.parent_id === a.id);
    const bal       = totalOf(a);
    const isOpen    = !collapsed[a.id];
    const isEditing = editId === a.id;
    const isParent  = children.length > 0;

    return [
      <tr key={a.id} style={{ background: depth === 0 && isParent ? '#f8f9ff' : undefined }}>
        {/* COA number */}
        <td style={{ color: '#888', fontSize: '0.82rem', whiteSpace: 'nowrap', paddingLeft: `${depth * 18 + 8}px`, width: '80px' }}>
          {isEditing && !a.is_system ? (
            <input
              value={editForm.account_number}
              onChange={e => setEditForm(f => ({ ...f, account_number: e.target.value }))}
              placeholder="10000"
              style={{ padding: '0.25rem 0.4rem', border: '1px solid #4f8ef7', borderRadius: '4px', fontSize: '0.8rem', width: '70px' }}
            />
          ) : (
            <span style={{ fontFamily: 'monospace', fontWeight: a.account_number ? 600 : 400 }}>
              {a.account_number ?? <span style={{ color: '#ddd' }}>—</span>}
            </span>
          )}
        </td>

        {/* Account name */}
        <td>
          {children.length > 0 && (
            <button
              onClick={() => toggle(a.id)}
              style={{ background: 'none', border: 'none', cursor: 'pointer', marginRight: '4px', color: '#aaa', fontSize: '0.75rem', padding: '0 2px' }}
            >
              {isOpen ? '▼' : '▶'}
            </button>
          )}
          {children.length === 0 && <span style={{ display: 'inline-block', width: '18px' }} />}
          {isEditing ? (
            <input
              value={editForm.name}
              onChange={e => setEditForm(f => ({ ...f, name: e.target.value }))}
              style={{ padding: '0.25rem 0.5rem', border: '1px solid #4f8ef7', borderRadius: '4px', fontSize: '0.9rem', width: '180px' }}
              autoFocus
            />
          ) : (
            <span style={{ fontWeight: depth === 0 ? 700 : isParent ? 600 : 400 }}>{a.name}</span>
          )}
        </td>

        {/* Balance */}
        <td style={{ textAlign: 'right', fontWeight: isParent ? 700 : 500, color: bal < 0 ? '#e74c3c' : bal > 0 ? '#1a1a2e' : '#bbb' }}>
          {isEditing ? (
            <input
              type="number"
              value={editForm.balance}
              onChange={e => setEditForm(f => ({ ...f, balance: e.target.value }))}
              style={{ padding: '0.25rem 0.4rem', border: '1px solid #4f8ef7', borderRadius: '4px', fontSize: '0.85rem', width: '130px', textAlign: 'right' }}
            />
          ) : bal !== 0 ? idr(bal) : <span style={{ color: '#ddd' }}>—</span>}
        </td>

        {/* Actions */}
        <td>
          <div className="actions">
            {isEditing ? (
              <>
                <button onClick={() => handleEdit(a.id)} className="btn btn-primary btn-sm">Simpan</button>
                <button onClick={() => setEditId(null)} className="btn btn-secondary btn-sm">Batal</button>
              </>
            ) : (
              <>
                <button onClick={() => startEdit(a)} className="btn btn-secondary btn-sm">Edit</button>
                {!a.is_system && (
                  <button onClick={() => handleDelete(a.id)} className="btn btn-danger btn-sm">Hapus</button>
                )}
              </>
            )}
          </div>
        </td>
      </tr>,
      ...(isOpen ? children.map(c => renderRow(c, depth + 1)) : []),
    ];
  };

  // ── Section renderer ─────────────────────────────────────────────────────────

  const renderSection = (type) => {
    const roots = accounts.filter(a => a.account_type === type && !a.parent_id);
    const total = sectionTotal(type);
    const color = TYPE_COLOR[type];
    const label = TYPE_LABEL[type];
    return (
      <>
        <tr>
          <td colSpan={4} style={{ background: color + '14', padding: '0.5rem 0.75rem', borderTop: `2px solid ${color}30` }}>
            <span style={{ fontWeight: 700, fontSize: '0.78rem', textTransform: 'uppercase', letterSpacing: '0.6px', color }}>
              {label}
            </span>
          </td>
        </tr>
        {roots.map(a => renderRow(a, 0))}
        <tr style={{ borderTop: `1px solid ${color}30` }}>
          <td style={{ paddingLeft: '8px', color: '#888', fontSize: '0.8rem', fontStyle: 'italic' }}></td>
          <td style={{ textAlign: 'right', fontWeight: 600, color: '#555', fontSize: '0.85rem', paddingRight: '0.5rem' }}>
            Total {label}
          </td>
          <td style={{ textAlign: 'right', fontWeight: 700, color, fontSize: '0.95rem' }}>{idr(total)}</td>
          <td />
        </tr>
        <tr><td colSpan={4} style={{ padding: '4px 0' }} /></tr>
      </>
    );
  };

  // ── Excel export ─────────────────────────────────────────────────────────────

  const downloadExcel = () => {
    const wb = XLSX.utils.book_new();
    const today = new Date().toLocaleDateString('id-ID');

    // Helper: flatten accounts for a type recursively
    const flattenType = (type, indent = 0) => {
      const addRows = (acc, d) => {
        const children = accounts.filter(c => c.parent_id === acc.id);
        const bal = totalOf(acc);
        const prefix = '  '.repeat(d);
        rows.push([
          acc.account_number ?? '',
          prefix + acc.name,
          bal,
        ]);
        if (!collapsed[acc.id]) children.forEach(c => addRows(c, d + 1));
      };
      const rows = [];
      accounts.filter(a => a.account_type === type && !a.parent_id).forEach(a => addRows(a, 0));
      return rows;
    };

    const numFmt = '#,##0';

    // ── Sheet 1: Laba Rugi ────────────────────────────────────────────────────
    const plData = [
      ['LAPORAN LABA RUGI'],
      [`Dicetak: ${today}`],
      [],
      ['No. Akun', 'Nama Akun', 'Saldo (Rp)'],
      [],
      ['', 'PENDAPATAN', ''],
      ...flattenType('revenue'),
      ['', 'Total Pendapatan', totalRevenue],
      [],
      ['', 'BEBAN', ''],
      ...flattenType('expense'),
      ['', 'Total Beban', totalExpense],
      [],
      ['', 'LABA BERSIH', netIncome],
    ];
    const wsP = XLSX.utils.aoa_to_sheet(plData);
    wsP['!cols'] = [{ wch: 12 }, { wch: 36 }, { wch: 20 }];
    const rpP = XLSX.utils.decode_range(wsP['!ref']);
    for (let r = 0; r <= rpP.e.r; r++) {
      const cell = wsP[XLSX.utils.encode_cell({ r, c: 2 })];
      if (cell && typeof cell.v === 'number') cell.z = numFmt;
    }
    XLSX.utils.book_append_sheet(wb, wsP, 'Laba Rugi');

    // ── Sheet 2: Neraca ───────────────────────────────────────────────────────
    const totalLiability = sectionTotal('liability');
    const totalEquity    = sectionTotal('equity');
    const bsData = [
      ['NERACA'],
      [`Dicetak: ${today}`],
      [],
      ['No. Akun', 'Nama Akun', 'Saldo (Rp)'],
      [],
      ['', 'ASET', ''],
      ...flattenType('asset'),
      ['', 'Total Aset', totalAsset],
      [],
      ['', 'KEWAJIBAN', ''],
      ...flattenType('liability'),
      ['', 'Total Kewajiban', totalLiability],
      [],
      ['', 'EKUITAS', ''],
      ...flattenType('equity'),
      ['', 'Total Ekuitas', totalEquity],
      [],
      ['', 'Total Kewajiban + Ekuitas', totalLiability + totalEquity],
    ];
    const wsB = XLSX.utils.aoa_to_sheet(bsData);
    wsB['!cols'] = [{ wch: 12 }, { wch: 36 }, { wch: 20 }];
    const rpB = XLSX.utils.decode_range(wsB['!ref']);
    for (let r = 0; r <= rpB.e.r; r++) {
      const cell = wsB[XLSX.utils.encode_cell({ r, c: 2 })];
      if (cell && typeof cell.v === 'number') cell.z = numFmt;
    }
    XLSX.utils.book_append_sheet(wb, wsB, 'Neraca');

    // ── Sheet 3: COA Lengkap ──────────────────────────────────────────────────
    const coaHeader = [['No. Akun', 'Nama Akun', 'Tipe', 'Saldo (Rp)']];
    const sortedAccounts = [...accounts].sort((a, b) => {
      const na = a.account_number ?? 99999;
      const nb = b.account_number ?? 99999;
      return na - nb || a.name.localeCompare(b.name);
    });
    const coaData = [
      ...coaHeader,
      ...sortedAccounts.map(a => [
        a.account_number ?? '',
        a.name,
        TYPE_LABEL[a.account_type] ?? a.account_type,
        Number(a.balance),
      ]),
    ];
    const wsC = XLSX.utils.aoa_to_sheet(coaData);
    wsC['!cols'] = [{ wch: 12 }, { wch: 36 }, { wch: 14 }, { wch: 20 }];
    const rpC = XLSX.utils.decode_range(wsC['!ref']);
    for (let r = 1; r <= rpC.e.r; r++) {
      const cell = wsC[XLSX.utils.encode_cell({ r, c: 3 })];
      if (cell && typeof cell.v === 'number') cell.z = numFmt;
    }
    XLSX.utils.book_append_sheet(wb, wsC, 'COA Lengkap');

    XLSX.writeFile(wb, `laporan-keuangan-${today.replace(/\//g, '-')}.xlsx`);
  };

  // ── Render ────────────────────────────────────────────────────────────────────

  const totalLiability = sectionTotal('liability');
  const totalEquity    = sectionTotal('equity');

  return (
    <>
      <div className="page-header">
        <h1>Laporan Keuangan</h1>
        <button onClick={downloadExcel} className="btn btn-secondary">⬇ Download Excel</button>
      </div>

      {/* Summary cards */}
      <div className="stats-grid" style={{ marginBottom: '1.5rem' }}>
        <div className={`stat-card${netIncome < 0 ? ' warning' : ''}`}>
          <div className="label">Laba Bersih</div>
          <div className="value" style={{ color: netIncome >= 0 ? '#27ae60' : '#e74c3c' }}>{idr(netIncome)}</div>
        </div>
        <div className="stat-card">
          <div className="label">Total Pendapatan</div>
          <div className="value" style={{ color: '#2c6fc2' }}>{idr(totalRevenue)}</div>
        </div>
        <div className="stat-card">
          <div className="label">Total Beban</div>
          <div className="value" style={{ color: '#e67e22' }}>{idr(totalExpense)}</div>
        </div>
        <div className="stat-card">
          <div className="label">Total Aset</div>
          <div className="value" style={{ color: '#27ae60' }}>{idr(totalAsset)}</div>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 340px', gap: '1.5rem', alignItems: 'start' }}>
        <div className="card">
          {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

          <table>
            <thead>
              <tr>
                <th style={{ width: '80px' }}>No. Akun</th>
                <th>Nama Akun</th>
                <th style={{ textAlign: 'right' }}>Saldo</th>
                <th style={{ width: '120px' }}></th>
              </tr>
            </thead>
            <tbody>
              {/* ── P&L ── */}
              <tr>
                <td colSpan={4} style={{ padding: '0.75rem 0.5rem 0.25rem', fontSize: '0.72rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.8px', color: '#999' }}>
                  Laporan Laba Rugi
                </td>
              </tr>
              {renderSection('revenue')}
              {renderSection('expense')}

              {/* Net Income */}
              <tr style={{ borderTop: '2px solid #e0e0e0' }}>
                <td style={{ paddingLeft: '8px', fontFamily: 'monospace', color: '#888', fontSize: '0.82rem' }}></td>
                <td style={{ fontWeight: 800, fontSize: '0.95rem', paddingLeft: '8px' }}>LABA BERSIH</td>
                <td style={{
                  textAlign: 'right', fontWeight: 800, fontSize: '1.05rem',
                  color: netIncome >= 0 ? '#27ae60' : '#e74c3c',
                }}>
                  {idr(netIncome)}
                </td>
                <td />
              </tr>

              {/* Spacer */}
              <tr><td colSpan={4} style={{ padding: '12px 0' }} /></tr>

              {/* ── Balance Sheet ── */}
              <tr>
                <td colSpan={4} style={{ padding: '0.75rem 0.5rem 0.25rem', fontSize: '0.72rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.8px', color: '#999' }}>
                  Neraca
                </td>
              </tr>
              {renderSection('asset')}
              {renderSection('liability')}
              {renderSection('equity')}

              {/* Balance check */}
              <tr style={{ borderTop: '2px solid #e0e0e0' }}>
                <td />
                <td style={{ fontWeight: 800, fontSize: '0.9rem', paddingLeft: '8px', color: '#555' }}>Total Kewajiban + Ekuitas</td>
                <td style={{ textAlign: 'right', fontWeight: 800, color: '#555' }}>
                  {idr(totalLiability + totalEquity)}
                </td>
                <td />
              </tr>
            </tbody>
          </table>
        </div>

        {/* Management form */}
        <div className="card">
          <h2 style={{ marginBottom: '1.25rem', fontSize: '1rem' }}>Tambah Akun</h2>
          <form onSubmit={handleAdd}>
            <div className="form-group">
              <label>No. Akun <span style={{ color: '#aaa', fontWeight: 400 }}>(5 digit)</span></label>
              <input
                value={form.account_number}
                onChange={e => setForm(f => ({ ...f, account_number: e.target.value }))}
                placeholder="mis. 11100, 41100..."
                maxLength={5}
              />
              {previewType && (
                <small style={{ color: TYPE_COLOR[typeFromNumber(form.account_number)], marginTop: '0.2rem', display: 'block', fontWeight: 600 }}>
                  Tipe: {previewType}
                </small>
              )}
            </div>
            <div className="form-group">
              <label>Nama Akun</label>
              <input
                value={form.name}
                onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
                placeholder="mis. Kas Besar, BCA..."
                required
              />
            </div>
            <div className="form-group">
              <label>Induk Akun <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
              <select value={form.parent_id} onChange={e => setForm(f => ({ ...f, parent_id: e.target.value }))}>
                <option value="">— Tanpa induk —</option>
                {accounts.map(a => (
                  <option key={a.id} value={a.id}>
                    {a.account_number ? `${a.account_number} · ` : ''}{a.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="form-group">
              <label>Saldo Awal (Rp)</label>
              <input
                type="number"
                value={form.balance}
                onChange={e => setForm(f => ({ ...f, balance: e.target.value }))}
                placeholder="0"
              />
              {Number(form.balance) !== 0 && (
                <small style={{ color: '#888', marginTop: '0.25rem', display: 'block' }}>{idr(Number(form.balance))}</small>
              )}
            </div>
            <button type="submit" className="btn btn-primary" style={{ width: '100%', justifyContent: 'center' }}>
              Tambah Akun
            </button>
          </form>
        </div>
      </div>
    </>
  );
}
