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
import StockMovementReport from './pages/StockMovementReport';
import Recipes from './pages/Recipes';
import Productions from './pages/Productions';
import Enumerations from './pages/Enumerations';
import SalesImport from './pages/SalesImport';
import FinancialReport from './pages/FinancialReport';
import FinancialStatement from './pages/FinancialStatement';
import ProfitLossComparison from './pages/ProfitLossComparison';
import AccountAdjustments from './pages/AccountAdjustments';
import OperationalExpenses from './pages/OperationalExpenses';
import OperationalExpenseForm from './pages/OperationalExpenseForm';
import OperationalExpenseReport from './pages/OperationalExpenseReport';
import AccountLedger from './pages/AccountLedger';
import Templates from './pages/Templates';
import DailyReport from './pages/DailyReport';
import VendorHistory from './pages/VendorHistory';
import Profile from './pages/Profile';
import Employees from './pages/hr/Employees';
import EmployeeForm from './pages/hr/EmployeeForm';
import EmployeeDetail from './pages/hr/EmployeeDetail';
import Positions from './pages/hr/Positions';
import PositionWageTemplates from './pages/hr/PositionWageTemplates';
import WageComponents from './pages/hr/WageComponents';
import HRImport from './pages/hr/HRImport';
import AttendanceDashboard from './pages/hr/AttendanceDashboard';
import FingerprintImport from './pages/hr/FingerprintImport';
import AttendanceSettings from './pages/hr/AttendanceSettings';
import AttendanceCorrections from './pages/hr/AttendanceCorrections';
import FaceDashboard from './pages/hr/FaceDashboard';
import FaceUnregistered from './pages/hr/FaceUnregistered';
import PerformanceDashboard from './pages/hr/PerformanceDashboard';
import EvaluationAdjustments from './pages/hr/EvaluationAdjustments';
import PerformancePolicies from './pages/hr/PerformancePolicies';
import Requests from './pages/hr/Requests';
import KasbonDashboard from './pages/hr/KasbonDashboard';
import KasbonForm from './pages/hr/KasbonForm';
import KasbonDetail from './pages/hr/KasbonDetail';
import PayrollDashboard from './pages/hr/PayrollDashboard';
import PayrollPeriodDetail from './pages/hr/PayrollPeriodDetail';
import AttendanceCutSimulator from './pages/hr/AttendanceCutSimulator';
import ThrDashboard from './pages/hr/ThrDashboard';
import BenefitFund from './pages/hr/BenefitFund';
import ThrRunDetail from './pages/hr/ThrRunDetail';
import Approvals from './pages/hr/Approvals';
import HRSettings from './pages/hr/HRSettings';
import ManpowerPlanning from './pages/hr/ManpowerPlanning';
import DocumentGenerator from './pages/hr/DocumentGenerator';
import OnboardingWizard from './pages/hr/OnboardingWizard';
import HRDashboard from './pages/hr/HRDashboard';
import BranchOperatorDashboard from './pages/hr/BranchOperatorDashboard';
import { BRANCH_OPERATOR_TILES } from './pages/hr/branchOperator';
import StaffDashboard from './pages/StaffDashboard';
import StaffKPI from './pages/hr/StaffKPI';
import NotificationBell from './components/NotificationBell';
import { getUser, canUseHR, canViewReports, canUseCore, isHROnly, isStaffDesk, isSuperuser, isBranchOperator, canUseBranchHR, operatorBranch } from './roles';
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

// The HR screens a branch operator shares with the HR desk.
function RequireBranchHR({ children }) {
  return canUseBranchHR() ? children : <Navigate to="/" replace />;
}

// Everywhere a branch operator may go. Several HR routes carry no guard at all
// (they are open to every HR-capable role), so rather than guarding each one
// against this role, it is confined here once — the same closed-by-default
// shape as middleware.RestrictBranchOperator on the server.
const BRANCH_OPERATOR_PATHS = [
  // Not /profile: it saves through PUT /api/users/:id, which this role cannot call.
  /^\/$/,
  /^\/hr\/attendance$/, /^\/hr\/attendance\/corrections$/,
  /^\/hr\/(requests|leave|overtime)$/,
  /^\/hr\/kasbon(\/.*)?$/,
];

function ConfineBranchOperator({ children }) {
  const { pathname } = useLocation();
  if (isBranchOperator() && !BRANCH_OPERATOR_PATHS.some((re) => re.test(pathname))) {
    return <Navigate to="/" replace />;
  }
  return children;
}

