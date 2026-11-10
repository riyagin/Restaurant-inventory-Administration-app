// The branch operator's screens, in the order they are used. Shared by the
// operator's dashboard tiles and their navbar so the two cannot disagree.
export const BRANCH_OPERATOR_TILES = [
  { to: '/hr/attendance', icon: '🕒', label: 'Absensi', desc: 'Cek kehadiran hari ini' },
  { to: '/hr/attendance/corrections', icon: '✏️', label: 'Koreksi Kehadiran', desc: 'Setengah hari, lupa absen' },
  { to: '/hr/requests', icon: '📝', label: 'Pengajuan', desc: 'Cuti, izin & lembur' },
  { to: '/hr/kasbon', icon: '💵', label: 'Kasbon', desc: 'Ajukan & pantau kasbon' },
];
