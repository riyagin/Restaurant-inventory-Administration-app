import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getBenefitFundSummary, getBenefitFundEmployee, previewBenefitClaim, createBenefitClaim,
  cancelBenefitClaim, payoutBenefitPension, getAllEmployees, getAccounts,
} from '../../api';
import CurrencyInput from '../../components/CurrencyInput';
import SearchSelect from '../../components/SearchSelect';
import { canApprove } from '../../roles';

// Dana Kesejahteraan Karyawan. Each employee has a monthly allowance (wage
// component type "Dana Kesejahteraan"). Sick claims are paid from that month's
// allowance; anything above it becomes a kasbon. On payday the unused part of the
// month moves to the pension fund, which is paid out on resignation.

const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));
const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';
const fmtMonth = (d) => d ? new Date(d.length === 7 ? `${d}-01` : d).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' }) : '-';
const todayISO = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
};
const thisMonth = () => todayISO().slice(0, 7);
const addMonths = (ym, n) => {
  const [y, m] = ym.split('-').map(Number);
  const d = new Date(y, m - 1 + n, 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
};
const yearsOfService = (join, until) => {
  if (!join) return '-';
  const a = new Date(join); const b = until ? new Date(until) : new Date();
  const months = (b.getFullYear() - a.getFullYear()) * 12 + b.getMonth() - a.getMonth();
  const y = Math.floor(months / 12); const m = months % 12;
  return [y && `${y} th`, m && `${m} bln`].filter(Boolean).join(' ') || '< 1 bln';
};
const isFundSource = (a) => a.account_type === 'asset' && !a.is_system;
const errMsg = (err, fallback) => err?.response?.data?.error || fallback;

const STATUS_LABEL = { active: 'Aktif', inactive: 'Nonaktif', resigned: 'Resign' };
const MOVEMENT_LABEL = {
  contribution: 'Iuran gajian', claim: 'Klaim sakit', claim_reversal: 'Pembatalan klaim',
  sweep_out: 'Sisa → pensiun', sweep_in: 'Dari dana kesehatan', payout: 'Pencairan', opening: 'Saldo awal',
};

export default function BenefitFund() {
  const [month, setMonth] = useState(thisMonth());
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [q, setQ] = useState('');
  const [detailId, setDetailId] = useState(null);
  const [claimFor, setClaimFor] = useState(null); // '' = pick employee
  const [accounts, setAccounts] = useState([]);

  const load = useCallback(() => {
    getBenefitFundSummary(month)
      .then(r => setData(r.data))
      .catch(() => setData(null))
      .finally(() => setLoading(false));
  }, [month]);
  useEffect(() => { load(); }, [load]);
  useEffect(() => {
    getAccounts().then(r => setAccounts((r.data || []).filter(isFundSource))).catch(() => setAccounts([]));
  }, []);

  const rows = useMemo(() => {
    const all = data?.rows || [];
    const s = q.trim().toLowerCase();
    if (!s) return all;
    return all.filter(r => r.full_name.toLowerCase().includes(s) || r.employee_code.toLowerCase().includes(s));
  }, [data, q]);

  const t = data?.totals || {};

  return (
    <div>
      <div className="page-header">
        <h1>Dana Kesejahteraan</h1>
        <button className="btn btn-primary" onClick={() => setClaimFor('')}>+ Klaim Sakit</button>
      </div>
      <p style={{ color: '#667', fontSize: '0.88rem', marginTop: '-0.5rem', maxWidth: '760px' }}>
        Jatah bulanan dari perusahaan (komponen gaji tipe <em>Dana Kesejahteraan</em>). Klaim sakit dibayar dari jatah bulan itu;
        kelebihannya menjadi kasbon. Saat gajian, sisa jatah yang tidak terpakai dipindah ke dana pensiun, yang dicairkan saat karyawan resign.
      </p>

      <div style={{ display: 'flex', gap: '0.5rem', margin: '1rem 0', flexWrap: 'wrap', alignItems: 'center' }}>
        <input type="month" value={month} onChange={e => { setLoading(true); setMonth(e.target.value || thisMonth()); }} style={{ ...inp, width: 'auto', margin: 0 }} />
        <input placeholder="Cari karyawan…" value={q} onChange={e => setQ(e.target.value)} style={{ ...inp, width: '240px', margin: 0 }} />
        {data && !data.month_open && (
          <span style={{ ...chip, background: '#eef1f6', color: '#445' }}>Penggajian bulan ini sudah ditutup</span>
        )}
      </div>

      <div style={tiles}>
        <Tile label="Jatah bulan ini" value={fmtIDR(t.monthly_amount)} sub={`${rows.length} karyawan`} />
        <Tile label="Terpakai klaim bulan ini" value={fmtIDR(t.covered_this_month)} />
        <Tile label="Saldo dana pensiun" value={fmtIDR(t.pension_balance)} sub="Liabilitas 21200" strong />
        <Tile label="Saldo dana kesehatan" value={fmtIDR(t.health_balance)}
          sub={t.health_balance < 0 ? 'Klaim dibayar, iuran menyusul saat gajian' : 'Liabilitas 21100'} />
      </div>

      <div className="card" style={{ padding: 0, overflowX: 'auto' }}>
        <table style={{ width: '100%' }}>
          <thead>
            <tr>
              <th>Karyawan</th><th>Masa kerja</th>
              <th style={num}>Jatah / bulan</th><th style={num}>Terpakai {fmtMonth(month)}</th><th style={num}>Sisa bulan ini</th>
              <th style={num}>Saldo pensiun</th><th style={num}>Total klaim</th><th></th>
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={8} style={emptyTd}>Memuat…</td></tr>}
            {!loading && rows.length === 0 && (
              <tr><td colSpan={8} style={emptyTd}>
                Belum ada karyawan dengan dana kesejahteraan. Tambahkan komponen bertipe <em>Dana Kesejahteraan</em> di{' '}
                <Link to="/hr/wage-components">Komponen Gaji</Link>, lalu pasang di struktur upah karyawan.
              </td></tr>
            )}
            {!loading && rows.map(r => {
              const left = Math.max(0, r.monthly_amount - r.covered_this_month);
              return (
                <tr key={r.id}>
                  <td>
                    <button className="btn-link" style={linkBtn} onClick={() => setDetailId(r.id)}>{r.full_name}</button>
                    <div style={{ fontSize: '0.78rem', color: '#889' }}>
                      {r.employee_code} · {r.branch_name || '-'}
                      {r.status !== 'active' && <span style={{ ...chip, marginLeft: '0.4rem' }}>{STATUS_LABEL[r.status] || r.status}</span>}
                    </div>
                  </td>
                  <td>{yearsOfService(r.join_date, r.resign_date)}</td>
                  <td style={num}>{fmtIDR(r.monthly_amount)}</td>
                  <td style={num}>{r.covered_this_month ? fmtIDR(r.covered_this_month) : '–'}
                    {r.claims_this_month > 0 && <div style={{ fontSize: '0.75rem', color: '#889' }}>{r.claims_this_month} klaim</div>}
                  </td>
                  <td style={{ ...num, color: left === 0 && r.monthly_amount > 0 ? '#c5221f' : undefined }}>{fmtIDR(left)}</td>
                  <td style={{ ...num, fontWeight: 600 }}>{fmtIDR(r.pension_balance)}</td>
                  <td style={num}>{r.claims_total ? `${r.claims_total}× · ${fmtIDR(r.claimed_total)}` : '–'}</td>
                  <td style={{ whiteSpace: 'nowrap' }}>
                    {r.status === 'active' && r.monthly_amount > 0 && (
                      <button className="btn btn-sm btn-secondary" onClick={() => setClaimFor(r.id)}>Klaim</button>
                    )}{' '}
                    <button className="btn btn-sm btn-primary" onClick={() => setDetailId(r.id)}>Detail</button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {claimFor !== null && (
        <ClaimModal presetEmployeeId={claimFor} accounts={accounts}
          onClose={() => setClaimFor(null)} onSaved={() => { setClaimFor(null); load(); }} />
      )}
      {detailId && (
        <DetailModal id={detailId} month={month} accounts={accounts}
          onClose={() => setDetailId(null)} onChanged={load} />
      )}
    </div>
  );
}

function Tile({ label, value, sub, strong }) {
  return (
    <div className="card" style={{ padding: '0.8rem 1rem', margin: 0 }}>
      <div style={{ fontSize: '0.78rem', color: '#667' }}>{label}</div>
      <div style={{ fontSize: strong ? '1.35rem' : '1.15rem', fontWeight: 700, marginTop: '0.15rem' }}>{value}</div>
      {sub && <div style={{ fontSize: '0.75rem', color: '#889', marginTop: '0.15rem' }}>{sub}</div>}
    </div>
  );
}

// ── Claim ────────────────────────────────────────────────────────────────────
function ClaimModal({ presetEmployeeId, accounts, onClose, onSaved }) {
  const [employees, setEmployees] = useState([]);
  const [form, setForm] = useState({
    employee_id: presetEmployeeId || '', claim_date: todayISO(), amount: '', description: '',
    fund_source_account_id: '', kasbon_sending_method: 'Tunai', kasbon_resolution_month: addMonths(thisMonth(), 1),
  });
  const [preview, setPreview] = useState(null);
  const [previewErr, setPreviewErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getAllEmployees({ status: 'active' }).then(setEmployees).catch(() => setEmployees([]));
  }, []);
  // Default to the first fund account until one is picked.
  const fundSource = form.fund_source_account_id || accounts[0]?.id || '';

  useEffect(() => {
    if (!form.employee_id || !form.claim_date) return undefined;
    const h = setTimeout(() => {
      previewBenefitClaim({ employee_id: form.employee_id, claim_date: form.claim_date, amount: Math.round(Number(form.amount || 0)) })
        .then(r => { setPreview(r.data); setPreviewErr(''); })
        .catch(err => { setPreview(null); setPreviewErr(errMsg(err, 'Gagal menghitung.')); });
    }, 250);
    return () => clearTimeout(h);
  }, [form.employee_id, form.claim_date, form.amount]);

  const shown = form.employee_id ? preview : null;
  const split = shown?.split;
  const allowance = shown?.allowance;
  const excess = split?.excess || 0;

  const submit = async () => {
    setError('');
    if (!form.employee_id) return setError('Pilih karyawan.');
    if (!(Number(form.amount) > 0)) return setError('Isi nominal klaim.');
    if (!form.description.trim()) return setError('Isi keterangan klaim.');
    if (!fundSource) return setError('Pilih sumber dana.');
    setBusy(true);
    try {
      await createBenefitClaim({ ...form, fund_source_account_id: fundSource, amount: Math.round(Number(form.amount)) });
      onSaved();
    } catch (err) {
      setError(errMsg(err, 'Gagal menyimpan klaim.'));
    } finally {
      setBusy(false);
    }
  };

  const empOptions = employees.map(e => ({ value: e.id, label: e.full_name, sub: e.employee_code }));

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Klaim Sakit</h3>
        <label style={lbl}>Karyawan</label>
        <SearchSelect options={empOptions} value={form.employee_id} onChange={v => setForm({ ...form, employee_id: v || '' })} placeholder="Cari karyawan…" />
        <div style={{ display: 'flex', gap: '0.75rem' }}>
          <div style={{ flex: 1 }}>
            <label style={lbl}>Tanggal</label>
            <input type="date" value={form.claim_date} max={todayISO()} onChange={e => setForm({ ...form, claim_date: e.target.value })} style={inp} />
          </div>
          <div style={{ flex: 1 }}>
            <label style={lbl}>Nominal (biaya berobat)</label>
            <CurrencyInput value={form.amount} onChange={e => setForm({ ...form, amount: e.target.value })} style={inp} />
          </div>
        </div>
        <label style={lbl}>Keterangan</label>
        <input value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} placeholder="mis. Rawat jalan — demam" style={inp} />
        <label style={lbl}>Dibayar dari</label>
        <select className="select-clean" value={fundSource} onChange={e => setForm({ ...form, fund_source_account_id: e.target.value })} style={inp}>
          <option value="">— pilih akun —</option>
          {accounts.map(a => <option key={a.id} value={a.id}>{a.account_number ? `${a.account_number} · ` : ''}{a.name}</option>)}
        </select>

        {form.employee_id && previewErr && <div style={{ ...errBox, marginTop: '0.75rem' }}>{previewErr}</div>}
        {allowance && (
          <div style={splitBox}>
            <Row k={`Jatah ${fmtMonth(allowance.month)}`} v={fmtIDR(allowance.monthly_amount)} />
            <Row k="Sudah terpakai" v={fmtIDR(allowance.covered)} />
            <Row k="Sisa jatah" v={fmtIDR(allowance.available)} bold />
            {Number(form.amount) > 0 && <>
              <hr style={{ border: 0, borderTop: '1px solid #dde3ec', margin: '0.4rem 0' }} />
              <Row k="Ditanggung dana kesehatan" v={fmtIDR(split.covered)} />
              <Row k="Kelebihan → kasbon" v={fmtIDR(excess)} bold={excess > 0} color={excess > 0 ? '#a06800' : undefined} />
            </>}
            {!allowance.open && <div style={{ color: '#c5221f', marginTop: '0.3rem' }}>Penggajian bulan ini sudah ditutup — klaim tidak bisa dicatat ke bulan ini.</div>}
            {allowance.monthly_amount === 0 && <div style={{ color: '#c5221f', marginTop: '0.3rem' }}>Karyawan ini tidak punya komponen Dana Kesejahteraan.</div>}
          </div>
        )}

        {excess > 0 && (
          <div style={{ ...splitBox, background: '#fff8e1' }}>
            <div style={{ fontWeight: 600, marginBottom: '0.25rem' }}>Kasbon {fmtIDR(excess)}</div>
            <div style={{ fontSize: '0.8rem', color: '#667' }}>
              Dibuat berstatus <em>Menunggu</em>: manajer menyetujui, lalu diproses di halaman Kasbon. Dipotong dari gaji bulan penyelesaian.
            </div>
            <div style={{ display: 'flex', gap: '0.75rem' }}>
              <div style={{ flex: 1 }}>
                <label style={lbl}>Metode pengiriman</label>
                <input list="bf-sending" value={form.kasbon_sending_method} onChange={e => setForm({ ...form, kasbon_sending_method: e.target.value })} style={inp} />
                <datalist id="bf-sending">{['Tunai', 'Transfer Bank', 'E-Wallet'].map(s => <option key={s} value={s} />)}</datalist>
              </div>
              <div style={{ flex: 1 }}>
                <label style={lbl}>Dipotong di gaji</label>
                <input type="month" value={form.kasbon_resolution_month} min={thisMonth()} onChange={e => setForm({ ...form, kasbon_resolution_month: e.target.value })} style={inp} />
              </div>
            </div>
          </div>
        )}

        {error && <div style={{ ...errBox, marginTop: '0.75rem' }}>{error}</div>}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={busy}>Batal</button>
          <button className="btn btn-primary" onClick={submit} disabled={busy || (allowance && (!allowance.open || allowance.monthly_amount === 0))}>
            {busy ? 'Menyimpan…' : 'Simpan Klaim'}
          </button>
        </div>
      </div>
    </div>
  );
}

function Row({ k, v, bold, color }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', fontWeight: bold ? 600 : 400, color }}>
      <span>{k}</span><span>{v}</span>
    </div>
  );
}

