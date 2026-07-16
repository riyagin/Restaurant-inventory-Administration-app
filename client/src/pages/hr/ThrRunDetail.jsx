import { useEffect, useState, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import {
  getThrRun, getThrLines, reviewThrLine, unreviewThrLine, reviewAllThrLines,
  closeThrRun, markThrRunPaid, getPositions, getBranches,
  downloadThrPayslip, downloadThrRunPayslips,
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
const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';
const numVal = (n) => Number(n ?? 0);
const ratioLabel = (months) => Number(months) >= 12 ? '1 bulan penuh' : `${Number(months)}/12`;

export default function ThrRunDetail() {
  const { id } = useParams();
  const [run, setRun] = useState(null);
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

  const [drawerLine, setDrawerLine] = useState(null);

  const locked = run && run.status !== 'open';

  const loadRun = useCallback(async () => {
    try {
      const { data } = await getThrRun(id);
      setRun(data.run);
      setSummary(data.summary);
    } catch {
      setError('Gagal memuat run THR');
    }
  }, [id]);

  const loadLines = useCallback(async () => {
    try {
      const { data } = await getThrLines(id, {
        q: q || undefined,
        position_id: positionId || undefined,
        branch_id: branchId || undefined,
        sort, order,
      });
      setLines(Array.isArray(data) ? data : []);
    } catch {
      setError('Gagal memuat baris THR');
    }
  }, [id, q, positionId, branchId, sort, order]);

  useEffect(() => {
    (async () => {
      setLoading(true);
      await loadRun();
      const [pos, br] = await Promise.all([getPositions().catch(() => ({ data: [] })), getBranches().catch(() => ({ data: [] }))]);
      setPositions(Array.isArray(pos.data) ? pos.data : []);
      setBranches(Array.isArray(br.data) ? br.data : []);
      setLoading(false);
    })();
  }, [loadRun]);

  useEffect(() => { loadLines(); }, [loadLines]);

  const refreshAll = async () => { await loadRun(); await loadLines(); };

  const totalThr = lines.reduce((a, l) => a + numVal(l.thr_amount), 0);

  const reviewedCount = summary?.reviewed_count ?? lines.filter((l) => l.reviewed).length;
  const lineCount = summary?.line_count ?? lines.length;
  const allReviewed = lineCount > 0 && reviewedCount >= lineCount;

  const doClose = async () => {
    setBusy(true); setError('');
    try { await closeThrRun(id); await refreshAll(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal menutup run'); }
    finally { setBusy(false); }
  };
  const doPaid = async () => {
    setBusy(true); setError('');
    try { await markThrRunPaid(id); await refreshAll(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal menandai dibayar'); }
    finally { setBusy(false); }
  };
  const doReviewAll = async () => {
    const remaining = lineCount - reviewedCount;
    if (!window.confirm(`Tandai ${remaining} baris yang belum direview sebagai sudah direview dengan nilai yang dihitung? Anda tetap dapat membuka kembali tiap baris selama run belum ditutup.`)) return;
    setBusy(true); setError('');
    try { await reviewAllThrLines(id); await refreshAll(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal mereview semua baris'); }
    finally { setBusy(false); }
  };

  const downloadable = run && (run.status === 'closed' || run.status === 'paid');
  const dateSlug = (d) => d ? new Date(d).toISOString().slice(0, 10) : '';

  const doDownloadOne = async (line, e) => {
    e.stopPropagation();
    setError('');
    try {
      const r = await downloadThrPayslip(line.id);
      saveBlob(r.data, `slip-thr-${line.employee_code}-${dateSlug(run.payment_date)}.pdf`);
    } catch {
      setError('Gagal mengunduh slip THR');
    }
  };

  const doDownloadAll = async () => {
    setBusy(true); setError('');
    try {
      const r = await downloadThrRunPayslips(id);
      saveBlob(r.data, `slip-thr-${dateSlug(run.payment_date)}.zip`);
    } catch {
      setError('Gagal mengunduh semua slip THR');
    } finally {
      setBusy(false);
    }
  };

  if (loading) return <div style={{ padding: 24 }}>Memuat…</div>;
  if (!run) return <div style={{ padding: 24 }}>Run tidak ditemukan. <Link to="/hr/thr">Kembali</Link></div>;

  return (
    <div style={{ padding: 24, maxWidth: 1300, margin: '0 auto' }}>
      <div style={{ marginBottom: 8 }}><Link to="/hr/thr" style={{ color: '#1967d2' }}>← Daftar Run THR</Link></div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 12, marginBottom: 16 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24 }}>{run.name}</h1>
          <div style={{ color: '#667', marginTop: 4 }}>
            Bayar: <strong>{fmtDate(run.payment_date)}</strong> · Status: <strong>{STATUS_LABELS[run.status] || run.status}</strong> · Direview {reviewedCount}/{lineCount}
          </div>
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {downloadable && (
            <button onClick={doDownloadAll} disabled={busy}
              style={{ background: '#fff', color: '#1967d2', border: '1px solid #1967d2', borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: 'pointer' }}>
              Unduh Semua Slip
            </button>
          )}
          {run.status === 'open' && !allReviewed && (
            <button onClick={doReviewAll} disabled={busy}
              title="Tandai semua baris direview tanpa membuka rincian"
              style={{ background: '#fff', color: '#1e7e34', border: '1px solid #1e7e34', borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: 'pointer' }}>
              Review Semua ({lineCount - reviewedCount})
            </button>
          )}
          {run.status === 'open' && (
            <button onClick={doClose} disabled={!allReviewed || busy}
              title={!allReviewed ? 'Semua baris harus direview' : ''}
              style={{ background: allReviewed ? '#1e7e34' : '#cdd', color: '#fff', border: 0, borderRadius: 8, padding: '10px 16px', fontWeight: 600, cursor: allReviewed ? 'pointer' : 'not-allowed' }}>
              Tutup Run
            </button>
          )}
          {run.status === 'closed' && (
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
          <option value="thr_amount:desc">THR (Tertinggi)</option>
          <option value="thr_amount:asc">THR (Terendah)</option>
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
              <th style={{ padding: 10, textAlign: 'center' }}>Masa Kerja</th>
              <th style={{ padding: 10, textAlign: 'center' }}>Proporsi</th>
              <th style={{ padding: 10, textAlign: 'right' }}>THR Dihitung</th>
              <th style={{ padding: 10, textAlign: 'right' }}>THR Final</th>
              <th style={{ padding: 10, textAlign: 'center' }}>✓</th>
              {downloadable && <th style={{ padding: 10, textAlign: 'center' }}>Slip</th>}
            </tr>
          </thead>
          <tbody>
            {lines.length === 0 ? (
              <tr><td colSpan={downloadable ? 10 : 9} style={{ padding: 16, color: '#889' }}>Tidak ada baris.</td></tr>
            ) : lines.map((l) => {
              const adjusted = numVal(l.thr_amount) !== numVal(l.computed_amount);
              return (
                <tr key={l.id} onClick={() => setDrawerLine(l)} style={{ borderTop: '1px solid #eef0f4', cursor: 'pointer' }}>
                  <td style={{ padding: 10 }}>
                    <div style={{ fontWeight: 600 }}>{l.employee_name}</div>
                    <div style={{ fontSize: 12, color: '#889' }}>{l.employee_code}</div>
                  </td>
                  <td style={{ padding: 10 }}>{l.position_name || '-'}</td>
                  <td style={{ padding: 10 }}>{l.branch_name || '-'}</td>
                  <td style={{ padding: 10, textAlign: 'right' }}>{fmtIDR(l.base_salary)}</td>
                  <td style={{ padding: 10, textAlign: 'center' }}>{l.months_worked} bln</td>
                  <td style={{ padding: 10, textAlign: 'center' }}>{ratioLabel(l.months_worked)}</td>
                  <td style={{ padding: 10, textAlign: 'right', color: '#889' }}>{fmtIDR(l.computed_amount)}</td>
                  <td style={{ padding: 10, textAlign: 'right', fontWeight: 700 }}>
                    {fmtIDR(l.thr_amount)}
                    {adjusted && <span title="Disesuaikan dari nilai hitung" style={{ marginLeft: 4, color: '#e37400' }}>*</span>}
                  </td>
                  <td style={{ padding: 10, textAlign: 'center' }}>{l.reviewed ? <span style={{ color: '#1e7e34', fontWeight: 700 }}>✓</span> : ''}</td>
                  {downloadable && (
                    <td style={{ padding: 10, textAlign: 'center' }}>
                      <button onClick={(e) => doDownloadOne(l, e)} title="Unduh Slip THR"
                        style={{ background: 'none', border: 0, cursor: 'pointer', fontSize: 16, color: '#1967d2' }}>
                        ⬇
                      </button>
                    </td>
                  )}
                </tr>
              );
            })}
          </tbody>
          {lines.length > 0 && (
            <tfoot>
              <tr style={{ background: '#f8f9fb', fontWeight: 700, borderTop: '2px solid #e6e8ee' }}>
                <td style={{ padding: 10 }} colSpan={7}>Total THR</td>
                <td style={{ padding: 10, textAlign: 'right' }}>{fmtIDR(totalThr)}</td>
                <td colSpan={downloadable ? 2 : 1}></td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>

      {drawerLine && (
        <ReviewDrawer
          line={drawerLine}
          locked={locked}
          onClose={() => setDrawerLine(null)}
          onSaved={async () => { setDrawerLine(null); await refreshAll(); }}
        />
      )}
    </div>
  );
}

function ReviewDrawer({ line, locked, onClose, onSaved }) {
  const [amount, setAmount] = useState(String(numVal(line.thr_amount)));
  const [note, setNote] = useState(line.review_note || '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const editable = !locked;

  const save = async () => {
    if (amount === '' || Number(amount) < 0) { setError('Nominal THR tidak boleh kosong/negatif'); return; }
    setBusy(true); setError('');
    try {
      await reviewThrLine(line.id, { thr_amount: Math.round(Number(amount)), review_note: note });
      await onSaved();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal menyimpan review');
      setBusy(false);
    }
  };

  const reopen = async () => {
    setBusy(true); setError('');
    try { await unreviewThrLine(line.id); await onSaved(); }
    catch (err) { setError(err?.response?.data?.error || 'Gagal membuka kembali'); setBusy(false); }
  };

  const resetToComputed = () => setAmount(String(numVal(line.computed_amount)));

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,.35)', display: 'flex', justifyContent: 'flex-end', zIndex: 100 }} onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} style={{ width: '46vw', minWidth: 340, maxWidth: '100%', background: '#fff', height: '100%', display: 'flex', flexDirection: 'column', boxShadow: '-2px 0 12px rgba(0,0,0,.15)' }}>
        <div style={{ flex: 1, overflowY: 'auto', padding: 20 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
            <h2 style={{ margin: 0, fontSize: 18 }}>Review THR</h2>
            <button onClick={onClose} style={{ background: 'none', border: 0, fontSize: 22, cursor: 'pointer', color: '#889' }}>×</button>
          </div>

          <div style={{ marginBottom: 16 }}>
            <div style={{ fontWeight: 600, fontSize: 16 }}>{line.employee_name}</div>
            <div style={{ fontSize: 13, color: '#889' }}>{line.employee_code} · {line.position_name || '-'} · {line.branch_name || '-'}</div>
          </div>

          {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 10, borderRadius: 6, marginBottom: 10 }}>{error}</div>}

          {/* Calculation breakdown */}
          <section style={{ marginBottom: 16, background: '#f6f7fa', borderRadius: 8, padding: 12 }}>
            <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 8px' }}>Perhitungan</h3>
            <Row k="Gaji Pokok" v={fmtIDR(line.base_salary)} />
            <Row k="Tanggal Bergabung" v={fmtDate(line.join_date)} />
            <Row k="Masa Kerja" v={`${line.months_worked} bulan`} />
            <Row k="Proporsi" v={ratioLabel(line.months_worked)} />
            <div style={{ borderTop: '1px solid #e0e3ea', marginTop: 6, paddingTop: 6 }}>
              <Row k="THR Dihitung" v={<strong>{fmtIDR(line.computed_amount)}</strong>} />
            </div>
          </section>

          {/* Editable final amount */}
          <section style={{ marginBottom: 16 }}>
            <h3 style={{ fontSize: 14, color: '#667', margin: '0 0 6px' }}>THR Final</h3>
            <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
              <input type="number" min="0" step="1000" value={amount} disabled={!editable}
                onChange={(e) => setAmount(e.target.value)}
                style={{ flex: 1, padding: 8, borderRadius: 6, border: '1px solid #ccd' }} />
              {editable && Number(amount) !== numVal(line.computed_amount) && (
                <button type="button" onClick={resetToComputed}
                  title="Kembalikan ke nilai hitung"
                  style={{ background: '#eef3ff', border: '1px solid #bdd7f9', borderRadius: 6, color: '#1967d2', fontSize: 12, padding: '8px 10px', cursor: 'pointer', whiteSpace: 'nowrap' }}>
                  Reset
                </button>
              )}
            </div>
            <p style={{ fontSize: 11, color: '#9aa', margin: '4px 0 0' }}>
              Default mengikuti nilai hitung — sesuaikan bila ada kesepakatan/kebijakan khusus.
            </p>

            <label style={{ display: 'block', fontSize: 13, marginBottom: 4, marginTop: 12 }}>Catatan</label>
            <textarea value={note} disabled={!editable} onChange={(e) => setNote(e.target.value)} rows={2}
              style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd' }} />
          </section>
        </div>

        <div style={{ borderTop: '1px solid #e6e8ee', padding: '12px 20px', background: '#fff' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 10, fontSize: 15 }}>
            <span>THR Final</span>
            <strong style={{ color: '#1e7e34' }}>{fmtIDR(Number(amount) || 0)}</strong>
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
            <div style={{ color: '#889', fontSize: 13 }}>Run terkunci — baris tidak dapat diubah.</div>
          )}
        </div>
      </div>
    </div>
  );
}

function Row({ k, v }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, padding: '2px 0' }}>
      <span style={{ color: '#667' }}>{k}</span>
      <span>{v}</span>
    </div>
  );
}
