import { useEffect, useState, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import {
  getPayrollPeriod, getPayrollLines, getPayrollLineReview,
  reviewPayrollLine, unreviewPayrollLine,
  closePayrollPeriod, markPayrollPeriodPaid, getPositions, getBranches,
  downloadPayslip, downloadPeriodPayslips,
} from '../../api';

// Trigger a browser download from an Axios blob response.
function saveBlob(data, filename) {
  const url = window.URL.createObjectURL(new Blob([data]));
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.URL.revokeObjectURL(url);
}

const STATUS_LABELS = { open: 'Terbuka', closed: 'Ditutup', paid: 'Dibayar' };
const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));
const fmtMonth = (d) => d ? new Date(d).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' }) : '-';
const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';
const numVal = (n) => Number(n ?? 0);

function ScoreBadge({ score }) {
  if (score === null || score === undefined) return <span style={{ color: '#aab' }}>—</span>;
  const s = Number(score);
  let bg = '#e6f4ea', color = '#1e7e34';
  if (s < 60) { bg = '#fce8e6'; color = '#c5221f'; }
  else if (s < 80) { bg = '#fff8e1'; color = '#a06800'; }
  return <span style={{ background: bg, color, padding: '2px 8px', borderRadius: 10, fontSize: 12, fontWeight: 700 }}>{s}</span>;
}

