import { useCallback, useEffect, useMemo, useState } from 'react';
import { createOperationalExpense, getAnnualRents } from '../api';
import CurrencyInput from '../components/CurrencyInput';

// Sewa tahunan — the "Tahunan" tab of the Sewa form.
//
// A year of rent paid up front is not a year of expense in the month it was
// paid. The server books the payment to the branch's "Sewa Dibayar di Muka"
// asset and moves one twelfth into Beban Sewa each month the lease runs; this
// tab shows that split before saving (the preview) and after (the running
// leases, month by month). The preview's arithmetic mirrors
// service.AmortizationSchedule — equal twelfths, the rounding remainder on the
// last month — so what is shown here is what will be posted.

const TERM = 12;

const rp = (n) => 'Rp ' + Number(n || 0).toLocaleString('id-ID');
const today = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
};
const parseDay = (s) => {
  const [y, m, d] = String(s).slice(0, 10).split('-').map(Number);
  return new Date(y, m - 1, d);
};
const ymd = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
const dayLabel = (s) => (s ? parseDay(s).toLocaleDateString('id-ID', { day: 'numeric', month: 'short', year: 'numeric' }) : '—');
const monthLabel = (d) => d.toLocaleDateString('id-ID', { month: 'short', year: 'numeric' });

// Calendar-month arithmetic that clamps the day: 31 Jan + 1 month is 28/29 Feb,
// not 3 March. Matches Go's AddDate closely enough for display — the server's
// figure is the one stored.
function addMonths(date, n) {
  const d = new Date(date.getFullYear(), date.getMonth() + n, 1);
  const last = new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate();
  d.setDate(Math.min(date.getDate(), last));
  return d;
}

function daysBetween(from, to) {
  return Math.round((to - from) / 86_400_000);
}

function schedulePreview(total, start) {
  if (!(total > 0) || !start) return [];
  const each = Math.floor(total / TERM);
  const first = new Date(start.getFullYear(), start.getMonth(), 1);
  let cumulative = 0;
  return Array.from({ length: TERM }, (_, i) => {
    const amount = i === TERM - 1 ? total - each * (TERM - 1) : each;
    cumulative += amount;
    return { seq: i + 1, month: new Date(first.getFullYear(), first.getMonth() + i, 1), amount, cumulative, remaining: total - cumulative };
  });
}

const STATUS = {
  posted: { label: 'Dibebankan', color: '#1f9d68', bg: '#e6f6ee' },
  scheduled: { label: 'Terjadwal', color: '#6b7380', bg: '#f1f3f7' },
  cancelled: { label: 'Dibatalkan', color: '#b0b6c0', bg: '#f6f7f9' },
};

// When the next lease year falls due, as a badge. A lease already followed by
// a later payment is settled and says so instead of counting down.
function DueBadge({ lease }) {
  if (lease.renewed) {
    return <span style={{ ...badge, color: '#1f9d68', background: '#e6f6ee' }}>sudah diperpanjang</span>;
  }
  const days = daysBetween(parseDay(today()), parseDay(lease.next_due_date));
  if (days < 0) return <span style={{ ...badge, color: '#c0392b', background: '#fdecea' }}>lewat {-days} hari</span>;
  if (days === 0) return <span style={{ ...badge, color: '#c0392b', background: '#fdecea' }}>jatuh tempo hari ini</span>;
  if (days <= 60) return <span style={{ ...badge, color: '#8a6a1f', background: '#fff4dc' }}>{days} hari lagi</span>;
  return <span style={{ ...badge, color: '#6b7380', background: '#f1f3f7' }}>{days} hari lagi</span>;
}

