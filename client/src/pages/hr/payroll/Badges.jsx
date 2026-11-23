

// SortableTh is a table header that toggles the table's sort on click.
export function SortableTh({ label, sortKey, sort, order, onSort, align = 'left' }) {
  const active = sort === sortKey;
  return (
    <th
      onClick={() => onSort(sortKey)}
      title={`Urutkan berdasarkan ${label}`}
      aria-sort={active ? (order === 'asc' ? 'ascending' : 'descending') : 'none'}
      style={{
        padding: 10, textAlign: align, cursor: 'pointer', userSelect: 'none',
        whiteSpace: 'nowrap', color: active ? '#1967d2' : undefined,
      }}>
      {label}
      <span style={{ marginLeft: 4, fontSize: 11, opacity: active ? 1 : 0.35 }}>
        {active ? (order === 'asc' ? '▲' : '▼') : '↕'}
      </span>
    </th>
  );
}

export function ScoreBadge({ score }) {
  if (score === null || score === undefined) return <span style={{ color: '#aab' }}>—</span>;
  const s = Number(score);
  let bg = '#e6f4ea', color = '#1e7e34';
  if (s < 60) { bg = '#fce8e6'; color = '#c5221f'; }
  else if (s < 80) { bg = '#fff8e1'; color = '#a06800'; }
  return <span style={{ background: bg, color, padding: '2px 8px', borderRadius: 10, fontSize: 12, fontWeight: 700 }}>{s}</span>;
}

// The grade the line was paid at (frozen at generation): which share of every
// grade-linked component it received.
export function GradeBadge({ grade, percent }) {
  if (!grade) return null;
  const pct = Number(percent);
  const full = pct >= 100;
  return (
    <span title={`Grade ${grade}: komponen yang ikut grade dibayar ${pct}%`}
      style={{ marginLeft: 4, background: full ? '#efe8fb' : '#fff1e6', color: full ? '#5b3cc4' : '#b3541e', padding: '2px 6px', borderRadius: 10, fontSize: 11, fontWeight: 700, whiteSpace: 'nowrap' }}>
      {grade} · {pct}%
    </span>
  );
}

// Ledger posting status for a closed period. The posting itself is automatic —
// queued inside the close transaction and written by a background worker — so this
// only reports what happened. The retry button appears solely for the failed case,
// where someone has fixed the cause (a missing account) and does not want to wait
// for the next retry sweep.
export function AccountingStatus({ posting, busy, onRetry }) {
  const base = { borderRadius: 8, padding: '10px 16px', fontWeight: 600, fontSize: 14, display: 'inline-flex', alignItems: 'center', gap: 8 };

  if (posting.status === 'posted') {
    return (
      <span title="Gaji sudah tercatat di buku besar" style={{ ...base, background: '#e6f4ea', color: '#1e7e34', border: '1px solid #b7e1c4' }}>
        ✓ Tercatat di Akuntansi
      </span>
    );
  }

  if (posting.status === 'pending') {
    return (
      <span title="Pencatatan ke buku besar sedang diproses di latar belakang" style={{ ...base, background: '#fff8e1', color: '#8a6d00', border: '1px solid #f0e0a0' }}>
        ⏳ Mencatat ke Akuntansi…
      </span>
    );
  }

  return (
    <span title={posting.last_error || 'Pencatatan gagal'}
      style={{ ...base, background: '#fce8e6', color: '#c5221f', border: '1px solid #f3b9b4' }}>
      ⚠ Akuntansi Gagal ({posting.attempts}×)
      <button onClick={onRetry} disabled={busy}
        style={{ background: '#c5221f', color: '#fff', border: 0, borderRadius: 6, padding: '4px 10px', fontWeight: 600, cursor: busy ? 'not-allowed' : 'pointer', fontSize: 12 }}>
        {busy ? 'Mencoba…' : 'Coba Lagi'}
      </button>
    </span>
  );
}
