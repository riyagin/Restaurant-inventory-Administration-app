# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

---

## Project Overview

**Inventory App** — a full-stack inventory and financial management system for a small F&B business. Built for Indonesian locale (IDR currency, `id-ID` formatting). The backend is being migrated from Node.js/Express to **Go**.

---

## Repository Structure

```
inventory-app/
  client/          React 19 SPA (Vite)
  server/          Node.js/Express legacy backend (being replaced)
  server-go/       Go backend (new — migration target)
  deploy/          PM2 config, Nginx config, backup script
  start.bat        Windows: launches both dev servers
```

---

## Development Commands

### Start Dev Servers (Windows)
```batch
start.bat          # Launches both frontend + backend in separate cmd windows
```

### Frontend
```bash
cd client
npm run dev        # Vite dev server → http://localhost:5173
npm run build      # Production build to client/dist/
npm run lint       # ESLint (flat config, ESLint 9)
npm run preview    # Preview production build
```

### Go Backend (new)
```bash
cd server-go
go run ./cmd/api         # Dev server → http://localhost:5000
go build -o api ./cmd/api  # Build binary
go test ./...            # Run tests

# Migrations (golang-migrate CLI required)
migrate -path migrations -database "$DATABASE_URL" up      # Apply all
migrate -path migrations -database "$DATABASE_URL" down 1  # Roll back one
migrate create -ext sql -dir migrations -seq <name>        # New migration
```

### Legacy Node Backend (keep running during migration)
```bash
cd server
npm run dev        # Node with --watch → http://localhost:5001 (shifted port)
npm start          # Production
```

### Database
```bash
# Initial schema (legacy, use migrations going forward)
psql -U postgres -d inventory_app -f server/schema.sql

# Reset admin password
go run ./server-go/cmd/reset-password <username> <new-password>
# or legacy: node server/reset-password.js

# Seed HR dev data (positions, employees, wage structures, work schedule, holidays, policy)
go run ./server-go/cmd/seed-hr
```

---

## Architecture: Go Backend (Migration Target)

### Stack
| Layer | Tool |
|---|---|
| HTTP router | `chi` |
| DB driver | `pgx/v5` |
| Query layer | `sqlc` (generates typed Go from `.sql` files) |
| Migrations | `golang-migrate` (SQL up/down files) |
| Auth | `golang-jwt/jwt` |
| File upload | stdlib `multipart` |
| Excel parsing | `excelize` |
| Config | `godotenv` |
| Process manager | PM2 (unchanged, points at Go binary) |

### Directory Layout (`server-go/`)
```
server-go/
  cmd/
    api/main.go            # Entry point, router wiring, server startup
    reset-password/main.go # Admin password reset utility
    seed-hr/main.go        # Dev-only HR seed (positions, employees, wage structures, holidays, policy)
  internal/
    db/                    # sqlc-generated query functions (do not edit manually)
    handler/               # HTTP handlers, one file per domain
      auth.go
      users.go
      items.go
      inventory.go
      invoices.go
      stock_transfers.go
      stock_opname.go
      dispatches.go
      recipes.go
      productions.go
      sales.go
      pos_import.go
      branches.go
      accounts.go
      account_adjustments.go
      reports.go
      stats.go
      activity_log.go
      enumerations.go
      invoice_templates.go
      vendors.go
      warehouses.go
      hr_employees.go
      hr_wages.go
      hr_import.go
      attendance.go
      attendance_device.go
      attendance_fingerprint.go
      attendance_settings.go
      performance.go
      leave.go
      kasbon.go
      payroll.go
      payslip.go
    middleware/
      auth.go              # JWT validation, requireAdmin, requireManager
      device_auth.go       # X-Device-Key auth for fingerprint/face devices
      ratelimit.go
    service/               # Business logic (FIFO deduction, CoA updates, unit conversion)
      inventory.go
      accounts.go
      pos_import.go
      hr_employees.go
      hr_import.go
      attendance.go
      attendance_reconcile.go
      attendance_state.go
      fingerprint_parser.go
      kasbon.go
      leave.go
      payroll.go
      payslip.go
      performance.go
  migrations/              # 001_initial.up.sql / .down.sql, etc.
  queries/                 # Raw .sql files that sqlc reads
  sqlc.yaml
  go.mod
```

