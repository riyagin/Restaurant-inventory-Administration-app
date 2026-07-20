import { useEffect, useState, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import {
  getPayrollPeriod, getPayrollLines, getPayrollLineReview,
  reviewPayrollLine, unreviewPayrollLine, reviewAllPayrollLines,
  closePayrollPeriod, markPayrollPeriodPaid, getPositions, getBranches,
  downloadPayslip, downloadPeriodPayslips,
  getWageComponents, getPayrollBonusEligible, applyPayrollBonus,
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
  const [showBonusModal, setShowBonusModal] = useState(false);

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
  const totalDeductions = lines.reduce((a, l) => a + numVal(l.component_deduction_total) + numVal(l.kasbon_deduction) + numVal(l.unpaid_leave_deduction) + numVal(l.half_day_deduction), 0);

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
  const doReviewAll = async () => {
    const remaining = lineCount - reviewedCount;
    if (!window.confirm(`Tandai ${remaining} baris yang belum direview sebagai sudah direview dengan nilai yang dihasilkan? Anda tetap dapat membuka kembali tiap baris selama periode belum ditutup.`)) return;
    setBusy(true); setError('');
    try { await reviewAllPayrollLines(id); await refreshAll(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal mereview semua baris'); }
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
            <button onClick={() => setShowBonusModal(true)} disabled={busy}
              style={{ background: '#fff', color: '#e37400', border: '1px solid #e37400', borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: 'pointer' }}>
              Distribusi Bonus
            </button>
          )}
          {period.status === 'open' && !allReviewed && (
            <button onClick={doReviewAll} disabled={busy}
              title="Tandai semua baris direview tanpa membuka rincian"
              style={{ background: '#fff', color: '#1e7e34', border: '1px solid #1e7e34', borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: 'pointer' }}>
              Review Semua ({lineCount - reviewedCount})
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
                <td style={{ padding: 10, textAlign: 'right' }}>{fmtIDR(numVal(l.overtime_amount) + numVal(l.overtime_hourly_amount) + numVal(l.public_holiday_amount))}</td>
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

      {showBonusModal && (
        <BonusDistributionModal
          periodId={id}
          onClose={() => setShowBonusModal(false)}
          onApplied={async () => { setShowBonusModal(false); await refreshAll(); }}
        />
      )}
    </div>
  );
}

function BonusDistributionModal({ periodId, onClose, onApplied }) {
  const [bonusComponents, setBonusComponents] = useState([]);
  const [selectedWC, setSelectedWC] = useState('');
  const [pot, setPot] = useState('');
  const [eligible, setEligible] = useState([]);
  const [checked, setChecked] = useState({});
  const [loadingEligible, setLoadingEligible] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getWageComponents().then(({ data }) => {
      const bonus = (Array.isArray(data) ? data : []).filter((c) => c.type === 'bonus');
      setBonusComponents(bonus);
      if (bonus.length === 1) setSelectedWC(bonus[0].id);
    }).catch(() => {});
  }, []);

  useEffect(() => {
    if (!selectedWC) { setEligible([]); setChecked({}); return; }
    setLoadingEligible(true); setError('');
    getPayrollBonusEligible(periodId, selectedWC)
      .then(({ data }) => {
        const rows = Array.isArray(data) ? data : [];
        setEligible(rows);
        const init = {};
        rows.forEach((r) => { init[r.line_component_id] = true; });
        setChecked(init);
      })
      .catch(() => setError('Gagal memuat daftar karyawan'))
      .finally(() => setLoadingEligible(false));
  }, [selectedWC, periodId]);

  const checkedIds = Object.keys(checked).filter((k) => checked[k]);
  const potNum = Number(pot) || 0;
  const perEmployee = checkedIds.length > 0 ? Math.floor(potNum / checkedIds.length) : 0;

  const toggle = (id) => setChecked((c) => ({ ...c, [id]: !c[id] }));
  const toggleAll = () => {
    const allOn = eligible.every((r) => checked[r.line_component_id]);
    const next = {};
    eligible.forEach((r) => { next[r.line_component_id] = !allOn; });
    setChecked(next);
  };

  const apply = async () => {
    if (!selectedWC || checkedIds.length === 0 || perEmployee <= 0) return;
    setBusy(true); setError('');
    try {
      await applyPayrollBonus(periodId, {
        wage_component_id: selectedWC,
        amount_per_employee: perEmployee,
        line_component_ids: checkedIds,
      });
      onApplied();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal mendistribusikan bonus');
      setBusy(false);
    }
  };

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 200 }} onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} style={{ background: '#fff', borderRadius: 12, width: 560, maxWidth: '95vw', maxHeight: '90vh', display: 'flex', flexDirection: 'column', boxShadow: '0 8px 32px rgba(0,0,0,.18)' }}>
        <div style={{ padding: '16px 20px', borderBottom: '1px solid #e6e8ee', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <h2 style={{ margin: 0, fontSize: 18 }}>Distribusi Bonus</h2>
          <button onClick={onClose} style={{ background: 'none', border: 0, fontSize: 22, cursor: 'pointer', color: '#889' }}>×</button>
        </div>

        <div style={{ flex: 1, overflowY: 'auto', padding: 20 }}>
          {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 10, borderRadius: 6, marginBottom: 12 }}>{error}</div>}

          <div style={{ marginBottom: 14 }}>
            <label style={{ display: 'block', fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Komponen Bonus</label>
            <select value={selectedWC} onChange={(e) => setSelectedWC(e.target.value)}
              style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', fontSize: 14 }}>
              <option value="">— Pilih komponen bonus —</option>
              {bonusComponents.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
          </div>

          <div style={{ marginBottom: 14 }}>
            <label style={{ display: 'block', fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Total Pot (Rp)</label>
            <input type="number" min="0" step="1000" value={pot} onChange={(e) => setPot(e.target.value)}
              placeholder="Masukkan total dana bonus"
              style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', fontSize: 14 }} />
          </div>

          {selectedWC && potNum > 0 && checkedIds.length > 0 && (
            <div style={{ background: '#f0f7ff', border: '1px solid #bdd7f9', borderRadius: 8, padding: '10px 14px', marginBottom: 14, fontSize: 14 }}>
              <span style={{ color: '#667' }}>{fmtIDR(potNum)} ÷ {checkedIds.length} karyawan = </span>
              <strong style={{ color: '#1967d2', fontSize: 16 }}>{fmtIDR(perEmployee)}</strong>
              <span style={{ color: '#667' }}> / karyawan</span>
            </div>
          )}

          {selectedWC && (
            <div>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
                <div style={{ fontSize: 13, fontWeight: 600, color: '#334' }}>
                  {loadingEligible ? 'Memuat…' : `${eligible.length} karyawan eligible`}
                </div>
                {eligible.length > 0 && (
                  <button onClick={toggleAll} style={{ background: 'none', border: '1px solid #ccd', borderRadius: 6, padding: '4px 10px', fontSize: 12, cursor: 'pointer' }}>
                    {eligible.every((r) => checked[r.line_component_id]) ? 'Batal Pilih Semua' : 'Pilih Semua'}
                  </button>
                )}
              </div>

              {!loadingEligible && eligible.length === 0 && (
                <div style={{ color: '#889', fontSize: 13, padding: '8px 0' }}>Tidak ada karyawan dengan komponen bonus ini pada periode ini.</div>
              )}

              {eligible.map((row) => (
                <label key={row.line_component_id} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 10px', borderRadius: 6, cursor: 'pointer', background: checked[row.line_component_id] ? '#f5f9ff' : '#fff', border: '1px solid', borderColor: checked[row.line_component_id] ? '#bdd7f9' : '#e6e8ee', marginBottom: 6 }}>
                  <input type="checkbox" checked={!!checked[row.line_component_id]} onChange={() => toggle(row.line_component_id)}
                    style={{ width: 16, height: 16, flexShrink: 0 }} />
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontWeight: 600, fontSize: 14 }}>{row.employee_name}</div>
                    <div style={{ fontSize: 12, color: '#889' }}>{row.employee_code}</div>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
                    <ScoreBadge score={row.performance_score} />
                    {row.reviewed && <span style={{ fontSize: 11, color: '#1e7e34', background: '#e6f4ea', padding: '2px 6px', borderRadius: 4 }}>✓ Reviewed</span>}
                  </div>
                </label>
              ))}
            </div>
          )}
        </div>

        <div style={{ padding: '12px 20px', borderTop: '1px solid #e6e8ee', display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <button onClick={onClose} disabled={busy}
            style={{ background: '#fff', border: '1px solid #ccd', borderRadius: 8, padding: '10px 16px', cursor: 'pointer' }}>
            Batal
          </button>
          <button onClick={apply} disabled={busy || !selectedWC || checkedIds.length === 0 || perEmployee <= 0}
            style={{ background: (selectedWC && checkedIds.length > 0 && perEmployee > 0) ? '#e37400' : '#ccc', color: '#fff', border: 0, borderRadius: 8, padding: '10px 20px', fontWeight: 600, cursor: (selectedWC && checkedIds.length > 0 && perEmployee > 0) ? 'pointer' : 'not-allowed' }}>
            {busy ? 'Menerapkan…' : `Terapkan ke ${checkedIds.length} Karyawan`}
          </button>
        </div>
      </div>
    </div>
  );
}

function calcAmounts(line, mult, overtimeDays, overtimeHours, holidayDays, components) {
  if (!line || !mult) return null;
  const overtimeAmt = Math.round(Number(overtimeDays) * Number(line.daily_rate) * mult.overtime);
  const overtimeHourlyAmt = Math.round(Number(overtimeHours) * Number(line.overtime_hourly_rate) * mult.overtime);
  const holidayAmt = Math.round(Number(holidayDays) * Number(line.daily_rate) * mult.holiday);
  const allowTotal = components.filter(c => c.type === 'allowance').reduce((s, c) => s + (Number(c.amount) || 0), 0);
  const bonusTotal = components.filter(c => c.type === 'bonus').reduce((s, c) => s + (Number(c.amount) || 0), 0);
  const gross = Number(line.base_salary) + allowTotal + bonusTotal + overtimeAmt + overtimeHourlyAmt + holidayAmt;
  const net = gross - Number(line.component_deduction_total) - Number(line.kasbon_deduction) - Number(line.unpaid_leave_deduction) - Number(line.half_day_deduction || 0);
  return { overtimeAmt, overtimeHourlyAmt, holidayAmt, gross, net };
}

function ReviewDrawer({ lineId, locked, onClose, onSaved }) {
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
          .filter((c) => c.type === 'bonus' || c.type === 'allowance')
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

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,.35)', display: 'flex', justifyContent: 'flex-end', zIndex: 100 }} onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} style={{ width: '50vw', minWidth: 360, maxWidth: '100%', background: '#fff', height: '100%', display: 'flex', flexDirection: 'column', boxShadow: '-2px 0 12px rgba(0,0,0,.15)' }}>
        {/* scrollable body */}
        <div style={{ flex: 1, overflowY: 'auto', padding: 20 }}>
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
                <div style={{ display: 'flex', gap: 8, marginBottom: holidaysWorked.length > 0 ? 8 : 0 }}>
                  {[['Hadir', att?.hadir], ['Absen', att?.absen], ['Terlambat', att?.terlambat], ['Cuti', att?.cuti], ['Libur Nasional', holidaysWorked.length]].map(([k, v]) => (
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

              {/* Performance */}
              <section style={{ marginBottom: 16 }}>
                <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 6px' }}>Evaluasi</h3>
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

              {/* Overtime requests */}
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

              {/* Editable fields */}
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

                <label style={{ display: 'block', fontSize: 13, marginBottom: 4, marginTop: 4 }}>Catatan</label>
                <textarea value={note} disabled={!editable} onChange={(e) => setNote(e.target.value)} rows={2}
                  style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd' }} />
              </section>
            </>
          )}
        </div>

        {/* sticky footer — always visible */}
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
              <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 2 }}>
                <span>Gaji Bersih</span>
                <strong style={{ color: '#1e7e34', fontSize: 15 }}>{fmtIDR(live ? live.net : line.net_pay)}</strong>
              </div>
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
