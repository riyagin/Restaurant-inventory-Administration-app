import axios from 'axios';

const api = axios.create({ baseURL: 'http://localhost:5000/api' });

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

let isRefreshing = false;
let refreshQueue = [];

api.interceptors.response.use(
  (res) => res,
  async (err) => {
    const original = err.config;
    if (err.response?.status === 401 && !original._retry) {
      // Don't try to refresh if the failing request was itself a refresh/logout
      if (original.url?.includes('/auth/')) {
        localStorage.removeItem('token');
        localStorage.removeItem('user');
        window.location.href = '/login';
        return Promise.reject(err);
      }

      if (isRefreshing) {
        // Queue this request to retry once refresh completes
        return new Promise((resolve, reject) => {
          refreshQueue.push({ resolve, reject });
        }).then(token => {
          original.headers.Authorization = `Bearer ${token}`;
          return api(original);
        });
      }

      original._retry = true;
      isRefreshing = true;

      try {
        const res = await api.post('/auth/refresh');
        const { token, user } = res.data;
        localStorage.setItem('token', token);
        localStorage.setItem('user', JSON.stringify(user));
        api.defaults.headers.common.Authorization = `Bearer ${token}`;
        refreshQueue.forEach(q => q.resolve(token));
        refreshQueue = [];
        original.headers.Authorization = `Bearer ${token}`;
        return api(original);
      } catch {
        refreshQueue.forEach(q => q.reject(err));
        refreshQueue = [];
        localStorage.removeItem('token');
        localStorage.removeItem('user');
        window.location.href = '/login';
        return Promise.reject(err);
      } finally {
        isRefreshing = false;
      }
    }
    return Promise.reject(err);
  }
);

export const login   = (data) => api.post('/auth/login', data);
export const logout  = ()     => api.post('/auth/logout');
export const refresh = ()     => api.post('/auth/refresh');

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
export const getVendorHistory = (id) => api.get(`/vendors/${id}/history`);

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
export const payInvoice = (id, data) => api.post(`/invoices/${id}/pay`, data);

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

export const getDivisionCategories = (params) => api.get('/division-categories', { params });
export const createDivisionCategory = (data) => api.post('/division-categories', data);
export const deleteDivisionCategory = (id) => api.delete(`/division-categories/${id}`);

export const getDispatches = () => api.get('/dispatches');
export const getDispatch = (id) => api.get(`/dispatches/${id}`);
export const createDispatch = (data) => api.post('/dispatches', data);

export const getSales = (params) => api.get('/sales', { params });
export const createSale = (data) => api.post('/sales', data);
export const deleteSale = (id) => api.delete(`/sales/${id}`);

export const parsePosXlsx    = (formData) => api.post('/pos-import/parse', formData, { headers: { 'Content-Type': 'multipart/form-data' } });
export const confirmPosImport = (data) => api.post('/pos-import/confirm', data);
export const getPosImports    = () => api.get('/pos-import');

export const getRecipes = () => api.get('/recipes');
export const getRecipe = (id) => api.get(`/recipes/${id}`);
export const createRecipe = (data) => api.post('/recipes', data);
export const updateRecipe = (id, data) => api.put(`/recipes/${id}`, data);
export const deleteRecipe = (id) => api.delete(`/recipes/${id}`);

export const getProductions = () => api.get('/productions');
export const createProduction = (data) => api.post('/productions', data);

export const getActivityLog = (params) => api.get('/activity-log', { params });
export const exportActivityLog = (params) => api.get('/activity-log/export', { params, responseType: 'blob' });
export const deleteActivityLog = (before_date) => api.delete('/activity-log', { data: { before_date } });

export const getStats = () => api.get('/stats');

export const getExpenseReport = (params) => api.get('/expense-report', { params });

export const getInventoryValueReport = (params) => api.get('/reports/inventory-value', { params });
export const getExpenseSummaryReport  = (params) => api.get('/reports/expense-summary', { params });
export const getFinancialReport       = (params)  => api.get('/reports/financial', { params });

export const getAccountAdjustments    = (params) => api.get('/account-adjustments', { params });
export const createAccountAdjustment  = (data)   => api.post('/account-adjustments', data);
