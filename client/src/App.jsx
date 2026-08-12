import { useEffect, useRef, useState } from 'react';
import { BrowserRouter, Routes, Route, Link, useLocation, Navigate, useNavigate } from 'react-router-dom';
import ConnectionError from './pages/ConnectionError';
import { subscribeConnection, getConnectionState } from './connection';
import Dashboard from './pages/Dashboard';
import Items from './pages/Items';
import ItemForm from './pages/ItemForm';
import Inventory from './pages/Inventory';
import InventoryForm from './pages/InventoryForm';
import Warehouses from './pages/Warehouses';
import Vendors from './pages/Vendors';
import Accounts from './pages/Accounts';
import Invoices from './pages/Invoices';
import InvoiceForm from './pages/InvoiceForm';
import Users from './pages/Users';
import Login from './pages/Login';
import ActivityLog from './pages/ActivityLog';
import StockTransfers from './pages/StockTransfers';
import Sales from './pages/Sales';
import Branches from './pages/Branches';
import DailyPurchases from './pages/DailyPurchases';
import DailyPurchaseForm from './pages/DailyPurchaseForm';
import PettyCash from './pages/PettyCash';
import CashTracking from './pages/CashTracking';
import Setoran from './pages/Setoran';
import Dispatch from './pages/Dispatch';
import StockHistoryPage from './pages/StockHistoryPage';
import StockOpname from './pages/StockOpname';
import ExpenseReport from './pages/ExpenseReport';
import InvoiceDetail from './pages/InvoiceDetail';
import TransferDetail from './pages/TransferDetail';
import DispatchDetail from './pages/DispatchDetail';
import StockOpnameDetail from './pages/StockOpnameDetail';
import NonStockItemDetail from './pages/NonStockItemDetail';
import StockItemDetail from './pages/StockItemDetail';
import InventoryValueReport from './pages/InventoryValueReport';
import PriceChangeReport from './pages/PriceChangeReport';
import UsageTrendReport from './pages/UsageTrendReport';
import Recipes from './pages/Recipes';
import Productions from './pages/Productions';
import Enumerations from './pages/Enumerations';
import SalesImport from './pages/SalesImport';
import FinancialReport from './pages/FinancialReport';
import FinancialStatement from './pages/FinancialStatement';
import ProfitLossComparison from './pages/ProfitLossComparison';
import AccountAdjustments from './pages/AccountAdjustments';
import Templates from './pages/Templates';
import DailyReport from './pages/DailyReport';
import VendorHistory from './pages/VendorHistory';
import Profile from './pages/Profile';
import Employees from './pages/hr/Employees';
import EmployeeForm from './pages/hr/EmployeeForm';
import EmployeeDetail from './pages/hr/EmployeeDetail';
import Positions from './pages/hr/Positions';
import WageComponents from './pages/hr/WageComponents';
import HRImport from './pages/hr/HRImport';
import AttendanceDashboard from './pages/hr/AttendanceDashboard';
import FingerprintImport from './pages/hr/FingerprintImport';
import AttendanceSettings from './pages/hr/AttendanceSettings';
import AttendanceCorrections from './pages/hr/AttendanceCorrections';
import FaceDashboard from './pages/hr/FaceDashboard';
import FaceUnregistered from './pages/hr/FaceUnregistered';
import PerformanceDashboard from './pages/hr/PerformanceDashboard';
import PerformancePolicies from './pages/hr/PerformancePolicies';
import Requests from './pages/hr/Requests';
import KasbonDashboard from './pages/hr/KasbonDashboard';
import KasbonForm from './pages/hr/KasbonForm';
import KasbonDetail from './pages/hr/KasbonDetail';
import PayrollDashboard from './pages/hr/PayrollDashboard';
import PayrollPeriodDetail from './pages/hr/PayrollPeriodDetail';
import ThrDashboard from './pages/hr/ThrDashboard';
import ThrRunDetail from './pages/hr/ThrRunDetail';
import Approvals from './pages/hr/Approvals';
import HRSettings from './pages/hr/HRSettings';
import ManpowerPlanning from './pages/hr/ManpowerPlanning';
import DocumentGenerator from './pages/hr/DocumentGenerator';
import OnboardingWizard from './pages/hr/OnboardingWizard';
import HRDashboard from './pages/hr/HRDashboard';
import StaffDashboard from './pages/StaffDashboard';
import StaffKPI from './pages/hr/StaffKPI';
import NotificationBell from './components/NotificationBell';
import { getUser, canUseHR, canViewReports, canUseCore, isHROnly, isStaffDesk, isSuperuser } from './roles';
import './App.css';

