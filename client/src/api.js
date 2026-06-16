import axios from 'axios';

const { apiBaseUrl } = await fetch('/config.json').then(r => r.json());

const api = axios.create({ baseURL: apiBaseUrl });

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

export const getStockOpnameDrafts = () => api.get('/stock-opname/drafts');
export const createStockOpnameDraft = (data) => api.post('/stock-opname/drafts', data);
export const updateStockOpnameDraft = (id, data) => api.put(`/stock-opname/drafts/${id}`, data);
export const deleteStockOpnameDraft = (id) => api.delete(`/stock-opname/drafts/${id}`);

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
export const deletePosImport  = (id)  => api.delete(`/pos-import/${id}`);

export const getRecipes = () => api.get('/recipes');
export const getRecipe = (id) => api.get(`/recipes/${id}`);
export const createRecipe = (data) => api.post('/recipes', data);
export const updateRecipe = (id, data) => api.put(`/recipes/${id}`, data);
export const deleteRecipe = (id) => api.delete(`/recipes/${id}`);

export const getProductions = () => api.get('/productions');
export const createProduction = (data) => api.post('/productions', data);

export const getInvoiceTemplates = () => api.get('/invoice-templates');
export const createInvoiceTemplate = (data) => api.post('/invoice-templates', data);
export const updateInvoiceTemplate = (id, data) => api.put(`/invoice-templates/${id}`, data);
export const deleteInvoiceTemplate = (id) => api.delete(`/invoice-templates/${id}`);

export const getActivityLog = (params) => api.get('/activity-log', { params });
export const exportActivityLog = (params) => api.get('/activity-log/export', { params, responseType: 'blob' });
export const deleteActivityLog = (before_date) => api.delete('/activity-log', { data: { before_date } });

export const getStats = (params) => api.get('/stats', { params });
export const getDailySalesByBranch = (date) => api.get('/stats/daily-sales', { params: { date } });
export const getStockFlow = (params) => api.get('/stats/stock-flow', { params });

export const getExpenseReport = (params) => api.get('/expense-report', { params });

export const getInventoryValueReport = (params) => api.get('/reports/inventory-value', { params });
export const getExpenseSummaryReport  = (params) => api.get('/reports/expense-summary', { params });
export const getFinancialReport       = (params)  => api.get('/reports/financial', { params });

export const getDailyReport           = (params) => api.get('/reports/daily', { params });
export const getAccountAdjustments    = (params) => api.get('/account-adjustments', { params });
export const createAccountAdjustment  = (data)   => api.post('/account-adjustments', data);
export const createAccountTransfer    = (data)   => api.post('/account-adjustments/transfer', data);

export const getEnumerations    = ()     => api.get('/enumerations');
export const createEnumeration  = (data) => api.post('/enumerations', data);
export const deleteEnumeration  = (id)   => api.delete(`/enumerations/${id}`);

// ── HR: Karyawan & Jabatan ──────────────────────────────────────────────────
export const getEmployees   = (params) => api.get('/hr/employees', { params });
export const getEmployee    = (id)     => api.get(`/hr/employees/${id}`);
export const createEmployee = (data)   => api.post('/hr/employees', data);
export const updateEmployee = (id, data) => api.put(`/hr/employees/${id}`, data);
export const deleteEmployee = (id)     => api.delete(`/hr/employees/${id}`);
export const uploadEmployeePhoto = (id, file) => {
  const form = new FormData();
  form.append('photo', file);
  return api.post(`/hr/employees/${id}/photo`, form, { headers: { 'Content-Type': 'multipart/form-data' } });
};
export const deleteEmployeePhoto = (id) => api.delete(`/hr/employees/${id}/photo`);

export const getPositions   = () => api.get('/hr/positions');
export const createPosition = (data) => api.post('/hr/positions', data);
export const updatePosition = (id, data) => api.put(`/hr/positions/${id}`, data);
export const deletePosition = (id) => api.delete(`/hr/positions/${id}`);

