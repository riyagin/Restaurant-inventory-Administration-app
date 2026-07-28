// Global backend-reachability state.
//
// Lives outside React (a plain pub/sub) because the axios interceptor in api.js
// is the thing that detects the failure, and it has no component to report to.
// App.jsx subscribes and renders the ConnectionError page over the top of the
// app — deliberately an overlay rather than a route swap, so in-progress page
// state survives a blip and the user lands back where they were.

const state = {
  down: false,
  // Human-readable cause, shown in the "detail teknis" block.
  detail: '',
  // 'network'  — request never reached the server (offline, DNS, refused)
  // 'timeout'  — request sent, no reply in time
  // 'server'   — reached a proxy but the backend is down (502/503/504)
  // 'config'   — /config.json itself was unreachable at boot
  kind: 'network',
  since: null,
};

const listeners = new Set();

function emit() {
  const snapshot = { ...state };
  listeners.forEach(fn => fn(snapshot));
}

// Subscribes to changes only — it deliberately does NOT invoke fn with the
// current value. Callers seed their own state from getConnectionState(), which
// keeps subscribing free of a synchronous setState during effect setup.
export function subscribeConnection(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function getConnectionState() {
  return { ...state };
}

export function reportConnectionDown(kind, detail) {
  // Keep the first failure's timestamp — "terputus sejak" should report when the
  // outage began, not when the most recent retry failed.
  if (!state.down) state.since = new Date();
  state.down = true;
  state.kind = kind || 'network';
  state.detail = detail || '';
  emit();
}

export function reportConnectionUp() {
  if (!state.down) return;
  state.down = false;
  state.detail = '';
  state.since = null;
  emit();
}

// Classifies an axios error, or returns null when the failure is a normal HTTP
// error the calling page should handle itself (404, 422, 401, …).
export function classifyConnectionError(err) {
  // Request was cancelled by us — not a connectivity problem.
  if (err?.code === 'ERR_CANCELED' || err?.name === 'CanceledError') return null;

  if (!err?.response) {
    if (err?.code === 'ECONNABORTED') {
      return { kind: 'timeout', detail: err.message || 'Permintaan melebihi batas waktu' };
    }
    return { kind: 'network', detail: err?.message || 'Tidak ada respons dari server' };
  }

  // A gateway reached us but could not reach the API process behind it.
  const status = err.response.status;
  if (status === 502 || status === 503 || status === 504) {
    return { kind: 'server', detail: `Server membalas ${status}` };
  }

  return null;
}
