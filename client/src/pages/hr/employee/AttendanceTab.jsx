import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getAttendance, getEmployeePerformance } from '../../../api';
import { StatusChip, SourceBadge, AnomalyChips, fmtTime } from '../AttendanceDashboard';
import { fmtDateTime, ymd } from '../../../time';
import { perfScoreColor } from './shared';

const STATUS_PRINT_LABELS = { present: 'Hadir', absent: 'Absen', leave: 'Libur', holiday: 'Libur Nasional' };

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
}

function anomalyText(r) {
  const parts = [];
  if (r.is_late) parts.push(`Terlambat ${r.late_minutes} mnt`);
  if (r.is_early_leave) parts.push('Pulang Awal');
  if (r.is_missing_checkout) parts.push('Tidak Absen Pulang');
  if (r.no_shift) parts.push('Tanpa Shift');
  return parts.join(', ');
}

// Printable monthly attendance, opened in its own window.
function buildAttendancePrintHtml({ employee, month, rows, perf }) {
  const [y, m] = month.split('-').map(Number);
  const monthLabel = new Date(y, m - 1, 1).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' });
  const printedAt = fmtDateTime(new Date(), { dateStyle: 'long', timeStyle: 'short' });

  const counts = { present: 0, absent: 0, leave: 0, holiday: 0 };
  rows.forEach(r => { if (counts[r.status] !== undefined) counts[r.status]++; });
  const lateCount = rows.filter(r => r.is_late).length;
  const score = perf?.score ?? 100;
  const violCount = perf?.violations?.length ?? 0;

  const sorted = [...rows].sort((a, b) => new Date(a.date) - new Date(b.date));
  const bodyRows = sorted.length === 0
    ? `<tr><td colspan="6" style="text-align:center;color:#888;padding:14px">Tidak ada catatan kehadiran pada bulan ini.</td></tr>`
    : sorted.map(r => {
        const d = new Date(r.date);
        const tgl = d.toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' });
        const hari = d.toLocaleDateString('id-ID', { weekday: 'long' });
        return `<tr>
          <td>${escapeHtml(tgl)}</td>
          <td>${escapeHtml(hari)}</td>
          <td class="c">${fmtTime(r.check_in) || '—'}</td>
          <td class="c">${fmtTime(r.check_out) || '—'}</td>
          <td class="c">${escapeHtml(STATUS_PRINT_LABELS[r.status] || r.status || '-')}</td>
          <td>${escapeHtml(anomalyText(r)) || ''}</td>
        </tr>`;
      }).join('');

  return `<!doctype html><html lang="id"><head><meta charset="utf-8">
<title>Kehadiran ${escapeHtml(employee?.full_name || '')} — ${escapeHtml(monthLabel)}</title>
<style>
  @page { size: A4; margin: 15mm; }
  * { box-sizing: border-box; }
  body { font-family: Arial, Helvetica, sans-serif; color: #1a1a2e; font-size: 12px; margin: 0; }
  h1 { font-size: 18px; margin: 0 0 2px; }
  .sub { color: #667; font-size: 12px; margin-bottom: 14px; }
  .info { display: grid; grid-template-columns: 1fr 1fr; gap: 2px 24px; margin-bottom: 12px; }
  .info div span { color: #667; display: inline-block; min-width: 120px; }
  .summary { display: flex; flex-wrap: wrap; gap: 8px; margin: 10px 0 14px; }
  .chip { border: 1px solid #d5dae3; border-radius: 4px; padding: 4px 10px; font-size: 11px; }
  .chip b { font-size: 13px; }
  table { width: 100%; border-collapse: collapse; }
  th, td { border: 1px solid #cfd5df; padding: 5px 8px; text-align: left; font-size: 11.5px; }
  th { background: #eef1f6; }
  td.c { text-align: center; }
  .sign { display: flex; justify-content: space-between; margin-top: 40px; }
  .sign div { text-align: center; width: 40%; }
  .sign .line { margin-top: 56px; border-top: 1px solid #333; padding-top: 4px; }
  .foot { margin-top: 18px; color: #999; font-size: 10px; }
  @media print { .noprint { display: none; } }
</style></head>
<body>
  <button class="noprint" onclick="window.print()" style="float:right;padding:8px 16px;cursor:pointer">Cetak</button>
  <h1>Laporan Kehadiran Bulanan</h1>
  <div class="sub">Periode: <b>${escapeHtml(monthLabel)}</b></div>
  <div class="info">
    <div><span>Nama</span> ${escapeHtml(employee?.full_name || '-')}</div>
    <div><span>Cabang</span> ${escapeHtml(employee?.branch_name || '-')}</div>
    <div><span>Kode Karyawan</span> ${escapeHtml(employee?.employee_code || '-')}</div>
    <div><span>Jabatan</span> ${escapeHtml(employee?.position_name || '-')}</div>
  </div>
  <div class="summary">
    <span class="chip">Hadir <b>${counts.present}</b></span>
    <span class="chip">Absen <b>${counts.absent}</b></span>
    <span class="chip">Libur <b>${counts.leave}</b></span>
    <span class="chip">Libur <b>${counts.holiday}</b></span>
    <span class="chip">Terlambat <b>${lateCount}</b></span>
    <span class="chip">Skor Evaluasi <b>${score}/100</b> (${violCount} pelanggaran)</span>
  </div>
  <table>
    <thead><tr><th>Tanggal</th><th>Hari</th><th>Masuk</th><th>Pulang</th><th>Status</th><th>Keterangan</th></tr></thead>
    <tbody>${bodyRows}</tbody>
  </table>
  <div class="sign">
    <div><div>Karyawan</div><div class="line">${escapeHtml(employee?.full_name || '')}</div></div>
    <div><div>Disetujui oleh</div><div class="line">(&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;)</div></div>
  </div>
  <div class="foot">Dicetak pada ${escapeHtml(printedAt)}</div>
</body></html>`;
}

