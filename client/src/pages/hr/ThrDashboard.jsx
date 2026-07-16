import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getThrRuns, createThrRun, deleteThrRun } from '../../api';

const STATUS_LABELS = { open: 'Terbuka', closed: 'Ditutup', paid: 'Dibayar' };
const STATUS_COLORS = {
  open:   { bg: '#fff8e1', color: '#a06800' },
  closed: { bg: '#e8f0fe', color: '#1967d2' },
  paid:   { bg: '#e6f4ea', color: '#1e7e34' },
};

const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));
const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'long', year: 'numeric' }) : '-';

function StatusChip({ status }) {
  const c = STATUS_COLORS[status] || STATUS_COLORS.open;
  return (
    <span style={{ background: c.bg, color: c.color, padding: '2px 10px', borderRadius: 12, fontSize: 13, fontWeight: 600 }}>
      {STATUS_LABELS[status] || status}
    </span>
  );
}

export default function ThrDashboard() {
  const navigate = useNavigate();
  const [runs, setRuns] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [name, setName] = useState('');
  const [paymentDate, setPaymentDate] = useState(() => new Date().toISOString().slice(0, 10));
  const [submitting, setSubmitting] = useState(false);
  const [warnings, setWarnings] = useState([]);
  const [contractSkipped, setContractSkipped] = useState([]);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const { data } = await getThrRuns();
      setRuns(Array.isArray(data) ? data : []);
    } catch {
      setError('Gagal memuat daftar THR');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const openForm = () => {
    setShowForm(true);
    setWarnings([]);
    setContractSkipped([]);
    setError('');
    const year = new Date().getFullYear();
    setName(`THR Idul Fitri ${year}`);
  };

  const handleDelete = async (e, id, label) => {
    e.stopPropagation();
    if (!window.confirm(`Hapus run THR "${label}"? Semua baris THR akan ikut terhapus.`)) return;
    setError('');
    try {
      await deleteThrRun(id);
      await load();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menghapus run THR');
    }
  };

  const submit = async (e) => {
    e.preventDefault();
    setSubmitting(true);
    setError('');
    setWarnings([]);
    setContractSkipped([]);
    try {
      const { data } = await createThrRun({ name: name.trim(), payment_date: paymentDate });
      if (Array.isArray(data?.skipped) && data.skipped.length > 0) {
        setWarnings(data.skipped);
      }
      if (Array.isArray(data?.contract) && data.contract.length > 0) {
        setContractSkipped(data.contract);
      }
      await load();
      setShowForm(false);
      const hasWarnings = (data?.skipped?.length || 0) > 0 || (data?.contract?.length || 0) > 0;
      if (data?.run?.id && !hasWarnings) {
        navigate(`/hr/thr/${data.run.id}`);
      }
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal membuat run THR');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={{ padding: 24, maxWidth: 1000, margin: '0 auto' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <h1 style={{ margin: 0, fontSize: 24 }}>THR (Tunjangan Hari Raya)</h1>
        <button onClick={openForm}
          style={{ background: '#1967d2', color: '#fff', border: 0, borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: 'pointer' }}>
          + Buat Run THR
        </button>
      </div>
      <p style={{ color: '#667', marginTop: 0, marginBottom: 16, fontSize: 13 }}>
        THR dihitung dari gaji pokok dan masa kerja: masa kerja ≥ 12 bulan menerima 1 bulan gaji pokok,
        kurang dari itu dihitung proporsional (bulan masa kerja dibulatkan ke atas ÷ 12 × gaji pokok).
        Karyawan kontrak (PKWT) tidak berhak menerima THR dan dikecualikan dari run.
      </p>

      {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 12, borderRadius: 8, marginBottom: 12 }}>{error}</div>}

      {warnings.length > 0 && (
        <div style={{ background: '#fff8e1', color: '#a06800', padding: 12, borderRadius: 8, marginBottom: 12 }}>
          <strong>{warnings.length} karyawan dilewati</strong> (tidak ada struktur gaji aktif): {warnings.join(', ')}
        </div>
      )}

      {contractSkipped.length > 0 && (
        <div style={{ background: '#eef1f6', color: '#445', padding: 12, borderRadius: 8, marginBottom: 12 }}>
          <strong>{contractSkipped.length} karyawan kontrak dikecualikan</strong> (tidak berhak THR): {contractSkipped.join(', ')}
        </div>
      )}

      {showForm && (
        <form onSubmit={submit} style={{ background: '#f8f9fb', border: '1px solid #e6e8ee', borderRadius: 10, padding: 16, marginBottom: 16, display: 'grid', gap: 12 }}>
          <div>
            <label style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}>Nama Run</label>
            <input type="text" value={name} onChange={(e) => setName(e.target.value)} required placeholder="mis. THR Idul Fitri 2026"
              style={{ padding: 8, borderRadius: 6, border: '1px solid #ccd', width: '100%', maxWidth: 360 }} />
          </div>
          <div>
            <label style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}>Tanggal Pembayaran</label>
            <input type="date" value={paymentDate} onChange={(e) => setPaymentDate(e.target.value)} required
              style={{ padding: 8, borderRadius: 6, border: '1px solid #ccd' }} />
            <div style={{ fontSize: 12, color: '#889', marginTop: 4 }}>Masa kerja tiap karyawan dihitung sampai tanggal ini.</div>
          </div>
          <div>
            <button type="submit" disabled={submitting}
              style={{ background: '#1e7e34', color: '#fff', border: 0, borderRadius: 6, padding: '8px 14px', fontWeight: 600, cursor: 'pointer' }}>
              {submitting ? 'Memproses…' : 'Buat & Hasilkan Baris'}
            </button>
            <button type="button" onClick={() => setShowForm(false)}
              style={{ marginLeft: 8, background: '#fff', border: '1px solid #ccd', borderRadius: 6, padding: '8px 14px', cursor: 'pointer' }}>
              Batal
            </button>
          </div>
        </form>
      )}

      {loading ? (
        <p>Memuat…</p>
      ) : runs.length === 0 ? (
        <p style={{ color: '#667' }}>Belum ada run THR.</p>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse', background: '#fff', borderRadius: 10, overflow: 'hidden', boxShadow: '0 1px 3px rgba(0,0,0,.08)' }}>
          <thead>
            <tr style={{ background: '#f1f3f7', textAlign: 'left' }}>
              <th style={{ padding: 12 }}>Nama</th>
              <th style={{ padding: 12 }}>Tanggal Bayar</th>
              <th style={{ padding: 12 }}>Status</th>
              <th style={{ padding: 12, textAlign: 'right' }}>Total THR</th>
              <th style={{ padding: 12, textAlign: 'center' }}>Direview</th>
              <th style={{ padding: 12 }}></th>
            </tr>
          </thead>
          <tbody>
            {runs.map((run) => (
              <tr key={run.id} onClick={() => navigate(`/hr/thr/${run.id}`)}
                style={{ borderTop: '1px solid #eef0f4', cursor: 'pointer' }}>
                <td style={{ padding: 12, fontWeight: 600 }}>{run.name}</td>
                <td style={{ padding: 12 }}>{fmtDate(run.payment_date)}</td>
                <td style={{ padding: 12 }}><StatusChip status={run.status} /></td>
                <td style={{ padding: 12, textAlign: 'right' }}>{fmtIDR(run.total_thr)}</td>
                <td style={{ padding: 12, textAlign: 'center' }}>{run.reviewed_count}/{run.line_count}</td>
                <td style={{ padding: '8px 12px' }}>
                  {run.status === 'open' && (
                    <button
                      onClick={(e) => handleDelete(e, run.id, run.name)}
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
