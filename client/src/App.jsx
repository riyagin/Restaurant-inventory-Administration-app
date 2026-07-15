import { useEffect, useRef, useState } from 'react';
import { BrowserRouter, Routes, Route, Link, useLocation, Navigate, useNavigate } from 'react-router-dom';
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
import Dispatch from './pages/Dispatch';
import StockHistoryPage from './pages/StockHistoryPage';
import StockOpname from './pages/StockOpname';
import ExpenseReport from './pages/ExpenseReport';
import InvoiceDetail from './pages/InvoiceDetail';
import TransferDetail from './pages/TransferDetail';
import DispatchDetail from './pages/DispatchDetail';
import StockOpnameDetail from './pages/StockOpnameDetail';
import NonStockItemDetail from './pages/NonStockItemDetail';
import InventoryValueReport from './pages/InventoryValueReport';
import Recipes from './pages/Recipes';
import Productions from './pages/Productions';
import Enumerations from './pages/Enumerations';
import SalesImport from './pages/SalesImport';
import FinancialReport from './pages/FinancialReport';
import AccountAdjustments from './pages/AccountAdjustments';
import InvoiceTemplates from './pages/InvoiceTemplates';
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
import PerformanceDashboard from './pages/hr/PerformanceDashboard';
import PerformancePolicies from './pages/hr/PerformancePolicies';
import Requests from './pages/hr/Requests';
import KasbonDashboard from './pages/hr/KasbonDashboard';
import KasbonForm from './pages/hr/KasbonForm';
import KasbonDetail from './pages/hr/KasbonDetail';
import PayrollDashboard from './pages/hr/PayrollDashboard';
import PayrollPeriodDetail from './pages/hr/PayrollPeriodDetail';
import Approvals from './pages/hr/Approvals';
import HRSettings from './pages/hr/HRSettings';
import ManpowerPlanning from './pages/hr/ManpowerPlanning';
import './App.css';

function getUser() {
  try { return JSON.parse(localStorage.getItem('user')); } catch { return null; }
}

function RequireAuth({ children }) {
  const token = localStorage.getItem('token');
  if (!token) return <Navigate to="/login" replace />;
  return children;
}

function RequireAdmin({ children }) {
  const user = getUser();
  if (user?.role !== 'admin') return <Navigate to="/" replace />;
  return children;
}

function RequireManagerOrAdmin({ children }) {
  const user = getUser();
  if (user?.role !== 'admin' && user?.role !== 'manager') return <Navigate to="/" replace />;
  return children;
}

