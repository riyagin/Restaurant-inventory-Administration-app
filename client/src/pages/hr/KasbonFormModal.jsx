import { useEffect, useState } from 'react';
import { getEmployees, getAccounts, createKasbon } from '../../api';
import CurrencyInput from '../../components/CurrencyInput';

const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));

// Furthest a repayment month may fall after the request month — must match the
// backend's service.MaxRepaymentMonths (server-go/internal/service/kasbon.go).
const MAX_REPAYMENT_MONTHS = 12;

// First-of-month date string (YYYY-MM-DD) for the current month + offset months.
function monthOption(offset) {
  const d = new Date();
  d.setDate(1);
  d.setMonth(d.getMonth() + offset);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  return `${y}-${m}-01`;
}
function monthLabel(value) {
  if (!value) return '';
  const d = new Date(value);
  return d.toLocaleDateString('id-ID', { month: 'long', year: 'numeric' });
}

// All selectable months: current month through +MAX_REPAYMENT_MONTHS.
const MONTH_OPTIONS = Array.from({ length: MAX_REPAYMENT_MONTHS + 1 }, (_, i) => monthOption(i));

const SENDING_SUGGESTIONS = ['Transfer Bank', 'Tunai', 'E-Wallet', 'Cek'];

// Same filter as invoice payment: all non-system asset accounts.
const isFundSource = (a) => a.account_type === 'asset' && !a.is_system;

// Split `total` rupiah across `n` installments as evenly as possible, giving any
// remainder rupiah to the earliest installments.
function distribute(total, n) {
  const t = Math.round(Number(total || 0));
  if (n <= 0) return [];
  const base = Math.floor(t / n);
  const rem = t - base * n;
  return Array.from({ length: n }, (_, i) => base + (i < rem ? 1 : 0));
}

