import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getServiceCharge, saveServiceChargePool, deleteServiceChargePool, setServiceChargeComponent,
} from '../../api';
import CurrencyInput from '../../components/CurrencyInput';

// Service Charge — set each branch's pot for the month BEFORE evaluation.
//
// The pot is a typed amount or a percentage of the branch's net revenue. It is
// split between the branch's employees whose wage structure carries the Service
// Charge component, one share each scaled by their grade's Service Charge %
// (A 100% → 1 share, B 50% → ½, 0% → none), so a share someone forfeits goes
// to the others. Payroll pays these shares (server: service/service_charge.go).

const monthNow = () => {
  const n = new Date();
  return `${n.getFullYear()}-${String(n.getMonth() + 1).padStart(2, '0')}`;
};
const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(Number(n || 0));
const fmtMonth = (m) => {
  const [y, mo] = m.split('-');
  return new Date(Number(y), Number(mo) - 1, 1).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' });
};

// Same rule as service.SplitByWeight: floor, then the leftover rupiah to the
// largest remainders (ties to the earlier row), so the parts sum to the pot.
function splitByWeight(total, weights) {
  const out = weights.map(() => 0);
  const sum = weights.reduce((s, w) => s + (w > 0 ? w : 0), 0);
  if (!sum || total <= 0) return out;
  const t = BigInt(Math.round(total));
  const S = BigInt(sum);
  const rems = [];
  let given = 0n;
  weights.forEach((w, i) => {
    if (w <= 0) return;
    const W = BigInt(w);
    const part = (t * W) / S;
    out[i] = part;
    given += part;
    rems.push({ i, r: (t * W) % S });
  });
  rems.sort((a, b) => (a.r === b.r ? a.i - b.i : (b.r > a.r ? 1 : -1)));
  for (let k = 0; given < t; k++) { out[rems[k % rems.length].i] += 1n; given += 1n; }
  return out.map(Number);
}

function poolFromForm(form, revenue) {
  if (form.mode === 'percent') {
    const p = parseFloat(String(form.percent).replace(',', '.'));
    if (isNaN(p) || revenue <= 0) return 0;
    const bp = Math.round(p * 100);
    return Math.floor((revenue * bp + 5000) / 10000);
  }
  return Number(form.amount || 0);
}

const formFromBranch = (b) => ({
  mode: b.pool?.input_mode || 'percent',
  amount: b.pool?.input_mode === 'amount' ? b.pool.pool_amount : '',
  percent: b.pool?.percent != null ? String(b.pool.percent).replace('.', ',') : '',
  note: b.pool?.note || '',
});

