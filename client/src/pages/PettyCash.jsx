import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getPettyCashDay, recordPettyCashOpening, recordPettyCashClosing,
} from '../api';
import { isAdminRole } from '../roles';
import CurrencyInput from '../components/CurrencyInput';

// Kas Kecil — the daily cash box count, one row per branch.
//
// The whole page is one equation made visible:
//
//   opening + setoran masuk − setoran keluar − pembelanjaan = seharusnya
//
// and then the counted closing against it. A gap is real money, so it is shown
// in red and cannot be saved without an explanation.

const rp = (n) => 'Rp ' + Number(n || 0).toLocaleString('id-ID');
const today = () => new Date().toISOString().slice(0, 10);

export default function PettyCash() {
  const isAdmin = isAdminRole();

  const [date, setDate] = useState(today());
  const [rows, setRows] = useState([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  // The branch whose form is open, plus its two draft figures.
  const [openBranch, setOpenBranch] = useState(null);
  const [mode, setMode] = useState('opening');
  const [amount, setAmount] = useState('');
  const [note, setNote] = useState('');
  const [saving, setSaving] = useState(false);

  const load = useCallback(() =>
    getPettyCashDay(date)
      .then(r => setRows(r.data || []))
      .catch(() => setError('Gagal memuat data kas kecil'))
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

  // What the draft closing implies, recomputed as the operator types so the
  // variance is visible before anything is committed.
  const draftVariance = (row) => {
    if (mode !== 'closing' || amount === '') return null;
    return Number(amount) - row.expected_closing;
  };

  const submit = async (e, row) => {
    e.preventDefault();
    if (amount === '') { setError('Jumlah wajib diisi'); return; }
    setError('');
    setSaving(true);
    try {
      const payload = { branch_id: row.branch_id, date, amount: Number(amount) };
      if (mode === 'opening') await recordPettyCashOpening(payload);
      else await recordPettyCashClosing({ ...payload, note });
      closeForm();
      await load();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan');
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Kas Kecil</h1>
        <div style={{display:'flex',gap:'0.75rem',alignItems:'center'}}>
          <input type="date" value={date} onChange={e => setDate(e.target.value)}
                 style={{padding:'0.45rem 0.6rem',border:'1px solid #ddd',borderRadius:'6px'}} />
          <Link to="/setoran" className="btn btn-secondary btn-sm">Setoran</Link>
          <Link to="/daily-purchases" className="btn btn-secondary btn-sm">Pembelanjaan</Link>
        </div>
      </div>

      {error && <div className="error-msg" style={{marginBottom:'1rem'}}>{error}</div>}
      {!isAdmin && (
        <div className="card" style={{marginBottom:'1rem',color:'#777',fontSize:'0.88rem'}}>
          Hanya admin yang dapat mencatat saldo awal dan akhir. Halaman ini bisa dilihat
          untuk mengetahui isi kas sebelum berbelanja.
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
                <span style={{color:'#777',fontSize:'0.85rem'}}>
                  Saldo buku besar: {rp(row.ledger_balance)}
                </span>
              </div>
              {isAdmin && (
                <div className="actions">
                  <button className="btn btn-secondary btn-sm" onClick={() => startForm(row, 'opening')}>
                    {row.has_opening ? 'Ubah Saldo Awal' : 'Catat Saldo Awal'}
                  </button>
                  <button className="btn btn-primary btn-sm"
                          disabled={!row.has_opening}
                          title={row.has_opening ? '' : 'Catat saldo awal dulu'}
                          onClick={() => startForm(row, 'closing')}>
                    {row.has_closing ? 'Ubah Saldo Akhir' : 'Catat Saldo Akhir'}
                  </button>
                </div>
              )}
            </div>

            {/* The equation, left to right. */}
            <div style={{display:'grid',gridTemplateColumns:'repeat(auto-fit,minmax(130px,1fr))',
                         gap:'0.6rem',fontSize:'0.9rem'}}>
              <Cell label="Saldo awal" value={row.has_opening ? rp(row.opening_amount) : '—'}
                    muted={!row.has_opening} />
              <Cell label="Setoran masuk" value={'+ ' + rp(row.cash_in)} />
              <Cell label="Setoran keluar" value={'− ' + rp(row.cash_out)} />
              <Cell label={`Pembelanjaan (${row.spending_count})`} value={'− ' + rp(row.spending)} />
              <Cell label="Seharusnya" value={rp(row.expected_closing)} strong />
              <Cell label="Dihitung" value={row.has_closing ? rp(row.closing_amount) : '—'}
                    muted={!row.has_closing} strong />
            </div>

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
                      {mode === 'opening' ? 'Saldo awal hari ini' : 'Saldo akhir hari ini'}
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
                             placeholder="mis. uang kembalian belum masuk"
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
                    Mengubah saldo awal akan menghapus saldo akhir hari ini — selisihnya
                    dihitung dari saldo awal, jadi harus dihitung ulang.
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
