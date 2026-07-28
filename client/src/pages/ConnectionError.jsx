import { useEffect, useRef, useState } from 'react';
import { checkHealth, API_BASE_URL } from '../api';
import { reportConnectionUp } from '../connection';

// Full-screen overlay shown whenever the backend is unreachable. Rendered above
// the app rather than replacing it, so whatever the user had open is still there
// once the connection comes back.

// Cause-specific copy — a DNS failure and a 502 need different things checked.
const KIND_COPY = {
  network: {
    title: 'Tidak dapat terhubung ke server',
    body: 'Aplikasi tidak menerima respons dari server. Periksa koneksi internet Anda, lalu coba lagi.',
  },
  timeout: {
    title: 'Server tidak merespons',
    body: 'Permintaan melebihi batas waktu. Koneksi mungkin sedang lambat atau server sedang sibuk.',
  },
  server: {
    title: 'Server sedang tidak tersedia',
    body: 'Server dapat dijangkau tetapi layanan di belakangnya sedang tidak berjalan. Biasanya ini berlangsung sementara saat pemeliharaan atau proses dimulai ulang.',
  },
  config: {
    title: 'Gagal memuat konfigurasi',
    body: 'Berkas konfigurasi aplikasi (config.json) tidak dapat dibaca, sehingga alamat server tidak diketahui. Aplikasi sementara memakai alamat bawaan.',
  },
};

// Retry backoff in seconds. Grows so a long outage doesn't hammer a server that
// is already struggling, then holds at 30s.
const BACKOFF = [5, 10, 15, 30];

const fmtTime = (d) =>
  d ? new Date(d).toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : null;

export default function ConnectionError({ kind, detail, since }) {
  const copy = KIND_COPY[kind] || KIND_COPY.network;

  const [attempt, setAttempt]   = useState(0);
  const [checking, setChecking] = useState(false);
  const [countdown, setCountdown] = useState(BACKOFF[0]);
  const [offline, setOffline]   = useState(!navigator.onLine);
  // Guards against overlapping probes (countdown firing while the manual button
  // is still in flight). A ref, not state, so the check is synchronous.
  const busyRef = useRef(false);

  const retry = async () => {
    if (busyRef.current) return;
    busyRef.current = true;
    setChecking(true);
    try {
      await checkHealth();
      reportConnectionUp();
      // Nothing else to do — App unmounts this overlay once state flips.
    } catch {
      const next = attempt + 1;
      setAttempt(next);
      setCountdown(BACKOFF[Math.min(next, BACKOFF.length - 1)]);
    } finally {
      setChecking(false);
      busyRef.current = false;
    }
  };

  // Auto-retry on a countdown, so an unattended screen recovers on its own.
  // The tick is a self-rescheduling timeout rather than a side effect inside a
  // state updater — updaters must stay pure (StrictMode invokes them twice, which
  // would fire two probes per tick).
  useEffect(() => {
    if (checking) return undefined;
    // Both branches go through a timer so no setState runs synchronously during
    // effect setup.
    const id = countdown <= 0
      ? setTimeout(retry, 0)
      : setTimeout(() => setCountdown(c => c - 1), 1000);
    return () => clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [countdown, checking]);

  // The browser knows about local connectivity before any request times out.
  useEffect(() => {
    const goOffline = () => setOffline(true);
    const goOnline  = () => { setOffline(false); retry(); };
    window.addEventListener('offline', goOffline);
    window.addEventListener('online', goOnline);
    return () => {
      window.removeEventListener('offline', goOffline);
      window.removeEventListener('online', goOnline);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="conn-title"
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 9999,
        background: 'rgba(20, 24, 35, 0.55)',
        backdropFilter: 'blur(3px)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '1rem',
        overflowY: 'auto',
      }}
    >
      <div
        style={{
          background: '#fff',
          borderRadius: 12,
          boxShadow: '0 10px 40px rgba(0,0,0,0.25)',
          maxWidth: 520,
          width: '100%',
          padding: '2rem',
          textAlign: 'center',
        }}
      >
        <div
          aria-hidden="true"
          style={{
            width: 64, height: 64, borderRadius: '50%',
            background: '#fdece9', color: '#c5221f',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: '1.9rem', margin: '0 auto 1rem',
          }}
        >
          ⚠
        </div>

        <h1 id="conn-title" style={{ fontSize: '1.35rem', margin: '0 0 0.6rem', color: '#1a1a2e' }}>
          {copy.title}
        </h1>

        <p style={{ margin: '0 0 1rem', color: '#5a6070', fontSize: '0.92rem', lineHeight: 1.55 }}>
          {copy.body}
        </p>

        {offline && (
          <div style={{
            background: '#fff4e5', color: '#c05621', borderRadius: 8,
            padding: '0.6rem 0.8rem', fontSize: '0.85rem', marginBottom: '1rem',
          }}>
            Perangkat ini sedang offline. Sambungkan kembali ke jaringan.
          </div>
        )}

        <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'center', flexWrap: 'wrap', marginBottom: '1rem' }}>
          <button className="btn btn-primary" onClick={retry} disabled={checking}>
            {checking ? 'Menghubungkan…' : 'Coba Lagi'}
          </button>
          <button className="btn btn-secondary" onClick={() => window.location.reload()}>
            Muat Ulang Halaman
          </button>
        </div>

        <div style={{ fontSize: '0.82rem', color: '#8a93a6', marginBottom: '1rem' }}>
          {checking
            ? 'Memeriksa koneksi…'
            : <>Mencoba otomatis dalam {countdown} detik{attempt > 0 && ` · ${attempt} percobaan gagal`}</>}
        </div>

        <details style={{ textAlign: 'left', borderTop: '1px solid #eef1f6', paddingTop: '0.75rem' }}>
          <summary style={{ cursor: 'pointer', fontSize: '0.82rem', color: '#8a93a6' }}>Detail teknis</summary>
          <dl style={{ margin: '0.6rem 0 0', fontSize: '0.8rem', color: '#5a6070', lineHeight: 1.6 }}>
            <div><strong>Alamat server:</strong> <code style={{ wordBreak: 'break-all' }}>{API_BASE_URL}</code></div>
            {detail && <div><strong>Penyebab:</strong> {detail}</div>}
            {since && <div><strong>Terputus sejak:</strong> {fmtTime(since)}</div>}
          </dl>
          <p style={{ fontSize: '0.78rem', color: '#8a93a6', marginTop: '0.6rem', marginBottom: 0 }}>
            Jika masalah berlanjut, hubungi administrator dan sertakan detail di atas.
          </p>
        </details>
      </div>
    </div>
  );
}