// Kasbon request form rendered as an overlay modal.
// Props: onClose(), onSaved(). presetEmployeeId optionally locks the employee.
export default function KasbonFormModal({ onClose, onSaved, presetEmployeeId }) {
  const [employees, setEmployees] = useState([]);
  const [accounts, setAccounts] = useState([]);

  const [form, setForm] = useState({
    employee_id: presetEmployeeId || '',
    amount: '',
    details: '',
    sending_method: '',
    fund_source_account_id: '',
    resolution_month: MONTH_OPTIONS[0],
  });
  const [split, setSplit] = useState(false);
  const [rows, setRows] = useState([
    { due_month: MONTH_OPTIONS[0], amount: '' },
    { due_month: MONTH_OPTIONS[1], amount: '' },
  ]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getEmployees().then(r => setEmployees(r.data?.data || [])).catch(() => setEmployees([]));
    getAccounts().then(r => setAccounts((r.data || []).filter(isFundSource))).catch(() => setAccounts([]));
  }, []);

  const total = Number(form.amount || 0);
  const splitSum = rows.reduce((s, r) => s + Number(r.amount || 0), 0);
  const roundedTotal = Math.round(total);

  const setRow = (idx, patch) => setRows(rs => rs.map((r, i) => i === idx ? { ...r, ...patch } : r));

  const addRow = () => {
    setRows(rs => {
      if (rs.length >= MONTH_OPTIONS.length) return rs;
      // Default the new row's month to the first unused month, if any.
      const used = new Set(rs.map(r => r.due_month));
      const nextMonth = MONTH_OPTIONS.find(m => !used.has(m)) || MONTH_OPTIONS[rs.length] || MONTH_OPTIONS[0];
      return [...rs, { due_month: nextMonth, amount: '' }];
    });
  };

  const removeRow = (idx) => setRows(rs => (rs.length <= 1 ? rs : rs.filter((_, i) => i !== idx)));

  const splitEvenly = () => {
    const amounts = distribute(roundedTotal, rows.length);
    setRows(rs => rs.map((r, i) => ({ ...r, amount: String(amounts[i] ?? '') })));
  };

  const submit = async () => {
    setError('');
    if (!form.employee_id) { setError('Pilih karyawan.'); return; }
    if (total <= 0) { setError('Jumlah kasbon harus lebih dari 0.'); return; }
    if (!form.details.trim()) { setError('Keterangan wajib diisi.'); return; }
    if (!form.sending_method.trim()) { setError('Metode pengiriman wajib diisi.'); return; }
    if (!form.fund_source_account_id) { setError('Pilih sumber dana.'); return; }

    // When split, the final (latest) installment month is the resolution month.
    const resolutionMonth = split
      ? rows.reduce((max, r) => (r.due_month > max ? r.due_month : max), rows[0].due_month)
      : form.resolution_month;

    const payload = {
      employee_id: form.employee_id,
      amount: roundedTotal,
      details: form.details.trim(),
      sending_method: form.sending_method.trim(),
      fund_source_account_id: form.fund_source_account_id,
      resolution_month: resolutionMonth,
    };

    if (split) {
      if (rows.some(r => Number(r.amount || 0) <= 0)) { setError('Setiap nominal cicilan harus lebih dari 0.'); return; }
      if (Math.round(splitSum) !== roundedTotal) { setError('Total cicilan harus sama dengan jumlah kasbon.'); return; }
      const months = rows.map(r => r.due_month);
      if (new Set(months).size !== months.length) { setError('Bulan cicilan tidak boleh sama.'); return; }
      payload.installments = rows.map(r => ({ due_month: r.due_month, amount: Math.round(Number(r.amount || 0)) }));
    }

    setSaving(true);
    try {
      await createKasbon(payload);
      onSaved();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan kasbon.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={overlay} onClick={saving ? undefined : onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.75rem' }}>
          <h3 style={{ margin: 0 }}>Pengajuan Kasbon Baru</h3>
          <button onClick={onClose} disabled={saving} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer', color: '#aaa' }}>✕</button>
        </div>

        {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

        <div className="form-group">
          <label>Karyawan</label>
          <select className="select-clean" value={form.employee_id} onChange={e => setForm({ ...form, employee_id: e.target.value })} disabled={!!presetEmployeeId}>
            <option value="">— Pilih karyawan —</option>
            {employees.map(emp => <option key={emp.id} value={emp.id}>{emp.full_name} ({emp.employee_code})</option>)}
          </select>
        </div>

        <div className="form-group">
          <label>Jumlah Kasbon (Rp)</label>
          <CurrencyInput value={form.amount} onChange={e => setForm({ ...form, amount: e.target.value })} />
        </div>

        <div className="form-group">
          <label>Keterangan</label>
          <textarea value={form.details} onChange={e => setForm({ ...form, details: e.target.value })} rows={2} style={{ width: '100%', resize: 'vertical' }} />
        </div>

        <div className="form-group">
          <label>Metode Pengiriman</label>
          <input list="sending-methods" value={form.sending_method} onChange={e => setForm({ ...form, sending_method: e.target.value })} placeholder="mis. Transfer Bank" />
          <datalist id="sending-methods">
            {SENDING_SUGGESTIONS.map(s => <option key={s} value={s} />)}
          </datalist>
        </div>

        <div className="form-group">
          <label>Akun Kas / Bank</label>
          <select className="select-clean" value={form.fund_source_account_id} onChange={e => setForm({ ...form, fund_source_account_id: e.target.value })}>
            <option value="">— Pilih akun —</option>
            {accounts.map(a => <option key={a.id} value={a.id}>{a.account_number ? `${a.account_number} · ` : ''}{a.name}</option>)}
          </select>
        </div>

        <div className="form-group">
          <label>Bulan Penyelesaian</label>
          <select
            className="select-clean"
            value={form.resolution_month}
            onChange={e => setForm({ ...form, resolution_month: e.target.value })}
            disabled={split}
          >
            {MONTH_OPTIONS.map(m => <option key={m} value={m}>{monthLabel(m)}</option>)}
          </select>
          {split && <div style={{ fontSize: '0.78rem', color: '#889', marginTop: '0.3rem' }}>Ditentukan otomatis dari cicilan terakhir.</div>}
        </div>

        <div className="form-group">
          <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <input type="checkbox" style={{ width: 'auto' }} checked={split} onChange={e => setSplit(e.target.checked)} />
            Bagi potongan menjadi beberapa cicilan
          </label>
        </div>

        {split && (
          <div className="card" style={{ background: '#fafbfc', marginBottom: '1rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.5rem', marginTop: 0 }}>
              <p style={{ fontSize: '0.82rem', color: '#667', margin: 0 }}>
                Setiap cicilan maksimal {MAX_REPAYMENT_MONTHS} bulan setelah pengajuan; jumlahnya harus sama dengan total kasbon.
              </p>
              <button type="button" className="btn btn-secondary" onClick={splitEvenly} style={{ whiteSpace: 'nowrap', padding: '0.35rem 0.7rem', fontSize: '0.8rem' }}>
                Bagi rata
              </button>
            </div>

            {rows.map((row, idx) => (
              <div key={idx} style={{ display: 'flex', gap: '0.6rem', alignItems: 'flex-end', marginTop: '0.6rem' }}>
                <div style={{ flex: 1.2 }}>
                  <label style={{ fontSize: '0.78rem', color: '#667', display: 'block', marginBottom: '0.2rem' }}>Cicilan {idx + 1} — Bulan</label>
                  <select className="select-clean" value={row.due_month} onChange={e => setRow(idx, { due_month: e.target.value })}>
                    {MONTH_OPTIONS.map(m => <option key={m} value={m}>{monthLabel(m)}</option>)}
                  </select>
                </div>
                <div style={{ flex: 1 }}>
                  <label style={{ fontSize: '0.78rem', color: '#667', display: 'block', marginBottom: '0.2rem' }}>Nominal (Rp)</label>
                  <CurrencyInput className="input-clean" value={row.amount} onChange={e => setRow(idx, { amount: e.target.value })} />
                </div>
                <button
                  type="button"
                  onClick={() => removeRow(idx)}
                  disabled={rows.length <= 1}
                  title="Hapus cicilan"
                  style={{
                    background: 'none', border: '1px solid #e0d4d4', borderRadius: '6px', height: '2.3rem', width: '2.3rem',
                    cursor: rows.length <= 1 ? 'not-allowed' : 'pointer', color: rows.length <= 1 ? '#ccc' : '#c5221f', flexShrink: 0,
                  }}
                >
                  ✕
                </button>
              </div>
            ))}

            <button
              type="button"
              className="btn btn-secondary"
              onClick={addRow}
              disabled={rows.length >= MONTH_OPTIONS.length}
              style={{ marginTop: '0.75rem', fontSize: '0.85rem' }}
            >
              + Tambah cicilan
            </button>

            <div style={{ fontSize: '0.85rem', marginTop: '0.75rem', color: Math.round(splitSum) === roundedTotal ? '#1e7e34' : '#c5221f' }}>
              Total cicilan: {fmtIDR(splitSum)} / {fmtIDR(total)}
            </div>
          </div>
        )}

        <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-primary" onClick={submit} disabled={saving} style={{ flex: 1, justifyContent: 'center' }}>
            {saving ? 'Menyimpan…' : 'Ajukan Kasbon'}
          </button>
          <button className="btn btn-secondary" onClick={onClose} disabled={saving}>Batal</button>
        </div>
      </div>
    </div>
  );
}

const overlay = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'flex-start', justifyContent: 'center', zIndex: 1000, overflowY: 'auto', padding: '2rem 1rem' };
const modal = { width: '620px', maxWidth: '95vw' };
