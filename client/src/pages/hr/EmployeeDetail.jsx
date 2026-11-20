import { useEffect, useState, useCallback } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { getEmployee, deleteEmployee } from '../../api';
import { AttendanceTab } from './employee/AttendanceTab';
import { CutiTab } from './employee/LeaveTab';
import { DokumenTab } from './employee/DocumentsTab';
import { Field } from './employee/Field';
import { HistoryTab } from './employee/HistoryTab';
import { KasbonTab } from './employee/KasbonTab';
import { ProfilePhotoCard } from './employee/ProfilePhotoCard';
import { ResignModal, TransitionPermanentModal } from './employee/EmploymentModals';
import { WageTab } from './employee/WageTab';
import { WelfareTab } from './employee/WelfareTab';
import { canEdit, contractDaysLeft, fmtDate, statusLabel } from './employee/shared';

const TABS = [
  { key: 'profil', label: 'Profil' },
  { key: 'gaji', label: 'Gaji' },
  { key: 'absensi', label: 'Absensi' },
  { key: 'riwayat', label: '30 Hari Terakhir' },
  { key: 'kasbon', label: 'Kasbon' },
  { key: 'kesejahteraan', label: 'Dana Kesejahteraan' },
  { key: 'cuti', label: 'Libur' },
  { key: 'dokumen', label: 'Dokumen' },
];