function RequireAuth({ children }) {
  const token = localStorage.getItem('token');
  if (!token) return <Navigate to="/login" replace />;
  return children;
}

// Route guards. Each mirrors a server-side middleware; the redirect just spares
// the user a screen full of 403s.
function RequireHR({ children }) {
  return canUseHR() ? children : <Navigate to="/" replace />;
}

function RequireReports({ children }) {
  return canViewReports() ? children : <Navigate to="/" replace />;
}

function RequireCore({ children }) {
  return canUseCore() ? children : <Navigate to="/" replace />;
}

function NavDropdown({ label, paths, children }) {
  const { pathname } = useLocation();
  const [open, setOpen] = useState(false);
  const ref = useRef(null);
  const isActive = paths.some(p => pathname.startsWith(p));

  useEffect(() => {
    const handler = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    const onKey = (e) => { if (e.key === 'Escape') setOpen(false); };
    document.addEventListener('mousedown', handler);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', handler);
      document.removeEventListener('keydown', onKey);
    };
  }, []);

  return (
    <div className="nav-dropdown" ref={ref}>
      <button
        className={`nav-dropdown-btn${isActive ? ' active' : ''}`}
        onClick={() => setOpen(o => !o)}
        aria-haspopup="true"
        aria-expanded={open}
      >
        {label} <span className="caret" aria-hidden="true">▼</span>
      </button>
      {open && (
        <div className="nav-dropdown-menu" onClick={() => setOpen(false)}>
          {children}
        </div>
      )}
    </div>
  );
}

// Mobile: collapsible section inside the burger drawer
function MobileSection({ label, paths, children }) {
  const { pathname } = useLocation();
  const [open, setOpen] = useState(paths.some(p => pathname.startsWith(p)));
  return (
    <div className="mobile-section">
      <button className="mobile-section-btn" onClick={() => setOpen(o => !o)} aria-expanded={open}>
        {label} <span className="caret" aria-hidden="true">{open ? '▲' : '▼'}</span>
      </button>
      {open && <div className="mobile-section-links">{children}</div>}
    </div>
  );
}

// ── Navigation model ────────────────────────────────────────────────────────
// One description of the menu, rendered twice (desktop dropdowns and the mobile
// drawer) so the two can't drift apart. A group is { label, links }, a link is
// [path, label], 'divider' inserts a rule, and { heading } labels a section
// inside a long menu.
const DIV = 'divider';
const head = (text) => ({ heading: text });

// The HR module, grouped by **how often you touch it** rather than by subsystem.
// It had grown to twenty-odd flat entries, which is unscannable; cadence is the
// axis that actually predicts what someone is looking for when they open the
// menu. The HR-only role gets these four as its whole navbar.
const HR_GROUPS = [
  { label: 'Karyawan', links: [
    ['/hr/employees', 'Karyawan'],
    ['/hr/onboarding', 'Onboarding Karyawan'],
    ['/hr/documents', 'Dokumen HR'],
    ['/hr/manpower', 'Rencana Tenaga Kerja'],
  ] },
  // Every working day.
  { label: 'Harian', links: [
    ['/hr/attendance', 'Absensi'],
    ['/hr/attendance/corrections', 'Koreksi Kehadiran'],
    DIV,
    ['/hr/requests', 'Pengajuan'],
    ['/hr/approvals', 'Persetujuan'],
    ['/hr/kasbon', 'Kasbon'],
  ] },
  // Once a month, or once a year.
  { label: 'Berkala', links: [
    ['/hr/payroll', 'Penggajian'],
    ['/hr/thr', 'THR'],
    DIV,
    ['/hr/performance', 'Evaluasi'],
    ['/hr/kpi', 'KPI & Tugas Harian'],
  ] },
  // Set up once, revisited rarely.
  { label: 'Pengaturan', links: [
    ['/hr/settings', 'Pengaturan HR'],
    ['/hr/positions', 'Jabatan'],
    ['/hr/wage-components', 'Komponen Gaji'],
    ['/hr/performance/policies', 'Kebijakan Kinerja'],
    ['/hr/attendance/settings', 'Pengaturan Absensi'],
    DIV,
    ['/hr/face', 'Wajah & Perangkat'],
    ['/hr/attendance/import', 'Impor Sidik Jari'],
    ['/hr/import', 'Impor Karyawan'],
  ] },
];