### Request Flow
```
React page → api.js (Axios + JWT) → Chi router (auth middleware) → handler → sqlc query → pgx → PostgreSQL
```

---

## Architecture: Legacy Node Backend (Reference)

The Express backend lives in `server/index.js` (~3271 lines). All routes, middleware, and business logic are in one file. **Do not add new features here** — all new work goes in the Go backend.

### Key Patterns (replicate in Go)
- **No ORM**: raw parameterized SQL (`$1, $2` placeholders) — same in Go via sqlc
- **Transactions**: `pool.connect()` + BEGIN/COMMIT/ROLLBACK for multi-step ops
- **Activity logging**: every mutation calls `logActivity()` — replicate with `logActivity()` helper in Go
- **FIFO inventory**: lot-based consumption — logic lives in `service/inventory.go`
- **CoA balance updates**: `accounts.balance` updated in real time on every transaction
- **Token blocklist**: `token_blocklist` table + hourly `setInterval` cleanup → Go goroutine with `time.Ticker`

---

## Frontend (React)

### Pages (`client/src/pages/`)
| Page | Route | Admin only |
|---|---|---|
| Dashboard | `/` | No |
| Login | `/login` | — |
| Profile | `/profile` | No |
| ActivityLog | `/activity` | Yes |
| Inventory | `/inventory` | No |
| InventoryForm | `/inventory/new`, `/inventory/:id/edit` | No |
| StockHistoryPage | `/inventory/:id/history` | No |
| Items | `/items` | Yes |
| ItemForm | `/items/new`, `/items/:id/edit` | Yes |
| Invoices | `/invoices` | No |
| InvoiceForm | `/invoices/new`, `/invoices/:id/edit` | No |
| InvoiceDetail | `/invoices/:id` | No |
| InvoiceTemplates | `/invoice-templates` | Yes |
| Vendors | `/vendors` | Yes |
| VendorHistory | `/vendors/:id/history` | Yes |
| Warehouses | `/warehouses` | Yes |
| Branches | `/branches` | Yes |
| Accounts | `/accounts` | Yes |
| Users | `/users` | Yes |
| StockTransfers | `/transfers` | No |
| TransferDetail | `/transfers/group/:id` | No |
| Dispatch | `/dispatch` | No |
| DispatchDetail | `/dispatch/:id` | No |
| StockOpname | `/stock-opname` | No |
| StockOpnameDetail | `/stock-opname/:id` | No |
| Enumerations | `/enumerations` | No |
| Recipes | `/recipes` | No |
| Productions | `/productions` | No |
| Sales | `/sales` | No |
| SalesImport | `/sales/import` | No |
| ExpenseReport | `/expense-report` | Yes |
| ExpenseSummary | `/reports/expense-summary` | Yes |
| DailyReport | `/reports/daily` | Yes |
| FinancialReport | `/reports/financial` | Yes |
| InventoryValueReport | `/reports/inventory-value` | Yes |
| AccountAdjustments | `/account-adjustments` | Yes |
| NonStockItemDetail | `/items/:id/non-stock` | Yes |
| Employees | `/hr/employees` | Manager+ |
| EmployeeForm | `/hr/employees/new`, `/hr/employees/:id/edit` | Manager+ |
| EmployeeDetail | `/hr/employees/:id` | Manager+ |
| Positions | `/hr/positions` | Manager+ |
| WageComponents | `/hr/wage-components` | Manager+ |
| HRImport | `/hr/import` | Manager+ |
| AttendanceDashboard | `/hr/attendance` | Manager+ |
| FingerprintImport | `/hr/attendance/import` | Manager+ |
| AttendanceSettings | `/hr/attendance/settings` | Manager+ |
| PerformanceDashboard | `/hr/performance` | Manager+ |
| PerformancePolicies | `/hr/performance/policies` | Manager+ |
| LeaveRequests | `/hr/leave` | Manager+ |
| KasbonDashboard | `/hr/kasbon` | Manager+ |
| KasbonForm | `/hr/kasbon/new` | Manager+ |
| KasbonDetail | `/hr/kasbon/:id` | Manager+ |
| PayrollDashboard | `/hr/payroll` | Manager+ |
| PayrollPeriodDetail | `/hr/payroll/:id` | Manager+ |
| HRSettings | `/hr/settings` | Manager+ (view/quick-links; company-info mutations still admin only) |

