import axios from 'axios';

const api = axios.create({ baseURL: 'http://localhost:5000/api' });

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

api.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem('token');
      localStorage.removeItem('user');
      window.location.href = '/login';
    }
    return Promise.reject(err);
  }
);

export const login = (data) => api.post('/auth/login', data);

export const getUsers = () => api.get('/users');
export const createUser = (data) => api.post('/users', data);
export const updateUser = (id, data) => api.put(`/users/${id}`, data);
export const deleteUser = (id) => api.delete(`/users/${id}`);

export const getItems = (params) => api.get('/items', { params });
export const getItem = (id) => api.get(`/items/${id}`);
export const createItem = (data) => api.post('/items', data);
export const updateItem = (id, data) => api.put(`/items/${id}`, data);
export const deleteItem = (id) => api.delete(`/items/${id}`);
export const getItemHistory   = (id) => api.get(`/items/${id}/history`);
export const getItemLastPrice = (id, params) => api.get(`/items/${id}/last-price`, { params });

export const getInventory = (params) => api.get('/inventory', { params });
export const getInventoryRecord = (id) => api.get(`/inventory/${id}`);
export const createInventoryRecord = (data) => api.post('/inventory', data);
export const updateInventoryRecord = (id, data) => api.put(`/inventory/${id}`, data);
export const deleteInventoryRecord = (id) => api.delete(`/inventory/${id}`);

export const getWarehouses = () => api.get('/warehouses');
export const createWarehouse = (data) => api.post('/warehouses', data);
export const updateWarehouse = (id, data) => api.put(`/warehouses/${id}`, data);
export const deleteWarehouse = (id) => api.delete(`/warehouses/${id}`);

export const getVendors = () => api.get('/vendors');
export const createVendor = (data) => api.post('/vendors', data);
export const updateVendor = (id, data) => api.put(`/vendors/${id}`, data);
export const deleteVendor = (id) => api.delete(`/vendors/${id}`);

export const getAccounts = () => api.get('/accounts');
export const createAccount = (data) => api.post('/accounts', data);
export const updateAccount = (id, data) => api.put(`/accounts/${id}`, data);
export const deleteAccount = (id) => api.delete(`/accounts/${id}`);

export const getStockHistory = (itemId, params) => api.get(`/stock-history/${itemId}`, { params });

export const getStockOpname = () => api.get('/stock-opname');
export const getStockOpnameById = (id) => api.get(`/stock-opname/${id}`);
export const createStockOpname = (data) => api.post('/stock-opname', data);

export const getInvoices = (params) => api.get('/invoices', { params });
export const getInvoice = (id) => api.get(`/invoices/${id}`);
export const createInvoice = (data) => api.post('/invoices', data);
export const updateInvoice = (id, data) => api.put(`/invoices/${id}`, data);
export const deleteInvoice = (id) => api.delete(`/invoices/${id}`);
export const uploadInvoicePhoto = (id, file) => {
  const form = new FormData();
  form.append('photo', file);
  return api.post(`/invoices/${id}/photo`, form, { headers: { 'Content-Type': 'multipart/form-data' } });
};
export const deleteInvoicePhoto = (id) => api.delete(`/invoices/${id}/photo`);

export const getStockTransfers = () => api.get('/stock-transfers');
export const getStockTransferGroup = (id) => api.get(`/stock-transfers/group/${id}`);
export const createStockTransfer = (data) => api.post('/stock-transfers', data);

export const getBranches = () => api.get('/branches');
export const createBranch = (data) => api.post('/branches', data);
export const updateBranch = (id, data) => api.put(`/branches/${id}`, data);
export const deleteBranch = (id) => api.delete(`/branches/${id}`);

export const getDivisions = (params) => api.get('/divisions', { params });
export const createDivision = (data) => api.post('/divisions', data);
export const updateDivision = (id, data) => api.put(`/divisions/${id}`, data);
export const deleteDivision = (id) => api.delete(`/divisions/${id}`);

export const getDispatches = () => api.get('/dispatches');
export const getDispatch = (id) => api.get(`/dispatches/${id}`);
export const createDispatch = (data) => api.post('/dispatches', data);

export const getSales = (params) => api.get('/sales', { params });
export const createSale = (data) => api.post('/sales', data);
export const deleteSale = (id) => api.delete(`/sales/${id}`);

export const getActivityLog = () => api.get('/activity-log');

export const getStats = () => api.get('/stats');

export const getExpenseReport = (params) => api.get('/expense-report', { params });

export const getInventoryValueReport = (params) => api.get('/reports/inventory-value', { params });
export const getExpenseSummaryReport  = (params) => api.get('/reports/expense-summary', { params });
