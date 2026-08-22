// The four ways into recording a standing bill.
//
// Three of them name a category outright — listrik, air, sewa are what a branch
// pays every month without fail, and they are worth a button each. The fourth is
// everything else: internet, telepon, gas, kebersihan, keamanan, perbaikan and
// whatever a branch has added since. Splitting the common three out is not
// cosmetic — it means the form arrives already knowing what is being paid, and
// can spend its screen on the two things that vary (branch and month) plus the
// twelve-month history that shows which months are still missing.
//
// Shared between the list page (which draws the buttons) and the form (which
// reads the kind out of the URL), so the two cannot disagree about what
// "lainnya" excludes.
//
// Icons are flat single-colour SVG glyphs from the project's own `Icon`
// component — Lucide geometry, ISC-licensed, inlined. Deliberately not fetched
// from an icon CDN: `client/config.json` is loaded at runtime on a VPS with no
// guarantee of outbound access, and an icon that fails to load leaves a button
// with no label of its own.

export const OP_KINDS = [
  {
    key: 'listrik',
    label: 'Listrik',
    categoryName: 'listrik',
    hint: 'Tagihan listrik bulanan per cabang',
    icon: 'zap',
    color: '#b26a00',
    tint: '#fff4e0',
  },
  {
    key: 'air',
    label: 'Air',
    categoryName: 'air',
    hint: 'Tagihan air / PDAM bulanan per cabang',
    icon: 'droplet',
    color: '#1668a8',
    tint: '#e6f2fb',
  },
  {
    key: 'sewa',
    label: 'Sewa',
    categoryName: 'sewa',
    hint: 'Sewa tempat untuk periode tertentu',
    icon: 'house',
    color: '#6b3fa0',
    tint: '#f1ecfa',
  },
  {
    key: 'lainnya',
    label: 'Lainnya',
    // No categoryName: the form asks which one, from everything the branch has
    // that is not one of the three above.
    categoryName: null,
    hint: 'Internet, telepon, gas, kebersihan, keamanan, perbaikan, dan lainnya',
    icon: 'grid',
    color: '#4a5568',
    tint: '#eef1f5',
  },
];

// The category names that have their own button. Compared lower-cased because a
// category is free text once someone adds one by hand.
export const NAMED_OP_CATEGORIES = OP_KINDS
  .filter(k => k.categoryName)
  .map(k => k.categoryName);

export const kindOf = (key) =>
  OP_KINDS.find(k => k.key === key) || OP_KINDS[OP_KINDS.length - 1];