### Components (`client/src/components/`)
- `CurrencyInput.jsx` — IDR currency input with formatting

### API Layer (`client/src/api.js`)
- Axios instance with `Authorization: Bearer <token>` header
- Base URL loaded from `/config.json` at runtime (allows VPS config without rebuild)
- Auto-refresh: on 401, queues in-flight requests, refreshes token, replays queue
- On refresh failure: clears localStorage, redirects to `/login`
- **67+ exported functions** across 23+ domains (auth, users, items, inventory, warehouses, vendors, accounts, stock-history, stock-opname, invoices, transfers, branches, divisions, division-categories, dispatches, sales, pos-import, recipes, productions, invoice-templates, activity-log, stats, reports, account-adjustments, enumerations, hr-employees, hr-positions, hr-wages, hr-import, attendance, performance, leave, kasbon, payroll, payslip, hr-settings)

### Frontend Conventions
- All state is local `useState` — no Redux, Zustand, or Context API
- Data fetched per-component in `useEffect`
- All UI text in **Indonesian**
- Currency: `Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR' })`
- `RequireAuth` HOC wraps all protected routes in `App.jsx`

---

## Database Schema

**PostgreSQL** with UUID primary keys (`gen_random_uuid()`), TIMESTAMPTZ (UTC), BigInt for currency (IDR cents).

### Tables

| Table | Purpose |
|---|---|
| `users` | Auth — username, password_hash, role (admin\|manager\|staff) |
| `token_blocklist` | Revoked JWT jti values with expiry |
| `accounts` | Chart of accounts — hierarchical (parent_id), types: asset/liability/equity/revenue/expense, system-protected |
| `warehouses` | Physical storage locations, linked to an inventory CoA account |
| `branches` | Business branches, each has revenue + expense accounts |
| `divisions` | Sub-units of a branch, each has revenue + expense + discount accounts |
| `division_categories` | Custom expense categories per division |
| `vendors` | Supplier master |
| `items` | Item master — name, code, units (JSONB array of {name, ratio}), is_stock |
| `inventory` | Current stock lots per item+warehouse — quantity, unit_index, value (cents) |
| `stock_history` | Immutable movement log — type, quantity_change, value, source_id/type |
| `stock_transfers` | Warehouse-to-warehouse transfers with group_id for batch |
| `stock_opname` | Physical count header — warehouse, operator |
| `stock_opname_items` | Per-item count results — recorded vs actual, waste_value |
| `dispatches` | Warehouse-to-branch/division dispatch header |
| `dispatch_items` | Items in a dispatch |
| `invoices` | Purchase & expense invoices — payment_status, amount_paid, photo_path |
| `invoice_items` | Line items on an invoice |
| `invoice_templates` | Reusable invoice skeletons |
| `invoice_template_items` | Line items on a template |
| `recipes` | Production recipes — output item, batch_size |
| `recipe_ingredients` | Ingredients per recipe |
| `productions` | Production batches — recipe, warehouse, batches run |
| `sales` | Manual sales records — account, amount, branch/division |
| `pos_imports` | POS Excel import header |
| `pos_import_lines` | Parsed lines per import — account, label, amount, line_type |
| `account_adjustments` | Manual journal entries — account, amount, description |
| `enumerations` | Item breakdowns — source item → output item with value transfer |
| `activity_log` | Audit trail — user, action (CREATE/UPDATE/DELETE), entity_type, description |
| `employees` | HR employee master — code, name, position, branch, bank details, status |
| `positions` | Job positions catalog (Kasir, Koki, etc.) |
| `wage_components` | Catalog of wage component types (allowance/bonus/deduction, fixed/variable) |
| `employee_wage_structures` | Versioned wage structures per employee — base_salary, daily_rate, effective_date |
| `employee_wage_components` | Components attached to a wage structure version — amount |
| `attendance_records` | Daily attendance per employee — check_in/out times, source (manual/fingerprint/face), status, anomaly flags |
| `attendance_devices` | Registered fingerprint/face devices — device_key, name, active |
| `work_schedules` | Weekly work schedule — day_of_week, start_time, end_time, late_grace_minutes |
| `public_holidays` | Public holiday dates — skipped during attendance reconciliation |
| `performance_policies` | Rules that trigger violations — violation_type, threshold_minutes, deduction_points |
| `performance_violations` | Per-employee violations (auto from attendance or manual) |
| `performance_scores` | Monthly rolled-up score per employee (100 − deductions) |
| `leave_types` | Leave type catalog — name, is_paid |
| `leave_requests` | Employee leave requests — dates, day_count, status (pending/approved/rejected/cancelled) |
| `leave_balances` | Annual leave quota and used days per employee |
| `kasbons` | Cash advance records — amount, installments, fund_source_account, status |
| `kasbon_installments` | Per-installment schedule for a kasbon (deducted on payroll close) |
| `payroll_periods` | Payroll period header — month, status (open/closed/paid) |
| `payroll_lines` | Per-employee line within a period — gross, deductions, net, reviewed flag |
| `hr_settings` | Company-level HR config — company_name, logo_path, payslip footer text |

