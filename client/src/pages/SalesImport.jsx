import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { getAccounts, getBranches, getDivisions, getDivisionCategories, parsePosXlsx, confirmPosImport, getPosImports } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);
const fmt = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'long', year: 'numeric' }) : '—';

export default function SalesImport() {
  const [accounts, setAccounts]   = useState([]);
  const [branches, setBranches]   = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [imports, setImports]     = useState([]);
  const [parsed, setParsed]       = useState(null);
  const [filename, setFilename]   = useState('');
  const [description, setDescription] = useState('');
  const [selectedBranchId, setSelectedBranchId] = useState('');
  const [divisionCategories, setDivisionCategories] = useState([]);

  // Mappings
  const [cashMappings, setCashMappings]           = useState([]); // [{label, gross, disc, net, amount, account_id}]
  const [revMappings, setRevMappings]             = useState([]); // [{label, gross, disc, net, amount, account_id, byPayment, expanded}]
  const [discMappings, setDiscMappings]           = useState([]); // [{label, amount, account_id}]
  const [biayaMappings, setBiayaMappings]         = useState([]); // [{label, amount, account_id}]
  const [commissionMappings, setCommissionMappings] = useState([]); // [{id, payment_label, real_amount, applied_commission}]
  const [commissionApplied, setCommissionApplied] = useState(false);
  const [biayaRows, setBiayaRows]                 = useState([]); // [{no_penjualan, category, product, biaya}]
  const [skippedRows, setSkippedRows]             = useState([]); // [{no_penjualan, category, product, gross, disc, net, payment, status}]
  const [showRawRows, setShowRawRows]             = useState(false);

  const [error, setError]         = useState('');
  const [uploading, setUploading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone]           = useState(false);
  const [expandedImport, setExpandedImport] = useState(null);
  const fileRef = useRef();

  const revenueAccounts  = accounts.filter(a => a.account_type === 'revenue');
  const cashAccounts     = accounts.filter(a => a.account_type === 'asset' && !a.is_system);
  const expenseAccounts  = accounts.filter(a => a.account_type === 'expense' && !a.is_system);

  useEffect(() => {
    getAccounts().then(r => setAccounts(r.data));
    getBranches().then(r => setBranches(r.data));
    getDivisions().then(r => setDivisions(r.data));
    getPosImports().then(r => setImports(r.data));
  }, []);

  // Clear categories when branch changes
  useEffect(() => {
    setDivisionCategories([]);
  }, [selectedBranchId]);

  const branchDivisions = divisions.filter(d => d.branch_id === selectedBranchId);

  const handleApplyAccounts = async () => {
    if (!selectedBranchId) return;
    const branchDivs = divisions.filter(d => d.branch_id === selectedBranchId);
    if (branchDivs.length === 0) return;

    const catResults = await Promise.all(
      branchDivs.map(div => getDivisionCategories({ division_id: div.id }))
    );

    const allCats = catResults.flatMap(r => r.data);
    setDivisionCategories(allCats);

    // Build cat name → revenue account id map across all divisions
    const catToRevAccountId = {};
    branchDivs.forEach((div, i) => {
      if (div.revenue_account_id) {
        catResults[i].data.forEach(cat => {
          catToRevAccountId[cat.name.toLowerCase()] = div.revenue_account_id;
        });
      }
    });

    // Auto-map cash accounts by matching label against account names
    setCashMappings(ms => ms.map(m => {
      if (m.account_id) return m;
      const match = cashAccounts.find(a => a.name.toLowerCase() === m.label.toLowerCase());
      return match ? { ...m, account_id: match.id } : m;
    }));

    // Apply revenue accounts and capture updated list for discount calc
    const updatedRevMappings = revMappings.map(m => {
      const revAccId = catToRevAccountId[m.label.toLowerCase()];
      return revAccId ? { ...m, account_id: revAccId } : m;
    });
    setRevMappings(updatedRevMappings);

    // Build per-division discount rows from updated revenue mappings
    const discRows = [];
    let totalAssigned = 0;
    branchDivs.forEach((div, i) => {
      const catNamesSet = new Set(catResults[i].data.map(c => c.name.toLowerCase()));
      const divDiscAmount = updatedRevMappings
        .filter(m => catNamesSet.has(m.label.toLowerCase()))
        .reduce((s, m) => s + m.disc, 0);
      if (divDiscAmount > 0) {
        discRows.push({ division_id: div.id, label: div.name, amount: divDiscAmount, account_id: div.discount_account_id || '' });
        totalAssigned += divDiscAmount;
      }
    });

    const unassigned = (parsed?.totalDisc || 0) - totalAssigned;
    const finalDiscRows = [];
    if (unassigned > 0) {
      finalDiscRows.push({ division_id: null, label: 'Diskon (tidak terpetakan)', amount: unassigned, account_id: '' });
    }
    finalDiscRows.push(...discRows);
    setDiscMappings(finalDiscRows);

    // Auto-fill biaya tambahan account from the lain-lain division's expense account
    const lainLainDiv = branchDivs.find(d => d.name.toLowerCase() === 'lain-lain');
    if (lainLainDiv?.expense_account_id) {
      setBiayaMappings(ms => ms.map(m => ({ ...m, account_id: lainLainDiv.expense_account_id })));
    }
  };

  const handleFileChange = async (file) => {
    if (!file) return;
    setError('');
    setUploading(true);
    setParsed(null);
    setDone(false);
    try {
      const form = new FormData();
      form.append('file', file);
      const r = await parsePosXlsx(form);
      const data = r.data;
      setParsed(data);
      setFilename(data.filename || file.name);
      setDescription(`POS Import ${data.date || file.name}`);

      setCashMappings(data.payments.map(p => ({
        label: p.name, gross: p.gross, disc: p.disc, net: p.net,
        amount: p.net, account_id: '',
      })));

      setRevMappings(data.categories.map(c => ({
        label: c.name, gross: c.gross, disc: c.disc, net: c.net,
        amount: c.net, account_id: '',
        byPayment: c.byPayment || [],
        expanded: false,
      })));

      const totalDisc  = data.totalDisc  || 0;
      const totalBiaya = data.totalBiaya || 0;
      setDiscMappings(totalDisc > 0 ? [{ division_id: null, label: 'Diskon', amount: totalDisc, account_id: '' }] : []);
      setBiayaMappings(totalBiaya > 0 ? [{ label: 'Biaya Tambahan', amount: totalBiaya, account_id: '' }] : []);
      setBiayaRows(data.biayaRows || []);
      setSkippedRows(data.skippedRows || []);
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal membaca file');
    } finally {
      setUploading(false);
    }
  };

  const handleDrop = (e) => {
    e.preventDefault();
    const file = e.dataTransfer.files[0];
    if (file) handleFileChange(file);
  };

  const totalCash     = cashMappings.reduce((s, m) => s + Number(m.amount || 0), 0);
  const totalRevenue  = revMappings.reduce((s, m) => s + Number(m.amount || 0), 0);
  const totalDiscount = discMappings.reduce((s, m) => s + Number(m.amount || 0), 0);
  const totalBiaya    = biayaMappings.reduce((s, m) => s + Number(m.amount || 0), 0);
  // Commission preview (before Apply) — informational only, not part of balance
  const totalCommissionPreview = commissionMappings.reduce((s, cm) => {
    if (cm.applied_commission != null) return s + Number(cm.applied_commission);
    const cashMap = cashMappings.find(m => m.label === cm.payment_label);
    if (!cashMap || cm.real_amount === '') return s;
    const comm = Number(cashMap.amount) - Number(cm.real_amount);
    return s + (comm > 0 ? comm : 0);
  }, 0);
  // Revenue is net-based (gross - disc - commission), so discount is already embedded.
  // Balance: net_revenue = cash_received + biaya
  const balanced = totalRevenue > 0 && totalRevenue === totalCash + totalBiaya;

  const updateCash       = (i, field, val) => setCashMappings(ms => ms.map((m, idx) => idx === i ? { ...m, [field]: val } : m));
  const updateRev        = (i, field, val) => setRevMappings(ms => ms.map((m, idx) => idx === i ? { ...m, [field]: val } : m));
  const updateDisc       = (i, field, val) => setDiscMappings(ms => ms.map((m, idx) => idx === i ? { ...m, [field]: val } : m));
  const updateBiaya      = (i, field, val) => setBiayaMappings(ms => ms.map((m, idx) => idx === i ? { ...m, [field]: val } : m));
  const updateCommission = (i, field, val) => setCommissionMappings(ms => ms.map((m, idx) => idx === i ? { ...m, [field]: val } : m));
  const addCommission    = () => setCommissionMappings(ms => [...ms, { id: Date.now(), payment_label: '', real_amount: '', applied_commission: null }]);
  const removeCommission = (i) => setCommissionMappings(ms => ms.filter((_, idx) => idx !== i));
  const toggleExpand     = (i) => setRevMappings(ms => ms.map((m, idx) => idx === i ? { ...m, expanded: !m.expanded } : m));

  const handleApplyCommission = () => {
    const rows = commissionMappings.filter(cm => cm.payment_label && cm.real_amount !== '');
    if (rows.length === 0) return;

    // Build per-payment commission info: label → { realAmt, posAmt, comm, commPct }
    const commByLabel = {};
    rows.forEach(cm => {
      const cashMap = cashMappings.find(m => m.label === cm.payment_label);
      if (!cashMap) return;
      const posAmt  = Number(cashMap.amount);
      const realAmt = Number(cm.real_amount);
      const comm    = posAmt - realAmt;
      if (comm > 0 && posAmt > 0) {
        commByLabel[cm.payment_label] = { posAmt, realAmt, comm, commPct: comm / posAmt };
      }
    });

    if (Object.keys(commByLabel).length === 0) return;

    // Update cash amounts to real received amounts
    setCashMappings(ms => ms.map(m => {
      const c = commByLabel[m.label];
      return c ? { ...m, amount: c.realAmt } : m;
    }));

    // Reduce each revenue category by commission proportional to its net contribution per payment.
    // Base is net (gross - disc) so commission is always applied to the post-discount value.
    setRevMappings(ms => ms.map(m => {
      if (!m.byPayment || m.byPayment.length === 0) return m;

      let cut = 0;
      m.byPayment.forEach(bp => {
        const c = commByLabel[bp.payment];
        if (c) cut += (bp.gross - bp.disc) * c.commPct;
      });

      if (cut <= 0) return m;

      const net = Number(m.net ?? (m.gross - (m.disc || 0)));
      return {
        ...m,
        original_amount: net,
        amount: Math.round(net - cut),
      };
    }));

    // Store applied commission amount per commission row
    setCommissionMappings(ms => ms.map(cm => {
      const c = commByLabel[cm.payment_label];
      return c ? { ...cm, applied_commission: c.comm } : cm;
    }));

    setCommissionApplied(true);
  };

  // Build a lookup: payment label → cash account name
  const payToAccount = Object.fromEntries(
    cashMappings.map(m => [m.label, cashAccounts.find(a => a.id === m.account_id)])
  );

  // Build a lookup: payment label → { commAmt, commPct } for commission preview/applied
  const commByLabel = {};
  commissionMappings.forEach(cm => {
    if (!cm.payment_label) return;
    const cashMap = cashMappings.find(m => m.label === cm.payment_label);
    if (!cashMap) return;
    let commAmt, commPct;
    if (cm.applied_commission != null) {
      commAmt = cm.applied_commission;
      const posAmt = Number(cashMap.amount) + commAmt;
      commPct = posAmt > 0 ? commAmt / posAmt : 0;
    } else if (cm.real_amount !== '') {
      const posAmt = Number(cashMap.amount);
      commAmt = Math.max(0, posAmt - Number(cm.real_amount));
      commPct = posAmt > 0 ? commAmt / posAmt : 0;
    } else return;
    if (commAmt > 0) commByLabel[cm.payment_label] = { commAmt, commPct };
  });
  const hasAnyCommission = Object.keys(commByLabel).length > 0;

  const handleSubmit = async () => {
    setError('');
    if (!balanced) { setError('Total pendapatan (net) harus sama dengan kas + biaya tambahan'); return; }
    if (revMappings.some(m => !m.account_id))   { setError('Pilih akun pendapatan untuk semua kategori'); return; }
    if (cashMappings.some(m => !m.account_id))  { setError('Pilih akun kas untuk semua metode pembayaran'); return; }
    if (discMappings.some(m => !m.account_id))  { setError('Pilih akun pendapatan untuk semua baris diskon'); return; }
    if (biayaMappings.some(m => !m.account_id)) { setError('Pilih akun biaya tambahan'); return; }
    setSubmitting(true);
    try {
      await confirmPosImport({
        date: parsed.date,
        description,
        filename,
        revenue_mappings:  revMappings.map(m => ({ label: m.label, account_id: m.account_id, amount: Number(m.amount) })),
        discount_mappings: discMappings.map(m => ({ label: m.label, account_id: m.account_id, amount: -Math.abs(Number(m.amount)) })),
        expense_mappings:  biayaMappings.map(m => ({ label: m.label, account_id: m.account_id, amount: Number(m.amount) })),
        cash_mappings:     cashMappings.map(m => ({ label: m.label, account_id: m.account_id, amount: Number(m.amount) })),
      });
      setDone(true);
      setParsed(null);
      setRevMappings([]); setCashMappings([]); setDiscMappings([]); setBiayaMappings([]);
      getPosImports().then(r => setImports(r.data));
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan');
    } finally {
      setSubmitting(false);
    }
  };

  const reset = () => {
    setParsed(null); setRevMappings([]); setCashMappings([]); setDiscMappings([]); setBiayaMappings([]);
    setCommissionMappings([]); setCommissionApplied(false);
    setBiayaRows([]); setSkippedRows([]); setShowRawRows(false);
    setDivisionCategories([]);
    setError(''); setDone(false); setFilename('');
    setSelectedBranchId('');
    if (fileRef.current) fileRef.current.value = '';
  };

  return (
    <>
      <div className="page-header">
        <h1>Import Penjualan POS</h1>
        <Link to="/sales" className="btn btn-secondary">← Kembali ke Penjualan</Link>
      </div>

      {done && (
        <div style={{ background: '#e6f9f0', border: '1px solid #b2dfdb', borderRadius: '8px', padding: '1rem 1.5rem', marginBottom: '1.5rem', color: '#1b5e45', fontWeight: 500 }}>
          Import berhasil disimpan! Saldo akun telah diperbarui.
          <button onClick={reset} style={{ marginLeft: '1rem' }} className="btn btn-secondary btn-sm">Import Lagi</button>
        </div>
      )}

      {/* Upload area */}
      {!parsed && (
        <div className="card" style={{ marginBottom: '1.5rem' }}>
          <h2 style={{ marginBottom: '1rem', fontSize: '1rem' }}>Unggah File Excel POS</h2>
          <div
            onDrop={handleDrop}
            onDragOver={e => e.preventDefault()}
            onClick={() => fileRef.current?.click()}
            style={{
              border: '2px dashed #c8d8f0', borderRadius: '8px', padding: '2.5rem',
              textAlign: 'center', cursor: 'pointer', background: '#f5f8ff',
            }}
          >
            <div style={{ fontSize: '2rem', marginBottom: '0.5rem' }}>📂</div>
            <div style={{ fontWeight: 600, color: '#2c3e7a' }}>Klik atau seret file Excel ke sini</div>
            <div style={{ fontSize: '0.85rem', color: '#888', marginTop: '0.3rem' }}>Format: .xlsx — ekspor dari aplikasi POS</div>
            <input ref={fileRef} type="file" accept=".xlsx,.xls" style={{ display: 'none' }}
              onChange={e => handleFileChange(e.target.files[0])} />
          </div>
          {uploading && <p style={{ color: '#888', marginTop: '0.75rem', textAlign: 'center' }}>Memproses file...</p>}
          {error && <div className="error-msg" style={{ marginTop: '0.75rem' }}>{error}</div>}
        </div>
      )}

      {/* Review & mapping */}
      {parsed && (
        <>
          {/* Header info */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: '1rem' }}>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>File</div>
                <div style={{ fontWeight: 500 }}>{filename}</div>
              </div>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Tanggal Transaksi</div>
                <div style={{ fontWeight: 600 }}>{fmt(parsed.date)}</div>
              </div>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Total Penjualan Kotor</div>
                <div style={{ fontWeight: 700, fontSize: '1.1rem', color: '#27ae60' }}>{idr(parsed.totalGross)}</div>
              </div>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Total Diskon</div>
                <div style={{ fontWeight: 700, fontSize: '1.1rem', color: '#e74c3c' }}>{idr(parsed.totalDisc)}</div>
              </div>
              <div>
                <div style={{ fontSize: '0.75rem', color: '#999', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: '0.3rem' }}>Deskripsi Import</div>
                <input
                  value={description}
                  onChange={e => setDescription(e.target.value)}
                  style={{ padding: '0.3rem 0.6rem', border: '1px solid #ddd', borderRadius: '4px', fontSize: '0.9rem', minWidth: '200px' }}
                />
              </div>
              <button onClick={reset} className="btn btn-secondary btn-sm" style={{ alignSelf: 'flex-end' }}>Ganti File</button>
            </div>

            {/* Auto-account row */}
            <div style={{ marginTop: '1rem', paddingTop: '1rem', borderTop: '1px solid #f0f0f0' }}>
              <div className="filters" style={{ alignItems: 'flex-end' }}>
                <div>
                  <label style={{ display: 'block', fontSize: '0.85rem', fontWeight: 500, color: '#444', marginBottom: '0.3rem' }}>Terapkan akun dari cabang</label>
                  <select
                    value={selectedBranchId}
                    onChange={e => setSelectedBranchId(e.target.value)}
                    style={{ minWidth: '200px' }}
                  >
                    <option value="">— Pilih cabang —</option>
                    {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
                  </select>
                </div>
                <button
                  onClick={handleApplyAccounts}
                  disabled={!selectedBranchId}
                  className="btn btn-primary btn-sm"
                >
                  Terapkan Akun Otomatis
                </button>
                {divisionCategories.length > 0 && (
                  <span style={{ fontSize: '0.78rem', color: '#2e7d32', background: '#e8f5e9', padding: '0.25rem 0.6rem', borderRadius: '4px', fontWeight: 500 }}>
                    ✓ {divisionCategories.length} kategori dari {branchDivisions.length} divisi diterapkan
                  </span>
                )}
              </div>
            </div>
          </div>

          {/* ── SECTION 1: Cash / Payment ── */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <div className="card-header" style={{ marginBottom: '1rem' }}>
              <div>
                <h2>1. Kas / Pembayaran (Debit)</h2>
                <p style={{ fontSize: '0.82rem', color: '#888', margin: '0.25rem 0 0' }}>
                  Petakan setiap metode pembayaran ke akun aset kas yang sesuai.
                </p>
              </div>
              <span style={{ fontWeight: 700, color: '#2c6fc2', fontSize: '1.1rem' }}>{idr(totalCash)}</span>
            </div>
            <table>
              <thead>
                <tr>
                  <th>Akun Kas</th>
                  <th style={{ textAlign: 'right' }}>Kotor</th>
                  <th style={{ textAlign: 'right' }}>Diskon</th>
                  <th style={{ textAlign: 'right' }}>Net Diterima</th>
                  <th>Pilih Akun</th>
                </tr>
              </thead>
              <tbody>
                {cashMappings.map((m, i) => {
                  const mappedAcct = cashAccounts.find(a => a.id === m.account_id);
                  return (
                  <tr key={i}>
                    <td>
                      {mappedAcct ? (
                        <div>
                          <div style={{ fontWeight: 600, fontSize: '0.9rem' }}>{mappedAcct.name}</div>
                          <div style={{ fontSize: '0.75rem', color: '#aaa' }}>{m.label}</div>
                        </div>
                      ) : (
                        <span style={{ fontWeight: 500, color: '#888' }}>{m.label}</span>
                      )}
                    </td>
                    <td style={{ textAlign: 'right', color: '#555', fontSize: '0.85rem' }}>{idr(m.gross)}</td>
                    <td style={{ textAlign: 'right', color: m.disc > 0 ? '#e74c3c' : '#ccc', fontSize: '0.85rem' }}>
                      {m.disc > 0 ? `-${idr(m.disc)}` : '—'}
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      <input
                        type="number"
                        value={m.amount}
                        onChange={e => updateCash(i, 'amount', e.target.value)}
                        style={{ width: '110px', textAlign: 'right', padding: '0.25rem 0.4rem', border: '1px solid #ddd', borderRadius: '4px', fontSize: '0.88rem' }}
                      />
                    </td>
                    <td>
                      <select
                        value={m.account_id}
                        onChange={e => updateCash(i, 'account_id', e.target.value)}
                        style={{ width: '100%', fontSize: '0.85rem' }}
                        required
                      >
                        <option value="">— Pilih akun —</option>
                        {cashAccounts.map(a => (
                          <option key={a.id} value={a.id}>
                            {a.account_number ? `${a.account_number} · ` : ''}{a.name}
                          </option>
                        ))}
                      </select>
                    </td>
                  </tr>
                  );
                })}
              </tbody>
              <tfoot>
                <tr>
                  <td colSpan={3} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>Total:</td>
                  <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#2c6fc2' }}>{idr(totalCash)}</td>
                  <td />
                </tr>
              </tfoot>
            </table>
          </div>

          {/* ── SECTION 2: Commission ── */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <div className="card-header" style={{ marginBottom: '1rem' }}>
              <div>
                <h2>2. Komisi Platform (Debit Beban)</h2>
                <p style={{ fontSize: '0.82rem', color: '#888', margin: '0.25rem 0 0' }}>
                  Input jumlah bersih yang benar-benar diterima per metode pembayaran. Komisi akan dicatat sebagai beban dan dikurangi dari kas saat diterapkan.
                </p>
              </div>
              {totalCommissionPreview > 0 && (
                <span style={{ fontWeight: 700, color: '#8e44ad', fontSize: '1.1rem' }}>{idr(totalCommissionPreview)}</span>
              )}
            </div>

            {commissionMappings.length > 0 ? (
              <table style={{ marginBottom: '0.75rem' }}>
                <thead>
                  <tr>
                    <th>Metode Pembayaran</th>
                    <th style={{ textAlign: 'right' }}>Jumlah POS</th>
                    <th style={{ textAlign: 'right' }}>Diterima Bersih</th>
                    <th style={{ textAlign: 'right' }}>Komisi</th>
                    <th style={{ textAlign: 'right' }}>% Komisi</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {commissionMappings.map((cm, i) => {
                    const cashMap = cashMappings.find(m => m.label === cm.payment_label);
                    const posAmt  = cashMap ? Number(cashMap.amount) : 0;
                    const realAmt = Number(cm.real_amount || 0);
                    const commAmt = cm.applied_commission != null
                      ? cm.applied_commission
                      : (posAmt > 0 && cm.real_amount !== '' ? Math.max(0, posAmt - realAmt) : 0);
                    const commPct = posAmt > 0 && commAmt > 0 ? (commAmt / posAmt * 100) : 0;
                    return (
                      <tr key={cm.id}>
                        <td>
                          <select
                            value={cm.payment_label}
                            onChange={e => updateCommission(i, 'payment_label', e.target.value)}
                            style={{ width: '100%', fontSize: '0.85rem' }}
                            disabled={cm.applied_commission != null}
                          >
                            <option value="">— Pilih metode bayar —</option>
                            {cashMappings.map(m => (
                              <option key={m.label} value={m.label}>{m.label}</option>
                            ))}
                          </select>
                        </td>
                        <td style={{ textAlign: 'right', color: '#555', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>
                          {posAmt > 0 ? idr(posAmt) : <span style={{ color: '#ccc' }}>—</span>}
                        </td>
                        <td style={{ textAlign: 'right' }}>
                          <input
                            type="number"
                            value={cm.real_amount}
                            onChange={e => updateCommission(i, 'real_amount', e.target.value)}
                            disabled={cm.applied_commission != null}
                            style={{ width: '120px', textAlign: 'right', padding: '0.25rem 0.4rem', border: '1px solid #ddd', borderRadius: '4px', fontSize: '0.88rem' }}
                          />
                        </td>
                        <td style={{ textAlign: 'right', fontWeight: 600, color: commAmt > 0 ? '#8e44ad' : '#ccc', fontSize: '0.88rem', whiteSpace: 'nowrap' }}>
                          {commAmt > 0 ? idr(commAmt) : '—'}
                        </td>
                        <td style={{ textAlign: 'right', fontSize: '0.88rem', whiteSpace: 'nowrap' }}>
                          {commPct > 0 ? (
                            <span style={{ background: '#f3e9fd', color: '#6c3483', padding: '0.15rem 0.5rem', borderRadius: '4px', fontWeight: 600 }}>
                              {commPct.toFixed(2)}%
                            </span>
                          ) : <span style={{ color: '#ccc' }}>—</span>}
                        </td>
                        <td>
                          <button
                            onClick={() => removeCommission(i)}
                            className="btn btn-danger btn-sm"
                            disabled={cm.applied_commission != null}
                          >×</button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            ) : (
              <p style={{ color: '#bbb', fontSize: '0.85rem', marginBottom: '0.75rem' }}>Belum ada baris komisi. Klik tombol di bawah untuk menambahkan.</p>
            )}

            <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', flexWrap: 'wrap' }}>
              <button onClick={addCommission} className="btn btn-secondary btn-sm">
                + Tambah Baris Komisi
              </button>
              {commissionMappings.length > 0 && !commissionApplied && (
                <button
                  onClick={handleApplyCommission}
                  className="btn btn-primary btn-sm"
                  disabled={commissionMappings.every(cm => !cm.payment_label || cm.real_amount === '')}
                >
                  Terapkan Komisi
                </button>
              )}
              {commissionApplied && (
                <span style={{ fontSize: '0.78rem', color: '#6c3483', background: '#f3e9fd', padding: '0.25rem 0.65rem', borderRadius: '4px', fontWeight: 600 }}>
                  ✓ Komisi diterapkan — kas disesuaikan ke jumlah bersih
                </span>
              )}
            </div>
          </div>

          {/* ── SECTION 3: Revenue Categories ── */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <div className="card-header" style={{ marginBottom: '1rem' }}>
              <div>
                <h2>3. Pendapatan per Kategori (Kredit)</h2>
                <p style={{ fontSize: '0.82rem', color: '#888', margin: '0.25rem 0 0' }}>
                  Klik kategori untuk melihat komposisi metode pembayaran. Jumlah adalah penjualan kotor (sebelum diskon).
                  {commissionApplied && ' Jumlah telah disesuaikan dengan komisi.'}
                </p>
              </div>
              <span style={{ fontWeight: 700, color: '#27ae60', fontSize: '1.1rem' }}>{idr(totalRevenue)}</span>
            </div>
            <table>
              <thead>
                <tr>
                  <th style={{ width: '1.5rem' }}></th>
                  <th>Kategori Produk</th>
                  <th style={{ textAlign: 'right' }}>Kotor POS</th>
                  <th style={{ textAlign: 'right' }}>Diskon</th>
                  {commissionApplied && <th style={{ textAlign: 'right' }}>Potongan Komisi</th>}
                  <th style={{ textAlign: 'right' }}>{commissionApplied ? 'Jumlah (Net - Komisi)' : 'Jumlah (Net)'}</th>
                  <th>Akun Pendapatan</th>
                </tr>
              </thead>
              <tbody>
                {revMappings.map((m, i) => {
                  const commCut = m.original_amount != null ? m.original_amount - Number(m.amount) : 0;
                  return (
                  <>
                    <tr key={i} style={{ cursor: 'pointer' }} onClick={() => toggleExpand(i)}>
                      <td style={{ color: '#aaa', fontSize: '0.75rem', textAlign: 'center', userSelect: 'none' }}>
                        {m.expanded ? '▼' : '▶'}
                      </td>
                      <td style={{ fontWeight: 500 }}>{m.label}</td>
                      <td style={{ textAlign: 'right', color: '#555', fontSize: '0.85rem' }}>{idr(m.gross)}</td>
                      <td style={{ textAlign: 'right', color: m.disc > 0 ? '#e74c3c' : '#ccc', fontSize: '0.85rem' }}>
                        {m.disc > 0 ? `-${idr(m.disc)}` : '—'}
                      </td>
                      {commissionApplied && (
                        <td style={{ textAlign: 'right', color: commCut > 0 ? '#8e44ad' : '#ccc', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>
                          {commCut > 0 ? (
                            <span title={`Asli: ${idr(m.original_amount)}`}>-{idr(commCut)}</span>
                          ) : '—'}
                        </td>
                      )}
                      <td style={{ textAlign: 'right' }} onClick={e => e.stopPropagation()}>
                        <input
                          type="number"
                          value={m.amount}
                          onChange={e => updateRev(i, 'amount', e.target.value)}
                          style={{ width: '110px', textAlign: 'right', padding: '0.25rem 0.4rem', border: '1px solid #ddd', borderRadius: '4px', fontSize: '0.88rem' }}
                        />
                      </td>
                      <td onClick={e => e.stopPropagation()}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                          <select
                            value={m.account_id}
                            onChange={e => updateRev(i, 'account_id', e.target.value)}
                            style={{ flex: 1, fontSize: '0.85rem' }}
                            required
                          >
                            <option value="">— Pilih akun —</option>
                            {revenueAccounts.map(a => (
                              <option key={a.id} value={a.id}>
                                {a.account_number ? `${a.account_number} · ` : ''}{a.name}
                              </option>
                            ))}
                          </select>
                          {m.account_id && divisionCategories.some(c => c.name.toLowerCase() === m.label.toLowerCase()) && (
                            <span title="Diisi otomatis dari kategori divisi" style={{ fontSize: '0.7rem', background: '#e8f5e9', color: '#2e7d32', borderRadius: '3px', padding: '0.1rem 0.35rem', whiteSpace: 'nowrap', fontWeight: 600 }}>
                              ✓ auto
                            </span>
                          )}
                        </div>
                      </td>
                    </tr>
                    {m.expanded && m.byPayment.length > 0 && (
                      <tr key={`${i}-expand`}>
                        <td colSpan={commissionApplied ? 7 : 6} style={{ background: '#f8f9ff', padding: '0.5rem 1rem 0.75rem 2.5rem' }}>
                          <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#555', marginBottom: '0.4rem', textTransform: 'uppercase', letterSpacing: '0.3px' }}>
                            Komposisi per metode pembayaran — {m.label}
                          </div>
                          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                            <thead>
                              <tr style={{ borderBottom: '1px solid #e8eeff' }}>
                                <th style={{ textAlign: 'left', fontWeight: 600, fontSize: '0.78rem', color: '#666', padding: '0.25rem 0.5rem' }}>Metode Bayar</th>
                                <th style={{ textAlign: 'right', fontWeight: 600, fontSize: '0.78rem', color: '#666', padding: '0.25rem 0.5rem' }}>Kotor</th>
                                <th style={{ textAlign: 'right', fontWeight: 600, fontSize: '0.78rem', color: '#666', padding: '0.25rem 0.5rem' }}>Diskon</th>
                                <th style={{ textAlign: 'right', fontWeight: 600, fontSize: '0.78rem', color: '#666', padding: '0.25rem 0.5rem' }}>Net</th>
                                {hasAnyCommission && <th style={{ textAlign: 'right', fontWeight: 600, fontSize: '0.78rem', color: '#8e44ad', padding: '0.25rem 0.5rem' }}>Komisi</th>}
                                <th style={{ textAlign: 'left', fontWeight: 600, fontSize: '0.78rem', color: '#666', padding: '0.25rem 0.5rem' }}>Akun Kas</th>
                              </tr>
                            </thead>
                            <tbody>
                              {m.byPayment.map((bp, j) => {
                                const acct = payToAccount[bp.payment];
                                const c = commByLabel[bp.payment];
                                const bpComm = c ? Math.round((bp.gross - bp.disc) * c.commPct) : 0;
                                return (
                                  <tr key={j} style={{ borderBottom: '1px solid #f0f0f0' }}>
                                    <td style={{ padding: '0.3rem 0.5rem', fontSize: '0.82rem', fontWeight: 500 }}>{bp.payment}</td>
                                    <td style={{ padding: '0.3rem 0.5rem', textAlign: 'right', fontSize: '0.82rem' }}>{idr(bp.gross)}</td>
                                    <td style={{ padding: '0.3rem 0.5rem', textAlign: 'right', fontSize: '0.82rem', color: bp.disc > 0 ? '#e74c3c' : '#ccc' }}>
                                      {bp.disc > 0 ? `-${idr(bp.disc)}` : '—'}
                                    </td>
                                    <td style={{ padding: '0.3rem 0.5rem', textAlign: 'right', fontSize: '0.82rem', fontWeight: 600 }}>{idr(bp.gross - bp.disc)}</td>
                                    {hasAnyCommission && (
                                      <td style={{ padding: '0.3rem 0.5rem', textAlign: 'right', fontSize: '0.82rem', color: bpComm > 0 ? '#8e44ad' : '#ccc', whiteSpace: 'nowrap' }}>
                                        {bpComm > 0 ? (
                                          <span title={`${(c.commPct * 100).toFixed(2)}% dari net`}>-{idr(bpComm)}</span>
                                        ) : '—'}
                                      </td>
                                    )}
                                    <td style={{ padding: '0.3rem 0.5rem', fontSize: '0.78rem' }}>
                                      {acct
                                        ? <span style={{ color: '#27ae60' }}>{acct.account_number ? `${acct.account_number} · ` : ''}{acct.name}</span>
                                        : <span style={{ color: '#f39c12' }}>Belum dipetakan</span>
                                      }
                                    </td>
                                  </tr>
                                );
                              })}
                            </tbody>
                            <tfoot>
                              <tr>
                                <td style={{ padding: '0.3rem 0.5rem', fontWeight: 700, fontSize: '0.82rem' }}>Total</td>
                                <td style={{ padding: '0.3rem 0.5rem', textAlign: 'right', fontWeight: 700, fontSize: '0.82rem' }}>{idr(m.byPayment.reduce((s, b) => s + b.gross, 0))}</td>
                                <td style={{ padding: '0.3rem 0.5rem', textAlign: 'right', fontWeight: 700, fontSize: '0.82rem', color: '#e74c3c' }}>
                                  {m.byPayment.some(b => b.disc > 0) ? `-${idr(m.byPayment.reduce((s, b) => s + b.disc, 0))}` : '—'}
                                </td>
                                <td style={{ padding: '0.3rem 0.5rem', textAlign: 'right', fontWeight: 700, fontSize: '0.82rem' }}>{idr(m.byPayment.reduce((s, b) => s + b.gross - b.disc, 0))}</td>
                                {hasAnyCommission && (
                                  <td style={{ padding: '0.3rem 0.5rem', textAlign: 'right', fontWeight: 700, fontSize: '0.82rem', color: '#8e44ad', whiteSpace: 'nowrap' }}>
                                    {(() => {
                                      const total = m.byPayment.reduce((s, b) => {
                                        const c = commByLabel[b.payment];
                                        return s + (c ? Math.round((b.gross - b.disc) * c.commPct) : 0);
                                      }, 0);
                                      return total > 0 ? `-${idr(total)}` : '—';
                                    })()}
                                  </td>
                                )}
                                <td />
                              </tr>
                            </tfoot>
                          </table>
                        </td>
                      </tr>
                    )}
                  </>
                  );
                })}
              </tbody>
              <tfoot>
                <tr>
                  <td colSpan={commissionApplied ? 5 : 4} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>Total:</td>
                  <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#27ae60' }}>{idr(totalRevenue)}</td>
                  <td />
                </tr>
              </tfoot>
            </table>
          </div>

          {/* ── SECTION 4: Discount ── */}
          {discMappings.length > 0 && (
            <div className="card" style={{ marginBottom: '1.5rem' }}>
              <div className="card-header" style={{ marginBottom: '1rem' }}>
                <div>
                  <h2>4. Diskon (Pengurangan Pendapatan)</h2>
                  <p style={{ fontSize: '0.82rem', color: '#888', margin: '0.25rem 0 0' }}>
                    Diskon dicatat sebagai pengurang akun pendapatan (negatif). Terapkan akun otomatis untuk memecah per divisi.
                  </p>
                </div>
                <span style={{ fontWeight: 700, color: '#e74c3c', fontSize: '1.1rem' }}>{idr(totalDiscount)}</span>
              </div>
              <table>
                <thead>
                  <tr>
                    <th>Divisi / Keterangan</th>
                    <th style={{ textAlign: 'right' }}>Jumlah</th>
                    <th>Akun Pendapatan (Pengurang)</th>
                  </tr>
                </thead>
                <tbody>
                  {discMappings.map((m, i) => (
                    <tr key={i} style={{ background: m.division_id === null ? '#fffbf0' : undefined }}>
                      <td>
                        {m.division_id !== null ? (
                          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}>
                            <span style={{ background: '#f0f4ff', color: '#2c5cc5', borderRadius: '4px', padding: '0.1rem 0.45rem', fontSize: '0.78rem', fontWeight: 600 }}>
                              {m.label}
                            </span>
                            <span style={{ fontSize: '0.75rem', color: '#888' }}>divisi</span>
                          </span>
                        ) : (
                          <span style={{ color: '#b45309', fontWeight: 500, fontSize: '0.88rem' }}>
                            {m.label}
                          </span>
                        )}
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        <input
                          type="number"
                          value={m.amount}
                          onChange={e => updateDisc(i, 'amount', e.target.value)}
                          style={{ width: '110px', textAlign: 'right', padding: '0.25rem 0.4rem', border: '1px solid #ddd', borderRadius: '4px', fontSize: '0.88rem' }}
                        />
                      </td>
                      <td>
                        <select
                          value={m.account_id}
                          onChange={e => updateDisc(i, 'account_id', e.target.value)}
                          style={{ width: '100%', fontSize: '0.85rem' }}
                          required
                        >
                          <option value="">— Pilih akun —</option>
                          {revenueAccounts.map(a => (
                            <option key={a.id} value={a.id}>
                              {a.account_number ? `${a.account_number} · ` : ''}{a.name}
                            </option>
                          ))}
                        </select>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* ── SECTION 5: Biaya Tambahan ── */}
          {biayaMappings.length > 0 && (
            <div className="card" style={{ marginBottom: '1.5rem' }}>
              <div className="card-header" style={{ marginBottom: '1rem' }}>
                <div>
                  <h2>5. Biaya Tambahan (Debit Beban)</h2>
                  <p style={{ fontSize: '0.82rem', color: '#888', margin: '0.25rem 0 0' }}>
                    Dari kolom AD file POS. Dicatat ke akun beban divisi lain-lain secara otomatis saat cabang dipilih.
                  </p>
                </div>
                <span style={{ fontWeight: 700, color: '#e67e22', fontSize: '1.1rem' }}>{idr(totalBiaya)}</span>
              </div>

              {/* Account mapping row */}
              <table style={{ marginBottom: biayaRows.length > 0 ? '1.5rem' : 0 }}>
                <thead>
                  <tr>
                    <th>Keterangan</th>
                    <th style={{ textAlign: 'right' }}>Jumlah</th>
                    <th>Akun Beban</th>
                  </tr>
                </thead>
                <tbody>
                  {biayaMappings.map((m, i) => (
                    <tr key={i}>
                      <td style={{ fontWeight: 500, fontSize: '0.88rem' }}>{m.label}</td>
                      <td style={{ textAlign: 'right' }}>
                        <input
                          type="number"
                          value={m.amount}
                          onChange={e => updateBiaya(i, 'amount', e.target.value)}
                          style={{ width: '110px', textAlign: 'right', padding: '0.25rem 0.4rem', border: '1px solid #ddd', borderRadius: '4px', fontSize: '0.88rem' }}
                        />
                      </td>
                      <td>
                        <select
                          value={m.account_id}
                          onChange={e => updateBiaya(i, 'account_id', e.target.value)}
                          style={{ width: '100%', fontSize: '0.85rem' }}
                          required
                        >
                          <option value="">— Pilih akun —</option>
                          {expenseAccounts.map(a => (
                            <option key={a.id} value={a.id}>
                              {a.account_number ? `${a.account_number} · ` : ''}{a.name}
                            </option>
                          ))}
                        </select>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>

              {/* Reference table: individual rows from column C + AD */}
              {biayaRows.length > 0 && (
                <>
                  <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#666', textTransform: 'uppercase', letterSpacing: '0.3px', marginBottom: '0.5rem' }}>
                    Referensi per transaksi ({biayaRows.length} baris)
                  </div>
                  <div style={{ maxHeight: '280px', overflowY: 'auto', border: '1px solid #f0f0f0', borderRadius: '6px' }}>
                    <table style={{ marginBottom: 0 }}>
                      <thead>
                        <tr style={{ position: 'sticky', top: 0, background: '#fff', zIndex: 1 }}>
                          <th>No Penjualan</th>
                          <th>Kategori</th>
                          <th>Produk</th>
                          <th style={{ textAlign: 'right' }}>Biaya Tambahan</th>
                        </tr>
                      </thead>
                      <tbody>
                        {biayaRows.map((r, i) => (
                          <tr key={i}>
                            <td style={{ fontFamily: 'monospace', fontSize: '0.82rem', color: '#4f8ef7', whiteSpace: 'nowrap' }}>{r.no_penjualan || '—'}</td>
                            <td>
                              <span style={{ background: '#f0f4ff', color: '#2c5cc5', padding: '0.1rem 0.45rem', borderRadius: '4px', fontSize: '0.78rem', fontWeight: 600 }}>
                                {r.category}
                              </span>
                            </td>
                            <td style={{ color: '#555', fontSize: '0.85rem' }}>{r.product || '—'}</td>
                            <td style={{ textAlign: 'right', fontWeight: 600, color: '#e67e22', fontSize: '0.85rem', whiteSpace: 'nowrap' }}>{idr(r.biaya)}</td>
                          </tr>
                        ))}
                      </tbody>
                      <tfoot>
                        <tr>
                          <td colSpan={3} style={{ textAlign: 'right', fontWeight: 600, color: '#555', paddingTop: '0.75rem' }}>Total:</td>
                          <td style={{ textAlign: 'right', fontWeight: 700, color: '#e67e22', paddingTop: '0.75rem', whiteSpace: 'nowrap' }}>{idr(biayaRows.reduce((s, r) => s + r.biaya, 0))}</td>
                        </tr>
                      </tfoot>
                    </table>
                  </div>
                </>
              )}
            </div>
          )}

          {/* Balance check + submit */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '1.5rem', flexWrap: 'wrap' }}>
              <div style={{ display: 'flex', gap: '1.5rem', fontSize: '0.88rem', color: '#555', alignItems: 'center', flexWrap: 'wrap' }}>
                <span>Pendapatan (Net): <strong style={{ color: '#27ae60' }}>{idr(totalRevenue)}</strong></span>
                <span style={{ color: '#aaa' }}>=</span>
                <span>Kas: <strong style={{ color: '#2c6fc2' }}>{idr(totalCash)}</strong></span>
                {totalBiaya > 0 && <>
                  <span style={{ color: '#aaa' }}>+</span>
                  <span>Biaya: <strong style={{ color: '#e67e22' }}>{idr(totalBiaya)}</strong></span>
                </>}
                {commissionApplied && totalCommissionPreview > 0 && (
                  <span style={{ fontSize: '0.78rem', color: '#8e44ad', background: '#f3e9fd', padding: '0.2rem 0.5rem', borderRadius: '4px', whiteSpace: 'nowrap' }}>
                    komisi {idr(totalCommissionPreview)} diserap
                  </span>
                )}
              </div>
              <div style={{
                display: 'flex', alignItems: 'center', gap: '0.5rem',
                padding: '0.5rem 0.9rem', borderRadius: '6px',
                background: balanced ? '#e6f9f0' : '#fdecea',
                color: balanced ? '#1b5e45' : '#c0392b',
                fontWeight: 600, fontSize: '0.9rem',
              }}>
                {balanced ? '✓ Seimbang' : '✗ Tidak seimbang'}
              </div>
              {error && <div style={{ color: '#e74c3c', fontWeight: 500, fontSize: '0.88rem' }}>{error}</div>}
              <button
                onClick={handleSubmit}
                disabled={submitting || !balanced}
                className="btn btn-primary"
                style={{ marginLeft: 'auto' }}
              >
                {submitting ? 'Menyimpan...' : 'Konfirmasi & Simpan Import'}
              </button>
            </div>
          </div>

          {/* Skipped (non-"dibayar") rows — review only, not included in report */}
          {skippedRows.length > 0 && (
            <div className="card" style={{ marginBottom: '1.5rem', borderLeft: '3px solid #f39c12' }}>
              <div className="card-header" style={{ marginBottom: '0.75rem' }}>
                <div>
                  <h2 style={{ color: '#b45309' }}>Transaksi Tidak Dibayar ({skippedRows.length} baris)</h2>
                  <p style={{ fontSize: '0.82rem', color: '#888', margin: '0.25rem 0 0' }}>
                    Baris berikut dikecualikan dari laporan karena status bukan "dibayar". Ditampilkan hanya untuk tinjauan.
                  </p>
                </div>
                <span style={{ fontWeight: 700, color: '#b45309', fontSize: '1rem' }}>
                  {idr(skippedRows.reduce((s, r) => s + r.gross, 0))} kotor
                </span>
              </div>
              <table>
                <thead>
                  <tr>
                    <th>No Penjualan</th>
                    <th>Status</th>
                    <th>Kategori</th>
                    <th>Produk</th>
                    <th>Metode Bayar</th>
                    <th style={{ textAlign: 'right' }}>Kotor</th>
                    <th style={{ textAlign: 'right' }}>Diskon</th>
                    <th style={{ textAlign: 'right' }}>Bersih</th>
                  </tr>
                </thead>
                <tbody>
                  {skippedRows.map((row, i) => (
                    <tr key={i} style={{ background: '#fffbf0' }}>
                      <td style={{ fontFamily: 'monospace', fontSize: '0.78rem', color: '#4f8ef7', whiteSpace: 'nowrap' }}>{row.no_penjualan || '—'}</td>
                      <td>
                        <span style={{ background: '#fef3c7', color: '#92400e', padding: '0.1rem 0.45rem', borderRadius: '4px', fontSize: '0.78rem', fontWeight: 600 }}>
                          {row.status}
                        </span>
                      </td>
                      <td style={{ fontSize: '0.85rem' }}>
                        <span style={{ background: '#f0f4ff', color: '#2c5cc5', padding: '0.1rem 0.45rem', borderRadius: '4px', fontSize: '0.78rem', fontWeight: 600 }}>
                          {row.category}
                        </span>
                      </td>
                      <td style={{ color: '#555', fontSize: '0.85rem' }}>{row.product || '—'}</td>
                      <td style={{ fontSize: '0.82rem', color: '#666' }}>{row.payment}</td>
                      <td style={{ textAlign: 'right', fontSize: '0.85rem', color: '#aaa' }}>{idr(row.gross)}</td>
                      <td style={{ textAlign: 'right', fontSize: '0.85rem', color: row.disc > 0 ? '#e74c3c' : '#ccc' }}>
                        {row.disc > 0 ? `-${idr(row.disc)}` : '—'}
                      </td>
                      <td style={{ textAlign: 'right', fontSize: '0.88rem', color: '#aaa' }}>{idr(row.net)}</td>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr>
                    <td colSpan={5} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>Total (dikecualikan):</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#aaa' }}>{idr(skippedRows.reduce((s, r) => s + r.gross, 0))}</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#e74c3c' }}>-{idr(skippedRows.reduce((s, r) => s + r.disc, 0))}</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#aaa' }}>{idr(skippedRows.reduce((s, r) => s + r.net, 0))}</td>
                  </tr>
                </tfoot>
              </table>
            </div>
          )}

          {/* Raw transactions from file — collapsible */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <div className="card-header" style={{ marginBottom: showRawRows ? '0.75rem' : 0, cursor: 'pointer' }} onClick={() => setShowRawRows(v => !v)}>
              <h2>{parsed.rows.length} baris transaksi dari file</h2>
              <span style={{ fontSize: '0.75rem', color: '#aaa', userSelect: 'none' }}>{showRawRows ? '▲ Sembunyikan' : '▼ Tampilkan'}</span>
            </div>
            {showRawRows && (
              <table>
                <thead>
                  <tr>
                    <th>No Penjualan</th>
                    <th>Kategori Produk</th>
                    <th>Nama Produk</th>
                    <th>Metode Bayar</th>
                    <th style={{ textAlign: 'right' }}>Penjualan Kotor</th>
                    <th style={{ textAlign: 'right' }}>Diskon</th>
                    <th style={{ textAlign: 'right' }}>Bersih</th>
                  </tr>
                </thead>
                <tbody>
                  {parsed.rows.map((row, i) => (
                    <tr key={i}>
                      <td style={{ fontFamily: 'monospace', fontSize: '0.78rem', color: '#4f8ef7', whiteSpace: 'nowrap' }}>{row.no_penjualan || '—'}</td>
                      <td style={{ fontSize: '0.85rem' }}>
                        <span style={{ background: '#f0f4ff', color: '#2c5cc5', padding: '0.1rem 0.45rem', borderRadius: '4px', fontSize: '0.78rem', fontWeight: 600 }}>
                          {row.category}
                        </span>
                      </td>
                      <td style={{ color: '#555', fontSize: '0.85rem' }}>{row.product || '—'}</td>
                      <td style={{ fontSize: '0.82rem', color: '#666' }}>{row.payment}</td>
                      <td style={{ textAlign: 'right', fontSize: '0.85rem' }}>{idr(row.gross)}</td>
                      <td style={{ textAlign: 'right', fontSize: '0.85rem', color: row.disc > 0 ? '#e74c3c' : '#ccc' }}>
                        {row.disc > 0 ? `-${idr(row.disc)}` : '—'}
                      </td>
                      <td style={{ textAlign: 'right', fontWeight: 600, fontSize: '0.88rem' }}>{idr(row.net)}</td>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr>
                    <td colSpan={4} style={{ textAlign: 'right', fontWeight: 600, paddingTop: '0.75rem', color: '#555' }}>Total:</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem' }}>{idr(parsed.rows.reduce((s, r) => s + r.gross, 0))}</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#e74c3c' }}>-{idr(parsed.rows.reduce((s, r) => s + r.disc, 0))}</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, paddingTop: '0.75rem', color: '#27ae60' }}>{idr(parsed.total)}</td>
                  </tr>
                </tfoot>
              </table>
            )}
          </div>
        </>
      )}

      {/* Import history */}
      {imports.length > 0 && (
        <div className="card">
          <div className="card-header" style={{ marginBottom: '0.75rem' }}>
            <h2>Riwayat Import ({imports.length})</h2>
          </div>
          <table>
            <thead>
              <tr>
                <th>Tanggal</th>
                <th>Deskripsi</th>
                <th style={{ textAlign: 'right' }}>Total</th>
                <th>File Sumber</th>
                <th>Dicatat oleh</th>
                <th>Waktu Dicatat</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {imports.map(imp => (
                <>
                  <tr key={imp.id} style={{ cursor: 'pointer' }} onClick={() => setExpandedImport(expandedImport === imp.id ? null : imp.id)}>
                    <td style={{ color: '#888', fontSize: '0.85rem' }}>{fmt(imp.date)}</td>
                    <td style={{ fontWeight: 500 }}>{imp.description}</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: '#27ae60' }}>{idr(imp.total_amount)}</td>
                    <td style={{ color: '#888', fontSize: '0.82rem' }}>{imp.source_file || '—'}</td>
                    <td style={{ color: '#888', fontSize: '0.82rem' }}>{imp.created_by_name || '—'}</td>
                    <td style={{ color: '#888', fontSize: '0.82rem' }}>{new Date(imp.created_at).toLocaleString('id-ID')}</td>
                    <td style={{ color: '#aaa', fontSize: '0.75rem', userSelect: 'none' }}>{expandedImport === imp.id ? '▼' : '▶'}</td>
                  </tr>
                  {expandedImport === imp.id && (
                    <tr key={`${imp.id}-detail`}>
                      <td colSpan={7} style={{ background: '#f8f9ff', padding: '0.75rem 1.5rem' }}>
                        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr 1fr', gap: '1.5rem' }}>
                          <div>
                            <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#666', textTransform: 'uppercase', marginBottom: '0.5rem' }}>Pendapatan (Kredit)</div>
                            {imp.lines.filter(l => l.line_type === 'revenue').map(l => (
                              <div key={l.id} style={{ display: 'flex', justifyContent: 'space-between', padding: '0.2rem 0', fontSize: '0.85rem', gap: '0.5rem' }}>
                                <span style={{ flex: 1 }}>{l.label}</span>
                                <span style={{ color: '#888', fontSize: '0.78rem' }}>{l.account_name}</span>
                                <span style={{ fontWeight: 600, color: '#27ae60' }}>{idr(l.amount)}</span>
                              </div>
                            ))}
                          </div>
                          <div>
                            <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#666', textTransform: 'uppercase', marginBottom: '0.5rem' }}>Diskon (Pengurang Pendapatan)</div>
                            {imp.lines.filter(l => l.line_type === 'discount').map(l => (
                              <div key={l.id} style={{ display: 'flex', justifyContent: 'space-between', padding: '0.2rem 0', fontSize: '0.85rem', gap: '0.5rem' }}>
                                <span style={{ flex: 1 }}>{l.label}</span>
                                <span style={{ color: '#888', fontSize: '0.78rem' }}>{l.account_name}</span>
                                <span style={{ fontWeight: 600, color: '#e74c3c' }}>{idr(l.amount)}</span>
                              </div>
                            ))}
                            {imp.lines.filter(l => l.line_type === 'discount').length === 0 && (
                              <span style={{ color: '#ccc', fontSize: '0.82rem' }}>Tidak ada diskon</span>
                            )}
                          </div>
                          <div>
                            <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#666', textTransform: 'uppercase', marginBottom: '0.5rem' }}>Biaya Tambahan (Debit)</div>
                            {imp.lines.filter(l => l.line_type === 'expense').map(l => (
                              <div key={l.id} style={{ display: 'flex', justifyContent: 'space-between', padding: '0.2rem 0', fontSize: '0.85rem', gap: '0.5rem' }}>
                                <span style={{ flex: 1 }}>{l.label}</span>
                                <span style={{ color: '#888', fontSize: '0.78rem' }}>{l.account_name}</span>
                                <span style={{ fontWeight: 600, color: '#e67e22' }}>{idr(l.amount)}</span>
                              </div>
                            ))}
                            {imp.lines.filter(l => l.line_type === 'expense').length === 0 && (
                              <span style={{ color: '#ccc', fontSize: '0.82rem' }}>Tidak ada biaya tambahan</span>
                            )}
                          </div>
                          <div>
                            <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#666', textTransform: 'uppercase', marginBottom: '0.5rem' }}>Kas / Pembayaran (Debit)</div>
                            {imp.lines.filter(l => l.line_type === 'cash').map(l => (
                              <div key={l.id} style={{ display: 'flex', justifyContent: 'space-between', padding: '0.2rem 0', fontSize: '0.85rem', gap: '0.5rem' }}>
                                <span style={{ flex: 1 }}>{l.label}</span>
                                <span style={{ color: '#888', fontSize: '0.78rem' }}>{l.account_name}</span>
                                <span style={{ fontWeight: 600, color: '#2c6fc2' }}>{idr(l.amount)}</span>
                              </div>
                            ))}
                          </div>
                        </div>
                      </td>
                    </tr>
                  )}
                </>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