function NavDropdown({ label, paths, children }) {
  const { pathname } = useLocation();
  const [open, setOpen] = useState(false);
  const ref = useRef(null);
  const isActive = paths.some(p => pathname.startsWith(p));

  useEffect(() => {
    const handler = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  return (
    <div className="nav-dropdown" ref={ref}>
      <button className={`nav-dropdown-btn${isActive ? ' active' : ''}`} onClick={() => setOpen(o => !o)}>
        {label} <span className="caret">▼</span>
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
      <button className="mobile-section-btn" onClick={() => setOpen(o => !o)}>
        {label} <span className="caret">{open ? '▲' : '▼'}</span>
      </button>
      {open && <div className="mobile-section-links">{children}</div>}
    </div>
  );
}

function Nav() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const user = getUser();
  const isAdminOrManager = user?.role === 'admin' || user?.role === 'manager';
  const isStaff = user?.role === 'staff';
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
    <Link to={to} className={isActive(to) ? 'active' : ''}>{label}</Link>
  );
  const menuLink = (to, label) => (
    <Link to={to} className={isActive(to) ? 'active' : ''}>{label}</Link>
  );

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
    <nav className="navbar" ref={drawerRef}>
      <span className="brand">InventoryPro</span>

      {/* Desktop nav */}
      <div className="nav-links nav-links-desktop">
        {link('/', 'Dasbor')}

        <NavDropdown label="Inventaris" paths={['/inventory', '/transfers', '/dispatch', '/stock-opname', '/enumerations', '/recipes', '/productions']}>
          {menuLink('/inventory', 'Inventaris')}
          {menuLink('/transfers', 'Transfer Gudang')}
          {menuLink('/dispatch', 'Pengiriman ke Cabang')}
          {menuLink('/stock-opname', 'Stok Opname')}
          {menuLink('/enumerations', 'Pencacahan')}
          <div className="nav-dropdown-divider" />
          {menuLink('/recipes', 'Resep Produksi')}
          {menuLink('/productions', 'Produksi')}
        </NavDropdown>

        {link('/invoices', 'Invoice')}
        <NavDropdown label="Penjualan" paths={['/sales']}>
          {menuLink('/sales', 'Catatan Penjualan')}
          {menuLink('/sales/import', 'Import dari POS')}
        </NavDropdown>

        {(isAdminOrManager || isStaff) && (
          <NavDropdown label="HR" paths={['/hr']}>
            {menuLink('/hr/attendance', 'Absensi')}
            {isAdminOrManager && menuLink('/hr/performance', 'Evaluasi')}
            {isAdminOrManager && menuLink('/hr/requests', 'Pengajuan')}
            {isAdminOrManager && menuLink('/hr/approvals', 'Persetujuan')}
            {menuLink('/hr/manpower', 'Rencana Tenaga Kerja')}
            {menuLink('/hr/kasbon', 'Kasbon')}
            {isAdminOrManager && menuLink('/hr/payroll', 'Penggajian')}
            {isAdminOrManager && menuLink('/hr/settings', 'Pengaturan')}
          </NavDropdown>
        )}

        <NavDropdown label="Laporan" paths={['/expense-report', '/reports']}>
          {menuLink('/reports/daily', 'Laporan Harian')}
          {menuLink('/reports/financial', 'Laporan Keuangan')}
          {menuLink('/expense-report', 'Laporan Pengeluaran')}
          {menuLink('/reports/inventory-value', 'Nilai Inventaris')}
        </NavDropdown>

        <NavDropdown label="Administrasi" paths={['/items', '/warehouses', '/vendors', '/accounts', '/branches', '/users', '/activity', '/account-adjustments', '/invoice-templates']}>
          {menuLink('/items', 'Barang')}
          {menuLink('/warehouses', 'Gudang')}
          {menuLink('/vendors', 'Vendor')}
          {menuLink('/accounts', 'Akun')}
          <div className="nav-dropdown-divider" />
          {menuLink('/branches', 'Cabang & Divisi')}
          {menuLink('/invoice-templates', 'Template Invoice')}
          {menuLink('/account-adjustments', 'Jurnal Manual')}
          <div className="nav-dropdown-divider" />
          {menuLink('/users', 'Pengguna')}
          {menuLink('/activity', 'Log Aktivitas')}
        </NavDropdown>
      </div>

      {user && (
        <div className="nav-user nav-user-desktop">
          <Link to="/profile" style={{ fontSize: '0.85rem', color: 'rgba(255,255,255,0.75)', textDecoration: 'none' }}>
            {user.username}
          </Link>
          <button onClick={logout} className="btn btn-secondary btn-sm">Keluar</button>
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
          <Link to="/" className={`mobile-link${isActive('/') ? ' active' : ''}`}>Dasbor</Link>

          <MobileSection label="Inventaris" paths={['/inventory', '/transfers', '/dispatch', '/stock-opname', '/enumerations', '/recipes', '/productions']}>
            <Link to="/inventory" className={isActive('/inventory') ? 'active' : ''}>Inventaris</Link>
            <Link to="/transfers" className={isActive('/transfers') ? 'active' : ''}>Transfer Gudang</Link>
            <Link to="/dispatch" className={isActive('/dispatch') ? 'active' : ''}>Pengiriman ke Cabang</Link>
            <Link to="/stock-opname" className={isActive('/stock-opname') ? 'active' : ''}>Stok Opname</Link>
            <Link to="/enumerations" className={isActive('/enumerations') ? 'active' : ''}>Pencacahan</Link>
            <Link to="/recipes" className={isActive('/recipes') ? 'active' : ''}>Resep Produksi</Link>
            <Link to="/productions" className={isActive('/productions') ? 'active' : ''}>Produksi</Link>
          </MobileSection>

          <Link to="/invoices" className={`mobile-link${isActive('/invoices') ? ' active' : ''}`}>Invoice</Link>

          <MobileSection label="Penjualan" paths={['/sales']}>
            <Link to="/sales" className={isActive('/sales') && !isActive('/sales/import') ? 'active' : ''}>Catatan Penjualan</Link>
            <Link to="/sales/import" className={isActive('/sales/import') ? 'active' : ''}>Import dari POS</Link>
          </MobileSection>

          {(isAdminOrManager || isStaff) && (
            <MobileSection label="HR" paths={['/hr']}>
              <Link to="/hr/attendance" className={isActive('/hr/attendance') ? 'active' : ''}>Absensi</Link>
              {isAdminOrManager && <Link to="/hr/performance" className={isActive('/hr/performance') ? 'active' : ''}>Evaluasi</Link>}
              {isAdminOrManager && <Link to="/hr/requests" className={isActive('/hr/requests') ? 'active' : ''}>Pengajuan</Link>}
              {isAdminOrManager && <Link to="/hr/approvals" className={isActive('/hr/approvals') ? 'active' : ''}>Persetujuan</Link>}
              <Link to="/hr/manpower" className={isActive('/hr/manpower') ? 'active' : ''}>Rencana Tenaga Kerja</Link>
              <Link to="/hr/kasbon" className={isActive('/hr/kasbon') ? 'active' : ''}>Kasbon</Link>
              {isAdminOrManager && <Link to="/hr/payroll" className={isActive('/hr/payroll') ? 'active' : ''}>Penggajian</Link>}
              {isAdminOrManager && <Link to="/hr/settings" className={isActive('/hr/settings') ? 'active' : ''}>Pengaturan</Link>}
            </MobileSection>
          )}

          <MobileSection label="Laporan" paths={['/expense-report', '/reports']}>
            <Link to="/reports/daily" className={isActive('/reports/daily') ? 'active' : ''}>Laporan Harian</Link>
            <Link to="/reports/financial" className={isActive('/reports/financial') ? 'active' : ''}>Laporan Keuangan</Link>
            <Link to="/expense-report" className={isActive('/expense-report') ? 'active' : ''}>Laporan Pengeluaran</Link>
            <Link to="/reports/inventory-value" className={isActive('/reports/inventory-value') ? 'active' : ''}>Nilai Inventaris</Link>
          </MobileSection>

          <MobileSection label="Administrasi" paths={['/items', '/warehouses', '/vendors', '/accounts', '/branches', '/users', '/activity', '/account-adjustments', '/invoice-templates']}>
            <Link to="/items" className={isActive('/items') ? 'active' : ''}>Barang</Link>
            <Link to="/warehouses" className={isActive('/warehouses') ? 'active' : ''}>Gudang</Link>
            <Link to="/vendors" className={isActive('/vendors') ? 'active' : ''}>Vendor</Link>
            <Link to="/accounts" className={isActive('/accounts') ? 'active' : ''}>Akun</Link>
            <Link to="/branches" className={isActive('/branches') ? 'active' : ''}>Cabang & Divisi</Link>
            <Link to="/invoice-templates" className={isActive('/invoice-templates') ? 'active' : ''}>Template Invoice</Link>
            <Link to="/account-adjustments" className={isActive('/account-adjustments') ? 'active' : ''}>Jurnal Manual</Link>
            <Link to="/users" className={isActive('/users') ? 'active' : ''}>Pengguna</Link>
            <Link to="/activity" className={isActive('/activity') ? 'active' : ''}>Log Aktivitas</Link>
          </MobileSection>

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

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/*" element={
          <RequireAuth>
            <Layout>
              <Routes>
                <Route path="/" element={<Dashboard />} />
                <Route path="/items" element={<Items />} />
                <Route path="/items/new" element={<ItemForm />} />
                <Route path="/items/edit/:id" element={<ItemForm />} />
                <Route path="/items/history/:id" element={<NonStockItemDetail />} />
                <Route path="/inventory" element={<Inventory />} />
                <Route path="/inventory/new" element={<InventoryForm />} />
                <Route path="/inventory/edit/:id" element={<InventoryForm />} />
                <Route path="/inventory/history/:itemId" element={<StockHistoryPage />} />
                <Route path="/invoices" element={<Invoices />} />
                <Route path="/invoices/new" element={<InvoiceForm />} />
                <Route path="/invoices/edit/:id" element={<InvoiceForm />} />
                <Route path="/invoices/view/:id" element={<InvoiceDetail />} />
                <Route path="/sales" element={<Sales />} />
                <Route path="/sales/import" element={<SalesImport />} />
                <Route path="/expense-report" element={<ExpenseReport />} />
                <Route path="/reports/inventory-value" element={<InventoryValueReport />} />
                <Route path="/reports/financial" element={<FinancialReport />} />
                <Route path="/reports/daily" element={<DailyReport />} />
                <Route path="/account-adjustments" element={<AccountAdjustments />} />
                <Route path="/transfers" element={<StockTransfers />} />
                <Route path="/transfers/group/:id" element={<TransferDetail />} />
                <Route path="/dispatch" element={<Dispatch />} />
                <Route path="/dispatches/:id" element={<DispatchDetail />} />
                <Route path="/stock-opname" element={<StockOpname />} />
                <Route path="/stock-opname/detail/:id" element={<StockOpnameDetail />} />
                <Route path="/recipes" element={<Recipes />} />
                <Route path="/productions" element={<Productions />} />
                <Route path="/enumerations" element={<Enumerations />} />
                <Route path="/warehouses" element={<Warehouses />} />
                <Route path="/vendors" element={<Vendors />} />
                <Route path="/vendors/:id/history" element={<VendorHistory />} />
                <Route path="/accounts" element={<Accounts />} />
                <Route path="/branches" element={<Branches />} />
                <Route path="/invoice-templates" element={<InvoiceTemplates />} />
                <Route path="/users" element={<Users />} />
                <Route path="/activity" element={<ActivityLog />} />
                <Route path="/profile" element={<Profile />} />
                <Route path="/hr/employees" element={<Employees />} />
                <Route path="/hr/employees/new" element={<RequireManagerOrAdmin><EmployeeForm /></RequireManagerOrAdmin>} />
                <Route path="/hr/employees/:id" element={<EmployeeDetail />} />
                <Route path="/hr/employees/:id/edit" element={<RequireManagerOrAdmin><EmployeeForm /></RequireManagerOrAdmin>} />
                <Route path="/hr/import" element={<RequireManagerOrAdmin><HRImport /></RequireManagerOrAdmin>} />
                <Route path="/hr/positions" element={<RequireManagerOrAdmin><Positions /></RequireManagerOrAdmin>} />
                <Route path="/hr/wage-components" element={<RequireManagerOrAdmin><WageComponents /></RequireManagerOrAdmin>} />
                <Route path="/hr/attendance" element={<AttendanceDashboard />} />
                <Route path="/hr/attendance/import" element={<RequireManagerOrAdmin><FingerprintImport /></RequireManagerOrAdmin>} />
                <Route path="/hr/attendance/settings" element={<RequireManagerOrAdmin><AttendanceSettings /></RequireManagerOrAdmin>} />
                <Route path="/hr/performance" element={<RequireManagerOrAdmin><PerformanceDashboard /></RequireManagerOrAdmin>} />
                <Route path="/hr/performance/policies" element={<RequireManagerOrAdmin><PerformancePolicies /></RequireManagerOrAdmin>} />
                <Route path="/hr/requests" element={<RequireManagerOrAdmin><Requests /></RequireManagerOrAdmin>} />
                <Route path="/hr/approvals" element={<RequireManagerOrAdmin><Approvals /></RequireManagerOrAdmin>} />
                {/* back-compat: old leave/overtime links land on the merged requests screen */}
                <Route path="/hr/leave" element={<RequireManagerOrAdmin><Requests /></RequireManagerOrAdmin>} />
                <Route path="/hr/manpower" element={<ManpowerPlanning />} />
                <Route path="/hr/kasbon" element={<KasbonDashboard />} />
                <Route path="/hr/kasbon/new" element={<RequireManagerOrAdmin><KasbonForm /></RequireManagerOrAdmin>} />
                <Route path="/hr/kasbon/:id" element={<KasbonDetail />} />
                <Route path="/hr/overtime" element={<RequireManagerOrAdmin><Requests /></RequireManagerOrAdmin>} />
                <Route path="/hr/payroll" element={<RequireManagerOrAdmin><PayrollDashboard /></RequireManagerOrAdmin>} />
                <Route path="/hr/payroll/:id" element={<RequireManagerOrAdmin><PayrollPeriodDetail /></RequireManagerOrAdmin>} />
                <Route path="/hr/settings" element={<RequireManagerOrAdmin><HRSettings /></RequireManagerOrAdmin>} />
              </Routes>
            </Layout>
          </RequireAuth>
        } />
      </Routes>
    </BrowserRouter>
  );
}