// ── HR: Komponen & Struktur Gaji ────────────────────────────────────────────
export const getWageComponents   = (params) => api.get('/hr/wage-components', { params });
export const createWageComponent = (data)    => api.post('/hr/wage-components', data);
export const updateWageComponent = (id, data) => api.put(`/hr/wage-components/${id}`, data);
export const deleteWageComponent = (id)      => api.delete(`/hr/wage-components/${id}`);

export const getEmployeeWage        = (id) => api.get(`/hr/employees/${id}/wage`);
export const getEmployeeWageHistory = (id) => api.get(`/hr/employees/${id}/wage/history`);
export const createEmployeeWage     = (id, data) => api.post(`/hr/employees/${id}/wage`, data);

// ── HR: Impor Massal Karyawan ───────────────────────────────────────────────
export const downloadHrImportTemplate = () => api.get('/hr/import/template', { responseType: 'blob' });
export const parseHrImport             = (formData) => api.post('/hr/import/parse', formData, { headers: { 'Content-Type': 'multipart/form-data' } });
export const confirmHrImport           = (data) => api.post('/hr/import/confirm', data);

// ── HR: Absensi / Kehadiran ─────────────────────────────────────────────────
export const getAttendance        = (params) => api.get('/hr/attendance', { params });
export const updateAttendance     = (id, data) => api.put(`/hr/attendance/${id}`, data);
export const reconcileAttendance  = (date) => api.post('/hr/attendance/reconcile', null, { params: { date } });

export const parseFingerprintImport   = (formData) => api.post('/hr/attendance/fingerprint-import/parse', formData, { headers: { 'Content-Type': 'multipart/form-data' } });
export const confirmFingerprintImport = (formData) => api.post('/hr/attendance/fingerprint-import/confirm', formData, { headers: { 'Content-Type': 'multipart/form-data' } });

// Work schedules
export const getWorkSchedules   = () => api.get('/hr/attendance/work-schedules');
export const saveWorkSchedule   = (data) => api.post('/hr/attendance/work-schedules', data);

// Public holidays
export const getPublicHolidays  = () => api.get('/hr/attendance/holidays');
export const createPublicHoliday = (data) => api.post('/hr/attendance/holidays', data);
export const deletePublicHoliday = (id) => api.delete(`/hr/attendance/holidays/${id}`);

// Attendance devices
export const getAttendanceDevices  = () => api.get('/hr/attendance/devices');
export const createAttendanceDevice = (data) => api.post('/hr/attendance/devices', data);
export const setAttendanceDeviceActive = (id, data) => api.put(`/hr/attendance/devices/${id}`, data);
export const deleteAttendanceDevice = (id) => api.delete(`/hr/attendance/devices/${id}`);

// ── HR: Penilaian Kinerja ───────────────────────────────────────────────────
// Performance policies
export const getPerformancePolicies   = () => api.get('/hr/performance/policies');
export const createPerformancePolicy  = (data) => api.post('/hr/performance/policies', data);
export const updatePerformancePolicy  = (id, data) => api.put(`/hr/performance/policies/${id}`, data);
export const deletePerformancePolicy  = (id) => api.delete(`/hr/performance/policies/${id}`);

// Scores & breakdown
export const getPerformanceScores     = (params) => api.get('/hr/performance/scores', { params });
export const getEmployeePerformance   = (id, params) => api.get(`/hr/employees/${id}/performance`, { params });

// Violations & evaluation
export const createPerformanceViolation = (data) => api.post('/hr/performance/violations', data);
export const deletePerformanceViolation = (id, reason) => api.delete(`/hr/performance/violations/${id}`, { params: { reason } });
export const evaluatePerformance        = (from, to) => api.post('/hr/performance/evaluate', null, { params: { from, to } });

// ── HR: Cuti (Leave Management) ─────────────────────────────────────────────
// Leave types
export const getLeaveTypes    = (params) => api.get('/hr/leave-types', { params });
export const createLeaveType  = (data) => api.post('/hr/leave-types', data);
export const updateLeaveType  = (id, data) => api.put(`/hr/leave-types/${id}`, data);
export const deleteLeaveType  = (id) => api.delete(`/hr/leave-types/${id}`);

