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
import SalesImport from './pages/SalesImport';
import FinancialReport from './pages/FinancialReport';
import AccountAdjustments from './pages/AccountAdjustments';
import VendorHistory from './pages/VendorHistory';
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

function Nav() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const user = getUser();
  const isAdmin = user?.role === 'admin';
  const isActive = (to) => to === '/' ? pathname === '/' : pathname.startsWith(to);
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
    <nav className="navbar">
      <span className="brand">InventoryPro</span>
      <div className="nav-links">
        {link('/', 'Dasbor')}

        <NavDropdown label="Inventaris" paths={['/inventory', '/transfers', '/dispatch', '/stock-opname', '/recipes', '/productions']}>
          {menuLink('/inventory', 'Inventaris')}
          {menuLink('/transfers', 'Transfer Stok')}
          {menuLink('/dispatch', 'Pengiriman ke Cabang')}
          {menuLink('/stock-opname', 'Stok Opname')}
          <div className="nav-dropdown-divider" />
          {menuLink('/recipes', 'Resep Produksi')}
          {menuLink('/productions', 'Produksi')}
        </NavDropdown>

        {link('/invoices', 'Invoice')}
        <NavDropdown label="Penjualan" paths={['/sales']}>
          {menuLink('/sales', 'Catatan Penjualan')}
          {menuLink('/sales/import', 'Import dari POS')}
        </NavDropdown>

        {isAdmin && (
          <NavDropdown label="Laporan" paths={['/expense-report', '/reports']}>
            {menuLink('/reports/financial', 'Laporan Keuangan')}
            {menuLink('/expense-report', 'Laporan Pengeluaran')}
            {menuLink('/reports/inventory-value', 'Nilai Inventaris')}
          </NavDropdown>
        )}

        {isAdmin && (
          <NavDropdown label="Administrasi" paths={['/items', '/warehouses', '/vendors', '/accounts', '/branches', '/users', '/activity', '/account-adjustments']}>
            {menuLink('/items', 'Barang')}
            {menuLink('/warehouses', 'Gudang')}
            {menuLink('/vendors', 'Vendor')}
            {menuLink('/accounts', 'Akun')}
            <div className="nav-dropdown-divider" />
            {menuLink('/branches', 'Cabang & Divisi')}
            {menuLink('/account-adjustments', 'Jurnal Manual')}
            <div className="nav-dropdown-divider" />
            {menuLink('/users', 'Pengguna')}
            {menuLink('/activity', 'Log Aktivitas')}
          </NavDropdown>
        )}
      </div>

      {user && (
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          <span style={{ fontSize: '0.85rem', color: 'rgba(255,255,255,0.55)' }}>{user.username}</span>
          <button onClick={logout} className="btn btn-secondary btn-sm">Keluar</button>
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
                <Route path="/items" element={<RequireAdmin><Items /></RequireAdmin>} />
                <Route path="/items/new" element={<RequireAdmin><ItemForm /></RequireAdmin>} />
                <Route path="/items/edit/:id" element={<RequireAdmin><ItemForm /></RequireAdmin>} />
                <Route path="/items/history/:id" element={<RequireAdmin><NonStockItemDetail /></RequireAdmin>} />
                <Route path="/inventory" element={<Inventory />} />
                <Route path="/inventory/new" element={<InventoryForm />} />
                <Route path="/inventory/edit/:id" element={<InventoryForm />} />
                <Route path="/inventory/history/:itemId" element={<StockHistoryPage />} />
                <Route path="/invoices" element={<Invoices />} />
                <Route path="/invoices/new" element={<InvoiceForm />} />
                <Route path="/invoices/edit/:id" element={<RequireAdmin><InvoiceForm /></RequireAdmin>} />
                <Route path="/invoices/view/:id" element={<InvoiceDetail />} />
                <Route path="/sales" element={<Sales />} />
                <Route path="/sales/import" element={<SalesImport />} />
                <Route path="/expense-report" element={<RequireAdmin><ExpenseReport /></RequireAdmin>} />
                <Route path="/reports/inventory-value" element={<RequireAdmin><InventoryValueReport /></RequireAdmin>} />
                <Route path="/reports/financial" element={<RequireAdmin><FinancialReport /></RequireAdmin>} />
                <Route path="/account-adjustments" element={<RequireAdmin><AccountAdjustments /></RequireAdmin>} />
                <Route path="/transfers" element={<StockTransfers />} />
                <Route path="/transfers/group/:id" element={<TransferDetail />} />
                <Route path="/dispatch" element={<Dispatch />} />
                <Route path="/dispatches/:id" element={<DispatchDetail />} />
                <Route path="/stock-opname" element={<StockOpname />} />
                <Route path="/stock-opname/detail/:id" element={<StockOpnameDetail />} />
                <Route path="/recipes" element={<Recipes />} />
                <Route path="/productions" element={<Productions />} />
                <Route path="/warehouses" element={<RequireAdmin><Warehouses /></RequireAdmin>} />
                <Route path="/vendors" element={<RequireAdmin><Vendors /></RequireAdmin>} />
                <Route path="/vendors/:id/history" element={<VendorHistory />} />
                <Route path="/accounts" element={<RequireAdmin><Accounts /></RequireAdmin>} />
                <Route path="/branches" element={<RequireAdmin><Branches /></RequireAdmin>} />
                <Route path="/users" element={<RequireAdmin><Users /></RequireAdmin>} />
                <Route path="/activity" element={<RequireAdmin><ActivityLog /></RequireAdmin>} />
              </Routes>
            </Layout>
          </RequireAuth>
        } />
      </Routes>
    </BrowserRouter>
  );
}