export default function PayrollPeriodDetail() {
  const { id } = useParams();
  const [period, setPeriod] = useState(null);
  const [summary, setSummary] = useState(null);
  const [lines, setLines] = useState([]);
  const [positions, setPositions] = useState([]);
  const [branches, setBranches] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const [q, setQ] = useState('');
  const [positionId, setPositionId] = useState('');
  const [branchId, setBranchId] = useState('');
  const [sort, setSort] = useState('name');
  const [order, setOrder] = useState('asc');

  const [drawerLineId, setDrawerLineId] = useState(null);

  const locked = period && period.status !== 'open';

  const loadPeriod = useCallback(async () => {
    try {
      const { data } = await getPayrollPeriod(id);
      setPeriod(data.period);
      setSummary(data.summary);
    } catch {
      setError('Gagal memuat periode');
    }
  }, [id]);

  const loadLines = useCallback(async () => {
    try {
      const { data } = await getPayrollLines(id, {
        q: q || undefined,
        position_id: positionId || undefined,
        branch_id: branchId || undefined,
        sort, order,
      });
      setLines(Array.isArray(data) ? data : []);
    } catch {
      setError('Gagal memuat baris penggajian');
    }
  }, [id, q, positionId, branchId, sort, order]);

  useEffect(() => {
    (async () => {
      setLoading(true);
      await loadPeriod();
      const [pos, br] = await Promise.all([getPositions().catch(() => ({ data: [] })), getBranches().catch(() => ({ data: [] }))]);
      setPositions(Array.isArray(pos.data) ? pos.data : []);
      setBranches(Array.isArray(br.data) ? br.data : []);
      setLoading(false);
    })();
  }, [loadPeriod]);

  useEffect(() => { loadLines(); }, [loadLines]);

  const refreshAll = async () => { await loadPeriod(); await loadLines(); };

  const totalGross = lines.reduce((a, l) => a + numVal(l.gross_pay), 0);
  const totalNet = lines.reduce((a, l) => a + numVal(l.net_pay), 0);
  const totalDeductions = lines.reduce((a, l) => a + numVal(l.component_deduction_total) + numVal(l.kasbon_deduction) + numVal(l.unpaid_leave_deduction), 0);

  const reviewedCount = summary?.reviewed_count ?? lines.filter((l) => l.reviewed).length;
  const lineCount = summary?.line_count ?? lines.length;
  const allReviewed = lineCount > 0 && reviewedCount >= lineCount;

  const doClose = async () => {
    setBusy(true); setError('');
    try { await closePayrollPeriod(id); await refreshAll(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal menutup periode'); }
    finally { setBusy(false); }
  };
  const doPaid = async () => {
    setBusy(true); setError('');
    try { await markPayrollPeriodPaid(id); await refreshAll(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal menandai dibayar'); }
    finally { setBusy(false); }
  };

  const downloadablePayslips = period && (period.status === 'closed' || period.status === 'paid');

  const monthSlug = (d) => d ? new Date(d).toISOString().slice(0, 7) : '';

  const doDownloadOne = async (line, e) => {
    e.stopPropagation();
    setError('');
    try {
      const r = await downloadPayslip(line.id);
      saveBlob(r.data, `slip-gaji-${line.employee_code}-${monthSlug(period.period_month)}.pdf`);
    } catch {
      setError('Gagal mengunduh slip gaji');
    }
  };

  const doDownloadAll = async () => {
    setBusy(true); setError('');
    try {
      const r = await downloadPeriodPayslips(id);
      saveBlob(r.data, `slip-gaji-periode-${monthSlug(period.period_month)}.zip`);
    } catch {
      setError('Gagal mengunduh semua slip gaji');
    } finally {
      setBusy(false);
    }
  };

  if (loading) return <div style={{ padding: 24 }}>Memuat…</div>;
  if (!period) return <div style={{ padding: 24 }}>Periode tidak ditemukan. <Link to="/hr/payroll">Kembali</Link></div>;

  return (
    <div style={{ padding: 24, maxWidth: 1300, margin: '0 auto' }}>
      <div style={{ marginBottom: 8 }}><Link to="/hr/payroll" style={{ color: '#1967d2' }}>← Daftar Periode</Link></div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 12, marginBottom: 16 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24 }}>Penggajian — {fmtMonth(period.period_month)}</h1>
          <div style={{ color: '#667', marginTop: 4 }}>
            Status: <strong>{STATUS_LABELS[period.status] || period.status}</strong> · Direview {reviewedCount}/{lineCount}
          </div>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          {downloadablePayslips && (
            <button onClick={doDownloadAll} disabled={busy}
              style={{ background: '#fff', color: '#1967d2', border: '1px solid #1967d2', borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: 'pointer' }}>
              Unduh Semua Slip
            </button>
          )}
          {period.status === 'open' && (
            <button onClick={doClose} disabled={!allReviewed || busy}
              title={!allReviewed ? 'Semua baris harus direview' : ''}
              style={{ background: allReviewed ? '#1e7e34' : '#cdd', color: '#fff', border: 0, borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: allReviewed ? 'pointer' : 'not-allowed' }}>
              Tutup Periode
            </button>
          )}
          {period.status === 'closed' && (
            <button onClick={doPaid} disabled={busy}
              style={{ background: '#1967d2', color: '#fff', border: 0, borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: 'pointer' }}>
              Tandai Dibayar
            </button>
          )}
        </div>
      </div>

      {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 12, borderRadius: 8, marginBottom: 12 }}>{error}</div>}

      {/* Filters */}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 12 }}>
        <input placeholder="Cari nama / kode…" value={q} onChange={(e) => setQ(e.target.value)}
          style={{ padding: 8, borderRadius: 6, border: '1px solid #ccd', minWidth: 200 }} />
        <select value={positionId} onChange={(e) => setPositionId(e.target.value)} style={{ padding: 8, borderRadius: 6, border: '1px solid #ccd' }}>
          <option value="">Semua Jabatan</option>
          {positions.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
        <select value={branchId} onChange={(e) => setBranchId(e.target.value)} style={{ padding: 8, borderRadius: 6, border: '1px solid #ccd' }}>
          <option value="">Semua Cabang</option>
          {branches.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
        </select>
        <select value={`${sort}:${order}`} onChange={(e) => { const [s, o] = e.target.value.split(':'); setSort(s); setOrder(o); }}
          style={{ padding: 8, borderRadius: 6, border: '1px solid #ccd' }}>
          <option value="name:asc">Nama A–Z</option>
          <option value="name:desc">Nama Z–A</option>
          <option value="net_pay:desc">Gaji Bersih (Tertinggi)</option>
          <option value="net_pay:asc">Gaji Bersih (Terendah)</option>
        </select>
      </div>

      <div style={{ overflowX: 'auto', background: '#fff', borderRadius: 10, boxShadow: '0 1px 3px rgba(0,0,0,.08)' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
          <thead>
            <tr style={{ background: '#f1f3f7', textAlign: 'left' }}>
              <th style={{ padding: 10 }}>Karyawan</th>
              <th style={{ padding: 10 }}>Jabatan</th>
              <th style={{ padding: 10 }}>Cabang</th>
              <th style={{ padding: 10, textAlign: 'right' }}>Gaji Pokok</th>
              <th style={{ padding: 10, textAlign: 'right' }}>Tunjangan</th>
              <th style={{ padding: 10, textAlign: 'right' }}>Bonus</th>
              <th style={{ padding: 10, textAlign: 'right' }}>Lembur</th>
              <th style={{ padding: 10, textAlign: 'right' }}>Kasbon</th>
              <th style={{ padding: 10, textAlign: 'right' }}>Pot. Cuti</th>
              <th style={{ padding: 10, textAlign: 'right' }}>Gaji Bersih</th>
              <th style={{ padding: 10, textAlign: 'center' }}>Skor</th>
              <th style={{ padding: 10, textAlign: 'center' }}>✓</th>
              {downloadablePayslips && <th style={{ padding: 10, textAlign: 'center' }}>Slip</th>}
            </tr>
          </thead>
          <tbody>
            {lines.length === 0 ? (
              <tr><td colSpan={downloadablePayslips ? 13 : 12} style={{ padding: 16, color: '#889' }}>Tidak ada baris.</td></tr>
            ) : lines.map((l) => (
              <tr key={l.id} onClick={() => setDrawerLineId(l.id)} style={{ borderTop: '1px solid #eef0f4', cursor: 'pointer' }}>
                <td style={{ padding: 10 }}>
                  <div style={{ fontWeight: 600 }}>{l.employee_name}</div>
                  <div style={{ fontSize: 12, color: '#889' }}>{l.employee_code}</div>
                </td>
                <td style={{ padding: 10 }}>{l.position_name || '-'}</td>
                <td style={{ padding: 10 }}>{l.branch_name || '-'}</td>
                <td style={{ padding: 10, textAlign: 'right' }}>{fmtIDR(l.base_salary)}</td>
                <td style={{ padding: 10, textAlign: 'right' }}>{fmtIDR(l.allowance_total)}</td>
                <td style={{ padding: 10, textAlign: 'right' }}>{fmtIDR(l.bonus_total)}</td>
                <td style={{ padding: 10, textAlign: 'right' }}>{fmtIDR(numVal(l.overtime_amount) + numVal(l.public_holiday_amount))}</td>
                <td style={{ padding: 10, textAlign: 'right', color: '#c5221f' }}>{fmtIDR(l.kasbon_deduction)}</td>
                <td style={{ padding: 10, textAlign: 'right', color: '#c5221f' }}>{fmtIDR(l.unpaid_leave_deduction)}</td>
                <td style={{ padding: 10, textAlign: 'right', fontWeight: 700 }}>{fmtIDR(l.net_pay)}</td>
                <td style={{ padding: 10, textAlign: 'center' }}><ScoreBadge score={l.performance_score} /></td>
                <td style={{ padding: 10, textAlign: 'center' }}>{l.reviewed ? <span style={{ color: '#1e7e34', fontWeight: 700 }}>✓</span> : ''}</td>
                {downloadablePayslips && (
                  <td style={{ padding: 10, textAlign: 'center' }}>
                    <button onClick={(e) => doDownloadOne(l, e)} title="Unduh Slip Gaji"
                      style={{ background: 'none', border: 0, cursor: 'pointer', fontSize: 16, color: '#1967d2' }}>
                      ⬇
                    </button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
          {lines.length > 0 && (
            <tfoot>
              <tr style={{ background: '#f8f9fb', fontWeight: 700, borderTop: '2px solid #e6e8ee' }}>
                <td style={{ padding: 10 }} colSpan={3}>Total</td>
                <td style={{ padding: 10, textAlign: 'right' }} colSpan={3}>Bruto: {fmtIDR(totalGross)}</td>
                <td style={{ padding: 10, textAlign: 'right' }} colSpan={3}>Potongan: {fmtIDR(totalDeductions)}</td>
                <td style={{ padding: 10, textAlign: 'right' }}>{fmtIDR(totalNet)}</td>
                <td colSpan={downloadablePayslips ? 3 : 2}></td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>

      {drawerLineId && (
        <ReviewDrawer
          lineId={drawerLineId}
          locked={locked}
          onClose={() => setDrawerLineId(null)}
          onSaved={async () => { setDrawerLineId(null); await refreshAll(); }}
        />
      )}
    </div>
  );
}

function ReviewDrawer({ lineId, locked, onClose, onSaved }) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const [overtimeDays, setOvertimeDays] = useState(0);
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

  const editable = !locked;
  const setCompAmount = (cid, amount) => setComponents((cs) => cs.map((c) => c.id === cid ? { ...c, amount } : c));

  const save = async () => {
    setBusy(true); setError('');
    try {
      await reviewPayrollLine(lineId, {
        overtime_days: Number(overtimeDays) || 0,
        public_holiday_days: Number(holidayDays) || 0,
        components: components
          .filter((c) => c.type === 'bonus')
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
  const editableComps = components.filter((c) => c.type === 'bonus');

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,.35)', display: 'flex', justifyContent: 'flex-end', zIndex: 100 }} onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} style={{ width: 460, maxWidth: '100%', background: '#fff', height: '100%', overflowY: 'auto', padding: 20, boxShadow: '-2px 0 12px rgba(0,0,0,.15)' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
          <h2 style={{ margin: 0, fontSize: 18 }}>Review Penggajian</h2>
          <button onClick={onClose} style={{ background: 'none', border: 0, fontSize: 22, cursor: 'pointer', color: '#889' }}>×</button>
        </div>

        {loading ? <p>Memuat…</p> : !line ? <p>Tidak ada data.</p> : (
          <>
            {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 10, borderRadius: 6, marginBottom: 10 }}>{error}</div>}

            {/* Attendance summary */}
            <section style={{ marginBottom: 16 }}>
              <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 6px' }}>Ringkasan Absensi</h3>
              <div style={{ display: 'flex', gap: 8 }}>
                {[['Hadir', att?.hadir], ['Absen', att?.absen], ['Terlambat', att?.terlambat], ['Cuti', att?.cuti]].map(([k, v]) => (
                  <div key={k} style={{ flex: 1, background: '#f6f7fa', borderRadius: 8, padding: 8, textAlign: 'center' }}>
                    <div style={{ fontSize: 18, fontWeight: 700 }}>{v ?? 0}</div>
                    <div style={{ fontSize: 11, color: '#889' }}>{k}</div>
                  </div>
                ))}
              </div>
            </section>

            {/* Performance */}
            <section style={{ marginBottom: 16 }}>
              <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 6px' }}>Kinerja</h3>
              <div style={{ marginBottom: 6 }}>Skor: <ScoreBadge score={line.performance_score} /></div>
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

            {/* Kasbon installments */}
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

            {/* Editable fields */}
            <section style={{ marginBottom: 16 }}>
              <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 6px' }}>Penyesuaian</h3>
              <label style={{ display: 'block', fontSize: 13, marginBottom: 4 }}>Hari Lembur</label>
              <input type="number" step="0.5" min="0" value={overtimeDays} disabled={!editable}
                onChange={(e) => setOvertimeDays(e.target.value)}
                style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', marginBottom: 10 }} />
              <label style={{ display: 'block', fontSize: 13, marginBottom: 4 }}>Hari Libur Nasional</label>
              <input type="number" step="0.5" min="0" value={holidayDays} disabled={!editable}
                onChange={(e) => setHolidayDays(e.target.value)}
                style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', marginBottom: 10 }} />

              {editableComps.length > 0 && (
                <>
                  <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Bonus / Variabel</div>
                  {editableComps.map((c) => (
                    <div key={c.id} style={{ marginBottom: 8 }}>
                      <label style={{ display: 'block', fontSize: 12, color: '#778' }}>{c.name}</label>
                      <input type="number" min="0" value={c.amount} disabled={!editable}
                        onChange={(e) => setCompAmount(c.id, e.target.value)}
                        style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd' }} />
                    </div>
                  ))}
                </>
              )}

              <label style={{ display: 'block', fontSize: 13, marginBottom: 4, marginTop: 4 }}>Catatan</label>
              <textarea value={note} disabled={!editable} onChange={(e) => setNote(e.target.value)} rows={2}
                style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd' }} />
            </section>

            <div style={{ background: '#f6f7fa', borderRadius: 8, padding: 10, marginBottom: 12, fontSize: 14 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Gaji Bruto</span><strong>{fmtIDR(line.gross_pay)}</strong></div>
              <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Gaji Bersih</span><strong>{fmtIDR(line.net_pay)}</strong></div>
            </div>

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
          </>
        )}
      </div>
    </div>
  );
}
