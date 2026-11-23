import { useEffect, useState, useCallback, useMemo } from 'react';
import { useParams, Link } from 'react-router-dom';
import * as XLSX from 'xlsx-js-style';
import { getPayrollPeriod, getPayrollLines, reviewAllPayrollLines, closePayrollPeriod, markPayrollPeriodPaid, getPositions, getBranches, downloadPayslip, downloadPeriodPayslips, getEmployee, retryPayrollPosting } from '../../api';
import { fmtIDR } from '../../format';
import { AccountingStatus, GradeBadge, ScoreBadge, SortableTh } from './payroll/Badges';
import { AttendanceCutModal } from './payroll/AttendanceCutModal';
import { BonusDistributionModal } from './payroll/BonusDistributionModal';
import { ReviewDrawer } from './payroll/ReviewDrawer';
import { SORT_COLUMNS, STATUS_LABELS, classifyBank, fmtMonth, numVal, saveBlob, sortLines } from './payroll/shared';

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
  const [cutLineId, setCutLineId] = useState(null);
  const [showBonusModal, setShowBonusModal] = useState(false);
  // Ledger posting status for this period, written by the background poster.
  // null for periods closed before automatic posting existed (those were posted
  // by hand through the old dialog).
  const [posting, setPosting] = useState(null);

  const locked = period && period.status !== 'open';

  const loadPeriod = useCallback(async () => {
    try {
      const { data } = await getPayrollPeriod(id);
      setPeriod(data.period);
      setSummary(data.summary);
      setPosting(data.posting || null);
    } catch {
      setError('Gagal memuat periode');
    }
  }, [id]);

  // Search / position / branch stay server-side; ordering is applied on the client
  // (see sortedLines) so clicking a header re-sorts instantly without a refetch.
  const loadLines = useCallback(async () => {
    try {
      const { data } = await getPayrollLines(id, {
        q: q || undefined,
        position_id: positionId || undefined,
        branch_id: branchId || undefined,
      });
      setLines(Array.isArray(data) ? data : []);
    } catch {
      setError('Gagal memuat baris penggajian');
    }
  }, [id, q, positionId, branchId]);

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

  // The ledger posting runs in the background, so the page follows it instead of
  // making the user reload. Polling stops as soon as it settles (posted/failed).
  useEffect(() => {
    if (posting?.status !== 'pending') return undefined;
    const t = setInterval(() => { loadPeriod(); }, 3000);
    return () => clearInterval(t);
  }, [posting?.status, loadPeriod]);

  const doRetryPosting = async () => {
    setBusy(true); setError('');
    try {
      const { data } = await retryPayrollPosting(id);
      setPosting(data);
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal mencatat ke akuntansi');
      await loadPeriod();
    } finally {
      setBusy(false);
    }
  };

  const sortedLines = useMemo(() => sortLines(lines, sort, order), [lines, sort, order]);

  // Clicking the active column flips direction; a new column starts in its natural
  // direction — highest-first for money/score, A–Z for text.
  const toggleSort = (key) => {
    if (sort === key) {
      setOrder((o) => (o === 'asc' ? 'desc' : 'asc'));
      return;
    }
    setSort(key);
    setOrder(SORT_COLUMNS[key]?.numeric ? 'desc' : 'asc');
  };

  const totalGross = lines.reduce((a, l) => a + numVal(l.gross_pay), 0);
  const totalNet = lines.reduce((a, l) => a + numVal(l.net_pay), 0);
  const totalDeductions = lines.reduce((a, l) => a + numVal(l.component_deduction_total) + numVal(l.kasbon_deduction) + numVal(l.unpaid_leave_deduction) + numVal(l.half_day_deduction) + numVal(l.attendance_cut_deduction), 0);

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
      saveBlob(r.data, `slip-gaji-periode-${monthSlug(period.period_month)}.pdf`);
    } catch {
      setError('Gagal mengunduh semua slip gaji');
    } finally {
      setBusy(false);
    }
  };

  // Export a bank-upload Excel: net pay per employee, one sheet per bank format
  // (BCA / Mandiri / Bank Lain). Pulls the full unfiltered line set so the file
  // is complete regardless of on-screen filters.
  const doExportBankFiles = async () => {
    setBusy(true); setError('');
    try {
      const { data: allLines } = await getPayrollLines(id, { sort: 'name', order: 'asc' });
      const rows = Array.isArray(allLines) ? allLines : [];
      if (rows.length === 0) { setError('Tidak ada baris untuk diekspor.'); return; }

      // Bank details live on the employee record, not the payroll line — fetch them
      // (limited concurrency) and index by employee id.
      const ids = [...new Set(rows.map((l) => l.employee_id))];
      const bankMap = {};
      const CHUNK = 8;
      for (let i = 0; i < ids.length; i += CHUNK) {
        const slice = ids.slice(i, i + CHUNK);
        const res = await Promise.all(slice.map((eid) => getEmployee(eid).then((r) => r.data).catch(() => null)));
        slice.forEach((eid, idx) => { bankMap[eid] = res[idx] || {}; });
      }

      const groups = { BCA: [], Mandiri: [], Lainnya: [] };
      for (const l of rows) {
        const emp = bankMap[l.employee_id] || {};
        const bankName = emp.bank_name || '';
        const rec = {
          name: l.employee_name,
          bankName,
          account: emp.bank_account_number || '',
          holder: emp.bank_account_holder || l.employee_name,
          amount: Math.round(numVal(l.net_pay)),
          flag: emp.bank_account_number ? '' : 'PERIKSA: rekening kosong',
        };
        groups[classifyBank(bankName)].push(rec);
      }

      const d = new Date(period.period_month);
      const monthLabel = fmtMonth(period.period_month);
      const remark = `GAJI ${String(d.getMonth() + 1).padStart(2, '0')}-${d.getFullYear()}`;
      const headStyle = { font: { bold: true }, fill: { fgColor: { rgb: 'F1F3F7' } } };

      const applyNum = (ws, cols) => {
        const r = XLSX.utils.decode_range(ws['!ref']);
        for (let row = 0; row <= r.e.r; row++) for (const c of cols) {
          const cell = ws[XLSX.utils.encode_cell({ r: row, c })];
          if (cell && typeof cell.v === 'number') cell.z = '#,##0';
        }
      };
      const boldRow = (ws, rowIdx) => {
        const r = XLSX.utils.decode_range(ws['!ref']);
        for (let c = 0; c <= r.e.c; c++) {
          const cell = ws[XLSX.utils.encode_cell({ r: rowIdx, c })];
          if (cell) cell.s = headStyle;
        }
      };

      const wb = XLSX.utils.book_new();

      if (groups.BCA.length) {
        const aoa = [
          [`Ekspor Payroll BCA — ${monthLabel}`],
          ['Format KlikBCA Bisnis (Multi Payroll). Proses lewat aplikasi checksum BCA sebelum diunggah.'],
          [],
          ['No.', 'Nama Penerima', 'No. Rekening', 'Nama Pemilik Rekening', 'Kode Bank (BI)', 'Jumlah (Rp)', 'Berita'],
        ];
        groups.BCA.forEach((r, i) => aoa.push([i + 1, r.name, r.account, r.holder, '', r.amount, r.flag || remark]));
        aoa.push(['', '', '', '', 'TOTAL', groups.BCA.reduce((a, r) => a + r.amount, 0), '']);
        const ws = XLSX.utils.aoa_to_sheet(aoa);
        ws['!cols'] = [{ wch: 5 }, { wch: 26 }, { wch: 18 }, { wch: 26 }, { wch: 14 }, { wch: 16 }, { wch: 22 }];
        applyNum(ws, [5]); boldRow(ws, 3);
        XLSX.utils.book_append_sheet(wb, ws, 'BCA');
      }

      if (groups.Mandiri.length) {
        const aoa = [
          [`Ekspor Payroll Mandiri — ${monthLabel}`],
          ['Format Kopra by Mandiri / MCM Payroll by File Upload. Validasi dengan "Download Template" sebelum diunggah.'],
          [],
          ['No.', 'Nama Penerima', 'No. Rekening', 'Nama Pemilik', 'Kode Bank', 'Jumlah (Rp)', 'Tipe Transfer', 'Keterangan'],
        ];
        groups.Mandiri.forEach((r, i) => aoa.push([i + 1, r.name, r.account, r.holder, '', r.amount, 'IN-HOUSE', r.flag || remark]));
        aoa.push(['', '', '', '', 'TOTAL', groups.Mandiri.reduce((a, r) => a + r.amount, 0), '', '']);
        const ws = XLSX.utils.aoa_to_sheet(aoa);
        ws['!cols'] = [{ wch: 5 }, { wch: 26 }, { wch: 18 }, { wch: 26 }, { wch: 12 }, { wch: 16 }, { wch: 14 }, { wch: 22 }];
        applyNum(ws, [5]); boldRow(ws, 3);
        XLSX.utils.book_append_sheet(wb, ws, 'Mandiri');
      }

      if (groups.Lainnya.length) {
        const aoa = [
          [`Ekspor Payroll — Bank Lain — ${monthLabel}`],
          ['Karyawan dengan bank selain BCA/Mandiri. Lengkapi Kode Bank (sandi BI) untuk transfer antar bank.'],
          [],
          ['No.', 'Nama Penerima', 'Bank', 'No. Rekening', 'Nama Pemilik', 'Kode Bank', 'Jumlah (Rp)', 'Keterangan'],
        ];
        groups.Lainnya.forEach((r, i) => aoa.push([i + 1, r.name, r.bankName, r.account, r.holder, '', r.amount, r.flag || remark]));
        aoa.push(['', '', '', '', '', 'TOTAL', groups.Lainnya.reduce((a, r) => a + r.amount, 0), '']);
        const ws = XLSX.utils.aoa_to_sheet(aoa);
        ws['!cols'] = [{ wch: 5 }, { wch: 26 }, { wch: 20 }, { wch: 18 }, { wch: 26 }, { wch: 12 }, { wch: 16 }, { wch: 22 }];
        applyNum(ws, [6]); boldRow(ws, 3);
        XLSX.utils.book_append_sheet(wb, ws, 'Bank Lain');
      }

      if (wb.SheetNames.length === 0) { setError('Tidak ada data untuk diekspor.'); return; }
      XLSX.writeFile(wb, `payroll-bank-${monthSlug(period.period_month)}.xlsx`);
    } catch {
      setError('Gagal mengekspor file payroll');
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
          {(allReviewed || locked) && (
            <button onClick={doExportBankFiles} disabled={busy}
              title="Ekspor data gaji ke Excel untuk unggah payroll bank (BCA / Mandiri)"
              style={{ background: '#fff', color: '#0b8043', border: '1px solid #0b8043', borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: 'pointer' }}>
              {busy ? 'Memproses…' : 'Ekspor Bank (Excel)'}
            </button>
          )}
          {locked && posting && (
            <AccountingStatus posting={posting} busy={busy} onRetry={doRetryPosting} />
          )}
          {downloadablePayslips && (
            <button onClick={doDownloadAll} disabled={busy}
              title="PDF A4 landscape, 2 karyawan per halaman. Potong di garis putus-putus untuk mendapat 2 slip A5 potrait."
              style={{ background: '#fff', color: '#1967d2', border: '1px solid #1967d2', borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: 'pointer' }}>
              Unduh Semua Slip (A4, 2 per halaman)
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
      </div>

      <div style={{ overflowX: 'auto', background: '#fff', borderRadius: 10, boxShadow: '0 1px 3px rgba(0,0,0,.08)' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
          <thead>
            <tr style={{ background: '#f1f3f7', textAlign: 'left' }}>
              {[
                { key: 'name', label: 'Karyawan' },
                { key: 'position', label: 'Jabatan' },
                { key: 'branch', label: 'Cabang' },
                { key: 'base_salary', label: 'Gaji Pokok', align: 'right' },
                { key: 'allowance_total', label: 'Tunjangan', align: 'right' },
                { key: 'bonus_total', label: 'Bonus', align: 'right' },
                { key: 'overtime', label: 'Lembur', align: 'right' },
                { key: 'kasbon_deduction', label: 'Kasbon', align: 'right' },
                { key: 'unpaid_leave_deduction', label: 'Pot. Libur/Alpha', align: 'right' },
                { key: 'attendance_cut_deduction', label: 'Telat/Pulang Awal', align: 'right' },
                { key: 'net_pay', label: 'Gaji Bersih', align: 'right' },
                { key: 'performance_score', label: 'Skor', align: 'center' },
                { key: 'reviewed', label: '✓', align: 'center' },
              ].map((c) => (
                <SortableTh key={c.key} label={c.label} sortKey={c.key} align={c.align}
                  sort={sort} order={order} onSort={toggleSort} />
              ))}
              {downloadablePayslips && <th style={{ padding: 10, textAlign: 'center' }}>Slip</th>}
            </tr>
          </thead>
          <tbody>
            {lines.length === 0 ? (
              <tr><td colSpan={downloadablePayslips ? 14 : 13} style={{ padding: 16, color: '#889' }}>Tidak ada baris.</td></tr>
            ) : sortedLines.map((l) => (
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
                <td style={{ padding: 10, textAlign: 'right', color: '#c5221f' }}>
                  {fmtIDR(l.unpaid_leave_deduction)}
                  {numVal(l.alpha_deduction) > 0 && (
                    <div style={{ fontSize: '0.74rem' }} title="Alpha yang tidak tertutup jatah libur">
                      alpha {l.alpha_unpaid_days} hari: {fmtIDR(l.alpha_deduction)}
                    </div>
                  )}
                </td>
                <td style={{ padding: 10, textAlign: 'right' }}>
                  {numVal(l.attendance_cut_deduction) > 0 ? (
                    <button onClick={(e) => { e.stopPropagation(); setCutLineId(l.id); }} title="Lihat rincian per hari"
                      style={{ background: 'none', border: 0, padding: 0, cursor: 'pointer', color: '#c5221f', textDecoration: 'underline dotted', font: 'inherit' }}>
                      {fmtIDR(l.attendance_cut_deduction)}
                    </button>
                  ) : fmtIDR(0)}
                </td>
                <td style={{ padding: 10, textAlign: 'right', fontWeight: 700 }}>{fmtIDR(l.net_pay)}</td>
                <td style={{ padding: 10, textAlign: 'center', whiteSpace: 'nowrap' }}><ScoreBadge score={l.performance_score} /><GradeBadge grade={l.performance_grade} percent={l.grade_pay_percent} /></td>
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
                <td style={{ padding: 10, textAlign: 'right' }} colSpan={4}>Potongan: {fmtIDR(totalDeductions)}</td>
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

      {cutLineId && <AttendanceCutModal lineId={cutLineId} onClose={() => setCutLineId(null)} />}

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
