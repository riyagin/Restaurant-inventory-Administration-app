import { useCallback, useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { getNotifications } from '../api';

// The navbar bell: what this user still owes, assembled by role on the server.
//
// Every entry carries a link and navigating closes the panel — a notification
// you cannot act on from where you are is just anxiety. The badge counts alerts
// and warnings only; `info` entries are context, not a demand.

const POLL_MS = 120000; // two minutes: these are day-scale duties, not chat

const SEVERITY_ORDER = { alert: 0, warn: 1, info: 2 };
const SEVERITY_LABEL = { alert: 'Terlambat', warn: 'Perlu tindakan', info: 'Info' };

export default function NotificationBell() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const [items, setItems] = useState([]);
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const ref = useRef(null);

  const load = useCallback(() => {
    getNotifications()
      .then((r) => setItems(Array.isArray(r.data) ? r.data : []))
      .catch(() => { /* the bell is never worth an error screen */ })
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, POLL_MS);
    return () => clearInterval(t);
  }, [load]);

  // Refresh on navigation: recording the purchase you were nagged about should
  // clear the nag by the time you land back on a page.
  useEffect(() => { load(); }, [pathname, load]);

  useEffect(() => { setOpen(false); }, [pathname]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    const onKey = (e) => { if (e.key === 'Escape') setOpen(false); };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const sorted = [...items].sort(
    (a, b) => (SEVERITY_ORDER[a.severity] ?? 3) - (SEVERITY_ORDER[b.severity] ?? 3),
  );
  const badge = items.filter((i) => i.severity === 'alert' || i.severity === 'warn').length;
  const alerts = items.filter((i) => i.severity === 'alert').length;

  const go = (item) => {
    setOpen(false);
    if (item.link) navigate(item.link);
  };

  const title = loading
    ? 'Memuat notifikasi'
    : badge === 0 ? 'Tidak ada tugas tertunda' : `${badge} tugas perlu tindakan`;

  return (
    <div className="notif" ref={ref}>
      <button
        type="button"
        className={`notif-btn${open ? ' open' : ''}`}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={title}
        title={title}
      >
        <span aria-hidden="true">🔔</span>
        {badge > 0 && (
          <span className={`notif-badge${alerts > 0 ? ' alert' : ''}`}>{badge > 99 ? '99+' : badge}</span>
        )}
      </button>

      {open && (
        <div className="notif-panel" role="dialog" aria-label="Tugas tertunda">
          <div className="notif-head">
            <strong>Tugas Tertunda</strong>
            {badge > 0 && <span className="notif-head-count">{badge}</span>}
          </div>

          <div className="notif-list">
            {loading && <div className="notif-empty">Memuat…</div>}
            {!loading && sorted.length === 0 && (
              <div className="notif-empty">
                <div style={{ fontSize: '1.6rem' }} aria-hidden="true">✓</div>
                Tidak ada tugas tertunda.
              </div>
            )}
            {sorted.map((n) => (
              <button key={n.id} type="button" className={`notif-item ${n.severity}`} onClick={() => go(n)}>
                <span className="notif-item-sev">{SEVERITY_LABEL[n.severity] || n.severity}</span>
                <span className="notif-item-title">{n.title}</span>
                <span className="notif-item-detail">{n.detail}</span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
