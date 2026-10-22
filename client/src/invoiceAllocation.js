// Allocation arithmetic for a faktur distribusi line (see
// components/InvoiceDistribution.jsx). The last allocation row of a line is
// "auto" until typed into: it stands for whatever is still unallocated.

const round6 = (x) => Math.round(x * 1e6) / 1e6;
export const ALLOC_TOLERANCE = 1e-4;

export const newAllocation = () => ({ dest: '', quantity: '', auto: true });

// Destination keys: `w:<warehouse>`, `b:<branch>`, `d:<branch>:<division>`.
export const destToPayload = (dest) => {
  const [kind, a, b] = (dest || '').split(':');
  if (kind === 'w') return { warehouse_id: a };
  if (kind === 'b') return { branch_id: a };
  if (kind === 'd') return { branch_id: a, division_id: b };
  return {};
};

export const allocationToDest = (al) => {
  if (al.warehouse_id) return `w:${al.warehouse_id}`;
  if (al.division_id) return `d:${al.branch_id}:${al.division_id}`;
  if (al.branch_id) return `b:${al.branch_id}`;
  return '';
};

/** The quantity allocation `ai` of `row` stands for — computed for the auto row. */
export function allocQty(row, ai) {
  const a = row.allocations[ai];
  if (!a.auto) return Number(a.quantity) || 0;
  const others = row.allocations.reduce((s, b, bi) => (bi === ai ? s : s + (Number(b.quantity) || 0)), 0);
  return Math.max(0, round6((Number(row.quantity) || 0) - others));
}

/** Line quantity minus everything allocated: >0 still to place, <0 over-allocated. */
export function unallocated(row) {
  const total = row.allocations.reduce((s, _, ai) => s + allocQty(row, ai), 0);
  return round6((Number(row.quantity) || 0) - total);
}
