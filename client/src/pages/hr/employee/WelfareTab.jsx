import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { getWelfareFundEmployee, payoutWelfareFund, getAccounts } from '../../../api';
import { canApprove } from '../../../roles';
import { todayLocal } from '../leaveRules';
import { fmtIDR } from '../../../format';
import { fmtDate } from './shared';

const WELFARE_MOVEMENT_LABEL = { contribution: 'Iuran gajian', payout: 'Pencairan', opening: 'Saldo awal' };

export function WelfareTab({ employeeId }) {
  const [d, setD] = useState(null);
  const [error, setError] = useState('');
  const [payout, setPayout] = useState(false);

  const load = useCallback(() => {
    getWelfareFundEmployee(employeeId)
      .then(r => { setD(r.data); setError(''); })
      .catch(err => setError(err.response?.data?.error || 'Gagal memuat dana kesejahteraan'));
  }, [employeeId]);
  useEffect(() => { load(); }, [load]);

  if (error) return <div className="error-msg">{error}</div>;
  if (!d) return <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Memuat…</p>;

  const active = d.employee.status === 'active';
  const mayPayout = !active && canApprove() && d.balance > 0 && d.unsettled_payrolls === 0;
  const tile = (label, value, sub, color) => (
    <div style={{ padding: '0.75rem 0.9rem', borderRadius: 6, background: '#f6f8fb' }}>
      <div style={{ fontSize: '0.72rem', color: '#667', textTransform: 'uppercase', letterSpacing: '0.03em' }}>{label}</div>
      <div style={{ fontSize: '1.35rem', fontWeight: 700, color: color || '#223' }}>{value}</div>
      {sub && <div style={{ fontSize: '0.75rem', color: '#889' }}>{sub}</div>}
    </div>
  );

  return (
    <div>
      <div className="card" style={{ marginBottom: '1rem' }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', gap: '0.6rem' }}>
          {tile('Saldo Dana Kesejahteraan', fmtIDR(d.balance), 'Liabilitas 21300', '#1e7e34')}
          {tile('Iuran per periode', fmtIDR(d.per_period_amount), d.per_period_amount > 0 ? 'dari struktur upah saat ini' : 'belum ada komponen')}
          {tile('Total iuran', fmtIDR(d.total_contributed))}
          {tile('Sudah dicairkan', fmtIDR(d.total_paid_out))}
        </div>
        <p style={{ fontSize: '0.82rem', color: '#667', margin: '0.75rem 0 0' }}>
          Iuran perusahaan (komponen gaji tipe <em>Dana Kesejahteraan</em>) ditambahkan setiap kali periode penggajian ditutup,
          terpisah dari Tunjangan Kesehatan. Seluruh saldo dicairkan saat karyawan pensiun / berhenti bekerja.
        </p>
        {d.per_period_amount === 0 && active && (
          <p style={{ fontSize: '0.82rem', color: '#a06800', margin: '0.5rem 0 0' }}>
            Karyawan ini belum punya komponen Dana Kesejahteraan. Tambahkan di <Link to="/hr/wage-components">Komponen Gaji</Link> lalu pasang di tab Gaji.
          </p>
        )}
        {!active && (
          <div style={{ marginTop: '0.75rem', padding: '0.6rem 0.8rem', borderRadius: 6, background: mayPayout ? '#e8f0fe' : '#f5f7fb', fontSize: '0.85rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
            <span>
              {d.balance <= 0
                ? 'Dana kesejahteraan sudah dicairkan / kosong.'
                : d.unsettled_payrolls > 0
                  ? <>Masih ada <strong>{d.unsettled_payrolls}</strong> periode penggajian yang belum ditutup/terposting. Selesaikan dulu agar iuran terakhir masuk.</>
                  : canApprove()
                    ? 'Karyawan sudah tidak aktif. Saldo dana kesejahteraan dapat dicairkan.'
                    : 'Pencairan dana kesejahteraan dilakukan oleh manajer.'}
            </span>
            {mayPayout && <button className="btn btn-primary btn-sm" onClick={() => setPayout(true)}>Cairkan Dana Kesejahteraan</button>}
          </div>
        )}
      </div>

      <div className="card">
        <h3 style={{ margin: '0 0 0.75rem', fontSize: '1rem' }}>Mutasi Dana Kesejahteraan</h3>
        {d.movements.length === 0 ? (
          <p style={{ color: '#888', textAlign: 'center', padding: '1.5rem' }}>Belum ada iuran. Iuran pertama masuk saat periode penggajian berikutnya ditutup.</p>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%' }}>
              <thead>
                <tr><th>Tanggal</th><th>Jenis</th><th>Periode</th><th>Keterangan</th><th style={{ textAlign: 'right' }}>Jumlah</th></tr>
              </thead>
              <tbody>
                {d.movements.map(m => (
                  <tr key={m.id}>
                    <td>{fmtDate(m.movement_date)}</td>
                    <td>{WELFARE_MOVEMENT_LABEL[m.movement_type] || m.movement_type}</td>
                    <td>{m.period_month ? new Date(m.period_month).toLocaleDateString('id-ID', { month: 'long', year: 'numeric' }) : '-'}</td>
                    <td style={{ color: '#667' }}>{m.note || '-'}</td>
                    <td style={{ textAlign: 'right', whiteSpace: 'nowrap', color: m.amount < 0 ? '#c5221f' : '#1e7e34' }}>{m.amount > 0 ? '+' : ''}{fmtIDR(m.amount)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {payout && (
        <WelfarePayoutModal employee={d.employee} balance={d.balance}
          onClose={() => setPayout(false)}
          onDone={(res) => { setPayout(false); load(); alert(`Dana kesejahteraan ${d.employee.full_name} dicairkan: ${fmtIDR(res.amount)}`); }} />
      )}
    </div>
  );
}

function WelfarePayoutModal({ employee, balance, onClose, onDone }) {
  const [accounts, setAccounts] = useState([]);
  const [date, setDate] = useState(todayLocal());
  const [fund, setFund] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getAccounts()
      .then(r => setAccounts((r.data || []).filter(a => a.account_type === 'asset' && !a.is_system)))
      .catch(() => setAccounts([]));
  }, []);

  const run = async () => {
    if (!fund) { setError('Pilih sumber dana.'); return; }
    setBusy(true); setError('');
    try {
      const r = await payoutWelfareFund(employee.id, { payout_date: date, fund_source_account_id: fund, note });
      onDone(r.data);
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal mencairkan.');
    } finally {
      setBusy(false);
    }
  };

  const lbl = { display: 'block', fontSize: '0.78rem', color: '#667', marginTop: '0.6rem', marginBottom: '0.2rem' };
  const inp = { width: '100%', padding: '0.5rem', border: '1px solid #d4d9e2', borderRadius: 6, boxSizing: 'border-box' };
  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000, padding: '1rem' }} onClick={onClose}>
      <div className="card" style={{ width: 480, maxWidth: '96vw' }} onClick={e => e.stopPropagation()}>
        <h3 style={{ marginTop: 0 }}>Cairkan Dana Kesejahteraan</h3>
        <div style={{ background: '#f5f7fb', borderRadius: 6, padding: '0.6rem 0.8rem', fontSize: '0.9rem' }}>
          {employee.full_name} — dicairkan <strong>{fmtIDR(balance)}</strong> (seluruh saldo)
        </div>
        <p style={{ fontSize: '0.8rem', color: '#667' }}>Kasbon yang masih berjalan <strong>tidak</strong> dipotong otomatis.</p>
        <label style={lbl}>Tanggal pencairan</label>
        <input type="date" value={date} max={todayLocal()} onChange={e => setDate(e.target.value)} style={inp} />
        <label style={lbl}>Dibayar dari</label>
        <select className="select-clean" value={fund} onChange={e => setFund(e.target.value)} style={inp}>
          <option value="">— pilih akun —</option>
          {accounts.map(x => <option key={x.id} value={x.id}>{x.account_number ? `${x.account_number} · ` : ''}{x.name}</option>)}
        </select>
        <label style={lbl}>Catatan (opsional)</label>
        <input value={note} onChange={e => setNote(e.target.value)} style={inp} />
        {error && <div className="error-msg" style={{ marginTop: '0.75rem' }}>{error}</div>}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1rem' }}>
          <button className="btn btn-secondary" onClick={onClose} disabled={busy}>Batal</button>
          <button className="btn btn-primary" onClick={run} disabled={busy}>{busy ? 'Memproses…' : 'Cairkan'}</button>
        </div>
      </div>
    </div>
  );
}
