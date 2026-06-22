import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getPayrollPeriods, createPayrollPeriod, deletePayrollPeriod } from '../../api';

const STATUS_LABELS = { open: 'Terbuka', closed: 'Ditutup', paid: 'Dibayar' };
const STATUS_COLORS = {
  open:   { bg: '#fff8e1', color: '#a06800' },
  closed: { bg: '#e8f0fe', color: '#1967d2' },
  paid:   { bg: '#e6f4ea', color: '#1e7e34' },
};

const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));
const fmtMonth = (d) => d ? new Date(d).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' }) : '-';

function StatusChip({ status }) {
  const c = STATUS_COLORS[status] || STATUS_COLORS.open;
  return (
    <span style={{ background: c.bg, color: c.color, padding: '2px 10px', borderRadius: 12, fontSize: 13, fontWeight: 600 }}>
      {STATUS_LABELS[status] || status}
    </span>
  );
}

export default function PayrollDashboard() {
  const navigate = useNavigate();
  const [periods, setPeriods] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7));
  const [submitting, setSubmitting] = useState(false);
  const [warnings, setWarnings] = useState([]);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const { data } = await getPayrollPeriods();
      setPeriods(Array.isArray(data) ? data : []);
    } catch {
      setError('Gagal memuat periode penggajian');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleDelete = async (e, id, label) => {
    e.stopPropagation();
    if (!window.confirm(`Hapus periode penggajian ${label}? Semua baris penggajian akan ikut terhapus.`)) return;
    setError('');
    try {
      await deletePayrollPeriod(id);
      await load();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menghapus periode penggajian');
    }
  };

  const submit = async (e) => {
    e.preventDefault();
    setSubmitting(true);
    setError('');
    setWarnings([]);
    try {
      const { data } = await createPayrollPeriod({ period_month: month });
      if (Array.isArray(data?.skipped) && data.skipped.length > 0) {
        setWarnings(data.skipped);
      }
      await load();
      setShowForm(false);
      if (data?.period?.id && (!data.skipped || data.skipped.length === 0)) {
        navigate(`/hr/payroll/${data.period.id}`);
      }
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal membuat periode penggajian');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={{ padding: 24, maxWidth: 1000, margin: '0 auto' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <h1 style={{ margin: 0, fontSize: 24 }}>Penggajian</h1>
        <button onClick={() => { setShowForm(true); setWarnings([]); setError(''); }}
          style={{ background: '#1967d2', color: '#fff', border: 0, borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: 'pointer' }}>
          + Buat Periode
        </button>
      </div>

      {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 12, borderRadius: 8, marginBottom: 12 }}>{error}</div>}

      {warnings.length > 0 && (
        <div style={{ background: '#fff8e1', color: '#a06800', padding: 12, borderRadius: 8, marginBottom: 12 }}>
          <strong>{warnings.length} karyawan dilewati</strong> (tidak ada struktur gaji aktif): {warnings.join(', ')}
        </div>
      )}

      {showForm && (
        <form onSubmit={submit} style={{ background: '#f8f9fb', border: '1px solid #e6e8ee', borderRadius: 10, padding: 16, marginBottom: 16 }}>
          <label style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}>Bulan Periode</label>
          <input type="month" value={month} onChange={(e) => setMonth(e.target.value)} required
            style={{ padding: 8, borderRadius: 6, border: '1px solid #ccd', marginRight: 8 }} />
          <button type="submit" disabled={submitting}
            style={{ background: '#1e7e34', color: '#fff', border: 0, borderRadius: 6, padding: '8px 14px', fontWeight: 600, cursor: 'pointer' }}>
            {submitting ? 'Memproses…' : 'Buat & Hasilkan Baris'}
          </button>
          <button type="button" onClick={() => setShowForm(false)}
            style={{ marginLeft: 8, background: '#fff', border: '1px solid #ccd', borderRadius: 6, padding: '8px 14px', cursor: 'pointer' }}>
            Batal
          </button>
        </form>
      )}

      {loading ? (
        <p>Memuat…</p>
      ) : periods.length === 0 ? (
        <p style={{ color: '#667' }}>Belum ada periode penggajian.</p>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse', background: '#fff', borderRadius: 10, overflow: 'hidden', boxShadow: '0 1px 3px rgba(0,0,0,.08)' }}>
          <thead>
            <tr style={{ background: '#f1f3f7', textAlign: 'left' }}>
              <th style={{ padding: 12 }}>Bulan</th>
              <th style={{ padding: 12 }}>Status</th>
              <th style={{ padding: 12, textAlign: 'right' }}>Total Gaji Bersih</th>
              <th style={{ padding: 12, textAlign: 'center' }}>Direview</th>
              <th style={{ padding: 12 }}></th>
            </tr>
          </thead>
          <tbody>
            {periods.map((p) => (
              <tr key={p.id} onClick={() => navigate(`/hr/payroll/${p.id}`)}
                style={{ borderTop: '1px solid #eef0f4', cursor: 'pointer' }}>
                <td style={{ padding: 12, fontWeight: 600 }}>{fmtMonth(p.period_month)}</td>
                <td style={{ padding: 12 }}><StatusChip status={p.status} /></td>
                <td style={{ padding: 12, textAlign: 'right' }}>{fmtIDR(p.total_net)}</td>
                <td style={{ padding: 12, textAlign: 'center' }}>{p.reviewed_count}/{p.line_count}</td>
                <td style={{ padding: '8px 12px' }}>
                  {p.status === 'open' && (
                    <button
                      onClick={(e) => handleDelete(e, p.id, fmtMonth(p.period_month))}
                      style={{ background: '#fce8e6', color: '#c5221f', border: 0, borderRadius: 6, padding: '4px 10px', fontSize: 13, fontWeight: 600, cursor: 'pointer' }}>
                      Hapus
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
