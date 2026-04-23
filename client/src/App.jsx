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
import ExpenseSummary from './pages/ExpenseSummary';
import './App.css';

function getUser() {
  try { return JSON.parse(localStorage.getItem('user')); } catch { return null; }
}

function RequireAuth({ children }) {
  const token = localStorage.getItem('token');
  if (!token) return <Navigate to="/login" replace />;
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
  const isActive = (to) => to === '/' ? pathname === '/' : pathname.startsWith(to);
  const link = (to, label) => (
    <Link to={to} className={isActive(to) ? 'active' : ''}>{label}</Link>
  );
  const menuLink = (to, label) => (
    <Link to={to} className={isActive(to) ? 'active' : ''}>{label}</Link>
  );

  const logout = () => {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    navigate('/login');
  };

  return (
    <nav className="navbar">
      <span className="brand">InventoryPro</span>
      <div className="nav-links">
        {link('/', 'Dashboard')}

        <NavDropdown label="Inventory" paths={['/inventory', '/transfers', '/dispatch', '/stock-opname']}>
          {menuLink('/inventory', 'Inventory')}
          {menuLink('/transfers', 'Stock Transfers')}
          {menuLink('/dispatch', 'Dispatch to Branch')}
          {menuLink('/stock-opname', 'Stock Opname')}
        </NavDropdown>

        {link('/invoices', 'Invoices')}
        {link('/sales', 'Sales')}

        <NavDropdown label="Reports" paths={['/expense-report', '/reports']}>
          {menuLink('/expense-report', 'Expense Report')}
          {menuLink('/reports/expense-summary', 'Expense Summary')}
          {menuLink('/reports/inventory-value', 'Inventory Value')}
        </NavDropdown>

        <NavDropdown label="Administration" paths={['/items', '/warehouses', '/vendors', '/accounts', '/branches', '/users', '/activity']}>
          {menuLink('/items', 'Items')}
          {menuLink('/warehouses', 'Warehouses')}
          {menuLink('/vendors', 'Vendors')}
          {menuLink('/accounts', 'Accounts')}
          <div className="nav-dropdown-divider" />
          {menuLink('/branches', 'Branches & Divisions')}
          <div className="nav-dropdown-divider" />
          {menuLink('/users', 'Users')}
          {menuLink('/activity', 'Activity Log')}
        </NavDropdown>
      </div>

      {user && (
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          <span style={{ fontSize: '0.85rem', color: 'rgba(255,255,255,0.55)' }}>{user.username}</span>
          <button onClick={logout} className="btn btn-secondary btn-sm">Logout</button>
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
                <Route path="/expense-report" element={<ExpenseReport />} />
                <Route path="/reports/expense-summary" element={<ExpenseSummary />} />
                <Route path="/reports/inventory-value" element={<InventoryValueReport />} />
                <Route path="/transfers" element={<StockTransfers />} />
                <Route path="/transfers/group/:id" element={<TransferDetail />} />
                <Route path="/dispatch" element={<Dispatch />} />
                <Route path="/dispatches/:id" element={<DispatchDetail />} />
                <Route path="/stock-opname" element={<StockOpname />} />
                <Route path="/stock-opname/detail/:id" element={<StockOpnameDetail />} />
                <Route path="/warehouses" element={<Warehouses />} />
                <Route path="/vendors" element={<Vendors />} />
                <Route path="/accounts" element={<Accounts />} />
                <Route path="/branches" element={<Branches />} />
                <Route path="/users" element={<Users />} />
                <Route path="/activity" element={<ActivityLog />} />
              </Routes>
            </Layout>
          </RequireAuth>
        } />
      </Routes>
    </BrowserRouter>
  );
}
