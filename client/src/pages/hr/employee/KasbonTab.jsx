import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { getEmployeeKasbons } from '../../../api';
import KasbonFormModal from '../KasbonFormModal';
import { fmtIDR } from '../../../format';

const KASBON_STATUS_LABELS = {
  pending: 'Menunggu', approved: 'Disetujui', rejected: 'Ditolak',
  processed: 'Diproses', resolved: 'Lunas', cancelled: 'Dibatalkan',
};

const KASBON_STATUS_COLORS = {
  pending: { bg: '#fff8e1', color: '#a06800' },
  approved: { bg: '#e8f0fe', color: '#1967d2' },
  rejected: { bg: '#fce8e6', color: '#c5221f' },
  processed: { bg: '#e6f4ea', color: '#1e7e34' },
  resolved: { bg: '#eef1f6', color: '#445' },
  cancelled: { bg: '#eef1f6', color: '#667' },
};

function KasbonStatusChip({ status }) {
  const c = KASBON_STATUS_COLORS[status] || KASBON_STATUS_COLORS.cancelled;
  return <span style={{ background: c.bg, color: c.color, padding: '0.15rem 0.55rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.8rem' }}>{KASBON_STATUS_LABELS[status] || status}</span>;
}


export function KasbonTab({ employeeId }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    getEmployeeKasbons(employeeId)
      .then(r => setRows(r.data || []))
      .catch(() => setRows([]))
      .finally(() => setLoading(false));
  }, [employeeId]);

  useEffect(() => { load(); }, [load]);

  // Outstanding: approved or processed, not yet resolved.
  const outstanding = rows
    .filter(k => k.status === 'approved' || k.status === 'processed')
    .reduce((s, k) => s + Number(k.amount || 0), 0);

  return (
    <div>
      <div className="card" style={{ marginBottom: '1rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <div style={{ fontSize: '0.72rem', color: '#667', textTransform: 'uppercase', letterSpacing: '0.03em' }}>Saldo Kasbon Berjalan</div>
          <div style={{ fontSize: '1.6rem', fontWeight: 700, color: outstanding > 0 ? '#c5221f' : '#1e7e34' }}>{fmtIDR(outstanding)}</div>
        </div>
        <button onClick={() => setShowModal(true)} className="btn btn-primary btn-sm">+ Pengajuan Kasbon</button>
      </div>

      <div className="card">
        <h3 style={{ margin: '0 0 0.75rem', fontSize: '1rem' }}>Riwayat Kasbon</h3>
        {loading ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>
        ) : rows.length === 0 ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Belum ada kasbon.</p>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%' }}>
              <thead>
                <tr><th>Nomor</th><th style={{ textAlign: 'right' }}>Jumlah</th><th>Penyelesaian</th><th>Status</th></tr>
              </thead>
              <tbody>
                {rows.map(k => (
                  <tr key={k.id}>
                    <td><Link to={`/hr/kasbon/${k.id}`}>{k.kasbon_number}</Link></td>
                    <td style={{ textAlign: 'right' }}>{fmtIDR(k.amount)}</td>
                    <td>{k.resolution_month ? new Date(k.resolution_month).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' }) : '-'}</td>
                    <td><KasbonStatusChip status={k.status} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {showModal && <KasbonFormModal presetEmployeeId={employeeId} onClose={() => setShowModal(false)} onSaved={() => { setShowModal(false); load(); }} />}
    </div>
  );
}