// For the operational navbar HR is one dropdown among many, so the same groups
// are flattened into it — but with their names kept as headings. A twenty-item
// list is only navigable if it tells you where you are in it.
const HR_COMBINED = HR_GROUPS.flatMap((g, i) => [
  ...(i === 0 ? [] : [DIV]),
  head(g.label),
  ...g.links.filter((l) => l !== DIV),
]);

function buildNav({ hrOnly, hr, reports }) {
  if (hrOnly) return HR_GROUPS;
  const groups = [
    { label: 'Inventaris', links: [
      ['/inventory', 'Inventaris'],
      ['/transfers', 'Transfer Gudang'],
      ['/dispatch', 'Pengiriman ke Cabang'],
      ['/stock-opname', 'Stok Opname'],
      ['/enumerations', 'Pencacahan'],
      DIV,
      ['/recipes', 'Resep Produksi'],
      ['/productions', 'Produksi'],
    ] },
    { label: 'Invoice', to: '/invoices' },
    // The cash side of the branch: what it spent from the box, what was counted
    // in the box, and what moved in or out of it. One dropdown because the three
    // are only meaningful together — a spend is checked against a count, and a
    // count only balances if the top-ups were recorded.
    { label: 'Kas Cabang', links: [
      ['/daily-purchases', 'Pembelanjaan Harian'],
      DIV,
      ['/petty-cash', 'Kas Kecil'],
      ['/cash-tracking', 'Pelacakan Kas'],
      ['/setoran', 'Setoran'],
    ] },
    { label: 'Penjualan', links: [
      ['/sales', 'Catatan Penjualan'],
      ['/sales/import', 'Import dari POS'],
    ] },
  ];
  if (hr) groups.push({ label: 'HR', links: HR_COMBINED });
  if (reports) {
    groups.push({ label: 'Laporan', links: [
      ['/reports/daily', 'Laporan Harian'],
      ['/reports/financial', 'Laporan Keuangan'],
      // The printable statement is reached from a button on Laporan Keuangan:
      // it is the same numbers in document form, not a separate report.
      ['/reports/profit-loss', 'Perbandingan Laba Rugi'],
      ['/expense-report', 'Laporan Pengeluaran'],
      ['/reports/inventory-value', 'Nilai Inventaris'],
      DIV,
      ['/reports/price-changes', 'Perubahan Harga'],
      ['/reports/usage-trend', 'Perubahan Pemakaian'],
    ] });
  }
  groups.push({ label: 'Administrasi', links: [
    ['/items', 'Barang'],
    ['/warehouses', 'Gudang'],
    ['/vendors', 'Vendor'],
    ['/accounts', 'Akun'],
    DIV,
    ['/branches', 'Cabang & Divisi'],
    // One entry for all three kinds — pembelanjaan, invoice, pengiriman.
    ['/templates', 'Template'],
    ['/account-adjustments', 'Jurnal Manual'],
    DIV,
    ['/users', 'Pengguna'],
    // The activity log rides with reports: same audience, same restriction.
    ...(reports ? [['/activity', 'Log Aktivitas']] : []),
  ] });
  return groups;
}

const isLink = (l) => Array.isArray(l);
const groupPaths = (g) => (g.to ? [g.to] : g.links.filter(isLink).map(([to]) => to));

