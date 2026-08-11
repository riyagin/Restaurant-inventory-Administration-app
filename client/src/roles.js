// Role model, mirroring server-go/internal/middleware/auth.go.
//
//   superuser       — every capability, approvals included.
//   admin / manager — full access; only manager may approve requests.
//   staff           — same reach as admin except reports and the activity log.
//   hr              — confined to the HR module, with its own nav and dashboard.
//   store_manager   — attendance entry only.
//
// These helpers are for shaping the UI. The server enforces the same rules; a
// hidden link is a courtesy, not a control.

export function getUser() {
  try { return JSON.parse(localStorage.getItem('user')); } catch { return null; }
}

export const getRole = () => getUser()?.role || '';

/**
 * The owner account: holds every capability, including approving requests,
 * which admin alone cannot do. Every capability check below short-circuits on
 * it, the same way middleware.hasRole does on the server — a role defined as
 * "all of them" must not be able to fall out of a check added later.
 */
export const isSuperuser = (role = getRole()) => role === 'superuser';

/** Everyone who works inside the HR module. */
export const canUseHR = (role = getRole()) =>
  isSuperuser(role) || role === 'admin' || role === 'manager' || role === 'staff' || role === 'hr';

/** Financial reports and the activity log. */
export const canViewReports = (role = getRole()) =>
  isSuperuser(role) || role === 'admin' || role === 'manager';

/** Approving kasbon, leave and overtime requests. */
export const canApprove = (role = getRole()) => isSuperuser(role) || role === 'manager';

/**
 * Administrative actions the pages gate on individually: deleting an invoice,
 * cancelling a daily purchase or a setoran, recording a petty cash count,
 * clearing the activity log.
 */
export const isAdminRole = (role = getRole()) => isSuperuser(role) || role === 'admin';

/** The HR-only role: HR screens are the whole application for them. */
export const isHROnly = (role = getRole()) => role === 'hr';

/** The operational (non-HR) side of the app: inventory, invoices, sales, admin. */
export const canUseCore = (role = getRole()) => role !== 'hr' && role !== 'store_manager';

/**
 * The back-office desk that owns the shared daily duties (purchasing, POS
 * import). Staff runs it; they get the task dashboard instead of the
 * charts-and-totals overview, which is a reporting surface they cannot open
 * anyway. Admin and manager keep the overview and see the same tasks in the bell.
 */
export const isStaffDesk = (role = getRole()) => role === 'staff';
