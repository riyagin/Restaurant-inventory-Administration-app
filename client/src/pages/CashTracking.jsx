import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getCashTrackingDay, recordCashDayOpening, recordCashDayClosing,
} from '../api';
import { getRole } from '../roles';
import CurrencyInput from '../components/CurrencyInput';

// Pelacakan Kas — the branch till.
//
// Not the same money as Kas Kecil, which is why it is not the same page. The
// till takes cash from customers and gives it up to setoran; the box is a float
// that only ever buys things. Mixing them would let a shortfall in one be
// covered by a surplus in the other, which is exactly the error worth catching.
//
//   kas awal + penjualan tunai + setoran masuk − setoran keluar − pengeluaran
//
// Every term but the two counts is already recorded elsewhere. The POS take is
// broken out by payment method: only the cash rows are reconciled against the
// drawer, the rest are shown so the day's takings can be checked against what
// the EDC and the platforms actually settled.

const rp = (n) => 'Rp ' + Number(n || 0).toLocaleString('id-ID');
const today = () => new Date().toISOString().slice(0, 10);

export default function CashTracking() {
  const isAdmin = getRole() === 'admin';

  const [date, setDate] = useState(today());
  const [rows, setRows] = useState([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  const [openBranch, setOpenBranch] = useState(null);
  const [mode, setMode] = useState('opening');
  const [amount, setAmount] = useState('');
  const [note, setNote] = useState('');
  const [saving, setSaving] = useState(false);

  const load = useCallback(() =>
    getCashTrackingDay(date)
      .then(r => setRows(r.data || []))
      .catch(() => setError('Gagal memuat data kas'))
      .finally(() => setLoading(false)),
  [date]);

  useEffect(() => { load(); }, [load]);

  const startForm = (row, which) => {
    setError('');
    setOpenBranch(row.branch_id);
    setMode(which);
    setNote(which === 'closing' ? (row.variance_note || '') : '');
    if (which === 'opening') {
      const suggested = row.has_opening ? row.opening_amount : row.suggested_opening;
      setAmount(suggested != null ? String(suggested) : '');
    } else {
      setAmount(row.has_closing ? String(row.closing_amount) : String(row.expected_closing));
    }
  };

  const closeForm = () => { setOpenBranch(null); setAmount(''); setNote(''); };

  const submit = async (e, row) => {
    e.preventDefault();
    if (amount === '') { setError('Jumlah wajib diisi'); return; }
    setError('');
    setSaving(true);
    try {
      const payload = { branch_id: row.branch_id, date, amount: Number(amount) };
      if (mode === 'opening') await recordCashDayOpening(payload);
      else await recordCashDayClosing({ ...payload, note });
      closeForm();
      await load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan');
    } finally {
      setSaving(false);
    }
  };

  const draftVariance = (row) =>
    (mode !== 'closing' || amount === '') ? null : Number(amount) - row.expected_closing;

  return (
    <>
      <div className="page-header">
        <h1>Pelacakan Kas</h1>
        <div style={{display:'flex',gap:'0.75rem',alignItems:'center',flexWrap:'wrap'}}>
          <input type="date" value={date} onChange={e => setDate(e.target.value)}
                 style={{padding:'0.45rem 0.6rem',border:'1px solid #ddd',borderRadius:'6px'}} />
          <Link to="/petty-cash" className="btn btn-secondary btn-sm">Kas Kecil</Link>
          <Link to="/setoran" className="btn btn-secondary btn-sm">Setoran</Link>
          <Link to="/sales/import" className="btn btn-secondary btn-sm">Import POS</Link>
        </div>
      </div>

      {error && <div className="error-msg" style={{marginBottom:'1rem'}}>{error}</div>}
      {!isAdmin && (
        <div className="card" style={{marginBottom:'1rem',color:'#777',fontSize:'0.88rem'}}>
          Hanya admin yang dapat mencatat kas awal dan akhir. Halaman ini bisa dilihat untuk
          memantau penjualan tunai dan selisih kas.
        </div>
      )}

      {loading ? (
        <div className="card" style={{color:'#999'}}>Memuat…</div>
      ) : rows.length === 0 ? (
        <div className="card" style={{color:'#999'}}>Belum ada cabang.</div>
      ) : rows.map(row => {
        const isOpen = openBranch === row.branch_id;
        const dv = isOpen ? draftVariance(row) : null;
        const variance = row.has_closing ? row.variance : null;

        return (
          <div className="card" key={row.branch_id} style={{marginBottom:'1rem'}}>
            <div style={{display:'flex',justifyContent:'space-between',alignItems:'center',
                         flexWrap:'wrap',gap:'0.75rem',marginBottom:'0.9rem'}}>
              <div>
                <h2 style={{margin:0,fontSize:'1.1rem'}}>{row.branch_name}</h2>
                {!row.has_pos_import && (
                  <span style={{color:'#e67e22',fontSize:'0.83rem'}}>
                    Penjualan POS hari ini belum diimpor
                  </span>
                )}
              </div>
              {isAdmin && (
                <div className="actions">
                  <button className="btn btn-secondary btn-sm" onClick={() => startForm(row, 'opening')}>
                    {row.has_opening ? 'Ubah Kas Awal' : 'Catat Kas Awal'}
                  </button>
                  <button className="btn btn-primary btn-sm"
                          disabled={!row.has_opening}
                          title={row.has_opening ? '' : 'Catat kas awal dulu'}
                          onClick={() => startForm(row, 'closing')}>
                    {row.has_closing ? 'Ubah Kas Akhir' : 'Tutup Kas'}
                  </button>
                </div>
              )}
            </div>

            <div style={{display:'grid',gridTemplateColumns:'repeat(auto-fit,minmax(130px,1fr))',
                         gap:'0.6rem',fontSize:'0.9rem'}}>
              <Cell label="Kas awal" value={row.has_opening ? rp(row.opening_amount) : '—'} muted={!row.has_opening} />
              <Cell label="Penjualan tunai" value={'+ ' + rp(row.cash_sales)} />
              <Cell label="Setoran masuk" value={'+ ' + rp(row.cash_in)} />
              <Cell label="Setoran keluar" value={'− ' + rp(row.cash_out)} />
              <Cell label="Pengeluaran tunai" value={'− ' + rp(row.cash_expenses)} />
              <Cell label="Seharusnya" value={rp(row.expected_closing)} strong />
              <Cell label="Dihitung" value={row.has_closing ? rp(row.closing_amount) : '—'}
                    muted={!row.has_closing} strong />
            </div>

            {row.settlement.length > 0 && (
              <details style={{marginTop:'0.9rem'}}>
                <summary style={{cursor:'pointer',fontSize:'0.86rem',color:'#4f8ef7'}}>
                  Rincian pembayaran POS — tunai {rp(row.cash_sales)}, non-tunai {rp(row.non_cash_sales)}
                </summary>
                <table style={{marginTop:'0.6rem',fontSize:'0.85rem'}}>
                  <thead>
                    <tr><th>Metode</th><th>Jenis</th><th style={{textAlign:'right'}}>Jumlah</th></tr>
                  </thead>
                  <tbody>
                    {row.settlement.map(s => (
                      <tr key={s.account_id}>
                        <td>{s.account_name}</td>
                        <td>
                          <span style={{fontSize:'0.75rem',padding:'0.1rem 0.45rem',borderRadius:'4px',
                                        ...(s.is_cash
                                            ? {background:'#e6f9f0',color:'#27ae60'}
                                            : {background:'#eef2f7',color:'#5b6b7f'})}}>
                            {s.is_cash ? 'Tunai (dihitung)' : 'Non-tunai'}
                          </span>
                        </td>
                        <td style={{textAlign:'right',fontWeight:600}}>{rp(s.amount)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <p style={{margin:'0.5rem 0 0',fontSize:'0.8rem',color:'#999'}}>
                  Hanya baris tunai yang dicocokkan dengan hitungan laci. Sisanya dicatat untuk
                  dicocokkan dengan setelmen EDC dan platform.
                </p>
              </details>
            )}

            {row.has_closing && (
              <div style={{marginTop:'0.9rem',padding:'0.7rem 0.9rem',borderRadius:'6px',
                           background: variance === 0 ? '#eef8f0' : '#fdeeee',
                           color: variance === 0 ? '#1f7a3d' : '#b3261e'}}>
                {variance === 0
                  ? 'Cocok — tidak ada selisih.'
                  : <>Selisih <strong>{rp(Math.abs(variance))}</strong> {variance > 0 ? '(lebih)' : '(kurang)'}
                     {row.variance_note && <> — {row.variance_note}</>}</>}
              </div>
            )}

            {isOpen && (
              <form onSubmit={e => submit(e, row)}
                    style={{marginTop:'1rem',paddingTop:'1rem',borderTop:'1px solid #eee'}}>
                <div style={{display:'flex',gap:'0.75rem',flexWrap:'wrap',alignItems:'flex-end'}}>
                  <label style={{flex:'1 1 200px'}}>
                    <div style={{fontSize:'0.85rem',color:'#555',marginBottom:'0.3rem'}}>
                      {mode === 'opening' ? 'Kas di laci pagi ini' : 'Kas di laci akhir hari'}
                    </div>
                    <CurrencyInput value={amount} onChange={e => setAmount(e.target.value)}
                                   style={{padding:'0.5rem 0.7rem',border:'1px solid #ddd',
                                           borderRadius:'6px',width:'100%'}} />
                  </label>
                  {mode === 'closing' && (
                    <label style={{flex:'2 1 260px'}}>
                      <div style={{fontSize:'0.85rem',color:'#555',marginBottom:'0.3rem'}}>
                        Keterangan selisih {dv ? '(wajib)' : '(opsional)'}
                      </div>
                      <input value={note} onChange={e => setNote(e.target.value)}
                             placeholder="mis. kembalian kurang, uang palsu ditolak"
                             style={{padding:'0.5rem 0.7rem',border:'1px solid #ddd',
                                     borderRadius:'6px',width:'100%'}} />
                    </label>
                  )}
                  <div className="actions">
                    <button type="submit" className="btn btn-primary btn-sm" disabled={saving}>Simpan</button>
                    <button type="button" className="btn btn-secondary btn-sm" onClick={closeForm}>Batal</button>
                  </div>
                </div>

                {mode === 'opening' && row.has_closing && (
                  <p style={{margin:'0.6rem 0 0',fontSize:'0.83rem',color:'#b3261e'}}>
                    Mengubah kas awal akan menghapus kas akhir hari ini — selisihnya dihitung
                    dari kas awal, jadi harus dihitung ulang.
                  </p>
                )}
                {dv != null && dv !== 0 && (
                  <p style={{margin:'0.6rem 0 0',fontSize:'0.88rem',color:'#b3261e'}}>
                    Selisih {rp(Math.abs(dv))} {dv > 0 ? 'lebih' : 'kurang'} dari seharusnya
                    ({rp(row.expected_closing)}).
                  </p>
                )}
              </form>
            )}
          </div>
        );
      })}
    </>
  );
}

function Cell({ label, value, muted, strong }) {
  return (
    <div>
      <div style={{fontSize:'0.78rem',color:'#888',marginBottom:'0.15rem'}}>{label}</div>
      <div style={{fontWeight: strong ? 700 : 500, color: muted ? '#bbb' : 'inherit'}}>{value}</div>
    </div>
  );
}