function Nav() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const user = getUser();
  const hrOnly = isHROnly(user?.role);
  // The dark red bar is the standing reminder that this session can do
  // anything, approvals included — worth noticing before you act, not after.
  const superuser = isSuperuser(user?.role);
  const groups = buildNav({
    hrOnly,
    hr: canUseHR(user?.role),
    reports: canViewReports(user?.role),
  });
  const [drawerOpen, setDrawerOpen] = useState(false);
  const isActive = (to) => to === '/' ? pathname === '/' : pathname.startsWith(to);

  // Close drawer on navigation
  useEffect(() => { setDrawerOpen(false); }, [pathname]);

  // Close drawer on outside click
  const drawerRef = useRef(null);
  useEffect(() => {
    if (!drawerOpen) return;
    const handler = (e) => { if (drawerRef.current && !drawerRef.current.contains(e.target)) setDrawerOpen(false); };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [drawerOpen]);

  const link = (to, label) => (
    <Link to={to} className={isActive(to) ? 'active' : ''} aria-current={isActive(to) ? 'page' : undefined}>{label}</Link>
  );

  // Within a group, only the most specific matching entry lights up — otherwise
  // /sales/import would also mark /sales as the current page.
  const activeIn = (group, to) => {
    if (!pathname.startsWith(to)) return false;
    return !groupPaths(group).some((p) => p !== to && p.startsWith(to) && pathname.startsWith(p));
  };

  const logout = async () => {
    try {
      const token = localStorage.getItem('token');
      if (token) {
        await fetch('http://localhost:5002/api/auth/logout', {
          method: 'POST',
          headers: { Authorization: `Bearer ${token}` },
        });
      }
    } catch { /* best-effort */ }
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    navigate('/login');
  };

  return (
    <nav className={`navbar${superuser ? ' navbar-superuser' : ''}`} ref={drawerRef} aria-label="Navigasi utama">
      <Link to="/" className="brand">
        <span className="brand-mark" aria-hidden="true">IP</span>
        <span className="brand-word">Inventory<b>Pro</b></span>
      </Link>

      {/* Desktop nav */}
      <div className="nav-links nav-links-desktop">
        {link('/', hrOnly ? 'Dasbor HR' : 'Dasbor')}
        {groups.map((g) => (g.to ? (
          <Link key={g.label} to={g.to} className={isActive(g.to) ? 'active' : ''}
            aria-current={isActive(g.to) ? 'page' : undefined}>{g.label}</Link>
        ) : (
          <NavDropdown key={g.label} label={g.label} paths={groupPaths(g)}>
            {g.links.map((l, i) => {
              if (l === DIV) return <div key={`div-${i}`} className="nav-dropdown-divider" />;
              if (l.heading) return <div key={`h-${i}`} className="nav-dropdown-heading">{l.heading}</div>;
              return <Link key={l[0]} to={l[0]} className={activeIn(g, l[0]) ? 'active' : ''}>{l[1]}</Link>;
            })}
          </NavDropdown>
        )))}
      </div>

      {/* Bell sits immediately left of the user block on desktop, and left of
          the burger on mobile — same place either way, and it must not vanish
          on the screen size most likely to be checking for pending work. */}
      {user && <div className="nav-bell"><NotificationBell /></div>}

      {user && (
        <div className="nav-user nav-user-desktop">
          <Link to="/profile" className="nav-user-name" title={`Profil: ${user.username}`}>
            <span className="nav-avatar" aria-hidden="true">{user.username.slice(0, 1)}</span>
            {user.username}
          </Link>
          <button onClick={logout} className="btn-nav">Keluar</button>
        </div>
      )}

      {/* Burger button (mobile only) */}
      <button className="burger-btn" onClick={() => setDrawerOpen(o => !o)} aria-label="Menu">
        <span className={`burger-icon${drawerOpen ? ' open' : ''}`}>
          <span /><span /><span />
        </span>
      </button>

      {/* Mobile drawer */}
      {drawerOpen && (
        <div className="mobile-drawer">
          <Link to="/" className={`mobile-link${isActive('/') ? ' active' : ''}`}>{hrOnly ? 'Dasbor HR' : 'Dasbor'}</Link>

          {groups.map((g) => (g.to ? (
            <Link key={g.label} to={g.to} className={`mobile-link${isActive(g.to) ? ' active' : ''}`}>{g.label}</Link>
          ) : (
            <MobileSection key={g.label} label={g.label} paths={groupPaths(g)}>
              {g.links.filter((l) => l !== DIV).map((l, i) => (l.heading
                ? <div key={`h-${i}`} className="mobile-section-heading">{l.heading}</div>
                : <Link key={l[0]} to={l[0]} className={activeIn(g, l[0]) ? 'active' : ''}>{l[1]}</Link>
              ))}
            </MobileSection>
          )))}

          {user && (
            <div className="mobile-drawer-footer">
              <Link to="/profile" className="mobile-link">Profil: {user.username}</Link>
              <button onClick={logout} className="btn btn-danger btn-sm" style={{ width: '100%' }}>Keluar</button>
            </div>
          )}
        </div>
      )}
    </nav>
  );
}

function Layout({ children }) {
  return (
    <>
      <Nav />
      <main className="main-content">{children}</main>
    </>
  );
}

