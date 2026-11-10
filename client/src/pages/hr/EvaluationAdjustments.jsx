import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getEvaluationAdjustments, applyEvaluationAdjustments,
  createAttendancePermission, setAttendanceReviewed,
} from '../../api';
import { fmtTime, monthWIB } from '../../time';
import { canApprove } from '../../roles';

// Penyesuaian Evaluasi — review a month's re-evaluation before applying it.
//
// Automatic violations are written every night from the attendance that exists
// at that moment. Attendance that arrives later (a fingerprint file imported
// afterwards, a correction) leaves them stale: absence penalties on days that are
// now present, lateness that was never scored, a monthly absence grace counted
// against days that simply were not imported yet. The server runs the
// re-evaluation as a dry run and returns the difference; nothing changes until
// "Terapkan" is pressed.

const REASONS = {
  stale_absence:   { label: 'Pinalti alpa basi',     hint: 'Hari ini sudah tercatat hadir/cuti, tapi pinalti alpa dari evaluasi malam masih menempel.', color: '#2e7d32', bg: '#e8f5e9' },
  not_scored:      { label: 'Belum dinilai',          hint: 'Data absen masuk setelah evaluasi malam, jadi pelanggarannya belum pernah dibuat.',        color: '#c62828', bg: '#fdecea' },
  absence_recount: { label: 'Alpa dihitung ulang',    hint: 'Toleransi alpa bulanan dan alpa berturut-turut dihitung ulang dari alpa yang sebenarnya.',   color: '#6a1b9a', bg: '#f3e5f5' },
  recalculated:    { label: 'Dihitung ulang',         hint: 'Dihitung ulang dari data absensi terbaru.',                                                   color: '#455a64', bg: '#eceff1' },
};

const STATUS_LABEL = { present: 'Hadir', absent: 'Alpa', leave: 'Cuti', holiday: 'Libur' };

const fmtDay = (iso) =>
  new Date(iso + 'T00:00:00').toLocaleDateString('id-ID', { weekday: 'short', day: 'numeric', month: 'short' });
const fmtMonth = (ym) =>
  new Date(ym + '-01T00:00:00').toLocaleDateString('id-ID', { month: 'long', year: 'numeric' });

function Tile({ value, label, tone }) {
  const color = tone === 'bad' ? '#c62828' : tone === 'good' ? '#2e7d32' : '#1a1a2e';
  return (
    <div className="card" style={{ padding: '0.8rem 1rem', margin: 0 }}>
      <div style={{ fontSize: '1.5rem', fontWeight: 700, color, fontVariantNumeric: 'tabular-nums' }}>{value}</div>
      <div style={{ fontSize: '0.78rem', color: '#666' }}>{label}</div>
    </div>
  );
}

function ReasonChip({ reason }) {
  const r = REASONS[reason] || REASONS.recalculated;
  return (
    <span title={r.hint} style={{ background: r.bg, color: r.color, padding: '0.1rem 0.45rem', borderRadius: '4px', fontSize: '0.72rem', fontWeight: 600, whiteSpace: 'nowrap' }}>
      {r.label}
    </span>
  );
}

