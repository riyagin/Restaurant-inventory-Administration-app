// Shared number and currency formatters (id-ID).
//
// Dates are not here: instants go through time.js, which renders them on the
// WIB wall clock regardless of the browser's zone.

const idrFormatter = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 });

// "Rp 12.500" via Intl (non-breaking space after Rp). Blank or null renders as Rp 0.
export const fmtIDR = (n) => idrFormatter.format(Number(n || 0));

// "Rp 12.500" with a plain space — the compact form some tables and slips use.
export const rp = (n) => 'Rp ' + Number(n || 0).toLocaleString('id-ID');

// Plain quantity, up to three decimals: "1.234,5".
export const num = (v) => Number(v ?? 0).toLocaleString('id-ID', { maximumFractionDigits: 3 });