// Renders the connection overlay whenever the backend is unreachable. Sits
// outside <Routes> so it covers every screen, including /login.
function ConnectionWatcher() {
  const [conn, setConn] = useState(getConnectionState());
  useEffect(() => subscribeConnection(setConn), []);
  if (!conn.down) return null;
  return <ConnectionError kind={conn.kind} detail={conn.detail} since={conn.since} />;
}

export default function App() {
  return (
    <BrowserRouter>
      <ConnectionWatcher />
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/*" element={
          <RequireAuth>
            <Layout>
              <Routes>
                <Route path="/" element={isHROnly() ? <HRDashboard /> : isStaffDesk() ? <StaffDashboard /> : <Dashboard />} />
                <Route path="/items" element={<RequireCore><Items /></RequireCore>} />
                <Route path="/items/new" element={<RequireCore><ItemForm /></RequireCore>} />
                <Route path="/items/edit/:id" element={<RequireCore><ItemForm /></RequireCore>} />
                <Route path="/items/history/:id" element={<RequireCore><NonStockItemDetail /></RequireCore>} />
                <Route path="/items/stock/:id" element={<RequireCore><StockItemDetail /></RequireCore>} />
                <Route path="/inventory" element={<RequireCore><Inventory /></RequireCore>} />
                <Route path="/inventory/new" element={<RequireCore><InventoryForm /></RequireCore>} />
                <Route path="/inventory/edit/:id" element={<RequireCore><InventoryForm /></RequireCore>} />
                <Route path="/inventory/history/:itemId" element={<RequireCore><StockHistoryPage /></RequireCore>} />
                <Route path="/invoices" element={<RequireCore><Invoices /></RequireCore>} />
                <Route path="/invoices/new" element={<RequireCore><InvoiceForm /></RequireCore>} />
                <Route path="/invoices/edit/:id" element={<RequireCore><InvoiceForm /></RequireCore>} />
                <Route path="/invoices/view/:id" element={<RequireCore><InvoiceDetail /></RequireCore>} />
                <Route path="/daily-purchases" element={<RequireCore><DailyPurchases /></RequireCore>} />
                <Route path="/daily-purchases/new" element={<RequireCore><DailyPurchaseForm /></RequireCore>} />
                <Route path="/templates" element={<RequireCore><Templates /></RequireCore>} />
                {/* The three template kinds used to be three pages. The old
                    paths stay as redirects: they are linked from the pages that
                    consume the templates, and from bookmarks. */}
                <Route path="/daily-purchase-templates" element={<Navigate to="/templates?tab=pembelanjaan" replace />} />
                <Route path="/invoice-templates" element={<Navigate to="/templates?tab=invoice" replace />} />
                <Route path="/dispatch-templates" element={<Navigate to="/templates?tab=pengiriman" replace />} />
                <Route path="/petty-cash" element={<RequireCore><PettyCash /></RequireCore>} />
                <Route path="/cash-tracking" element={<RequireCore><CashTracking /></RequireCore>} />
                <Route path="/setoran" element={<RequireCore><Setoran /></RequireCore>} />
                <Route path="/sales" element={<RequireCore><Sales /></RequireCore>} />
                <Route path="/sales/import" element={<RequireCore><SalesImport /></RequireCore>} />
                <Route path="/expense-report" element={<RequireReports><ExpenseReport /></RequireReports>} />
                <Route path="/reports/inventory-value" element={<RequireReports><InventoryValueReport /></RequireReports>} />
                <Route path="/reports/price-changes" element={<RequireReports><PriceChangeReport /></RequireReports>} />
                <Route path="/reports/usage-trend" element={<RequireReports><UsageTrendReport /></RequireReports>} />
                <Route path="/reports/financial" element={<RequireReports><FinancialReport /></RequireReports>} />
                <Route path="/reports/statement" element={<RequireReports><FinancialStatement /></RequireReports>} />
                <Route path="/reports/profit-loss" element={<RequireReports><ProfitLossComparison /></RequireReports>} />
                <Route path="/reports/daily" element={<RequireReports><DailyReport /></RequireReports>} />
                <Route path="/account-adjustments" element={<RequireCore><AccountAdjustments /></RequireCore>} />
                <Route path="/transfers" element={<RequireCore><StockTransfers /></RequireCore>} />
                <Route path="/transfers/group/:id" element={<RequireCore><TransferDetail /></RequireCore>} />
                <Route path="/dispatch" element={<RequireCore><Dispatch /></RequireCore>} />
                <Route path="/dispatches/:id" element={<RequireCore><DispatchDetail /></RequireCore>} />
                <Route path="/stock-opname" element={<RequireCore><StockOpname /></RequireCore>} />
                <Route path="/stock-opname/detail/:id" element={<RequireCore><StockOpnameDetail /></RequireCore>} />
                <Route path="/recipes" element={<RequireCore><Recipes /></RequireCore>} />
                <Route path="/productions" element={<RequireCore><Productions /></RequireCore>} />
                <Route path="/enumerations" element={<RequireCore><Enumerations /></RequireCore>} />
                <Route path="/warehouses" element={<RequireCore><Warehouses /></RequireCore>} />
                <Route path="/vendors" element={<RequireCore><Vendors /></RequireCore>} />
                <Route path="/vendors/:id/history" element={<RequireCore><VendorHistory /></RequireCore>} />
                <Route path="/accounts" element={<RequireCore><Accounts /></RequireCore>} />
                <Route path="/branches" element={<RequireCore><Branches /></RequireCore>} />
                <Route path="/users" element={<RequireCore><Users /></RequireCore>} />
                <Route path="/activity" element={<RequireReports><ActivityLog /></RequireReports>} />
                <Route path="/profile" element={<Profile />} />
                <Route path="/hr/onboarding" element={<RequireHR><OnboardingWizard /></RequireHR>} />
                <Route path="/hr/documents" element={<RequireHR><DocumentGenerator /></RequireHR>} />
                <Route path="/hr/employees" element={<Employees />} />
                <Route path="/hr/employees/new" element={<RequireHR><EmployeeForm /></RequireHR>} />
                <Route path="/hr/employees/:id" element={<EmployeeDetail />} />
                <Route path="/hr/employees/:id/edit" element={<RequireHR><EmployeeForm /></RequireHR>} />
                <Route path="/hr/import" element={<RequireHR><HRImport /></RequireHR>} />
                <Route path="/hr/positions" element={<RequireHR><Positions /></RequireHR>} />
                <Route path="/hr/wage-components" element={<RequireHR><WageComponents /></RequireHR>} />
                <Route path="/hr/attendance" element={<AttendanceDashboard />} />
                <Route path="/hr/attendance/corrections" element={<RequireHR><AttendanceCorrections /></RequireHR>} />
                <Route path="/hr/attendance/import" element={<RequireHR><FingerprintImport /></RequireHR>} />
                <Route path="/hr/attendance/settings" element={<RequireHR><AttendanceSettings /></RequireHR>} />
                <Route path="/hr/face" element={<RequireHR><FaceDashboard /></RequireHR>} />
                <Route path="/hr/face/unregistered" element={<RequireHR><FaceUnregistered /></RequireHR>} />
                <Route path="/hr/performance" element={<RequireHR><PerformanceDashboard /></RequireHR>} />
                <Route path="/hr/performance/policies" element={<RequireHR><PerformancePolicies /></RequireHR>} />
                <Route path="/hr/requests" element={<RequireHR><Requests /></RequireHR>} />
                <Route path="/hr/approvals" element={<RequireHR><Approvals /></RequireHR>} />
                {/* back-compat: old leave/overtime links land on the merged requests screen */}
                <Route path="/hr/leave" element={<RequireHR><Requests /></RequireHR>} />
                <Route path="/hr/manpower" element={<ManpowerPlanning />} />
                <Route path="/hr/kasbon" element={<KasbonDashboard />} />
                <Route path="/hr/kasbon/new" element={<RequireHR><KasbonForm /></RequireHR>} />
                <Route path="/hr/kasbon/:id" element={<KasbonDetail />} />
                <Route path="/hr/overtime" element={<RequireHR><Requests /></RequireHR>} />
                <Route path="/hr/payroll" element={<RequireHR><PayrollDashboard /></RequireHR>} />
                <Route path="/hr/payroll/:id" element={<RequireHR><PayrollPeriodDetail /></RequireHR>} />
                <Route path="/hr/thr" element={<RequireHR><ThrDashboard /></RequireHR>} />
                <Route path="/hr/thr/:id" element={<RequireHR><ThrRunDetail /></RequireHR>} />
                <Route path="/hr/settings" element={<RequireHR><HRSettings /></RequireHR>} />
                <Route path="/hr/kpi" element={<RequireHR><StaffKPI /></RequireHR>} />
              </Routes>
            </Layout>
          </RequireAuth>
        } />
      </Routes>
    </BrowserRouter>
  );
}