export default function AnnualRentPanel({ branches, categories, accounts, kind }) {
  const [branchId, setBranchId] = useState('');
  const [dueDate, setDueDate] = useState(today());
  const [amount, setAmount] = useState('');
  const [date, setDate] = useState(today());
  const [creditAccountId, setCreditAccountId] = useState('');
  const [reference, setReference] = useState('');
  const [notes, setNotes] = useState('');

  const [leases, setLeases] = useState([]);
  const [expanded, setExpanded] = useState(null);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState('');

  const category = useMemo(
    () => categories.find(c => c.branch_id === branchId && c.name.toLowerCase() === 'sewa'),
    [categories, branchId],
  );

  // Rent is usually paid by transfer, but the branch box is the sensible
  // default — same rule as the monthly tab. Set when the branch is picked
  // rather than in an effect, and never over a choice already made.
  const pickBranch = (id) => {
    setBranchId(id);
    const petty = branches.find(b => b.id === id)?.petty_cash_account_id;
    if (petty) setCreditAccountId(prev => prev || petty);
  };

  const loadLeases = useCallback(() => {
    getAnnualRents({ branch_id: branchId || undefined })
      .then(r => setLeases(r.data || []))
      .catch(() => setLeases([]));
  }, [branchId]);

  useEffect(() => { loadLeases(); }, [loadLeases]);

  const total = Math.round(Number(amount) || 0);
  const start = useMemo(() => (dueDate ? parseDay(dueDate) : null), [dueDate]);
  const preview = useMemo(() => schedulePreview(total, start), [total, start]);
  const coverageEnd = start ? addMonths(start, TERM) : null;
  if (coverageEnd) coverageEnd.setDate(coverageEnd.getDate() - 1);
  const nextDue = start ? addMonths(start, TERM) : null;
  const thisMonth = new Date(new Date().getFullYear(), new Date().getMonth(), 1);
  const postedNow = preview.filter(p => p.month <= thisMonth).length;

  // A second lease covering the same days for the same branch is usually a
  // double entry. Warned, not blocked: a branch can rent two spaces.
  const overlapping = useMemo(() => {
    if (!branchId || !start) return null;
    const end = addMonths(start, TERM);
    return leases.find(l => l.branch_id === branchId
      && parseDay(l.coverage_start) < end && start <= parseDay(l.coverage_end));
  }, [leases, branchId, start]);

  const submit = async (e) => {
    e.preventDefault();
    setError('');
    setSaved('');
    if (!branchId) { setError('Cabang wajib dipilih'); return; }
    if (!category) { setError('Cabang ini belum punya kategori Sewa'); return; }
    if (!dueDate) { setError('Tanggal jatuh tempo wajib diisi'); return; }
    if (!creditAccountId) { setError('Sumber dana wajib dipilih'); return; }
    if (!(total >= TERM)) { setError('Harga sewa per tahun wajib diisi'); return; }

    setSaving(true);
    try {
      const r = await createOperationalExpense({
        billing: 'annual',
        due_date: dueDate,
        date,
        branch_id: branchId,
        category_id: category.id,
        credit_account_id: creditAccountId,
        amount: total,
        reference,
        notes,
      });
      const n = r.data.months_amortized || 0;
      setSaved(`${r.data.number} tersimpan — sewa ${dayLabel(dueDate)} s/d ${dayLabel(ymd(coverageEnd))}`
        + (n > 0 ? `, ${n} bulan langsung dibebankan` : ''));
      setAmount('');
      setReference('');
      setNotes('');
      loadLeases();
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan sewa tahunan');
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}
      {saved && (
        <div style={{
          background: '#e6f9f0', border: '1px solid #b2dfdb', color: '#1b5e45',
          borderRadius: '8px', padding: '0.6rem 0.9rem', marginBottom: '1rem', fontSize: '0.88rem', fontWeight: 600,
        }}>
          {saved}
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 420px), 1fr))', gap: '1.5rem', alignItems: 'start' }}>
        <div className="card">
          <form onSubmit={submit}>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(190px,1fr))', gap: '0.85rem' }}>
              <Field label="Cabang">
                <select value={branchId} required style={fieldStyle} onChange={e => pickBranch(e.target.value)}>
                  <option value="">— pilih cabang —</option>
                  {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
                </select>
              </Field>

              <Field label="Tanggal jatuh tempo" hint="awal masa sewa">
                <input type="date" value={dueDate} required style={fieldStyle} onChange={e => setDueDate(e.target.value)} />
              </Field>

              <Field label="Harga sewa per tahun">
                <CurrencyInput value={amount} style={fieldStyle} onChange={e => setAmount(e.target.value)} />
              </Field>

              <Field label="Tanggal bayar" hint="kapan uangnya keluar">
                <input type="date" value={date} required style={fieldStyle} onChange={e => setDate(e.target.value)} />
              </Field>

              <Field label="Dibayar dari">
                <select value={creditAccountId} required style={fieldStyle} onChange={e => setCreditAccountId(e.target.value)}>
                  <option value="">— pilih akun —</option>
                  {accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
                </select>
              </Field>

              <Field label="No. referensi" hint="opsional — no. perjanjian sewa">
                <input value={reference} style={fieldStyle} onChange={e => setReference(e.target.value)} />
              </Field>

              <Field label="Catatan" hint="opsional">
                <input value={notes} style={fieldStyle} onChange={e => setNotes(e.target.value)} />
              </Field>
            </div>

            {start && (
              <div style={{
                marginTop: '1rem', background: kind.tint, borderRadius: '8px', padding: '0.65rem 0.9rem',
                fontSize: '0.84rem', lineHeight: 1.55, color: '#33384a',
              }}>
                Masa sewa <strong>{dayLabel(dueDate)}</strong> s/d <strong>{dayLabel(ymd(coverageEnd))}</strong>.
                Jatuh tempo berikutnya <strong>{dayLabel(ymd(nextDue))}</strong>.
                {total > 0 && <> Beban per bulan <strong>{rp(Math.floor(total / TERM))}</strong>.</>}
              </div>
            )}

            {overlapping && (
              <div style={{
                marginTop: '0.75rem', background: '#fff8e6', border: '1px solid #f0e0b0', color: '#8a6a1f',
                borderRadius: '8px', padding: '0.6rem 0.9rem', fontSize: '0.84rem', lineHeight: 1.5,
              }}>
                Cabang ini sudah punya sewa tahunan <strong>{overlapping.number}</strong> untuk{' '}
                {dayLabel(overlapping.coverage_start)} s/d {dayLabel(overlapping.coverage_end)}, yang tumpang tindih
                dengan masa sewa ini. Lanjutkan hanya jika memang menyewa dua tempat.
              </div>
            )}

            <div style={{ display: 'flex', gap: '0.6rem', marginTop: '1.25rem' }}>
              <button type="submit" className="btn btn-primary" disabled={saving}>
                {saving ? 'Menyimpan…' : 'Simpan Sewa Tahunan'}
              </button>
            </div>
          </form>

          {/* The split before it is committed: how much becomes expense each
              month and how much is still prepaid after it. */}
          {preview.length > 0 && (
            <div style={{ marginTop: '1.5rem' }}>
              <h3 style={{ fontSize: '0.95rem', margin: '0 0 0.25rem' }}>Pratinjau Amortisasi</h3>
              <p style={{ fontSize: '0.78rem', color: '#8a93a3', margin: '0 0 0.6rem', lineHeight: 1.5 }}>
                Pembayaran dicatat sebagai <strong>Sewa Dibayar di Muka</strong>, lalu dibebankan 1/12 setiap bulan.
                {postedNow > 0 && <> {postedNow} bulan yang sudah berjalan langsung dibebankan saat disimpan.</>}
              </p>
              <div style={{ overflowX: 'auto' }}>
                <table style={{ fontSize: '0.84rem' }}>
                  <thead>
                    <tr>
                      <th>#</th><th>Bulan</th>
                      <th style={{ textAlign: 'right' }}>Beban</th>
                      <th style={{ textAlign: 'right' }}>Akumulasi</th>
                      <th style={{ textAlign: 'right' }}>Sisa di Muka</th>
                    </tr>
                  </thead>
                  <tbody>
                    {preview.map(p => {
                      const now = p.month <= thisMonth;
                      return (
                        <tr key={p.seq} style={now ? { background: '#f6fbf8' } : undefined}>
                          <td style={{ color: '#aaa', padding: '0.45rem 0.75rem' }}>{p.seq}</td>
                          <td style={{ padding: '0.45rem 0.75rem', whiteSpace: 'nowrap' }}>
                            {monthLabel(p.month)}
                            {now && <span style={{ ...badge, marginLeft: '0.4rem', ...statusStyle('posted') }}>saat disimpan</span>}
                          </td>
                          <td style={{ textAlign: 'right', padding: '0.45rem 0.75rem', whiteSpace: 'nowrap' }}>{rp(p.amount)}</td>
                          <td style={{ textAlign: 'right', padding: '0.45rem 0.75rem', whiteSpace: 'nowrap', color: '#666' }}>{rp(p.cumulative)}</td>
                          <td style={{ textAlign: 'right', padding: '0.45rem 0.75rem', whiteSpace: 'nowrap', fontWeight: 600 }}>{rp(p.remaining)}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>

        {/* Leases already paid: how far through its year each one is, what is
            still prepaid, and when the next year falls due. */}
        <div className="card">
          <div className="card-header" style={{ marginBottom: '0.9rem' }}>
            <h2 style={{ fontSize: '1rem', margin: 0 }}>Sewa Tahunan</h2>
            <span style={{ fontSize: '0.75rem', color: '#aaa' }}>
              {branchId ? branches.find(b => b.id === branchId)?.name : 'semua cabang'}
            </span>
          </div>

          {leases.length === 0 ? (
            <p style={{ color: '#bbb', fontSize: '0.88rem', padding: '0.5rem 0' }}>Belum ada sewa tahunan.</p>
          ) : leases.map(l => {
            const amount = Number(l.amount || 0);
            const pct = amount > 0 ? (Number(l.amortized) / amount) * 100 : 0;
            const open = expanded === l.id;
            const failed = l.schedule.some(s => s.last_error);
            return (
              <div key={l.id} style={{ borderTop: '1px solid #eef1f5', padding: '0.85rem 0' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.5rem', alignItems: 'baseline', flexWrap: 'wrap' }}>
                  <span style={{ fontWeight: 700, fontSize: '0.9rem' }}>
                    {l.branch_name}
                    <span style={{ fontFamily: 'monospace', fontWeight: 400, color: '#8a93a3', fontSize: '0.78rem', marginLeft: '0.4rem' }}>{l.number}</span>
                  </span>
                  <span style={{ fontWeight: 700, fontSize: '0.9rem', whiteSpace: 'nowrap' }}>{rp(amount)}<span style={{ color: '#aaa', fontWeight: 400 }}>/thn</span></span>
                </div>
                <div style={{ fontSize: '0.78rem', color: '#8a93a3', marginTop: '0.15rem' }}>
                  {dayLabel(l.coverage_start)} s/d {dayLabel(l.coverage_end)}
                </div>

                <div style={{ height: '7px', background: '#f1f3f7', borderRadius: '4px', marginTop: '0.55rem', overflow: 'hidden' }}>
                  <div style={{ width: `${pct}%`, height: '100%', background: kind.color, borderRadius: '4px' }} />
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.5rem', fontSize: '0.76rem', color: '#6b7380', marginTop: '0.3rem', flexWrap: 'wrap' }}>
                  <span>Dibebankan {rp(l.amortized)} ({l.months_posted}/{TERM} bln)</span>
                  <span>Sisa di muka <strong style={{ color: '#33384a' }}>{rp(l.remaining)}</strong></span>
                </div>

                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.5rem', marginTop: '0.5rem', flexWrap: 'wrap' }}>
                  <span style={{ fontSize: '0.78rem', color: '#555', display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}>
                    Jatuh tempo berikutnya {dayLabel(l.next_due_date)} <DueBadge lease={l} />
                  </span>
                  <button type="button" className="btn btn-secondary btn-sm" onClick={() => setExpanded(open ? null : l.id)}>
                    {open ? 'Tutup jadwal' : 'Lihat jadwal'}
                  </button>
                </div>

                {failed && (
                  <div style={{ fontSize: '0.76rem', color: '#c0392b', marginTop: '0.4rem' }}>
                    Ada bulan yang gagal dibukukan — lihat jadwal.
                  </div>
                )}

                {open && (
                  <table style={{ fontSize: '0.8rem', marginTop: '0.6rem' }}>
                    <thead>
                      <tr><th>Bulan</th><th style={{ textAlign: 'right' }}>Beban</th><th>Status</th></tr>
                    </thead>
                    <tbody>
                      {l.schedule.map(s => (
                        <tr key={s.seq}>
                          <td style={{ padding: '0.35rem 0.75rem', whiteSpace: 'nowrap' }}>{monthLabel(parseDay(s.period_month))}</td>
                          <td style={{ padding: '0.35rem 0.75rem', textAlign: 'right', whiteSpace: 'nowrap' }}>{rp(s.amount)}</td>
                          <td style={{ padding: '0.35rem 0.75rem' }}>
                            <span style={{ ...badge, ...statusStyle(s.status) }}>{STATUS[s.status]?.label || s.status}</span>
                            {s.last_error && <div style={{ color: '#c0392b', fontSize: '0.72rem', marginTop: '0.15rem' }}>{s.last_error}</div>}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </>
  );
}

const statusStyle = (s) => ({ color: STATUS[s]?.color || '#666', background: STATUS[s]?.bg || '#f1f3f7' });

const badge = {
  display: 'inline-block', fontSize: '0.7rem', fontWeight: 600, padding: '0.1rem 0.4rem',
  borderRadius: '4px', whiteSpace: 'nowrap',
};

const fieldStyle = {
  padding: '0.45rem 0.6rem', border: '1px solid #ddd',
  borderRadius: '6px', fontSize: '0.9rem', width: '100%',
};

function Field({ label, hint, children }) {
  return (
    <label>
      <div style={{ fontSize: '0.82rem', color: '#555', marginBottom: '0.3rem' }}>
        {label}{hint && <span style={{ color: '#999' }}> · {hint}</span>}
      </div>
      {children}
    </label>
  );
}
