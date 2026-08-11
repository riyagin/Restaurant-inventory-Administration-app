import { Link } from 'react-router-dom';
import { getUser, canApprove } from '../../roles';

// Landing page for the HR role — the HR module is the whole application for
// them, so this replaces the operational dashboard with a map of it. Grouped by
// the shape of the work rather than alphabetically, so the daily screens
// (attendance, requests) sit at the top and the once-a-month ones lower down.
const groups = (approver) => [
  {
    title: 'Karyawan',
    desc: 'Data induk dan berkas.',
    items: [
      { to: '/hr/employees', icon: '👥', label: 'Karyawan', desc: 'Daftar & profil karyawan' },
      { to: '/hr/onboarding', icon: '🚀', label: 'Onboarding', desc: 'Karyawan baru, langkah demi langkah' },
      { to: '/hr/documents', icon: '📄', label: 'Dokumen HR', desc: 'PKWT, PKWTT, SP, Paklaring' },
      { to: '/hr/manpower', icon: '🗺️', label: 'Rencana Tenaga Kerja', desc: 'Kebutuhan per cabang' },
    ],
  },
  {
    title: 'Harian',
    desc: 'Dikerjakan setiap hari kerja.',
    items: [
      { to: '/hr/attendance', icon: '🕒', label: 'Absensi', desc: 'Rekap kehadiran harian' },
      { to: '/hr/attendance/corrections', icon: '✏️', label: 'Koreksi Kehadiran', desc: 'Perbaiki catatan absen' },
      { to: '/hr/requests', icon: '📝', label: 'Pengajuan', desc: 'Cuti & lembur' },
      { to: '/hr/approvals', icon: '✅', label: 'Persetujuan',
        desc: approver ? 'Antrean persetujuan' : 'Antrean persetujuan (hanya manajer yang menyetujui)' },
      { to: '/hr/kasbon', icon: '💵', label: 'Kasbon', desc: 'Pinjaman & cicilan karyawan' },
    ],
  },
  {
    title: 'Berkala',
    desc: 'Siklus bulanan dan tahunan.',
    items: [
      { to: '/hr/payroll', icon: '🧾', label: 'Penggajian', desc: 'Periode gaji & slip' },
      { to: '/hr/thr', icon: '🎁', label: 'THR', desc: 'Tunjangan Hari Raya' },
      { to: '/hr/performance', icon: '📈', label: 'Evaluasi', desc: 'Skor kinerja bulanan' },
      { to: '/hr/kpi', icon: '🎯', label: 'KPI & Tugas Harian', desc: 'Target staf & pencapaian bulanan' },
    ],
  },
  {
    title: 'Pengaturan',
    desc: 'Disiapkan sekali, jarang diubah.',
    items: [
      { to: '/hr/settings', icon: '🏢', label: 'Pengaturan HR', desc: 'Data perusahaan & penomoran surat' },
      { to: '/hr/positions', icon: '🏷️', label: 'Jabatan', desc: 'Katalog jabatan' },
      { to: '/hr/wage-components', icon: '🧮', label: 'Komponen Gaji', desc: 'Tunjangan & potongan' },
      { to: '/hr/performance/policies', icon: '⚖️', label: 'Kebijakan Kinerja', desc: 'Aturan pelanggaran & poin' },
      { to: '/hr/attendance/settings', icon: '⚙️', label: 'Pengaturan Absensi', desc: 'Jadwal kerja & hari libur' },
      { to: '/hr/face', icon: '🙂', label: 'Wajah & Perangkat', desc: 'Enrollment & kesehatan perangkat' },
      { to: '/hr/attendance/import', icon: '🖐️', label: 'Impor Sidik Jari', desc: 'Unggah data mesin absensi' },
      { to: '/hr/import', icon: '📥', label: 'Impor Karyawan', desc: 'Unggah massal dari Excel' },
    ],
  },
];

export default function HRDashboard() {
  const user = getUser();
  const approver = canApprove();
  const GROUPS = groups(approver);

  return (
    <>
      <div className="page-header">
        <div>
          <h1>Dasbor HR</h1>
          <p style={{ color: 'var(--ink-3)', margin: '0.2rem 0 0' }}>
            Selamat datang{user?.username ? `, ${user.username}` : ''}. Semua proses kepegawaian ada di sini.
          </p>
        </div>
      </div>

      {GROUPS.map((g) => {
        return (
          <section key={g.title} style={{ marginBottom: '1.6rem' }}>
            <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.6rem', marginBottom: '0.7rem' }}>
              <h2 style={{ fontSize: '1rem', fontWeight: 600 }}>{g.title}</h2>
              <span style={{ fontSize: '0.82rem', color: 'var(--ink-3)' }}>{g.desc}</span>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(230px, 1fr))', gap: '0.8rem' }}>
              {g.items.map((i) => (
                <Link key={i.to} to={i.to} className="hrd-tile">
                  <span className="hrd-tile-icon" aria-hidden="true">{i.icon}</span>
                  <span>
                    <span className="hrd-tile-label">{i.label}</span>
                    <span className="hrd-tile-desc">{i.desc}</span>
                  </span>
                </Link>
              ))}
            </div>
          </section>
        );
      })}
    </>
  );
}