// Manpower planning
export const getManpowerPlanning  = (params) => api.get('/hr/manpower-planning', { params });

// Leave requests
export const getLeaveRequests     = (params) => api.get('/hr/leave-requests', { params });
export const createLeaveRequest   = (data) => api.post('/hr/leave-requests', data);
export const approveLeaveRequest  = (id, note) => api.post(`/hr/leave-requests/${id}/approve`, { note });
export const rejectLeaveRequest   = (id, note) => api.post(`/hr/leave-requests/${id}/reject`, { note });
export const cancelLeaveRequest   = (id, note) => api.post(`/hr/leave-requests/${id}/cancel`, { note });

// Per-employee balance + history
export const getLeaveBalance        = (id, year) => api.get(`/hr/employees/${id}/leave-balance`, { params: { year } });
export const setLeaveBalanceQuota   = (id, data) => api.put(`/hr/employees/${id}/leave-balance`, data);
export const getEmployeeLeaveRequests = (id) => api.get(`/hr/employees/${id}/leave-requests`);

// ── HR: Kasbon (Cash Advance) ───────────────────────────────────────────────
export const getKasbons      = (params) => api.get('/hr/kasbons', { params });
export const getKasbon       = (id) => api.get(`/hr/kasbons/${id}`);
export const createKasbon    = (data) => api.post('/hr/kasbons', data);
export const updateKasbon    = (id, data) => api.put(`/hr/kasbons/${id}`, data);
export const approveKasbon   = (id, data) => api.post(`/hr/kasbons/${id}/approve`, data);
export const rejectKasbon    = (id, note) => api.post(`/hr/kasbons/${id}/reject`, { note });
export const cancelKasbon    = (id) => api.post(`/hr/kasbons/${id}/cancel`);
export const processKasbon   = (id, formData) => api.post(`/hr/kasbons/${id}/process`, formData, { headers: { 'Content-Type': 'multipart/form-data' } });

// For the kasbon detail page: a kasbon's own employee kasbon history is fetched via
// the list filtered by employee_id.
export const getEmployeeKasbons = (employeeId) => api.get('/hr/kasbons', { params: { employee_id: employeeId } });

// ── HR: Payroll (Penggajian) ────────────────────────────────────────────────
export const getPayrollPeriods   = () => api.get('/hr/payroll/periods');
export const createPayrollPeriod = (data) => api.post('/hr/payroll/periods', data);
export const getPayrollPeriod    = (id) => api.get(`/hr/payroll/periods/${id}`);
export const getPayrollLines     = (id, params) => api.get(`/hr/payroll/periods/${id}/lines`, { params });
export const getPayrollLineReview = (lineId) => api.get(`/hr/payroll/lines/${lineId}/review`);
export const reviewPayrollLine   = (lineId, data) => api.post(`/hr/payroll/lines/${lineId}/review`, data);
export const unreviewPayrollLine = (lineId) => api.post(`/hr/payroll/lines/${lineId}/unreview`);
export const regeneratePayrollLine = (id, employeeId) => api.post(`/hr/payroll/periods/${id}/regenerate-line/${employeeId}`);
export const closePayrollPeriod  = (id) => api.post(`/hr/payroll/periods/${id}/close`);
export const markPayrollPeriodPaid = (id) => api.post(`/hr/payroll/periods/${id}/mark-paid`);

// ── HR: Slip Gaji (Payslips) + Pengaturan HR ────────────────────────────────
export const downloadPayslip        = (lineId)   => api.get(`/hr/payroll/lines/${lineId}/payslip`, { responseType: 'blob' });
export const downloadPeriodPayslips = (periodId) => api.get(`/hr/payroll/periods/${periodId}/payslips`, { responseType: 'blob' });

export const getHRSettings    = () => api.get('/hr/settings');
export const updateHRSettings = (data) => api.put('/hr/settings', data);
export const uploadHRLogo     = (formData) => api.post('/hr/settings/logo', formData, { headers: { 'Content-Type': 'multipart/form-data' } });
