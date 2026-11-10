// Business time zone helpers.
//
// The server stores instants (timestamptz) and sends them as ISO strings. What a
// person reads must be the WIB wall clock of the branch — not the zone of the
// browser that happens to open the page (a laptop on UTC, a phone abroad) — so
// every instant is rendered through these helpers, and every "today" / "this
// month" is the WIB calendar day.
//
// Calendar dates (YYYY-MM-DD strings from `date` columns) have no time zone and
// are handled with ymd()/ym(), which only do calendar arithmetic.

export const TZ = 'Asia/Jakarta';

const fmtCache = new Map();
function formatter(locale, opts) {
  const key = locale + JSON.stringify(opts);
  let f = fmtCache.get(key);
  if (!f) {
    f = new Intl.DateTimeFormat(locale, { timeZone: TZ, ...opts });
    fmtCache.set(key, f);
  }
  return f;
}

const toDate = (v) => (v instanceof Date ? v : new Date(v));
const valid = (v) => v !== null && v !== undefined && v !== '' && !Number.isNaN(toDate(v).getTime());

// "08.11" — WIB time of an instant.
export const fmtTime = (ts, { seconds = false } = {}) =>
  valid(ts) ? formatter('id-ID', { hour: '2-digit', minute: '2-digit', ...(seconds ? { second: '2-digit' } : {}), hourCycle: 'h23' }).format(toDate(ts)) : null;

// "5 Okt 2026 08.11" — WIB date + time of an instant. Pass Intl options to change the shape.
export const fmtDateTime = (ts, opts = { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' }) =>
  valid(ts) ? formatter('id-ID', { hourCycle: 'h23', ...opts }).format(toDate(ts)) : null;

// "5 Okt 2026" — WIB calendar date of an instant.
export const fmtInstantDate = (ts, opts = { day: 'numeric', month: 'short', year: 'numeric' }) =>
  valid(ts) ? formatter('id-ID', opts).format(toDate(ts)) : null;

// "2026-10-05" — WIB calendar date of an instant (default: now).
export const wibDate = (ts = new Date()) => {
  if (!valid(ts)) return '';
  const p = Object.fromEntries(formatter('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit' })
    .formatToParts(toDate(ts)).map((x) => [x.type, x.value]));
  return `${p.year}-${p.month}-${p.day}`;
};

export const todayWIB = () => wibDate(new Date());
export const monthWIB = () => todayWIB().slice(0, 7);

// "08:11" — WIB wall clock of an instant, for <input type="time">.
export const wibHHMM = (ts, fallback = '') => {
  if (!valid(ts)) return fallback;
  const p = Object.fromEntries(formatter('en-GB', { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
    .formatToParts(toDate(ts)).map((x) => [x.type, x.value]));
  return `${p.hour}:${p.minute}`;
};

// A WIB wall-clock entry (date + "HH:MM") as an RFC3339 instant for the API.
export const wibRFC3339 = (dateIso, hhmm) => `${dateIso}T${hhmm}:00+07:00`;

// Calendar arithmetic on local Date objects built with new Date(y, m, d):
// format without going through UTC (toISOString would shift a WIB midnight to
// the previous day).
export const ymd = (d) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
export const ym = (d) => ymd(d).slice(0, 7);

// "YYYY-MM-DD" n days / months away from today on the WIB calendar.
export const shiftTodayWIB = (days) => {
  const d = new Date(todayWIB() + 'T00:00:00');
  d.setDate(d.getDate() + days);
  return ymd(d);
};
export const shiftMonthsWIB = (months) => {
  const d = new Date(todayWIB() + 'T00:00:00');
  d.setMonth(d.getMonth() + months);
  return ymd(d);
};
