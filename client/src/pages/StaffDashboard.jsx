import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { getDailyTasks } from '../api';
import { getUser } from '../roles';

// Dashboard for the back-office desk: the three things they start work in, and
// an honest account of what the desk still owes.
//
// The task board is not a checklist — completion is derived from the data
// (a purchase invoice dated D, a POS import that lands on branch B), so this
// screen reports reality rather than what someone remembered to tick.

const SHORTCUTS = [
  { to: '/invoices/new', icon: '🧾', label: 'Pembelian', desc: 'Catat invoice pembelian baru',
    secondary: [['/invoices', 'Semua invoice']] },
  { to: '/transfers', icon: '🔄', label: 'Transfer Gudang', desc: 'Pindahkan stok antar gudang',
    secondary: [['/stock-opname', 'Stok opname']] },
  { to: '/dispatch', icon: '🚚', label: 'Pengiriman ke Cabang', desc: 'Kirim barang ke cabang & divisi',
    secondary: [['/templates?tab=pengiriman', 'Template pengiriman']] },
];

const DAYS_SHOWN = 7;

const iso = (d) => d.toISOString().slice(0, 10);
const daysAgo = (n) => { const d = new Date(); d.setDate(d.getDate() - n); return iso(d); };

const fmtDay = (s) => {
  const d = new Date(`${s}T00:00:00`);
  return d.toLocaleDateString('id-ID', { weekday: 'short', day: 'numeric', month: 'short' });
};

const relativeDay = (s) => {
  const today = iso(new Date());
  if (s === today) return 'Hari ini';
  if (s === daysAgo(1)) return 'Kemarin';
  return fmtDay(s);
};

export default function StaffDashboard() {
  const user = getUser();
  const [tasks, setTasks] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    setLoading(true);
    getDailyTasks({ from: daysAgo(DAYS_SHOWN - 1), to: iso(new Date()) })
      .then((r) => { setTasks(Array.isArray(r.data) ? r.data : []); setError(''); })
      .catch(() => setError('Gagal memuat tugas harian.'))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  const pending = useMemo(() => tasks.filter((t) => !t.done), [tasks]);
  const overdue = useMemo(() => pending.filter((t) => t.overdue), [pending]);
  const dueToday = useMemo(() => pending.filter((t) => !t.overdue), [pending]);

  // Group the whole window by date so the section reads as a diary rather than
  // a flat list: what is outstanding, and on which day it was missed.
  const byDate = useMemo(() => {
    const map = new Map();
    for (const t of tasks) {
      if (!map.has(t.date)) map.set(t.date, []);
      map.get(t.date).push(t);
    }
    return [...map.entries()];
  }, [tasks]);

  const doneCount = tasks.length - pending.length;

  return (
    <>
      <div className="page-header">
        <div>
          <h1>Dasbor</h1>
          <p style={{ color: 'var(--ink-3)', margin: '0.2rem 0 0' }}>
            Selamat datang{user?.username ? `, ${user.username}` : ''}.
          </p>
        </div>
      </div>

      <section style={{ marginBottom: '1.8rem' }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: '0.9rem' }}>
          {SHORTCUTS.map((s) => (
            <div key={s.to} className="sd-shortcut">
              <Link to={s.to} className="sd-shortcut-main">
                <span className="sd-shortcut-icon" aria-hidden="true">{s.icon}</span>
                <span>
                  <span className="sd-shortcut-label">{s.label}</span>
                  <span className="sd-shortcut-desc">{s.desc}</span>
                </span>
              </Link>
              <div className="sd-shortcut-links">
                {s.secondary.map(([to, label]) => (
                  <Link key={to} to={to}>{label}</Link>
                ))}
              </div>
            </div>
          ))}
        </div>
      </section>

      <section>
        <div className="card-header" style={{ marginBottom: '0.8rem' }}>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.6rem', flexWrap: 'wrap' }}>
            <h2 style={{ fontSize: '1.05rem', fontWeight: 600 }}>Tugas Tertunda</h2>
            <span style={{ fontSize: '0.82rem', color: 'var(--ink-3)' }}>
              {DAYS_SHOWN} hari terakhir · {doneCount} dari {tasks.length} selesai
            </span>
          </div>
          <button className="btn btn-secondary btn-sm" onClick={load} disabled={loading}>
            {loading ? 'Memuat…' : '↻ Segarkan'}
          </button>
        </div>

        {error && <div className="error-msg">{error}</div>}

        {!loading && !error && (
          <div style={{ display: 'flex', gap: '0.6rem', flexWrap: 'wrap', marginBottom: '1rem' }}>
            <TaskStat label="Terlambat" value={overdue.length} tone={overdue.length ? 'alert' : 'ok'} />
            <TaskStat label="Belum selesai hari ini" value={dueToday.length} tone={dueToday.length ? 'warn' : 'ok'} />
          </div>
        )}

        {loading && <p style={{ color: 'var(--ink-3)' }}>Memuat tugas…</p>}

        {!loading && !error && pending.length === 0 && tasks.length > 0 && (
          <div className="sd-clear">
            <div className="sd-clear-badge" aria-hidden="true">✓</div>
            <div>
              <strong>Semua tugas selesai.</strong>
              <div style={{ color: 'var(--ink-3)', fontSize: '0.88rem' }}>
                Tidak ada pembelian atau import POS yang tertinggal dalam {DAYS_SHOWN} hari terakhir.
              </div>
            </div>
          </div>
        )}

        {!loading && !error && tasks.length === 0 && (
          <p style={{ color: 'var(--ink-3)' }}>Belum ada tugas harian yang dikonfigurasi.</p>
        )}

        {!loading && byDate.map(([date, items]) => {
          const left = items.filter((t) => !t.done);
          if (left.length === 0) return null;
          return (
            <div key={date} className="sd-day">
              <div className="sd-day-head">
                <span className="sd-day-name">{relativeDay(date)}</span>
                <span className="sd-day-count">{left.length} tertunda · {items.length - left.length} selesai</span>
              </div>
              {left.map((t) => (
                <Link
                  key={`${t.definition_id}-${t.branch_id || ''}`}
                  to={t.link_path || '/'}
                  className={`sd-task${t.overdue ? ' overdue' : ''}`}
                >
                  <span className="sd-task-dot" aria-hidden="true" />
                  <span style={{ flex: 1, minWidth: 0 }}>
                    <span className="sd-task-name">
                      {t.name}{t.branch_name ? ` — ${t.branch_name}` : ''}
                    </span>
                    <span className="sd-task-meta">
                      {t.overdue ? 'Terlambat' : 'Belum dikerjakan'} · {fmtDay(t.date)}
                    </span>
                  </span>
                  <span className="sd-task-go" aria-hidden="true">→</span>
                </Link>
              ))}
            </div>
          );
        })}
      </section>
    </>
  );
}

function TaskStat({ label, value, tone }) {
  return (
    <div className={`sd-stat ${tone}`}>
      <span className="sd-stat-value">{value}</span>
      <span className="sd-stat-label">{label}</span>
    </div>
  );
}