export default function EmployeeDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [emp, setEmp]   = useState(null);
  const [tab, setTab]   = useState('profil');
  const [error, setError] = useState('');
  const [showTransition, setShowTransition] = useState(false);
  const [showResign, setShowResign] = useState(false);

  const editable = canEdit();

  const loadEmployee = useCallback(() => {
    getEmployee(id)
      .then(r => setEmp(r.data))
      .catch(() => setError('Karyawan tidak ditemukan'));
  }, [id]);

  useEffect(() => { loadEmployee(); }, [loadEmployee]);

  const handleDelete = async () => {
    if (!confirm('Yakin hapus karyawan ini? Sebaiknya ubah status menjadi nonaktif jika sudah ada data terkait.')) return;
    try {
      await deleteEmployee(id);
      navigate('/hr/employees');
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus karyawan');
    }
  };

  if (error) return <div className="error-msg">{error}</div>;
  if (!emp) return <div style={{ color: '#999', padding: '2rem' }}>Memuat...</div>;

  return (
    <>
      <div className="page-header">
        <h1>{emp.full_name}</h1>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <button onClick={() => navigate('/hr/employees')} className="btn btn-secondary">Kembali</button>
          {editable && (emp.employment_type === 'contract' || emp.employment_type === 'probation') && (
            <button onClick={() => setShowTransition(true)} className="btn btn-primary" style={{ background: '#1e7e34', borderColor: '#1e7e34' }}>
              Jadikan Karyawan Tetap
            </button>
          )}
          {editable && emp.status !== 'resigned' && (
            <button onClick={() => setShowResign(true)} className="btn btn-primary" style={{ background: '#7b2cbf', borderColor: '#7b2cbf' }}>
              Tandai Resign
            </button>
          )}
          {editable && <Link to={`/hr/employees/${id}/edit`} className="btn btn-primary">Edit</Link>}
          {editable && <button onClick={handleDelete} className="btn btn-danger">Hapus</button>}
        </div>
      </div>

      <div style={{ display: 'flex', gap: '0.5rem', borderBottom: '1px solid #e8e8e8', marginBottom: '1rem', flexWrap: 'wrap' }}>
        {TABS.map(t => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={tab === t.key ? 'btn btn-primary btn-sm' : 'btn btn-secondary btn-sm'}
            style={{ borderRadius: '6px 6px 0 0' }}
          >
            {t.label}
          </button>
        ))}
      </div>

      {emp.status === 'resigned' && (
        <div className="card" style={{ marginBottom: '1rem', borderLeft: '4px solid #7b2cbf', background: '#faf5ff' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span style={{ fontSize: '1.1rem' }}>🚪</span>
            <strong style={{ color: '#7b2cbf' }}>
              Karyawan telah resign{emp.resign_date ? ` per ${fmtDate(emp.resign_date)}` : ''}.
            </strong>
          </div>
        </div>
      )}

      {(() => {
        if (!emp.contract_end_date || emp.status !== 'active') return null;
        if (emp.employment_type === 'probation') {
          const days = contractDaysLeft(emp.contract_end_date);
          if (days == null) return null;
          const due = days <= 0;
          const when = fmtDate(emp.contract_end_date);
          const msg = days < 0
            ? `Masa percobaan telah berakhir ${Math.abs(days)} hari lalu (${when}) — status kepegawaian belum diputuskan.`
            : days === 0
              ? `Masa percobaan berakhir hari ini (${when}).`
              : `Dalam masa percobaan sampai ${when} (${days} hari lagi).`;
          return (
            <div
              className="card"
              style={{ marginBottom: '1rem', borderLeft: `4px solid ${due ? '#c5221f' : '#f0a020'}`, background: due ? '#fdece9' : '#fffaf0' }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                <span style={{ fontSize: '1.1rem' }}>{due ? '⛔' : '⏳'}</span>
                <strong style={{ color: due ? '#c5221f' : '#a06800' }}>{msg}</strong>
                {editable && (
                  <span style={{ marginLeft: 'auto', display: 'flex', gap: '0.75rem', fontSize: '0.85rem' }}>
                    <a href="#" onClick={e => { e.preventDefault(); setShowTransition(true); }}>Jadikan Tetap →</a>
                    <Link to={`/hr/employees/${id}/edit`}>Ubah ke Kontrak →</Link>
                  </span>
                )}
              </div>
            </div>
          );
        }
        if (emp.employment_type !== 'contract') return null;
        const days = contractDaysLeft(emp.contract_end_date);
        if (days == null || days > 30) return null;
        const overdue = days < 0;
        const msg = overdue
          ? `Kontrak telah berakhir ${Math.abs(days)} hari lalu (${fmtDate(emp.contract_end_date)}).`
          : days === 0
            ? `Kontrak berakhir hari ini (${fmtDate(emp.contract_end_date)}).`
            : `Kontrak akan berakhir dalam ${days} hari (${fmtDate(emp.contract_end_date)}).`;
        return (
          <div
            className="card"
            style={{ marginBottom: '1rem', borderLeft: `4px solid ${overdue ? '#c5221f' : '#f0a020'}`, background: overdue ? '#fdece9' : '#fffaf0' }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <span style={{ fontSize: '1.1rem' }}>{overdue ? '⛔' : '⚠️'}</span>
              <strong style={{ color: overdue ? '#c5221f' : '#a06800' }}>{msg}</strong>
              {editable && <Link to={`/hr/employees/${id}/edit`} style={{ marginLeft: 'auto', fontSize: '0.85rem' }}>Perpanjang / Ubah →</Link>}
            </div>
          </div>
        );
      })()}

      {tab === 'profil' && (
        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 2fr) minmax(0, 1fr)', gap: '1rem', alignItems: 'start' }}>
          <div>
            <div className="card" style={{ marginBottom: '1rem' }}>
              <div className="card-header"><h2>Identitas</h2></div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.5rem 1.5rem' }}>
                <Field label="Kode Karyawan" value={emp.employee_code} />
                <Field label="Nama Lengkap" value={emp.full_name} />
                <Field label="Jabatan" value={emp.position_name} />
                <Field label="Cabang" value={emp.branch_name} />
                <Field label="Divisi" value={emp.division_name || 'Tanpa divisi'} />
                <Field label="Tanggal Lahir" value={fmtDate(emp.dob)} />
                <Field label="Tanggal Bergabung" value={fmtDate(emp.join_date)} />
                <Field label="NIK / KTP" value={emp.national_id} />
                <Field label="Status" value={statusLabel(emp.status)} />
                <Field label="Data Wajah" value={
                  <span
                    className="badge"
                    style={{
                      background: emp.has_face ? '#e6f4ea' : '#eef1f6',
                      color: emp.has_face ? '#1e7e34' : '#8a93a6',
                    }}
                  >
                    {emp.has_face
                      ? `Terdaftar${emp.face_enrolled_at ? ' · ' + fmtDate(emp.face_enrolled_at) : ''}`
                      : 'Belum terdaftar'}
                  </span>
                } />
                {emp.status === 'resigned' && emp.resign_date && (
                  <Field label="Tanggal Resign" value={fmtDate(emp.resign_date)} />
                )}
                <Field label="Tipe Kepegawaian" value={emp.employment_type === 'contract' ? 'Kontrak' : emp.employment_type === 'probation' ? 'Masa Percobaan' : 'Tetap'} />
                {emp.employment_type === 'probation' && (
                  <Field label="Akhir Masa Percobaan" value={fmtDate(emp.contract_end_date)} />
                )}
                {emp.employment_type === 'contract' && (
                  <Field label="Berakhir Kontrak" value={fmtDate(emp.contract_end_date)} />
                )}
                {emp.employment_type === 'permanent' && emp.permanent_since && (
                  <Field label="Tetap Sejak" value={fmtDate(emp.permanent_since)} />
                )}
              </div>
            </div>

            <div className="card" style={{ marginBottom: '1rem' }}>
              <div className="card-header"><h2>Kontak</h2></div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.5rem 1.5rem' }}>
                <Field label="Telepon" value={emp.phone} />
                <Field label="Email" value={emp.email} />
                <Field label="Alamat" value={emp.address} />
              </div>
            </div>

            <div className="card">
              <div className="card-header"><h2>Rekening Bank</h2></div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.5rem 1.5rem' }}>
                <Field label="Nama Bank" value={emp.bank_name} />
                <Field label="Nomor Rekening" value={emp.bank_account_number} />
                <Field label="Atas Nama" value={emp.bank_account_holder} />
              </div>
            </div>
          </div>

          <ProfilePhotoCard emp={emp} editable={editable} onChanged={loadEmployee} />
        </div>
      )}

      {tab === 'gaji' && <WageTab employeeId={id} editable={editable} isContract={emp.employment_type === 'contract'} positionId={emp.position_id} positionName={emp.position_name} />}
      {tab === 'absensi' && <AttendanceTab employeeId={id} employee={emp} />}
      {tab === 'riwayat' && <HistoryTab employeeId={id} />}
      {tab === 'kasbon' && <KasbonTab employeeId={id} />}
      {tab === 'kesejahteraan' && <WelfareTab employeeId={id} />}
      {tab === 'cuti' && <CutiTab employeeId={id} />}
      {tab === 'dokumen' && <DokumenTab employeeId={id} editable={editable} />}

      {showTransition && (
        <TransitionPermanentModal
          employee={emp}
          onClose={() => setShowTransition(false)}
          onDone={() => { setShowTransition(false); loadEmployee(); }}
        />
      )}

      {showResign && (
        <ResignModal
          employee={emp}
          onClose={() => setShowResign(false)}
          onDone={() => { setShowResign(false); loadEmployee(); }}
        />
      )}
    </>
  );
}
