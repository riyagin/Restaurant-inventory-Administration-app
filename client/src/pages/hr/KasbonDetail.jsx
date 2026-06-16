import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { getKasbon } from '../../api';

const SERVER = 'http://localhost:5000';

const STATUS_LABELS = {
  pending: 'Menunggu', approved: 'Disetujui', rejected: 'Ditolak',
  processed: 'Diproses', resolved: 'Lunas', cancelled: 'Dibatalkan',
};
const STATUS_COLORS = {
  pending: { bg: '#fff8e1', color: '#a06800' },
  approved: { bg: '#e8f0fe', color: '#1967d2' },
  rejected: { bg: '#fce8e6', color: '#c5221f' },
  processed: { bg: '#e6f4ea', color: '#1e7e34' },
  resolved: { bg: '#eef1f6', color: '#445' },
  cancelled: { bg: '#eef1f6', color: '#667' },
};

const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));
const fmtDate = (d) => d ? new Date(d).toLocaleString('id-ID', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' }) : '-';
const fmtDateOnly = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';
const fmtMonth = (d) => d ? new Date(d).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' }) : '-';

function StatusChip({ status }) {
  const c = STATUS_COLORS[status] || STATUS_COLORS.cancelled;
  return <span style={{ background: c.bg, color: c.color, padding: '0.15rem 0.55rem', borderRadius: '4px', fontWeight: 600, fontSize: '0.8rem' }}>{STATUS_LABELS[status] || status}</span>;
}

function Field({ label, value }) {
  return (
    <div style={{ marginBottom: '0.85rem' }}>
      <div style={{ fontSize: '0.75rem', color: '#8a93a6', textTransform: 'uppercase', letterSpacing: '0.03em' }}>{label}</div>
      <div style={{ fontWeight: 500 }}>{value || '-'}</div>
    </div>
  );
}

function TimelineItem({ done, label, when }) {
  return (
    <div style={{ display: 'flex', gap: '0.6rem', alignItems: 'flex-start', marginBottom: '0.6rem' }}>
      <div style={{ width: 12, height: 12, borderRadius: '50%', marginTop: 4, background: done ? '#1e7e34' : '#d4d9e2', flexShrink: 0 }} />
      <div>
        <div style={{ fontWeight: done ? 600 : 400, color: done ? '#223' : '#99a' }}>{label}</div>
        {when && <div style={{ fontSize: '0.78rem', color: '#889' }}>{when}</div>}
      </div>
    </div>
  );
}

export default function KasbonDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [k, setK] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    getKasbon(id).then(r => setK(r.data)).catch(() => setError('Kasbon tidak ditemukan'));
  }, [id]);

  if (error) return <div className="error-msg">{error}</div>;
  if (!k) return <div style={{ color: '#999', padding: '2rem' }}>Memuat…</div>;

  const installments = k.installments || [];

  return (
    <>
      <div className="page-header">
        <h1>Kasbon {k.kasbon_number}</h1>
        <button onClick={() => navigate('/hr/kasbon')} className="btn btn-secondary">Kembali</button>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 2fr) minmax(0, 1fr)', gap: '1rem', alignItems: 'start' }}>
        <div>
          <div className="card" style={{ marginBottom: '1rem' }}>
            <div className="card-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h2>Rincian</h2><StatusChip status={k.status} />
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.5rem 1.5rem' }}>
              <Field label="Jumlah" value={fmtIDR(k.amount)} />
              <Field label="Sumber Dana" value={k.fund_source_name} />
              <Field label="Metode Pengiriman" value={k.sending_method} />
              <Field label="Tanggal Pengajuan" value={fmtDateOnly(k.request_date)} />
              <Field label="Bulan Penyelesaian" value={fmtMonth(k.resolution_month)} />
            </div>
            <Field label="Keterangan" value={k.details} />
            {k.approval_note && <Field label="Catatan Persetujuan" value={k.approval_note} />}
          </div>

          <div className="card">
            <div className="card-header"><h2>Rencana Cicilan</h2></div>
            {installments.length === 0 ? (
              <div style={{ color: '#999', padding: '0.5rem 0' }}>Belum ada cicilan.</div>
            ) : (
              <table style={{ width: '100%' }}>
                <thead>
                  <tr><th>Bulan Jatuh Tempo</th><th style={{ textAlign: 'right' }}>Nominal</th><th>Status</th></tr>
                </thead>
                <tbody>
                  {installments.map(ins => (
                    <tr key={ins.id}>
                      <td>{fmtMonth(ins.due_month)}</td>
                      <td style={{ textAlign: 'right' }}>{fmtIDR(ins.amount)}</td>
                      <td>{ins.status === 'deducted'
                        ? <span style={{ color: '#1e7e34', fontWeight: 600 }}>Terpotong</span>
                        : <span style={{ color: '#a06800' }}>Belum</span>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>

        <div>
          <div className="card" style={{ marginBottom: '1rem' }}>
            <div className="card-header"><h2>Linimasa</h2></div>
            <TimelineItem done label="Diajukan" when={fmtDateOnly(k.request_date)} />
            <TimelineItem
              done={['approved', 'processed', 'resolved'].includes(k.status)}
              label={k.status === 'rejected' ? 'Ditolak' : 'Disetujui'}
              when={k.approved_at ? fmtDate(k.approved_at) : ''}
            />
            <TimelineItem done={['processed', 'resolved'].includes(k.status)} label="Diproses (dana dikirim)" when={k.processed_at ? fmtDate(k.processed_at) : ''} />
            <TimelineItem done={k.status === 'resolved'} label="Lunas" when="" />
            {k.status === 'cancelled' && <TimelineItem done label="Dibatalkan" when="" />}
          </div>

          {k.evidence_photo_path && (
            <div className="card">
              <div className="card-header"><h2>Bukti</h2></div>
              <img src={`${SERVER}/uploads/${k.evidence_photo_path}`} alt="Bukti kasbon" style={{ width: '100%', borderRadius: '8px', border: '1px solid #e8e8e8' }} />
            </div>
          )}

          {k.last_resolved && (
            <div className="card" style={{ marginTop: '1rem' }}>
              <div className="card-header"><h2>Kasbon Lunas Terakhir</h2></div>
              <Field label="Tanggal" value={fmtDateOnly(k.last_resolved.processed_at || k.last_resolved.request_date)} />
              <Field label="Jumlah" value={fmtIDR(k.last_resolved.amount)} />
            </div>
          )}
        </div>
      </div>
    </>
  );
}