// The landing page, picked per role. This must be a component, not an inline
// ternary in the <Route> element: App renders once (on /login, before anyone is
// signed in), so an inline choice would be frozen at "not logged in" and the
// navigate('/') after login would show the wrong dashboard until a reload.
function Home() {
  if (isHROnly()) return <Navigate to="/hr" replace />;
  if (isBranchOperator()) return <BranchOperatorDashboard />;
  if (isStaffDesk()) return <StaffDashboard />;
  return <Dashboard />;
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
// [path, label], 'divider' inserts a rule, { heading } labels a stretch of a
// long menu, and { section, links } is a collapsible section inside it.
const DIV = 'divider';

// A collapsible section inside a dropdown: { section, links }.
const sub = (label, links) => ({ section: label, links });

// The HR module, grouped by **function**: each section is one subsystem, so
// whoever is looking for "the attendance thing" or "the policy thing" opens one
// section and finds everything for it there. The HR-only role gets these
// sections as its whole navbar; everyone else gets them as collapsible
// sections inside a single HR dropdown.
const HR_GROUPS = [
  { label: 'Karyawan', links: [
    ['/hr/employees', 'Data Karyawan'],
    ['/hr/onboarding', 'Onboarding Karyawan'],
    ['/hr/documents', 'Dokumen HR'],
    ['/hr/manpower', 'Rencana Tenaga Kerja'],
    DIV,
    ['/hr/import', 'Impor Karyawan'],
  ] },
  { label: 'Absensi', links: [
    ['/hr/attendance', 'Dasbor Absensi'],
    ['/hr/attendance/corrections', 'Koreksi Kehadiran'],
    DIV,
    ['/hr/face', 'Wajah & Perangkat'],
    ['/hr/attendance/import', 'Impor Sidik Jari'],
  ] },
  // Leave, overtime and sick requests, and the queue that approves them.
  { label: 'Izin & Pengajuan', links: [
    ['/hr/requests', 'Pengajuan'],
    ['/hr/approvals', 'Persetujuan'],
  ] },
  { label: 'Gaji & Tunjangan', links: [
    ['/hr/payroll', 'Penggajian'],
    ['/hr/thr', 'THR'],
    ['/hr/benefit-fund', 'Dana Kesejahteraan'],
    ['/hr/kasbon', 'Kasbon'],
  ] },
  { label: 'Kinerja', links: [
    ['/hr/performance', 'Evaluasi'],
    ['/hr/performance/adjustments', 'Penyesuaian Evaluasi'],
    ['/hr/kpi', 'KPI & Tugas Harian'],
  ] },
  // The rules the other sections run on.
  { label: 'Kebijakan', links: [
    ['/hr/settings', 'Pengaturan HR'],
    ['/hr/positions', 'Jabatan'],
    ['/hr/wage-components', 'Komponen Gaji & Grade'],
    ['/hr/wage-templates', 'Template Gaji Jabatan'],
    ['/hr/attendance/settings', 'Pengaturan Absensi'],
    ['/hr/performance/policies', 'Kebijakan Kinerja'],
  ] },
];

// For the operational navbar HR is one dropdown among many, so each group
// becomes a collapsible section inside it rather than a twenty-item flat list.
const HR_COMBINED = HR_GROUPS.map((g) => sub(g.label, g.links));

function buildNav({ hrOnly, operator, hr, reports }) {
  if (hrOnly) return HR_GROUPS;
  // Four screens need no dropdowns: each is a top-level link.
  if (operator) return BRANCH_OPERATOR_TILES.map((t) => ({ label: t.label, to: t.to }));
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
      ['/reports/operational-expenses', 'Beban Operasional'],
      ['/reports/inventory-value', 'Nilai Inventaris'],
      DIV,
      ['/reports/price-changes', 'Perubahan Harga'],
      ['/reports/usage-trend', 'Perubahan Pemakaian'],
      ['/reports/stock-movement', 'Pergerakan Stok'],
    ] });
  }
  // "Administrasi" had become two different jobs wearing one label: defining
  // the things the system is made of (items, warehouses, accounts, users), which
  // you touch when something changes; and the back-office work you actually do
  // on a given day (recording a bill, correcting the ledger, reading the audit
  // trail). Splitting on that line is why Setting is master data and Admin is
  // the desk.
  groups.push({ label: 'Setting', links: [
    ['/items', 'Barang'],
    ['/warehouses', 'Gudang'],
    ['/vendors', 'Vendor'],
    ['/accounts', 'Akun'],
    DIV,
    ['/branches', 'Cabang & Divisi'],
    // One entry for all three kinds — pembelanjaan, invoice, pengiriman.
    ['/templates', 'Template'],
    DIV,
    ['/users', 'Pengguna'],
  ] });
  groups.push({ label: 'Admin', links: [
    ['/operational-expenses', 'Beban Operasional'],
    ['/account-adjustments', 'Jurnal Manual'],
    // The activity log rides with reports: same audience, same restriction.
    ...(reports ? [['/activity', 'Log Aktivitas']] : []),
  ] });
  return groups;
}

