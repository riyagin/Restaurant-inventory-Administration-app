import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getFaceOverview } from '../../api';

// Face & device dashboard — coverage of stored face enrollments plus the health
// of the attendance-device fleet that captures them. The per-employee list of
// who is still missing an enrollment lives at /hr/face/unregistered.

const SOURCE_META = {
  face:        { label: 'Wajah',      color: '#1a56b0', bg: '#e8f0fe' },
  fingerprint: { label: 'Sidik Jari', color: '#b45309', bg: '#fff3e0' },
  manual:      { label: 'Manual',     color: '#555',    bg: '#eeeeee' },
};

const fmtDateTime = (ts) =>
  ts ? new Date(ts).toLocaleString('id-ID', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : null;

// "Terakhir aktif" reads better as an age than an absolute timestamp — an
// operator scanning the fleet wants to spot the kiosk that went quiet.
function relativeAge(ts) {
  if (!ts) return { label: 'Belum pernah', stale: true };
  const mins = Math.round((Date.now() - new Date(ts).getTime()) / 60000);
  if (mins < 1)     return { label: 'Baru saja', stale: false };
  if (mins < 60)    return { label: `${mins} menit lalu`, stale: false };
  const hours = Math.round(mins / 60);
  if (hours < 24)   return { label: `${hours} jam lalu`, stale: hours >= 12 };
  const days = Math.round(hours / 24);
  return { label: `${days} hari lalu`, stale: true };
}

function StatTile({ label, value, sub, accent, to }) {
  const body = (
    <div
      className="card"
      style={{
        margin: 0,
        borderLeft: `4px solid ${accent}`,
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        gap: '0.15rem',
      }}
    >
      <div style={{ fontSize: '0.75rem', color: '#8a93a6', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
        {label}
      </div>
      <div style={{ fontSize: '1.75rem', fontWeight: 700, color: '#1a1a2e', lineHeight: 1.1 }}>{value}</div>
      {sub && <div style={{ fontSize: '0.8rem', color: '#8a93a6' }}>{sub}</div>}
    </div>
  );
  return to ? <Link to={to} style={{ textDecoration: 'none' }}>{body}</Link> : body;
}

// Coverage bar: enrolled portion in green, remainder grey.
function CoverageBar({ enrolled, total }) {
  const pct = total > 0 ? Math.round((enrolled / total) * 100) : 0;
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
      <div style={{ flex: 1, height: 8, background: '#eef1f6', borderRadius: 4, overflow: 'hidden', minWidth: 80 }}>
        <div style={{ width: `${pct}%`, height: '100%', background: pct === 100 ? '#1e7e34' : '#4f80e1' }} />
      </div>
      <span style={{ fontSize: '0.8rem', fontWeight: 600, color: '#555', minWidth: 38, textAlign: 'right' }}>{pct}%</span>
    </div>
  );
}

export default function FaceDashboard() {
  const [data, setData]       = useState(null);
  const [loading, setLoading] = useState(true);
  const [days, setDays]       = useState(30);
  const [error, setError]     = useState('');

  useEffect(() => {
    setLoading(true);
    setError('');
    getFaceOverview({ days })
      .then(r => setData(r.data))
      .catch(() => setError('Gagal memuat ringkasan data wajah'))
      .finally(() => setLoading(false));
  }, [days]);

  if (loading && !data) {
    return <div className="card" style={{ textAlign: 'center', padding: '2.5rem', color: '#aaa' }}>Memuat…</div>;
  }
  if (error) {
    return <div className="card" style={{ textAlign: 'center', padding: '2.5rem', color: '#c5221f' }}>{error}</div>;
  }
  if (!data) return null;

  const { total_active: total, enrolled, missing } = data;
  const pct = total > 0 ? Math.round((enrolled / total) * 100) : 0;
  const devices = data.devices || [];
  const activeDevices = devices.filter(d => d.is_active).length;
  const versions = data.versions || [];
  const sources = data.sources || [];
  const sourceTotal = sources.reduce((s, x) => s + x.count, 0);
  // Branches that have staff but no kiosk can never enrol anyone locally.
  const branchesWithoutDevice = (data.branches || []).filter(b => b.total > 0 && b.device_count === 0);

  return (
    <>
      <div className="page-header">
        <h1>Wajah &amp; Perangkat</h1>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <Link to="/hr/face/unregistered" className="btn btn-primary">Belum Terdaftar ({missing})</Link>
          <Link to="/hr/attendance/settings" className="btn btn-secondary">Kelola Perangkat</Link>
        </div>
      </div>

      {/* ── stat tiles ── */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '0.75rem', marginBottom: '1rem' }}>
        <StatTile label="Karyawan Aktif" value={total} accent="#607d8b" />
        <StatTile label="Wajah Terdaftar" value={enrolled} sub={`${pct}% dari karyawan aktif`} accent="#1e7e34" />
        <StatTile
          label="Belum Terdaftar"
          value={missing}
          sub={missing > 0 ? 'Klik untuk melihat daftar' : 'Semua sudah terdaftar'}
          accent={missing > 0 ? '#c5221f' : '#1e7e34'}
          to={missing > 0 ? '/hr/face/unregistered' : undefined}
        />
        <StatTile
          label="Perangkat Aktif"
          value={`${activeDevices} / ${devices.length}`}
          sub="perangkat absensi terdaftar"
          accent="#4f80e1"
        />
      </div>

      {/* ── warnings ── */}
      {versions.length > 1 && (
        <div className="card" style={{ marginBottom: '1rem', borderLeft: '4px solid #f0a020', background: '#fffaf0' }}>
          <strong style={{ color: '#a06800' }}>⚠️ Data wajah tersimpan dalam {versions.length} versi model berbeda</strong>
          <p style={{ margin: '0.4rem 0 0.6rem', fontSize: '0.85rem', color: '#7a5a10' }}>
            Embedding hanya bisa dicocokkan oleh perangkat yang memakai model yang sama. Karyawan pada versi minoritas
            perlu didaftarkan ulang agar bisa dikenali.
          </p>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem' }}>
            {versions.map(v => (
              <span key={v.version} className="badge" style={{ background: '#fff', border: '1px solid #f0d090', color: '#a06800' }}>
                {v.version} · {v.count} karyawan
              </span>
            ))}
          </div>
        </div>
      )}

      {branchesWithoutDevice.length > 0 && (
        <div className="card" style={{ marginBottom: '1rem', borderLeft: '4px solid #f0a020', background: '#fffaf0' }}>
          <strong style={{ color: '#a06800' }}>⚠️ Cabang tanpa perangkat absensi</strong>
          <p style={{ margin: '0.4rem 0 0', fontSize: '0.85rem', color: '#7a5a10' }}>
            {branchesWithoutDevice.map(b => b.branch_name).join(', ')} memiliki karyawan aktif tetapi belum punya
            perangkat, sehingga pendaftaran wajah tidak bisa dilakukan di sana.
          </p>
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: '1rem', alignItems: 'start' }}>

        {/* ── coverage per branch ── */}
        <div className="card">
          <div className="card-header"><h2>Cakupan per Cabang</h2></div>
          {(data.branches || []).length === 0 ? (
            <div style={{ color: '#aaa', padding: '1rem 0' }}>Belum ada cabang.</div>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Cabang</th>
                  <th style={{ width: '35%' }}>Cakupan</th>
                  <th style={{ textAlign: 'right' }}>Terdaftar</th>
                  <th style={{ textAlign: 'right' }}>Belum</th>
                  <th style={{ textAlign: 'right' }}>Perangkat</th>
                </tr>
              </thead>
              <tbody>
                {data.branches.map(b => (
                  <tr key={b.branch_id}>
                    <td style={{ fontWeight: 500 }}>{b.branch_name}</td>
                    <td><CoverageBar enrolled={b.enrolled} total={b.total} /></td>
                    <td style={{ textAlign: 'right' }}>{b.enrolled} / {b.total}</td>
                    <td style={{ textAlign: 'right', color: b.missing > 0 ? '#c5221f' : '#8a93a6', fontWeight: b.missing > 0 ? 600 : 400 }}>
                      {b.missing}
                    </td>
                    <td style={{ textAlign: 'right', color: b.device_count === 0 ? '#c5221f' : '#555' }}>{b.device_count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {/* ── source mix ── */}
        <div className="card">
          <div className="card-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <h2>Sumber Absen Masuk</h2>
            <select value={days} onChange={e => setDays(Number(e.target.value))} style={{ fontSize: '0.82rem' }}>
              <option value={7}>7 hari terakhir</option>
              <option value={30}>30 hari terakhir</option>
              <option value={90}>90 hari terakhir</option>
            </select>
          </div>
          {sourceTotal === 0 ? (
            <div style={{ color: '#aaa', padding: '1rem 0' }}>Belum ada absen masuk pada periode ini.</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.7rem' }}>
              {sources.map(s => {
                const meta = SOURCE_META[s.source] || { label: s.source, color: '#555', bg: '#eee' };
                const p = Math.round((s.count / sourceTotal) * 100);
                return (
                  <div key={s.source}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.82rem', marginBottom: '0.25rem' }}>
                      <span style={{ fontWeight: 600, color: meta.color }}>{meta.label}</span>
                      <span style={{ color: '#8a93a6' }}>{s.count} · {p}%</span>
                    </div>
                    <div style={{ height: 8, background: '#eef1f6', borderRadius: 4, overflow: 'hidden' }}>
                      <div style={{ width: `${p}%`, height: '100%', background: meta.color }} />
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* ── device fleet ── */}
        <div className="card">
          <div className="card-header"><h2>Perangkat Absensi</h2></div>
          {devices.length === 0 ? (
            <div style={{ color: '#aaa', padding: '1rem 0' }}>
              Belum ada perangkat terdaftar. <Link to="/hr/attendance/settings">Daftarkan perangkat</Link>.
            </div>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Nama</th>
                  <th>Cabang</th>
                  <th>Status</th>
                  <th>Terakhir Aktif</th>
                  <th style={{ textAlign: 'right' }}>Absen Hari Ini</th>
                </tr>
              </thead>
              <tbody>
                {devices.map(d => {
                  const age = relativeAge(d.last_seen_at);
                  return (
                    <tr key={d.id}>
                      <td style={{ fontWeight: 500 }}>{d.name}</td>
                      <td>{d.branch_name || <span style={{ color: '#c5221f' }}>Belum diatur</span>}</td>
                      <td>
                        <span className="badge" style={d.is_active
                          ? { background: '#e6f4ea', color: '#1e7e34' }
                          : { background: '#fce8e6', color: '#c5221f' }}>
                          {d.is_active ? 'Aktif' : 'Nonaktif'}
                        </span>
                      </td>
                      <td
                        title={fmtDateTime(d.last_seen_at) || 'Belum pernah terhubung'}
                        style={{ color: age.stale ? '#c5221f' : '#555', fontSize: '0.85rem' }}
                      >
                        {age.label}
                      </td>
                      <td style={{ textAlign: 'right', fontWeight: d.check_ins_today > 0 ? 600 : 400, color: d.check_ins_today > 0 ? '#1a1a2e' : '#8a93a6' }}>
                        {d.check_ins_today}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        {/* ── recent enrollments ── */}
        <div className="card">
          <div className="card-header"><h2>Pendaftaran Wajah Terbaru</h2></div>
          {(data.recent_enrollments || []).length === 0 ? (
            <div style={{ color: '#aaa', padding: '1rem 0' }}>Belum ada data wajah yang terdaftar.</div>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Nama</th>
                  <th>Cabang</th>
                  <th>Model</th>
                  <th>Waktu</th>
                </tr>
              </thead>
              <tbody>
                {data.recent_enrollments.map(e => (
                  <tr key={e.id}>
                    <td>
                      <Link to={`/hr/employees/${e.id}`} style={{ fontWeight: 500, textDecoration: 'none', color: '#1a1a2e' }}>
                        {e.full_name}
                      </Link>
                      <div style={{ fontSize: '0.72rem', color: '#999', fontFamily: 'monospace' }}>{e.employee_code}</div>
                    </td>
                    <td>{e.branch_name}</td>
                    <td style={{ fontSize: '0.78rem', color: '#8a93a6', fontFamily: 'monospace' }}>
                      {e.face_embedding_version || '—'}
                    </td>
                    <td style={{ fontSize: '0.82rem', color: '#555' }}>{fmtDateTime(e.face_enrolled_at) || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </>
  );
}
