import { useEffect, useState } from 'react';
import { getPayrollLineReview, reviewPayrollLine, unreviewPayrollLine } from '../../../api';
import { fmtIDR } from '../../../format';
import { GradeBadge, ScoreBadge } from './Badges';
import { calcAmounts, fmtDate, numVal } from './shared';

export function ReviewDrawer({ lineId, locked, onClose, onSaved }) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const [overtimeDays, setOvertimeDays] = useState(0);
  const [overtimeHours, setOvertimeHours] = useState(0);
  const [holidayDays, setHolidayDays] = useState(0);
  const [components, setComponents] = useState([]);
  const [note, setNote] = useState('');

  useEffect(() => {
    (async () => {
      setLoading(true); setError('');
      try {
        const { data: d } = await getPayrollLineReview(lineId);
        setData(d);
        setOvertimeDays(Number(d.line.overtime_days ?? 0));
        setOvertimeHours(Number(d.line.overtime_hours ?? 0));
        setHolidayDays(Number(d.line.public_holiday_days ?? 0));
        setNote(d.line.review_note || '');
        setComponents((d.components || []).map((c) => ({ ...c })));
      } catch {
        setError('Gagal memuat detail review');
      } finally {
        setLoading(false);
      }
    })();
  }, [lineId]);

  const live = calcAmounts(data?.line, data?.multipliers, overtimeDays, overtimeHours, holidayDays, components);

  const editable = !locked;
  const setCompAmount = (cid, amount) => setComponents((cs) => cs.map((c) => c.id === cid ? { ...c, amount } : c));

  const save = async () => {
    setBusy(true); setError('');
    try {
      await reviewPayrollLine(lineId, {
        overtime_days: Number(overtimeDays) || 0,
        overtime_hours: Number(overtimeHours) || 0,
        public_holiday_days: Number(holidayDays) || 0,
        components: components
          .filter((c) => c.type === 'bonus' || c.type === 'allowance' || c.type === 'daily_allowance')
          .map((c) => ({ id: c.id, amount: Number(c.amount) || 0 })),
        review_note: note,
      });
      await onSaved();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan review');
      setBusy(false);
    }
  };

  const reopen = async () => {
    setBusy(true); setError('');
    try { await unreviewPayrollLine(lineId); await onSaved(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal membuka kembali'); setBusy(false); }
  };

  const att = data?.attendance;
  const line = data?.line;
  const holidaysWorked = data?.holidays_worked ?? [];
  const overtimeRequests = data?.overtime_requests ?? [];
  const bonusComps = components.filter((c) => c.type === 'bonus');
  const allowanceComps = components.filter((c) => c.type === 'allowance');
  const dailyComps = components.filter((c) => c.type === 'daily_allowance');

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,.35)', display: 'flex', justifyContent: 'flex-end', zIndex: 100 }} onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} style={{ width: '50vw', minWidth: 360, maxWidth: '100%', background: '#fff', height: '100%', display: 'flex', flexDirection: 'column', boxShadow: '-2px 0 12px rgba(0,0,0,.15)' }}>
        <div style={{ flex: 1, overflowY: 'auto', padding: 20 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
            <h2 style={{ margin: 0, fontSize: 18 }}>Review Penggajian</h2>
            <button onClick={onClose} style={{ background: 'none', border: 0, fontSize: 22, cursor: 'pointer', color: '#889' }}>×</button>
          </div>

          {loading ? <p>Memuat…</p> : !line ? <p>Tidak ada data.</p> : (
            <>
              {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 10, borderRadius: 6, marginBottom: 10 }}>{error}</div>}

              <section style={{ marginBottom: 16 }}>
                <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 6px' }}>Ringkasan Absensi</h3>
                <div style={{ display: 'flex', gap: 8, marginBottom: holidaysWorked.length > 0 ? 8 : 0 }}>
                  {[['Hadir', att?.hadir], ['Absen', att?.absen], ['Terlambat', att?.terlambat], ['Libur', att?.cuti], ['Libur Nasional', holidaysWorked.length]].map(([k, v]) => (
                    <div key={k} style={{ flex: 1, background: k === 'Libur Nasional' && v > 0 ? '#fff8e1' : '#f6f7fa', borderRadius: 8, padding: 8, textAlign: 'center', border: k === 'Libur Nasional' && v > 0 ? '1px solid #f9a825' : 'none' }}>
                      <div style={{ fontSize: 18, fontWeight: 700, color: k === 'Libur Nasional' && v > 0 ? '#a06800' : 'inherit' }}>{v ?? 0}</div>
                      <div style={{ fontSize: 11, color: k === 'Libur Nasional' && v > 0 ? '#a06800' : '#889' }}>{k}</div>
                    </div>
                  ))}
                </div>
                {holidaysWorked.length > 0 && (
                  <div style={{ background: '#fff8e1', border: '1px solid #f9a825', borderRadius: 8, padding: '8px 10px', fontSize: 12 }}>
                    <div style={{ fontWeight: 600, color: '#a06800', marginBottom: 4 }}>Masuk pada hari libur nasional:</div>
                    <ul style={{ margin: 0, paddingLeft: 16, color: '#7a5200' }}>
                      {holidaysWorked.map((h) => (
                        <li key={h.id}>{fmtDate(h.date)} — {h.name}</li>
                      ))}
                    </ul>
                  </div>
                )}
              </section>

              <section style={{ marginBottom: 16 }}>
                <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 6px' }}>Evaluasi</h3>
                <div style={{ marginBottom: 6 }}>
                  Skor: <ScoreBadge score={line.performance_score} />
                  <GradeBadge grade={data.grade?.performance_grade} percent={data.grade?.grade_pay_percent} />
                </div>
                {data.grade?.performance_grade && (
                  <div style={{ fontSize: 12, color: '#889', marginBottom: 6 }}>
                    Komponen yang ikut grade dibayar {data.grade.grade_pay_percent}% (grade {data.grade.performance_grade}).
                  </div>
                )}
                {(data.violations || []).length === 0 ? (
                  <div style={{ fontSize: 13, color: '#889' }}>Tidak ada pelanggaran.</div>
                ) : (
                  <ul style={{ margin: 0, paddingLeft: 18, fontSize: 13 }}>
                    {data.violations.map((v) => (
                      <li key={v.id}>{fmtDate(v.date)} — {v.policy_name || 'Manual'} (−{v.points})</li>
                    ))}
                  </ul>
                )}
              </section>

              <section style={{ marginBottom: 16 }}>
                <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 6px' }}>Cicilan Kasbon Jatuh Tempo</h3>
                {(data.installments || []).length === 0 ? (
                  <div style={{ fontSize: 13, color: '#889' }}>Tidak ada cicilan.</div>
                ) : (
                  <ul style={{ margin: 0, paddingLeft: 18, fontSize: 13 }}>
                    {data.installments.map((i) => (
                      <li key={i.id}>{fmtDate(i.due_month)} — {fmtIDR(i.amount)}</li>
                    ))}
                  </ul>
                )}
              </section>

              <section style={{ marginBottom: 16 }}>
                <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 6px' }}>Permintaan Lembur</h3>
                {overtimeRequests.length === 0 ? (
                  <div style={{ fontSize: 13, color: '#889' }}>Tidak ada permintaan lembur pada periode ini.</div>
                ) : (
                  <>
                    <ul style={{ margin: 0, paddingLeft: 18, fontSize: 13 }}>
                      {overtimeRequests.map((req) => (
                        <li key={req.id}>
                          {fmtDate(req.date)} — <strong>{Number(req.hours).toFixed(1)} jam</strong>
                          {req.reason ? <span style={{ color: '#667' }}> · {req.reason}</span> : null}
                        </li>
                      ))}
                    </ul>
                    <div style={{ fontSize: 12, color: '#667', marginTop: 6 }}>
                      Total: <strong>{overtimeRequests.reduce((s, r) => s + Number(r.hours || 0), 0).toFixed(1)} jam</strong>
                    </div>
                  </>
                )}
              </section>

              <section style={{ marginBottom: 16 }}>
                <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 6px' }}>Penyesuaian</h3>
                <label style={{ display: 'block', fontSize: 13, marginBottom: 4 }}>Hari Lembur</label>
                <input type="number" step="0.5" min="0" value={overtimeDays} disabled={!editable}
                  onChange={(e) => setOvertimeDays(e.target.value)}
                  style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', marginBottom: 10 }} />
                <label style={{ display: 'block', fontSize: 13, marginBottom: 4 }}>
                  Jam Lembur{line.overtime_hourly_rate ? ` (${fmtIDR(line.overtime_hourly_rate)}/jam)` : ''}
                </label>
                <input type="number" step="0.5" min="0" value={overtimeHours} disabled={!editable}
                  onChange={(e) => setOvertimeHours(e.target.value)}
                  style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd' }} />
                <p style={{ fontSize: 11, color: '#9aa', margin: '2px 0 10px' }}>
                  Diisi manual — tidak dihitung otomatis dari jam absensi.
                </p>
                <label style={{ display: 'block', fontSize: 13, marginBottom: 4 }}>Hari Libur Nasional (masuk kerja)</label>
                <div style={{ display: 'flex', gap: 6, alignItems: 'center', marginBottom: 4 }}>
                  <input type="number" step="0.5" min="0" value={holidayDays} disabled={!editable}
                    onChange={(e) => setHolidayDays(e.target.value)}
                    style={{ flex: 1, padding: 8, borderRadius: 6, border: holidayDays > 0 ? '1px solid #f9a825' : '1px solid #ccd' }} />
                  {editable && Number(holidayDays) !== holidaysWorked.length && (
                    <button type="button"
                      onClick={() => setHolidayDays(holidaysWorked.length)}
                      title={`Sinkronkan dengan data absensi terkini (${holidaysWorked.length} hari)`}
                      style={{ background: '#fff8e1', border: '1px solid #f9a825', borderRadius: 6, color: '#a06800', fontSize: 12, padding: '8px 10px', cursor: 'pointer', whiteSpace: 'nowrap', fontWeight: 600 }}>
                      Sinkron ({holidaysWorked.length})
                    </button>
                  )}
                </div>
                {editable && Number(holidayDays) !== holidaysWorked.length && (
                  <p style={{ fontSize: 11, color: '#a06800', margin: '0 0 10px', background: '#fff8e1', padding: '4px 8px', borderRadius: 4 }}>
                    Data absensi menunjukkan {holidaysWorked.length} hari libur nasional — klik Sinkron untuk memperbarui.
                  </p>
                )}
                {(editable && Number(holidayDays) === holidaysWorked.length) && (
                  <p style={{ fontSize: 11, color: '#9aa', margin: '2px 0 10px' }}>
                    Otomatis diisi dari hari libur nasional yang tercatat hadir — dapat disesuaikan.
                  </p>
                )}

                {allowanceComps.length > 0 && (
                  <>
                    <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Tunjangan / Variabel</div>
                    {allowanceComps.map((c) => (
                      <div key={c.id} style={{ marginBottom: 8 }}>
                        <label style={{ display: 'block', fontSize: 12, color: '#778', marginBottom: 2 }}>{c.name}</label>
                        <div style={{ display: 'flex', gap: 6 }}>
                          <input type="number" min="0" value={c.amount} disabled={!editable}
                            onChange={(e) => setCompAmount(c.id, e.target.value)}
                            style={{ flex: 1, padding: 8, borderRadius: 6, border: Number(c.amount) === 0 ? '1px solid #e0b0b0' : '1px solid #ccd', background: Number(c.amount) === 0 ? '#fff8f8' : '#fff' }} />
                          {editable && Number(c.amount) !== 0 && (
                            <button type="button" onClick={() => setCompAmount(c.id, 0)}
                              title="Tidak eligible — set ke 0"
                              style={{ background: 'none', border: '1px solid #e0b0b0', borderRadius: 6, color: '#c5221f', fontSize: 11, padding: '0 10px', cursor: 'pointer', whiteSpace: 'nowrap' }}>
                              Tidak Eligible
                            </button>
                          )}
                        </div>
                      </div>
                    ))}
                  </>
                )}

                {bonusComps.length > 0 && (
                  <>
                    <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Bonus / Variabel</div>
                    {bonusComps.map((c) => (
                      <div key={c.id} style={{ marginBottom: 8 }}>
                        <label style={{ display: 'block', fontSize: 12, color: '#778' }}>{c.name}</label>
                        <input type="number" min="0" value={c.amount} disabled={!editable}
                          onChange={(e) => setCompAmount(c.id, e.target.value)}
                          style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd' }} />
                      </div>
                    ))}
                  </>
                )}

                {dailyComps.length > 0 && (
                  <>
                    <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 2 }}>Dibayar Harian (Tunai)</div>
                    <p style={{ fontSize: 11, color: '#9aa', margin: '0 0 6px' }}>
                      Sudah dibagikan tunai setiap hari di luar transfer gaji. Tidak menambah gaji bruto/bersih — hanya tampil di slip gaji.
                    </p>
                    {dailyComps.map((c) => (
                      <div key={c.id} style={{ marginBottom: 8 }}>
                        <label style={{ display: 'block', fontSize: 12, color: '#778', marginBottom: 2 }}>{c.name}</label>
                        <input type="number" min="0" value={c.amount} disabled={!editable}
                          onChange={(e) => setCompAmount(c.id, e.target.value)}
                          style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px dashed #ccd', background: '#fafbfd' }} />
                      </div>
                    ))}
                  </>
                )}

                <label style={{ display: 'block', fontSize: 13, marginBottom: 4, marginTop: 4 }}>Catatan</label>
                <textarea value={note} disabled={!editable} onChange={(e) => setNote(e.target.value)} rows={2}
                  style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd' }} />
              </section>
            </>
          )}
        </div>

        {!loading && line && (
          <div style={{ borderTop: '1px solid #e6e8ee', padding: '12px 20px', background: '#fff' }}>
            <div style={{ background: '#f6f7fa', borderRadius: 8, padding: '10px 12px', marginBottom: 10, fontSize: 14 }}>
              {live && (
                <div style={{ fontSize: 12, color: '#889', marginBottom: 6, display: 'flex', flexDirection: 'column', gap: 2 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Gaji Pokok</span><span>{fmtIDR(line.base_salary)}</span></div>
                  {live.overtimeAmt > 0 && <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Lembur Harian</span><span>{fmtIDR(live.overtimeAmt)}</span></div>}
                  {live.overtimeHourlyAmt > 0 && <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Lembur Jam</span><span>{fmtIDR(live.overtimeHourlyAmt)}</span></div>}
                  {live.holidayAmt > 0 && <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Libur Nasional</span><span>{fmtIDR(live.holidayAmt)}</span></div>}
                </div>
              )}
              <div style={{ display: 'flex', justifyContent: 'space-between', borderTop: live ? '1px solid #e0e3ea' : 'none', paddingTop: live ? 6 : 0 }}>
                <span>Gaji Bruto</span>
                <strong>{fmtIDR(live ? live.gross : line.gross_pay)}</strong>
              </div>
              {numVal(line.half_day_deduction) > 0 && (
                <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 2, color: '#c5221f', fontSize: 12 }}>
                  <span>Potongan Setengah Hari ({Number(line.half_day_hours || 0).toLocaleString('id-ID', { maximumFractionDigits: 2 })} jam)</span>
                  <span>−{fmtIDR(line.half_day_deduction)}</span>
                </div>
              )}
              {numVal(line.attendance_cut_deduction) > 0 && (
                <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 2, color: '#c5221f', fontSize: 12 }}>
                  <span>Potongan Telat / Pulang Awal ({(Number(line.attendance_cut_minutes || 0) / 60).toLocaleString('id-ID', { maximumFractionDigits: 2 })} jam)</span>
                  <span>−{fmtIDR(line.attendance_cut_deduction)}</span>
                </div>
              )}
              <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 2 }}>
                <span>Gaji Bersih</span>
                <strong style={{ color: '#1e7e34', fontSize: 15 }}>{fmtIDR(live ? live.net : line.net_pay)}</strong>
              </div>
              {live && live.dailyPaidTotal > 0 && (
                <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 4, fontSize: 12, color: '#889' }}>
                  <span>Dibayar harian tunai (di luar transfer)</span>
                  <span>{fmtIDR(live.dailyPaidTotal)}</span>
                </div>
              )}
            </div>

            {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 8, borderRadius: 6, marginBottom: 8, fontSize: 13 }}>{error}</div>}

            {editable ? (
              <div style={{ display: 'flex', gap: 8 }}>
                <button onClick={save} disabled={busy}
                  style={{ flex: 1, background: '#1e7e34', color: '#fff', border: 0, borderRadius: 8, padding: '10px', fontWeight: 600, cursor: 'pointer' }}>
                  {busy ? 'Menyimpan…' : 'Tandai Sudah Direview'}
                </button>
                {line.reviewed && (
                  <button onClick={reopen} disabled={busy}
                    style={{ background: '#fff', border: '1px solid #ccd', borderRadius: 8, padding: '10px 14px', cursor: 'pointer' }}>
                    Buka Kembali
                  </button>
                )}
              </div>
            ) : (
              <div style={{ color: '#889', fontSize: 13 }}>Periode terkunci — baris tidak dapat diubah.</div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