### Key DB Rules
- Hard deletes only (no soft delete)
- `accounts.balance` updated in real time on every financial transaction
- Inventory stored at lowest unit (unit_index = 0) — all conversions use `items.units` JSONB ratios
- FIFO lot consumption: always deduct from oldest `inventory` rows first
- Currency: BigInt cents throughout; never use NUMERIC/FLOAT for money

---

## API Endpoints (96 total)

**Auth** (3): POST /api/auth/login, /logout, /refresh

**Users** (4): GET/POST /api/users, PUT/DELETE /api/users/:id

**Warehouses** (4): CRUD /api/warehouses

**Vendors** (5): CRUD /api/vendors + GET /api/vendors/:id/history

**Items** (7): CRUD /api/items + GET /:id/last-price + GET /:id/history

**Accounts** (4): CRUD /api/accounts

**Inventory** (5): CRUD /api/inventory

**Stock History** (1): GET /api/stock-history/:itemId

**Stock Opname** (3): GET list, GET /:id, POST /api/stock-opname

**Stock Transfers** (3): GET list, POST, GET /group/:groupId — /api/stock-transfers

**Invoices** (8): CRUD /api/invoices + POST /:id/pay + POST/DELETE /:id/photo

**Invoice Templates** (4): CRUD /api/invoice-templates

**Dispatches** (3): GET list, GET /:id, POST /api/dispatches

**Branches** (4): CRUD /api/branches

**Divisions** (4): CRUD /api/divisions

**Division Categories** (3): GET, POST, DELETE /:id — /api/division-categories

**Recipes** (5): CRUD /api/recipes + GET /:id detail

**Productions** (2): GET list, POST /api/productions

**Sales** (3): GET, POST, DELETE /:id — /api/sales

**POS Import** (4): POST /parse, POST /confirm, GET list, DELETE /:id — /api/pos-import

**Account Adjustments** (3): GET, POST, POST /transfer — /api/account-adjustments

**Activity Log** (3): GET, GET /export, DELETE — /api/activity-log

**Enumerations** (3): GET, POST, DELETE /:id — /api/enumerations

**Reports** (4): GET /api/reports/financial, /daily, /inventory-value, /expense-summary

**Stats** (3): GET /api/stats, /stats/daily-sales, /stats/stock-flow