// ── Employee detail + payout ─────────────────────────────────────────────────
function DetailModal({ id, month, accounts, onClose, onChanged }) {
  const [d, setD] = useState(null);
  const [tab, setTab] = useState('claims');
  const [payout, setPayout] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    getBenefitFundEmployee(id, month).then(r => setD(r.data)).catch(err => setError(errMsg(err, 'Gagal memuat.')));
  }, [id, month]);
  useEffect(() => { load(); }, [load]);

  const cancel = async (c) => {
    const note = prompt(`Batalkan klaim ${c.claim_number}? Alasan (opsional):`);
    if (note === null) return;
    try { await cancelBenefitClaim(c.id, note); load(); onChanged(); }
    catch (err) { alert(errMsg(err, 'Gagal membatalkan.')); }
  };

  const emp = d?.employee;
  const canPayout = emp && emp.status !== 'active' && canApprove();

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={{ ...modal, width: '760px' }} onClick={e => e.stopPropagation()}>
        {!d && !error && <div style={emptyTd}>Memuat…</div>}
        {error && <div style={errBox}>{error}</div>}
        {d && <>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '1rem' }}>
            <div>
              <h3 style={{ margin: 0 }}>{emp.full_name}</h3>
              <div style={{ fontSize: '0.85rem', color: '#667' }}>
                {emp.employee_code} · {emp.branch_name || '-'} · masuk {fmtDate(emp.join_date)} ({yearsOfService(emp.join_date, emp.resign_date)})
                {emp.status !== 'active' && <> · <strong>{STATUS_LABEL[emp.status] || emp.status}</strong>{emp.resign_date && ` ${fmtDate(emp.resign_date)}`}</>}
              </div>
            </div>
            <button className="btn btn-secondary btn-sm" onClick={onClose}>Tutup</button>
          </div>

          <div style={{ ...tiles, marginTop: '1rem' }}>
            <Tile label={`Sisa jatah ${fmtMonth(d.allowance.month)}`} value={fmtIDR(d.allowance.available)} sub={`dari ${fmtIDR(d.allowance.monthly_amount)}`} />
            <Tile label="Dana kesehatan" value={fmtIDR(d.health_balance)} sub={d.health_balance < 0 ? 'Iuran bulan ini belum masuk' : undefined} />
            <Tile label="Dana pensiun" value={fmtIDR(d.pension_balance)} strong />
          </div>

          {emp.status !== 'active' && (
            <div style={{ ...splitBox, background: canPayout ? '#e8f0fe' : '#f5f7fb' }}>
              {d.pension_balance <= 0 && d.health_balance >= 0
                ? <>Dana pensiun sudah dicairkan / kosong.</>
                : d.unsettled_payrolls > 0
                  ? <>Masih ada <strong>{d.unsettled_payrolls}</strong> periode penggajian yang belum ditutup/terposting. Selesaikan dulu agar iuran terakhir masuk.</>
                  : canPayout
                    ? <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem' }}>
                        <span>Karyawan sudah tidak aktif. Dana pensiun dapat dicairkan; bulan terakhir diselesaikan otomatis.</span>
                        <button className="btn btn-primary" onClick={() => setPayout(true)}>Cairkan Dana Pensiun</button>
                      </div>
                    : <>Pencairan dana pensiun dilakukan oleh manajer.</>}
            </div>
          )}

          <div style={{ display: 'flex', gap: '0.25rem', margin: '1rem 0 0.5rem' }}>
            {[['claims', `Klaim (${d.claims.length})`], ['movements', `Mutasi (${d.movements.length})`]].map(([k, l]) => (
              <button key={k} className={`btn btn-sm ${tab === k ? 'btn-primary' : 'btn-secondary'}`} onClick={() => setTab(k)}>{l}</button>
            ))}
          </div>

          {tab === 'claims' && (
            <div style={{ maxHeight: '340px', overflowY: 'auto' }}>
              <table style={{ width: '100%', fontSize: '0.85rem' }}>
                <thead><tr><th>Tanggal</th><th>Nomor</th><th>Keterangan</th><th style={num}>Klaim</th><th style={num}>Ditanggung</th><th>Kasbon</th><th></th></tr></thead>
                <tbody>
                  {d.claims.length === 0 && <tr><td colSpan={7} style={emptyTd}>Belum pernah klaim.</td></tr>}
                  {d.claims.map(c => (
                    <tr key={c.id} style={{ opacity: c.status === 'cancelled' ? 0.5 : 1 }}>
                      <td>{fmtDate(c.claim_date)}</td>
                      <td>{c.claim_number}{c.status === 'cancelled' && <div style={{ fontSize: '0.75rem', color: '#c5221f' }}>dibatalkan</div>}</td>
                      <td>{c.description}</td>
                      <td style={num}>{fmtIDR(c.total_amount)}</td>
                      <td style={num}>{fmtIDR(c.covered_amount)}</td>
                      <td>{c.kasbon_id ? <Link to={`/hr/kasbon/${c.kasbon_id}`}>{c.kasbon_number}</Link> : '–'}
                        {c.kasbon_id && <div style={{ fontSize: '0.75rem', color: '#889' }}>{fmtIDR(c.excess_amount)} · {c.kasbon_status}</div>}
                      </td>
                      <td>{c.status === 'paid' && <button className="btn btn-sm btn-link" onClick={() => cancel(c)}>Batalkan</button>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {tab === 'movements' && (
            <div style={{ maxHeight: '340px', overflowY: 'auto' }}>
              <table style={{ width: '100%', fontSize: '0.85rem' }}>
                <thead><tr><th>Tanggal</th><th>Dana</th><th>Jenis</th><th>Keterangan</th><th style={num}>Jumlah</th></tr></thead>
                <tbody>
                  {d.movements.map(m => (
                    <tr key={m.id}>
                      <td>{fmtDate(m.movement_date)}</td>
                      <td>{m.fund === 'pension' ? 'Pensiun' : 'Kesehatan'}</td>
                      <td>{MOVEMENT_LABEL[m.movement_type] || m.movement_type}{m.claim_number && ` · ${m.claim_number}`}</td>
                      <td style={{ color: '#667' }}>{m.note}</td>
                      <td style={{ ...num, color: m.amount < 0 ? '#c5221f' : '#1e7e34' }}>{m.amount > 0 ? '+' : ''}{fmtIDR(m.amount)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {payout && (
            <PayoutModal emp={emp} detail={d} accounts={accounts}
              onClose={() => setPayout(false)}
              onDone={(res) => {
                setPayout(false); load(); onChanged();
                alert(`Dana pensiun ${emp.full_name} dicairkan: ${fmtIDR(res.amount)}`);
              }} />
          )}
        </>}
      </div>
    </div>
  );
}

function PayoutModal({ emp, detail, accounts, onClose, onDone }) {
  const [date, setDate] = useState(todayISO());
  const [fund, setFund] = useState(accounts[0]?.id || '');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  // Estimate of the final-month settlement the server will do (it is authoritative).
  const a = detail.allowance;
  const finalSweep = detail.movements.some(m => m.movement_type === 'contribution' && m.period_month?.slice(0, 7) === a.month)
    ? 0 : Math.max(0, a.monthly_amount - a.covered);
  const estimate = detail.pension_balance + finalSweep;

  const run = async () => {
    if (!fund) return setError('Pilih sumber dana.');
    setBusy(true); setError('');
    try {
      const r = await payoutBenefitPension(emp.id, { payout_date: date, fund_source_account_id: fund, note });
      onDone(r.data);
    } catch (err) {
      setError(errMsg(err, 'Gagal mencairkan.'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={overlay} onClick={onClose}>
      <div className="card" style={modal} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Cairkan Dana Pensiun</h3>
        <div style={splitBox}>
          <Row k="Saldo dana pensiun" v={fmtIDR(detail.pension_balance)} />
          <Row k={`Sisa jatah bulan terakhir (${fmtMonth(a.month)})`} v={fmtIDR(finalSweep)} />
          <Row k="Perkiraan dicairkan" v={fmtIDR(estimate)} bold />
        </div>
        <p style={{ fontSize: '0.8rem', color: '#667' }}>
          Bulan terakhir diselesaikan seperti gajian (iuran masuk, sisa ke pensiun), lalu seluruh saldo pensiun dibayar.
          Kasbon yang masih berjalan <strong>tidak</strong> dipotong otomatis.
        </p>
        <label style={lbl}>Tanggal pencairan</label>
        <input type="date" value={date} max={todayISO()} onChange={e => setDate(e.target.value)} style={inp} />
        <label style={lbl}>Dibayar dari</label>
        <select className="select-clean" value={fund} onChange={e => setFund(e.target.value)} style={inp}>
          <option value="">— pilih akun —</option>
          {accounts.map(x => <option key={x.id} value={x.id}>{x.account_number ? `${x.account_number} · ` : ''}{x.name}</option>)}
        </select>
        <label style={lbl}>Catatan (opsional)</label>
        <input value={note} onChange={e => setNote(e.target.value)} style={inp} />
        {error && <div style={{ ...errBox, marginTop: '0.75rem' }}>{error}</div>}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={busy}>Batal</button>
          <button className="btn btn-primary" onClick={run} disabled={busy}>{busy ? 'Memproses…' : 'Cairkan'}</button>
        </div>
      </div>
    </div>
  );
}

const overlay = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000, padding: '1rem' };
const modal = { width: '520px', maxWidth: '96vw', maxHeight: '92vh', overflowY: 'auto' };
const lbl = { display: 'block', fontSize: '0.78rem', color: '#667', marginTop: '0.6rem', marginBottom: '0.2rem' };
const inp = { width: '100%', padding: '0.5rem', border: '1px solid #d4d9e2', borderRadius: '6px', boxSizing: 'border-box' };
const errBox = { background: '#fce8e6', color: '#c5221f', padding: '0.5rem 0.75rem', borderRadius: '6px', fontSize: '0.85rem' };
const splitBox = { background: '#f5f7fb', borderRadius: '6px', padding: '0.6rem 0.8rem', marginTop: '0.75rem', fontSize: '0.85rem', lineHeight: 1.7 };
const tiles = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.75rem', marginBottom: '1rem' };
const chip = { background: '#fce8e6', color: '#c5221f', padding: '0.05rem 0.45rem', borderRadius: '4px', fontSize: '0.72rem', fontWeight: 600 };
const num = { textAlign: 'right', whiteSpace: 'nowrap' };
const emptyTd = { textAlign: 'center', color: '#888', padding: '1.5rem' };
const linkBtn = { background: 'none', border: 0, padding: 0, color: '#1967d2', cursor: 'pointer', fontWeight: 600, textAlign: 'left' };