const isLink = (l) => Array.isArray(l);
const linkPaths = (links) => links.flatMap((l) => (isLink(l) ? [l[0]] : l.section ? linkPaths(l.links) : []));
const groupPaths = (g) => (g.to ? [g.to] : linkPaths(g.links));

// A section inside a dropdown or the mobile drawer. It starts open when it holds
// the current page, so reopening the menu lands you where you already are.
function NavSection({ label, paths, mobile, children }) {
  const { pathname } = useLocation();
  const [open, setOpen] = useState(() => paths.some((p) => pathname.startsWith(p)));
  return (
    <div className={mobile ? 'mobile-subsection' : 'nav-dropdown-section'}>
      <button
        type="button"
        className={mobile ? 'mobile-subsection-btn' : 'nav-dropdown-section-btn'}
        // The dropdown closes on any click inside it; a section toggle is not a choice.
        onClick={(e) => { e.stopPropagation(); setOpen((o) => !o); }}
        aria-expanded={open}
      >
        {label} <span className="caret" aria-hidden="true">{open ? '▲' : '▼'}</span>
      </button>
      {open && <div className={mobile ? 'mobile-subsection-links' : 'nav-dropdown-section-links'}>{children}</div>}
    </div>
  );
}

function Nav() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const user = getUser();
  const hrOnly = isHROnly(user?.role);
  const operator = isBranchOperator(user?.role);
  const homeLabel = hrOnly ? 'Dasbor HR' : operator ? 'Dasbor Cabang' : 'Dasbor';
  // The dark red bar is the standing reminder that this session can do
  // anything, approvals included — worth noticing before you act, not after.
  const superuser = isSuperuser(user?.role);
  const groups = buildNav({
    hrOnly,
    operator,
    hr: canUseHR(user?.role),
    reports: canViewReports(user?.role),
  });
  const [drawerOpen, setDrawerOpen] = useState(false);
  const homePath = hrOnly ? '/hr' : '/';
  // The home link matches exactly — '/hr' is also the prefix of every HR screen.
  const isActive = (to) => to === homePath ? pathname === homePath : pathname.startsWith(to);

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

  // The entries of one dropdown (or drawer section), sections included.
  // Dividers are dropped on mobile, where every row is already full-width.
  const menuItems = (group, links, mobile) => links.map((l, i) => {
    if (l === DIV) return mobile ? null : <div key={`div-${i}`} className="nav-dropdown-divider" />;
    if (l.heading) return <div key={`h-${i}`} className={mobile ? 'mobile-section-heading' : 'nav-dropdown-heading'}>{l.heading}</div>;
    if (l.section) {
      return (
        <NavSection key={`s-${l.section}`} label={l.section} paths={linkPaths(l.links)} mobile={mobile}>
          {menuItems(group, l.links, mobile)}
        </NavSection>
      );
    }
    return <Link key={l[0]} to={l[0]} className={activeIn(group, l[0]) ? 'active' : ''}>{l[1]}</Link>;
  });

  const logout = async () => {
    try {
      const token = localStorage.getItem('token');
      if (token) {
        await fetch('http://localhost:5000/api/auth/logout', {
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
      <Link to={homePath} className="brand">
        <span className="brand-mark" aria-hidden="true">IP</span>
        <span className="brand-word">Inventory<b>Pro</b></span>
      </Link>

      {/* Desktop nav */}
      <div className="nav-links nav-links-desktop">
        {link(homePath, homeLabel)}
        {groups.map((g) => (g.to ? (
          <Link key={g.label} to={g.to} className={isActive(g.to) ? 'active' : ''}
            aria-current={isActive(g.to) ? 'page' : undefined}>{g.label}</Link>
        ) : (
          <NavDropdown key={g.label} label={g.label} paths={groupPaths(g)}>
            {menuItems(g, g.links, false)}
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
            {operator && operatorBranch()?.name && <span style={{ opacity: 0.7 }}>· {operatorBranch().name}</span>}
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
          <Link to={homePath} className={`mobile-link${isActive(homePath) ? ' active' : ''}`}>{homeLabel}</Link>

          {groups.map((g) => (g.to ? (
            <Link key={g.label} to={g.to} className={`mobile-link${isActive(g.to) ? ' active' : ''}`}>{g.label}</Link>
          ) : (
            <MobileSection key={g.label} label={g.label} paths={groupPaths(g)}>
              {menuItems(g, g.links, true)}
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
      <main className="main-content"><ConfineBranchOperator>{children}</ConfineBranchOperator></main>
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
                <Route path="/" element={<Home />} />
                <Route path="/hr" element={<RequireHR><HRDashboard /></RequireHR>} />
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
                <Route path="/reports/operational-expenses" element={<RequireReports><OperationalExpenseReport /></RequireReports>} />
                <Route path="/reports/inventory-value" element={<RequireReports><InventoryValueReport /></RequireReports>} />
                <Route path="/reports/price-changes" element={<RequireReports><PriceChangeReport /></RequireReports>} />
                <Route path="/reports/usage-trend" element={<RequireReports><UsageTrendReport /></RequireReports>} />
                <Route path="/reports/stock-movement" element={<RequireReports><StockMovementReport /></RequireReports>} />
                <Route path="/reports/financial" element={<RequireReports><FinancialReport /></RequireReports>} />
                <Route path="/reports/statement" element={<RequireReports><FinancialStatement /></RequireReports>} />
                <Route path="/reports/profit-loss" element={<RequireReports><ProfitLossComparison /></RequireReports>} />
                <Route path="/reports/daily" element={<RequireReports><DailyReport /></RequireReports>} />
                <Route path="/account-adjustments" element={<RequireCore><AccountAdjustments /></RequireCore>} />
                <Route path="/operational-expenses" element={<RequireCore><OperationalExpenses /></RequireCore>} />
                <Route path="/operational-expenses/new" element={<RequireCore><OperationalExpenseForm /></RequireCore>} />
                {/* The postings behind one account. Registered after /accounts so the
                    list keeps its own path. */}
                <Route path="/accounts/:id" element={<RequireCore><AccountLedger /></RequireCore>} />
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
                <Route path="/hr/wage-templates" element={<RequireHR><PositionWageTemplates /></RequireHR>} />
                <Route path="/hr/wage-components" element={<RequireHR><WageComponents /></RequireHR>} />
                <Route path="/hr/attendance" element={<AttendanceDashboard />} />
                <Route path="/hr/attendance/corrections" element={<RequireBranchHR><AttendanceCorrections /></RequireBranchHR>} />
                <Route path="/hr/attendance/import" element={<RequireHR><FingerprintImport /></RequireHR>} />
                <Route path="/hr/attendance/settings" element={<RequireHR><AttendanceSettings /></RequireHR>} />
                <Route path="/hr/face" element={<RequireHR><FaceDashboard /></RequireHR>} />
                <Route path="/hr/face/unregistered" element={<RequireHR><FaceUnregistered /></RequireHR>} />
                <Route path="/hr/performance" element={<RequireHR><PerformanceDashboard /></RequireHR>} />
                <Route path="/hr/performance/adjustments" element={<RequireHR><EvaluationAdjustments /></RequireHR>} />
                <Route path="/hr/performance/policies" element={<RequireHR><PerformancePolicies /></RequireHR>} />
                <Route path="/hr/requests" element={<RequireBranchHR><Requests /></RequireBranchHR>} />
                <Route path="/hr/approvals" element={<RequireHR><Approvals /></RequireHR>} />
                {/* back-compat: old leave/overtime links land on the merged requests screen */}
                <Route path="/hr/leave" element={<RequireBranchHR><Requests /></RequireBranchHR>} />
                <Route path="/hr/manpower" element={<ManpowerPlanning />} />
                <Route path="/hr/kasbon" element={<KasbonDashboard />} />
                <Route path="/hr/kasbon/new" element={<RequireBranchHR><KasbonForm /></RequireBranchHR>} />
                <Route path="/hr/kasbon/:id" element={<KasbonDetail />} />
                <Route path="/hr/overtime" element={<RequireBranchHR><Requests /></RequireBranchHR>} />
                <Route path="/hr/payroll" element={<RequireHR><PayrollDashboard /></RequireHR>} />
                <Route path="/hr/payroll/attendance-cut" element={<RequireHR><AttendanceCutSimulator /></RequireHR>} />
                <Route path="/hr/payroll/:id" element={<RequireHR><PayrollPeriodDetail /></RequireHR>} />
                <Route path="/hr/thr" element={<RequireHR><ThrDashboard /></RequireHR>} />
                <Route path="/hr/benefit-fund" element={<RequireHR><BenefitFund /></RequireHR>} />
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