**Expense Report** (1): GET /api/expense-report

**HR Employees & Positions** (11): CRUD /api/hr/employees + photo upload/delete; CRUD /api/hr/positions

**HR Wages** (6): GET/POST /api/hr/wage-components (CRUD); GET/POST /api/hr/employees/:id/wage + GET history

**HR Import** (3): GET template, POST parse, POST confirm — /api/hr/import

**HR Attendance — JWT** (14): GET/PUT /api/hr/attendance; POST reconcile; fingerprint parse/confirm; work-schedules GET/POST; holidays GET/POST/DELETE; devices CRUD

**HR Attendance — Device key** (2): POST /api/hr/attendance/device/event, GET /api/hr/attendance/device/employees

**HR Performance** (9): policies CRUD; GET scores; GET employee performance; POST/DELETE violations; POST evaluate

**HR Leave** (11): leave-types CRUD; leave-requests GET/POST/cancel/approve/reject; employee leave-balance GET/PUT; employee leave-requests GET

**HR Kasbon** (8): GET/POST /api/hr/kasbons; GET/:id; PUT/:id; POST process/cancel/approve/reject

**HR Payroll** (11): periods GET/POST/:id/lines/regenerate-line/close/mark-paid; lines review/unreview/payslip; period payslips ZIP

**HR Settings** (3): GET/PUT /api/hr/settings; POST /api/hr/settings/logo

_(HR total: ~78 endpoints; grand total: ~174)_

---

## Role-Based Access

- **admin**: Full access to all routes including HR settings mutations
- **manager**: Full access to HR module (employees, wages, attendance, performance, leave, kasbon, payroll); exclusive rights to approve/reject kasbon and leave requests; same access as admin on non-HR routes (the `RequireAdmin` middleware also accepts `manager` for backward-compatibility)
- **staff**: Read-only on most resources; blocked from items CRUD, warehouses, vendors, accounts, users, activity log, reports, account adjustments, invoice templates, branches, divisions; no access to HR wage/payroll/kasbon/leave data
- **device-key**: Machine accounts for fingerprint/face attendance devices; authenticated via `X-Device-Key` header (no JWT); access only to `/api/hr/attendance/device/*` endpoints
- Enforced at the route level via `RequireAdmin`, `RequireAdminOrManager`, `RequireManager`, and `DeviceAuth` middleware; also reflected in frontend navigation (`isAdminOrManager` guard on HR nav group)

---

## Environment Variables

`server-go/.env` (or `server/.env` for legacy):
```
DB_HOST=localhost
DB_PORT=5432
DB_NAME=inventory_app
DB_USER=postgres
DB_PASSWORD=seesaw
JWT_SECRET=inventory_secret_change_in_prod
PORT=5000
```

For Go, build DATABASE_URL from these: `postgres://user:pass@host:port/dbname`

---

## Production Deployment (Ubuntu VPS)

- **OS**: Ubuntu 22.04 LTS
- **Process manager**: PM2 (`deploy/ecosystem.config.cjs`) → will point at Go binary after migration
- **Web server**: Nginx (`deploy/nginx.conf`) — reverse proxy `/api/` to port 5000, serves `client/dist/` as static, `/uploads/` as file alias
- **SSL**: Let's Encrypt (Certbot)
- **Backups**: `deploy/backup.sh` — daily pg_dump + uploads tarball, 30-day retention
- **Uploads dir**: `server/uploads/` (invoice photos) — persisted across deploys

After Go migration, PM2 script changes from `./server/index.js` to `./server-go/api`.

---

## Locale & Conventions

- All UI text: **Indonesian** (`id-ID`)
- Currency: IDR, BigInt cents in DB, `Intl.NumberFormat('id-ID', ...)` in UI
- Dates: ISO 8601 in DB, locale-formatted in UI
- Timestamps: `TIMESTAMPTZ` (UTC)
- Primary keys: UUID (`gen_random_uuid()` in SQL, `uuid.New()` in Go)
- Hard deletes only
- Default credentials: `admin` / `admin123`