function ScoreChange({ before, after }) {
  const d = after - before;
  return (
    <span style={{ fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' }}>
      <span style={{ color: '#888' }}>{before}</span>
      <span style={{ color: '#bbb', margin: '0 0.35rem' }}>→</span>
      <strong>{after}</strong>
      {d !== 0 && (
        <span style={{ marginLeft: '0.4rem', fontSize: '0.8rem', fontWeight: 600, color: d > 0 ? '#2e7d32' : '#c62828' }}>
          ({d > 0 ? '+' : ''}{d})
        </span>
      )}
    </span>
  );
}

// Days far outside the shift without a permit. The attendance itself is always
// recorded with the real times; the manager decides how the day counts:
//   - "Izin susulan": grant the permit after the fact → the day becomes a half
//     day (half-day points, pay cut for the lost hours) instead of late/early.
//   - "Tetap": keep it as late / early leave; the day leaves this list.
function ReviewDays({ data, onChanged }) {
  const manager = canApprove();
  const [busy, setBusy] = useState('');
  const [err, setErr] = useState('');

  const grant = async (d) => {
    const reason = window.prompt(`Alasan izin susulan untuk ${d.full_name} (${fmtDay(d.date)}):`);
    if (!reason || !reason.trim()) return;
    setBusy(d.record_id); setErr('');
    try {
      const types = d.kind === 'both' ? ['late_arrival', 'early_departure']
        : d.kind === 'early' ? ['early_departure'] : ['late_arrival'];
      for (const type of types) {
        await createAttendancePermission({ employee_id: d.employee_id, date: d.date, type, reason: reason.trim(), approve_now: true });
      }
      onChanged();
    } catch (e) {
      setErr(e.response?.data?.error || 'Gagal memberikan izin susulan.');
    } finally {
      setBusy('');
    }
  };

  const keep = async (d) => {
    setBusy(d.record_id); setErr('');
    try { await setAttendanceReviewed(d.record_id, true); onChanged(); }
    catch (e) { setErr(e.response?.data?.error || 'Gagal menyimpan peninjauan.'); }
    finally { setBusy(''); }
  };

  return (
    <>
      <h2 style={{ fontSize: '1.05rem', margin: '0 0 0.5rem' }}>Perlu Ditinjau: Datang/Pulang Jauh di Luar Shift</h2>
      <div className="card" style={{ overflowX: 'auto', marginBottom: '1.25rem' }}>
        <p style={{ margin: '0 0 0.6rem', color: '#555', fontSize: '0.84rem' }}>
          Terlambat ≥ {data.review_late_minutes} menit atau pulang awal ≥ {data.review_early_minutes} menit tanpa izin.
          Jam absen tetap tercatat apa adanya. Beri <strong>izin susulan</strong> jika ada alasan sah (hari itu menjadi setengah hari),
          atau <strong>tetapkan</strong> sebagai terlambat/pulang awal. Ambang diatur di Pengaturan HR.
        </p>
        {err && <div style={{ background: '#fdecea', color: '#c62828', padding: '0.4rem 0.6rem', borderRadius: 4, marginBottom: '0.5rem', fontSize: '0.84rem' }}>{err}</div>}
        {data.review_days.length === 0 ? (
          <p style={{ color: '#888', margin: 0, fontSize: '0.85rem' }}>Tidak ada di bulan ini.</p>
        ) : (
          <table>
            <thead><tr><th>Tanggal</th><th>Karyawan</th><th>Masuk</th><th>Pulang</th><th>Di luar shift</th><th></th></tr></thead>
            <tbody>
              {data.review_days.map((d) => (
                <tr key={d.record_id}>
                  <td style={{ whiteSpace: 'nowrap' }}>{fmtDay(d.date)}</td>
                  <td>
                    <Link to={`/hr/employees/${d.employee_id}`}>{d.full_name}</Link>
                    <div style={{ fontSize: '0.75rem', color: '#999', fontFamily: 'monospace' }}>{d.employee_code}</div>
                  </td>
                  <td>{fmtTime(d.check_in) || '—'}</td>
                  <td>{fmtTime(d.check_out) || '—'}</td>
                  <td style={{ fontSize: '0.84rem', whiteSpace: 'nowrap' }}>
                    {d.late_minutes > 0 && <div style={{ color: '#c62828' }}>Terlambat {d.late_minutes} mnt</div>}
                    {d.early_minutes > 0 && <div style={{ color: '#c62828' }}>Pulang awal {d.early_minutes} mnt</div>}
                  </td>
                  <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                    {manager ? (
                      <span style={{ display: 'inline-flex', gap: '0.35rem' }}>
                        <button className="btn btn-primary btn-sm" disabled={busy === d.record_id} onClick={() => grant(d)}>Izin susulan</button>
                        <button className="btn btn-secondary btn-sm" disabled={busy === d.record_id} onClick={() => keep(d)}>Tetap {d.kind === 'early' ? 'pulang awal' : 'terlambat'}</button>
                      </span>
                    ) : (
                      <span style={{ fontSize: '0.78rem', color: '#888' }}>Menunggu manajer</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}

export default function EvaluationAdjustments() {
  const [month, setMonth] = useState(monthWIB());
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState('');
  const [msg, setMsg] = useState('');
  const [search, setSearch] = useState('');
  const [expanded, setExpanded] = useState(() => new Set());

  // Only the async part lives here; state is reset by whoever starts a load.
  const fetchPreview = useCallback((m) =>
    getEvaluationAdjustments(m)
      .then((r) => { setData(r.data); setExpanded(new Set()); })
      .catch((e) => { setData(null); setError(e.response?.data?.error || 'Gagal menghitung pratinjau.'); })
      .finally(() => setLoading(false)), []);

  useEffect(() => { fetchPreview(month); }, [month, fetchPreview]);

  const load = () => { setLoading(true); setError(''); fetchPreview(month); };
  const changeMonth = (m) => { setMonth(m); setMsg(''); setError(''); setLoading(true); };

  const employees = useMemo(() => {
    const list = data?.employees || [];
    const q = search.trim().toLowerCase();
    if (!q) return list;
    return list.filter((e) => e.full_name.toLowerCase().includes(q) || e.employee_code.toLowerCase().includes(q));
  }, [data, search]);

  const reasonCounts = useMemo(() => {
    const c = {};
    for (const e of data?.employees || []) for (const ch of e.changes) c[ch.reason] = (c[ch.reason] || 0) + 1;
    return c;
  }, [data]);

  const toggle = (id) => setExpanded((s) => {
    const n = new Set(s);
    if (n.has(id)) n.delete(id); else n.add(id);
    return n;
  });

  const nothingToDo = data && data.employees.length === 0 && data.new_absent === 0;

  const handleApply = async () => {
    const n = data?.employees.length || 0;
    if (!window.confirm(`Terapkan penyesuaian evaluasi ${fmtMonth(month)}?\n\nPelanggaran otomatis ${data.from} s/d ${data.to} dihitung ulang; skor ${n} karyawan berubah. Pelanggaran manual tidak disentuh.`)) return;
    setApplying(true);
    setMsg('');
    try {
      await applyEvaluationAdjustments(month);
      setMsg(`Penyesuaian ${fmtMonth(month)} diterapkan.`);
      load();
    } catch (e) {
      setError(e.response?.data?.error || 'Gagal menerapkan penyesuaian.');
    } finally {
      setApplying(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Penyesuaian Evaluasi</h1>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <Link to="/hr/performance" className="btn btn-secondary">Evaluasi Karyawan</Link>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '1rem' }}>
        <p style={{ margin: '0 0 0.85rem', color: '#555', fontSize: '0.88rem', maxWidth: 820 }}>
          Pelanggaran otomatis dihitung setiap malam dari data absensi saat itu. Jika data masuk belakangan
          (impor sidik jari, koreksi), hasilnya bisa basi. Halaman ini menghitung ulang bulan yang dipilih
          <strong> tanpa menyimpan apa pun</strong> dan menunjukkan perbedaannya. Perubahan baru disimpan
          setelah Anda menekan <em>Terapkan</em>. Pelanggaran manual tidak pernah diubah.
        </p>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.85rem', alignItems: 'flex-end' }}>
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Bulan</label>
            <input type="month" value={month} max={monthWIB()} onChange={(e) => e.target.value && changeMonth(e.target.value)} />
          </div>
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Cari Nama / Kode</label>
            <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="cari…" />
          </div>
          <button onClick={load} className="btn btn-secondary btn-sm" disabled={loading}>
            {loading ? 'Menghitung…' : 'Hitung Ulang Pratinjau'}
          </button>
          <div style={{ flex: 1 }} />
          <button onClick={handleApply} className="btn btn-primary" disabled={loading || applying || !data || nothingToDo}>
            {applying ? 'Menerapkan…' : 'Terapkan Penyesuaian'}
          </button>
        </div>
        {data && (
          <div style={{ marginTop: '0.6rem', fontSize: '0.78rem', color: '#888' }}>
            Rentang dievaluasi: {fmtDay(data.from)} – {fmtDay(data.to)} (hari ini belum dinilai)
          </div>
        )}
      </div>

      {msg && <div className="card" style={{ marginBottom: '1rem', background: '#e8f5e9', color: '#2e7d32' }}>{msg}</div>}
      {error && <div className="card" style={{ marginBottom: '1rem', background: '#fdecea', color: '#c62828' }}>{error}</div>}

      {loading && !data && (
        <div className="card"><p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Menghitung ulang satu bulan… ini bisa memakan waktu sebentar.</p></div>
      )}

      {data && (
        <>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', gap: '0.75rem', marginBottom: '1rem', opacity: loading ? 0.5 : 1 }}>
            <Tile value={data.employees.length} label="Karyawan berubah skor/pelanggaran" />
            <Tile value={data.removed} label="Pelanggaran akan dihapus" tone={data.removed ? 'good' : undefined} />
            <Tile value={data.added} label="Pelanggaran akan ditambah" tone={data.added ? 'bad' : undefined} />
            <Tile value={data.new_absent} label="Hari tanpa data → alpa" tone={data.new_absent ? 'bad' : undefined} />
            <Tile value={data.leave_punches.length} label="Absen saat cuti (tinjau)" tone={data.leave_punches.length ? 'bad' : undefined} />
            <Tile value={data.review_days.length} label="Datang/pulang jauh di luar shift" tone={data.review_days.length ? 'bad' : undefined} />
          </div>

          <ReviewDays data={data} onChanged={load} />

          {Object.keys(reasonCounts).length > 0 && (
            <div className="card" style={{ marginBottom: '1rem', display: 'flex', flexWrap: 'wrap', gap: '0.5rem 1.25rem', fontSize: '0.82rem' }}>
              {Object.entries(REASONS).filter(([k]) => reasonCounts[k]).map(([k, r]) => (
                <span key={k} style={{ display: 'inline-flex', gap: '0.4rem', alignItems: 'center' }}>
                  <ReasonChip reason={k} />
                  <span style={{ color: '#555' }}>{reasonCounts[k]} · {r.hint}</span>
                </span>
              ))}
            </div>
          )}

          <div className="card" style={{ overflowX: 'auto', marginBottom: '1.25rem', opacity: loading ? 0.5 : 1 }}>
            {nothingToDo ? (
              <p style={{ color: '#2e7d32', textAlign: 'center', padding: '1.5rem' }}>
                Evaluasi {fmtMonth(month)} sudah sesuai dengan data absensi terbaru. Tidak ada yang perlu disesuaikan.
              </p>
            ) : employees.length === 0 ? (
              <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Tidak ada karyawan yang cocok dengan pencarian.</p>
            ) : (
              <table>
                <thead>
                  <tr><th></th><th>Karyawan</th><th>Cabang</th><th>Skor sekarang → sesudah</th><th>Perubahan</th></tr>
                </thead>
                <tbody>
                  {employees.map((e) => {
                    const id = e.employee_id;
                    const open = expanded.has(id);
                    const rm = e.changes.filter((c) => c.change === 'removed').length;
                    const ad = e.changes.length - rm;
                    return (
                      <Fragment key={id}>
                        <tr style={{ cursor: 'pointer' }} onClick={() => toggle(id)}>
                          <td style={{ width: 24, color: '#999' }}>{open ? '▾' : '▸'}</td>
                          <td>
                            <Link to={`/hr/employees/${id}`} onClick={(ev) => ev.stopPropagation()} style={{ fontWeight: 500 }}>{e.full_name}</Link>
                            <div style={{ fontSize: '0.75rem', color: '#999', fontFamily: 'monospace' }}>{e.employee_code}</div>
                          </td>
                          <td style={{ fontSize: '0.85rem' }}>{e.branch_name}</td>
                          <td><ScoreChange before={e.score_before} after={e.score_after} /></td>
                          <td style={{ fontSize: '0.82rem', color: '#555', whiteSpace: 'nowrap' }}>
                            {rm > 0 && <span style={{ color: '#2e7d32' }}>−{rm} dihapus</span>}
                            {rm > 0 && ad > 0 && ' · '}
                            {ad > 0 && <span style={{ color: '#c62828' }}>+{ad} ditambah</span>}
                            {rm === 0 && ad === 0 && 'skor tersimpan diperbarui'}
                          </td>
                        </tr>
                        {open && (
                          <tr>
                            <td></td>
                            <td colSpan={4} style={{ background: '#fafafa', padding: '0.5rem 0.75rem' }}>
                              {e.changes.length === 0 ? (
                                <span style={{ color: '#888', fontSize: '0.82rem' }}>Pelanggaran tidak berubah; skor tersimpan belum dihitung ulang.</span>
                              ) : (
                                <table style={{ fontSize: '0.82rem' }}>
                                  <thead>
                                    <tr><th>Tanggal</th><th>Status hari</th><th>Pelanggaran</th><th style={{ textAlign: 'right' }}>Poin</th><th>Alasan</th></tr>
                                  </thead>
                                  <tbody>
                                    {e.changes.map((c, i) => (
                                      <tr key={i}>
                                        <td style={{ whiteSpace: 'nowrap' }}>{fmtDay(c.date)}</td>
                                        <td>{STATUS_LABEL[c.status] || c.status || '—'}</td>
                                        <td style={{ textDecoration: c.change === 'removed' ? 'line-through' : 'none', color: c.change === 'removed' ? '#888' : '#1a1a2e' }}>
                                          {c.change === 'removed' ? 'Hapus: ' : 'Tambah: '}{c.policy_name}
                                        </td>
                                        <td style={{ textAlign: 'right', fontWeight: 600, color: c.change === 'removed' ? '#2e7d32' : '#c62828', fontVariantNumeric: 'tabular-nums' }}>
                                          {c.change === 'removed' ? '+' : '−'}{c.points}
                                        </td>
                                        <td><ReasonChip reason={c.reason} /></td>
                                      </tr>
                                    ))}
                                  </tbody>
                                </table>
                              )}
                            </td>
                          </tr>
                        )}
                      </Fragment>
                    );
                  })}
                </tbody>
              </table>
            )}
          </div>

          <h2 style={{ fontSize: '1.05rem', margin: '0 0 0.5rem' }}>Absen saat Cuti</h2>
          <div className="card" style={{ overflowX: 'auto' }}>
            <p style={{ margin: '0 0 0.6rem', color: '#555', fontSize: '0.84rem' }}>
              Karyawan absen di hari cuti yang sudah disetujui. Status tetap <strong>cuti</strong>, jadi hari ini tidak
              dinilai terlambat/pulang awal dan tidak dipotong. Jika karyawan sebenarnya bekerja, batalkan cutinya atau
              ubah statusnya di <Link to="/hr/attendance/corrections">Koreksi Kehadiran</Link>.
            </p>
            {data.leave_punches.length === 0 ? (
              <p style={{ color: '#888', margin: 0, fontSize: '0.85rem' }}>Tidak ada di bulan ini.</p>
            ) : (
              <table>
                <thead><tr><th>Tanggal</th><th>Karyawan</th><th>Cabang</th><th>Masuk</th><th>Pulang</th></tr></thead>
                <tbody>
                  {data.leave_punches.map((p) => (
                    <tr key={p.record_id}>
                      <td style={{ whiteSpace: 'nowrap' }}>{fmtDay(p.date)}</td>
                      <td>
                        <Link to={`/hr/employees/${p.employee_id}`}>{p.full_name}</Link>
                        <div style={{ fontSize: '0.75rem', color: '#999', fontFamily: 'monospace' }}>{p.employee_code}</div>
                      </td>
                      <td style={{ fontSize: '0.85rem' }}>{p.branch_name}</td>
                      <td>{fmtTime(p.check_in) || '—'}</td>
                      <td>{fmtTime(p.check_out) || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </>
      )}
    </>
  );
}
