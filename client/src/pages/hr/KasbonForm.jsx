import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getEmployees, getAccounts, createKasbon } from '../../api';
import CurrencyInput from '../../components/CurrencyInput';

const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));

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

const SENDING_SUGGESTIONS = ['Transfer Bank', 'Tunai', 'E-Wallet', 'Cek'];

// Fund-source accounts: asset (and cash-like) accounts only.
const isFundSource = (a) => a.account_type === 'asset';

export default function KasbonForm() {
  const navigate = useNavigate();
  const [employees, setEmployees] = useState([]);
  const [accounts, setAccounts] = useState([]);
  const monthOptions = [monthOption(0), monthOption(1), monthOption(2)];

  const [form, setForm] = useState({
    employee_id: '',
    amount: '',
    details: '',
    sending_method: '',
    fund_source_account_id: '',
    resolution_month: monthOptions[0],
  });
  const [split, setSplit] = useState(false);
  const [rows, setRows] = useState([
    { due_month: monthOptions[0], amount: '' },
    { due_month: monthOptions[1], amount: '' },
  ]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getEmployees().then(r => setEmployees(r.data?.data || [])).catch(() => setEmployees([]));
    getAccounts().then(r => setAccounts((r.data || []).filter(isFundSource))).catch(() => setAccounts([]));
  }, []);

  const total = Number(form.amount || 0);
  const splitSum = rows.reduce((s, r) => s + Number(r.amount || 0), 0);

  const submit = async () => {
    setError('');
    if (!form.employee_id) { setError('Pilih karyawan.'); return; }
    if (total <= 0) { setError('Jumlah kasbon harus lebih dari 0.'); return; }
    if (!form.details.trim()) { setError('Keterangan wajib diisi.'); return; }
    if (!form.sending_method.trim()) { setError('Metode pengiriman wajib diisi.'); return; }
    if (!form.fund_source_account_id) { setError('Pilih sumber dana.'); return; }

    const payload = {
      employee_id: form.employee_id,
      amount: Math.round(total),
      details: form.details.trim(),
      sending_method: form.sending_method.trim(),
      fund_source_account_id: form.fund_source_account_id,
      resolution_month: form.resolution_month,
    };

    if (split) {
      if (splitSum !== Math.round(total)) {
        setError('Total cicilan harus sama dengan jumlah kasbon.');
        return;
      }
      if (rows[0].due_month === rows[1].due_month) {
        setError('Bulan cicilan tidak boleh sama.');
        return;
      }
      payload.installments = rows.map(r => ({ due_month: r.due_month, amount: Math.round(Number(r.amount || 0)) }));
    }

    setSaving(true);
    try {
      await createKasbon(payload);
      navigate('/hr/kasbon');
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan kasbon.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <div className="page-header">
        <h1>Pengajuan Kasbon Baru</h1>
        <button onClick={() => navigate('/hr/kasbon')} className="btn btn-secondary">Kembali</button>
      </div>

      <div className="card" style={{ maxWidth: 620 }}>
        {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

        <div className="form-group">
          <label>Karyawan</label>
          <select value={form.employee_id} onChange={e => setForm({ ...form, employee_id: e.target.value })}>
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
          <label>Sumber Dana (akun aset / kas)</label>
          <select value={form.fund_source_account_id} onChange={e => setForm({ ...form, fund_source_account_id: e.target.value })}>
            <option value="">— Pilih akun —</option>
            {accounts.map(a => <option key={a.id} value={a.id}>{a.name}{a.account_number ? ` (${a.account_number})` : ''}</option>)}
          </select>
        </div>

        <div className="form-group">
          <label>Bulan Penyelesaian</label>
          <select
            value={form.resolution_month}
            onChange={e => {
              setForm({ ...form, resolution_month: e.target.value });
              setRows(rs => [{ ...rs[0], due_month: e.target.value }, rs[1]]);
            }}
            disabled={split}
          >
            {monthOptions.map(m => <option key={m} value={m}>{monthLabel(m)}</option>)}
          </select>
        </div>

        <div className="form-group">
          <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <input type="checkbox" checked={split} onChange={e => setSplit(e.target.checked)} />
            Bagi potongan menjadi 2 cicilan
          </label>
        </div>

        {split && (
          <div className="card" style={{ background: '#fafbfc', marginBottom: '1rem' }}>
            <p style={{ fontSize: '0.82rem', color: '#667', marginTop: 0 }}>
              Kedua cicilan harus dalam rentang 2 bulan dan jumlahnya sama dengan total kasbon.
            </p>
            {rows.map((row, idx) => (
              <div key={idx} style={{ display: 'flex', gap: '0.75rem', marginBottom: '0.5rem' }}>
                <div style={{ flex: 1 }}>
                  <label style={{ fontSize: '0.78rem', color: '#667' }}>Cicilan {idx + 1} — Bulan</label>
                  <select value={row.due_month} onChange={e => setRows(rs => rs.map((r, i) => i === idx ? { ...r, due_month: e.target.value } : r))} style={{ width: '100%' }}>
                    {monthOptions.map(m => <option key={m} value={m}>{monthLabel(m)}</option>)}
                  </select>
                </div>
                <div style={{ flex: 1 }}>
                  <label style={{ fontSize: '0.78rem', color: '#667' }}>Nominal (Rp)</label>
                  <CurrencyInput value={row.amount} onChange={e => setRows(rs => rs.map((r, i) => i === idx ? { ...r, amount: e.target.value } : r))} />
                </div>
              </div>
            ))}
            <div style={{ fontSize: '0.85rem', marginTop: '0.5rem', color: splitSum === Math.round(total) ? '#1e7e34' : '#c5221f' }}>
              Total cicilan: {fmtIDR(splitSum)} / {fmtIDR(total)}
            </div>
          </div>
        )}

        <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-primary" onClick={submit} disabled={saving} style={{ flex: 1, justifyContent: 'center' }}>
            {saving ? 'Menyimpan…' : 'Ajukan Kasbon'}
          </button>
          <button className="btn btn-secondary" onClick={() => navigate('/hr/kasbon')} disabled={saving}>Batal</button>
        </div>
      </div>
    </div>
  );
}