function BranchCard({ b, month, locked, onSaved }) {
  const [form, setForm] = useState(() => formFromBranch(b));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  // The form starts from the saved pool; the parent remounts the card (key)
  // when that pool changes, so no effect is needed to resync it.

  const pot = poolFromForm(form, b.revenue);
  const amounts = useMemo(() => splitByWeight(pot, b.shares.map(s => s.weight)), [pot, b.shares]);
  const dirty = !b.pool || b.pool.input_mode !== form.mode || b.pool.pool_amount !== pot || (b.pool.note || '') !== form.note;
  const revenueMoved = b.pool && b.pool.revenue_amount !== b.revenue;

  const save = async () => {
    setError('');
    const body = { branch_id: b.branch_id, month, input_mode: form.mode, note: form.note };
    if (form.mode === 'amount') {
      if (form.amount === '' || form.amount == null) { setError('Isi nominal service charge'); return; }
      body.amount = Math.round(Number(form.amount));
    } else {
      const p = parseFloat(String(form.percent).replace(',', '.'));
      if (isNaN(p) || p < 0 || p > 100) { setError('Persentase harus 0–100'); return; }
      body.percent = p;
    }
    setSaving(true);
    try { await saveServiceChargePool(body); onSaved(); }
    catch (err) { setError(err.response?.data?.error || 'Gagal menyimpan'); }
    finally { setSaving(false); }
  };

  const remove = async () => {
    if (!window.confirm(`Hapus service charge ${b.branch_name}? Payroll akan kembali memakai nominal tetap × grade.`)) return;
    setSaving(true);
    try { await deleteServiceChargePool(b.branch_id, month); onSaved(); }
    catch (err) { setError(err.response?.data?.error || 'Gagal menghapus'); }
    finally { setSaving(false); }
  };

  const eligible = b.shares.filter(s => s.weight > 0).length;

  return (
    <div className="card" style={{ marginBottom: '1rem' }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', gap: '1rem', alignItems: 'flex-start' }}>
        <div>
          <h2 style={{ margin: 0, fontSize: '1.05rem' }}>{b.branch_name}</h2>
          <div style={{ fontSize: '0.8rem', color: '#667', marginTop: 2 }}>
            {b.member_count} karyawan ikut service charge · {eligible} berhak
            {!b.pool && <span style={{ marginLeft: 8, color: '#a06800', fontWeight: 600 }}>Belum diatur</span>}
          </div>
        </div>
        <div style={{ textAlign: 'right' }}>
          <div style={{ fontSize: '0.75rem', color: '#888' }}>Pendapatan bersih {fmtMonth(month)}</div>
          <div style={{ fontWeight: 700, fontSize: '1.15rem' }}>{fmtIDR(b.revenue)}</div>
          {revenueMoved && (
            <div style={{ fontSize: '0.75rem', color: '#a06800' }} title="Pendapatan berubah sejak disimpan (mis. impor POS terlambat). Simpan ulang untuk memakai angka terbaru.">
              saat disimpan: {fmtIDR(b.pool.revenue_amount)}
            </div>
          )}
        </div>
      </div>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '1rem', alignItems: 'flex-end', marginTop: '1rem', paddingTop: '1rem', borderTop: '1px solid #eee' }}>
        <div>
          <label style={{ display: 'block', fontSize: '0.75rem', color: '#888', marginBottom: 4 }}>Cara menentukan</label>
          <div style={{ display: 'inline-flex', border: '1px solid #ccd', borderRadius: 6, overflow: 'hidden' }}>
            {[['percent', '% Pendapatan'], ['amount', 'Nominal']].map(([k, label]) => (
              <button key={k} type="button" disabled={locked}
                onClick={() => setForm(f => ({ ...f, mode: k }))}
                style={{ border: 'none', padding: '0.4rem 0.8rem', cursor: locked ? 'default' : 'pointer',
                  background: form.mode === k ? '#1a73e8' : '#fff', color: form.mode === k ? '#fff' : '#334', fontWeight: 600 }}>
                {label}
              </button>
            ))}
          </div>
        </div>
        {form.mode === 'percent' ? (
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Persentase</label>
            <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
              <input inputMode="decimal" value={form.percent} disabled={locked} style={{ width: 90 }} placeholder="mis. 5"
                onChange={e => setForm(f => ({ ...f, percent: e.target.value.replace(/[^0-9.,]/g, '') }))} />
              <span>%</span>
            </div>
          </div>
        ) : (
          <div>
            <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Nominal (Rp)</label>
            <CurrencyInput value={form.amount} disabled={locked} style={{ width: 160 }}
              onChange={e => setForm(f => ({ ...f, amount: e.target.value }))} />
          </div>
        )}
        <div>
          <div style={{ fontSize: '0.75rem', color: '#888' }}>Dibagikan ke karyawan</div>
          <div style={{ fontWeight: 700, fontSize: '1.15rem', color: '#1e7e34' }}>{fmtIDR(pot)}</div>
          {form.mode === 'amount' && b.revenue > 0 && pot > 0 && (
            <div style={{ fontSize: '0.75rem', color: '#667' }}>{((pot / b.revenue) * 100).toLocaleString('id-ID', { maximumFractionDigits: 2 })}% dari pendapatan</div>
          )}
        </div>
        <div style={{ flex: '1 1 180px' }}>
          <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Catatan (opsional)</label>
          <input value={form.note} disabled={locked} style={{ width: '100%' }}
            onChange={e => setForm(f => ({ ...f, note: e.target.value }))} />
        </div>
        {!locked && (
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button className="btn btn-primary" onClick={save} disabled={saving || !dirty}>
              {saving ? 'Menyimpan…' : 'Simpan'}
            </button>
            {b.pool && <button className="btn btn-secondary" onClick={remove} disabled={saving}>Hapus</button>}
          </div>
        )}
      </div>
      {error && <div className="error-msg" style={{ marginTop: '0.75rem' }}>{error}</div>}

      {b.shares.length === 0 ? (
        <p style={{ color: '#888', fontSize: '0.85rem', margin: '1rem 0 0' }}>
          Tidak ada karyawan aktif di cabang ini yang struktur gajinya memuat komponen Service Charge.
        </p>
      ) : (
        <div style={{ overflowX: 'auto', marginTop: '1rem' }}>
          <table>
            <thead>
              <tr>
                <th>Karyawan</th><th>Skor</th><th>Grade</th>
                <th title="Persentase Service Charge yang dibayar grade ini — bobot bagiannya dari pot">Bobot</th>
                <th style={{ textAlign: 'right' }}>Bagian {dirty && b.pool ? '(pratinjau)' : ''}</th>
              </tr>
            </thead>
            <tbody>
              {b.shares.map((s, i) => (
                <tr key={s.employee_id} style={s.weight === 0 ? { color: '#999' } : undefined}>
                  <td>
                    <Link to={`/hr/employees/${s.employee_id}`}>{s.full_name}</Link>
                    <div style={{ fontSize: '0.75rem', color: '#999', fontFamily: 'monospace' }}>{s.employee_code}</div>
                  </td>
                  <td>{s.score ?? <span title="Belum dievaluasi — dibayar penuh">—</span>}</td>
                  <td>{s.grade || '—'}</td>
                  <td>{s.weight === 0 ? <span style={{ color: '#c5221f' }}>Tidak berhak</span> : `${s.weight}%`}</td>
                  <td style={{ textAlign: 'right', fontWeight: 600 }}>{fmtIDR(amounts[i])}</td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr>
                <td colSpan={4} style={{ textAlign: 'right', fontSize: '0.85rem', color: '#667' }}>Total</td>
                <td style={{ textAlign: 'right', fontWeight: 700 }}>{fmtIDR(amounts.reduce((a, x) => a + x, 0))}</td>
              </tr>
            </tfoot>
          </table>
          {eligible === 0 && pot > 0 && (
            <div className="error-msg" style={{ marginTop: '0.5rem' }}>
              Tidak ada karyawan yang berhak bulan ini — pot tidak dibagikan.
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default function ServiceCharge() {
  const [month, setMonth] = useState(monthNow());
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [pickComponent, setPickComponent] = useState('');

  const load = () => {
    setLoading(true);
    setError('');
    getServiceCharge({ month })
      .then(r => { setData(r.data); setPickComponent(r.data?.component?.id || ''); })
      .catch(err => { setData(null); setError(err.response?.data?.error || 'Gagal memuat data service charge'); })
      .finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, [month]); // eslint-disable-line react-hooks/exhaustive-deps -- reload on filter change only

  const saveComponent = async () => {
    try { await setServiceChargeComponent(pickComponent); load(); }
    catch (err) { alert(err.response?.data?.error || 'Gagal menyimpan komponen'); }
  };

  const period = data?.payroll_period;
  const locked = period && period.status !== 'open';

  return (
    <>
      <div className="page-header">
        <div>
          <h1>Service Charge</h1>
          <div style={{ color: '#667', fontSize: '0.9rem' }}>Tentukan pot per cabang sebelum evaluasi. Bagian tiap karyawan mengikuti grade evaluasinya.</div>
        </div>
        <Link to="/hr/performance" className="btn btn-secondary">Lanjut ke Evaluasi →</Link>
      </div>

      <div className="card" style={{ marginBottom: '1.25rem', display: 'flex', flexWrap: 'wrap', gap: '1.5rem', alignItems: 'flex-end' }}>
        <div>
          <label style={{ display: 'block', fontSize: '0.75rem', color: '#888' }}>Bulan</label>
          <input type="month" value={month} onChange={e => setMonth(e.target.value)} />
        </div>
        {data && (
          <>
            <div>
              <div style={{ fontSize: '0.75rem', color: '#888' }}>Total pendapatan bersih</div>
              <div style={{ fontWeight: 700 }}>{fmtIDR(data.total_revenue)}</div>
            </div>
            <div>
              <div style={{ fontSize: '0.75rem', color: '#888' }}>Total service charge</div>
              <div style={{ fontWeight: 700, color: '#1e7e34' }}>{fmtIDR(data.total_pool)}</div>
            </div>
          </>
        )}
      </div>

      {data && (
        <div className="card" style={{ marginBottom: '1.25rem', background: data.component ? undefined : '#fff8e1' }}>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.75rem', alignItems: 'center' }}>
            <span style={{ fontSize: '0.9rem' }}>
              {data.component
                ? <>Komponen gaji yang dibayar dari pot: <b>{data.component.name}</b>{!data.component.grade_linked && ' (tidak terkait grade — semua anggota mendapat bagian penuh)'}</>
                : <b>Pilih komponen gaji yang mewakili Service Charge agar pot bisa dibagikan.</b>}
            </span>
            <select value={pickComponent} onChange={e => setPickComponent(e.target.value)}>
              <option value="">— pilih komponen —</option>
              {data.candidates.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
            <button className="btn btn-secondary btn-sm" onClick={saveComponent}
              disabled={pickComponent === (data.component?.id || '')}>Simpan Komponen</button>
          </div>
        </div>
      )}

      {period && (
        <div className="card" style={{ marginBottom: '1.25rem', background: locked ? '#fce8e6' : '#e8f0fe', fontSize: '0.9rem' }}>
          {locked
            ? <>Periode gaji {fmtMonth(month)} sudah <b>{period.status === 'paid' ? 'dibayar' : 'ditutup'}</b> — service charge bulan ini tidak dapat diubah.</>
            : period.line_count > 0
              ? <>Baris gaji {fmtMonth(month)} sudah dibuat. Setelah mengubah service charge, <Link to={`/hr/payroll/${period.id}`}>buat ulang baris gaji</Link> agar jumlah barunya ikut dibayar.</>
              : <>Periode gaji {fmtMonth(month)} sudah dibuka; service charge akan dipakai saat baris gaji dibuat.</>}
        </div>
      )}

      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
      {loading && !data ? (
        <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>
      ) : data?.branches.map(b => (
        <BranchCard key={`${month}:${b.branch_id}:${b.pool?.updated_at || 'none'}`} b={b} month={month} locked={locked} onSaved={load} />
      ))}
    </>
  );
}