export function AttendanceTab({ employeeId, employee }) {
  const now = new Date();
  const [month, setMonth] = useState(`${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`);
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(false);
  const [perf, setPerf] = useState(null);

  useEffect(() => {
    const [y, m] = month.split('-').map(Number);
    const from = `${month}-01`;
    const to = ymd(new Date(y, m, 0)); // last day of month (toISOString would give the day before in WIB)
    setLoading(true);
    getAttendance({ employee_id: employeeId, date_from: from, date_to: to })
      .then(r => setRows(r.data?.data || []))
      .catch(() => setRows([]))
      .finally(() => setLoading(false));
    getEmployeePerformance(employeeId, { month })
      .then(r => setPerf(r.data))
      .catch(() => setPerf(null));
  }, [employeeId, month]);

  const score = perf?.score ?? 100;
  const violCount = perf?.violations?.length ?? 0;
  const sc = perfScoreColor(score);

  const handlePrint = () => {
    const html = buildAttendancePrintHtml({ employee, month, rows, perf });
    const win = window.open('', '_blank');
    if (!win) return; // popup blocked
    win.document.write(html);
    win.document.close();
    win.focus();
    setTimeout(() => { try { win.print(); } catch { /* user can use the Cetak button */ } }, 300);
  };

  return (
    <div className="card">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem', gap: '0.5rem', flexWrap: 'wrap' }}>
        <h3 style={{ margin: 0, fontSize: '1rem' }}>Kehadiran Bulanan</h3>
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <input type="month" value={month} onChange={e => setMonth(e.target.value)} />
          <button onClick={handlePrint} disabled={loading} className="btn btn-secondary btn-sm" title="Cetak kehadiran bulan ini">🖨️ Cetak</button>
        </div>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', padding: '0.85rem 1rem', borderRadius: '6px', background: sc.bg, marginBottom: '1rem' }}>
        <div>
          <div style={{ fontSize: '0.72rem', color: '#667', textTransform: 'uppercase', letterSpacing: '0.03em' }}>Skor Evaluasi Bulan Ini</div>
          <div style={{ fontSize: '1.6rem', fontWeight: 700, color: sc.color }}>{score}<span style={{ fontSize: '0.85rem', color: '#999' }}> / 100</span></div>
        </div>
        <div style={{ fontSize: '0.85rem', color: '#667' }}>{violCount} pelanggaran</div>
        <Link to="/hr/performance" style={{ marginLeft: 'auto', fontSize: '0.85rem' }}>Lihat rincian evaluasi →</Link>
      </div>
      {loading ? (
        <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>
      ) : rows.length === 0 ? (
        <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Tidak ada catatan kehadiran pada bulan ini.</p>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table>
            <thead>
              <tr><th>Tanggal</th><th>Masuk</th><th>Sumber</th><th>Pulang</th><th>Sumber</th><th>Status</th><th>Anomali</th></tr>
            </thead>
            <tbody>
              {rows.map(r => (
                <tr key={r.id}>
                  <td>{new Date(r.date).toLocaleDateString('id-ID', { weekday: 'short', day: '2-digit', month: 'short' })}</td>
                  <td>{fmtTime(r.check_in)}</td>
                  <td><SourceBadge source={r.check_in_source} /></td>
                  <td>{fmtTime(r.check_out)}</td>
                  <td><SourceBadge source={r.check_out_source} /></td>
                  <td><StatusChip status={r.status} /></td>
                  <td><AnomalyChips record={r} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

