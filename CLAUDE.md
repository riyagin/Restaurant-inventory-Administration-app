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
go test ./...            # Run tests — NOTE: integration tests hit the real DB

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

# Seed a full month of attendance covering every anomaly + performance policy,
# then create that month's payroll period and generate its lines
go run ./server-go/cmd/seed-month-attendance -month=2026-07
go run ./server-go/cmd/seed-month-attendance -components=false -payroll=false  # attendance only
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
| DispatchTemplates | `/dispatch-templates` | Yes |
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
| PriceChangeReport | `/reports/price-changes` | Yes |
| UsageTrendReport | `/reports/usage-trend` | Yes |
| AccountAdjustments | `/account-adjustments` | Yes |
| NonStockItemDetail | `/items/history/:id` | Yes |
| StockItemDetail | `/items/stock/:id` | Yes |
| Employees | `/hr/employees` | Manager+ |
| EmployeeForm | `/hr/employees/new`, `/hr/employees/:id/edit` | Manager+ |
| EmployeeDetail | `/hr/employees/:id` | Manager+ |
| Positions | `/hr/positions` | Manager+ |
| WageComponents | `/hr/wage-components` | Manager+ |
| HRImport | `/hr/import` | Manager+ |
| AttendanceDashboard | `/hr/attendance` | Manager+ |
| FingerprintImport | `/hr/attendance/import` | Manager+ |
| AttendanceSettings | `/hr/attendance/settings` | Manager+ |
| FaceDashboard | `/hr/face` | Manager+ |
| FaceUnregistered | `/hr/face/unregistered` | Manager+ |
| PerformanceDashboard | `/hr/performance` | Manager+ |
| PerformancePolicies | `/hr/performance/policies` | Manager+ |
| LeaveRequests | `/hr/leave` | Manager+ |
| KasbonDashboard | `/hr/kasbon` | Manager+ |
| KasbonForm | `/hr/kasbon/new` | Manager+ |
| KasbonDetail | `/hr/kasbon/:id` | Manager+ |
| PayrollDashboard | `/hr/payroll` | Manager+ |
| PayrollPeriodDetail | `/hr/payroll/:id` | Manager+ |
| HRSettings | `/hr/settings` | Manager+ (view/quick-links; company-info mutations still admin only) |
| OnboardingWizard | `/hr/onboarding` | Manager+ (animated multi-step: creates employee + uploads signed docs) |
| DocumentGenerator | `/hr/documents` | Manager+ (generate PKWT/PKWTT/Surat Peringatan/Paklaring as DOCX/PDF) |

### Components (`client/src/components/`)
- `CurrencyInput.jsx` — IDR currency input with formatting

### API Layer (`client/src/api.js`)
- Axios instance with `Authorization: Bearer <token>` header
- Base URL loaded from `/config.json` at runtime (allows VPS config without rebuild)
- Auto-refresh: on 401, queues in-flight requests, refreshes token, replays queue
- On refresh failure: clears localStorage, redirects to `/login`
- **67+ exported functions** across 23+ domains (auth, users, items, inventory, warehouses, vendors, accounts, stock-history, stock-opname, invoices, transfers, branches, divisions, division-categories, dispatches, sales, pos-import, recipes, productions, invoice-templates, dispatch-templates, activity-log, stats, reports, account-adjustments, enumerations, hr-employees, hr-positions, hr-wages, hr-import, attendance, performance, leave, kasbon, payroll, payslip, hr-settings)

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
| `division_categories` | POS revenue labels per division — matched by name against POS import lines to auto-fill revenue accounts. Despite the name, nothing to do with expense |
| `expense_categories` | Subaccount breakdown under a division's expense account — name + the child COA account it posts to |
| `vendors` | Supplier master |
| `items` | Item master — name, code, units (JSONB array of {name, ratio}), is_stock, min_stock (low-stock threshold, in the base unit; 0 = unset) |
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
| `dispatch_templates` | Reusable dispatch skeletons — default warehouse/branch/division/notes |
| `dispatch_template_items` | Items on a dispatch template (item + unit only; quantity is never stored) |
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
| `wage_components` | Catalog of wage component types (allowance/bonus/deduction/daily_allowance, fixed/variable) |
| `employee_wage_structures` | Versioned wage structures per employee — base_salary, daily_rate, effective_date |
| `employee_wage_components` | Components attached to a wage structure version — amount |
| `attendance_records` | Daily attendance per employee — check_in/out times, source (manual/fingerprint/face), status, anomaly flags |
| `attendance_devices` | Registered fingerprint/face devices — device_key, name, active, last_seen_at |
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
| `payroll_postings` | Queue/status of a closed period's automatic ledger posting — status, attempts, last_error, journal_entry_id |
| `hr_settings` | Company-level HR config — company_name, logo_path, payslip footer text |
| `employee_documents` | Signed scans/files filed per employee (onboarding uploads + kept letters) — doc_type, title, file_path, is_signed, uploaded_by. Files live in the uploads dir; served through an authenticated download endpoint, not `/uploads/` |

### Key DB Rules
- Hard deletes only (no soft delete)
- `accounts.balance` updated in real time on every financial transaction
- Accounts payable is split per vendor: each vendor owns a sub-account `Utang Usaha - <name>` (numbers 20101+) under the system parent `Utang Usaha` (20100), linked via `vendors.account_id`. Invoices with no vendor post to `Utang Usaha - Lainnya` (20999). The 20100 parent holds no balance of its own. Resolve the account with `service.VendorPayableAccountID()`, never by looking up 20100 directly
- `items.units` runs **largest → smallest**, so the base unit is the **last** entry (`unit_index = len-1`), not index 0. `perPrev` on entry i says how many of unit i fit in one unit i-1
- Inventory is stored at the base unit, and **nothing in the deduction path converts units** — `service.FIFODeduct` compares the requested quantity directly against `inventory.quantity`. Every lot of an item must therefore share one `unit_index`. Changing an item's units rescales its lots in the same transaction (`handler.rescaleInventoryForUnits`); dropping a unit that stock is held in is rejected outright. `items.min_stock` is denominated in the same base unit and is rescaled by the same factor on a units edit — a threshold left unconverted would silently change what it means. The item list compares it against the summed on-hand quantity to flag low stock
- Purchase invoice lines and dispatch lines are **entered in any unit and converted to the base unit** before touching inventory. The rate is `handler.resolveLineConversion()`: the item's own `perPrev` chain, unless the line carries a one-off `conversion_factor` override (a supplier's dus holding 20 where the catalogue says 24). The override is stored on `invoice_items.conversion_factor` / `dispatch_items.conversion_factor` — **never** written back to `items.units` — and every reversal (invoice edit/delete, dispatch edit/cancel) must unwind at the *stored* factor, not today's catalogue figure. `stock_history` for these paths records base-unit quantities
- A parent account's reported total is **its own balance plus its children's**, not the children alone (`totalOf` in Accounts.jsx, `effectiveBalance` in FinancialReport.jsx). Pure grouping accounts carry zero so they are unaffected, but a division expense account holds both: dispatch usage debits the parent directly, purchases debit its expense-category children. Summing only children silently drops the dispatch spending
- Operational expense is split by `expense_categories`: each row is a named child account under a division's `expense_account_id`, created together with its account in one transaction (`handler/expense_categories.go`). An expense invoice's `expense_category_id` routes the debit there; blank posts to the division parent as before. `invoiceExpenseAccountID` resolves category → division → branch, and edit/delete reversals must pass the **stored** category so the credit lands on the account that was debited. A category whose account has journal lines cannot be deleted
- A branch's slice of the P&L is a question about the **chart of accounts**, not about the journal: `journal_lines` has no branch dimension and does not need one, because every revenue/expense account is branch-scoped by construction. `handler.accountBranchOwnerSQL` resolves it — a branch owns its own revenue/expense accounts, a division's revenue/expense/discount accounts belong to `divisions.branch_id`, and everything hung *underneath* those inherits the owner via a recursive descent (that is how expense categories and `Beban Gaji - <cabang>` land on the right branch without their own link). Accounts no branch owns (e.g. `Pendapatan Ongkir DO` 49900) go to a separate **Umum** column — never spread across branches, so a branch's profit only carries costs it actually owns. `GET /api/reports/profit-loss-by-branch` takes its amounts from the journal, so it includes dispatch usage, payroll, opname write-offs and manual journals; the org-wide `GET /api/reports/financial` still derives its *period* figures from the source tables (sales, pos_import_lines, invoices) and therefore does not see those. The two can disagree — do not treat the older endpoint as the reference when they do
- Every dispatch auto-creates a mirror **expense invoice** (`handler.CreateDispatch`, `invoices.dispatch_id` set, `payment_status = 'dispatched'`) — that invoice *is* how the consumption reaches the division's expense account, and it carries the same items as the dispatch's `stock_history` rows. So the dispatch is visible twice in the source tables at slightly different values (invoice at booked price, `stock_history` at FIFO cost). **Any aggregate must pick exactly one side.** Reading `stock_history WHERE source_type = 'dispatch'` and `invoices WHERE invoice_type = 'expense'` in the same total doubles every dispatch — that was the dashboard chart's bug (`handler.StockFlow`), which reported ~2× spend on every day. Aggregates that read invoices *only* (financial report, expense summary, expense report) are correct as they stand and must keep including dispatch invoices, since that is where dispatch consumption enters. Cash reports are the exception: `handler.CashSummary` filters `dispatch_id IS NULL`, because the cash for those goods already left as a purchase invoice. The journal is safe by construction — the dispatch posts one `service.Post` entry and the auto-invoice posts none. Per-item views of *purchase* history are the other side of the same coin: an `invoice_items` row on a dispatch's mirror invoice is stock leaving at cost, not a purchase, so `GetItemLastPrice`, `GetItemPurchaseHistory` and the three `GetItemPrice*` rollups all filter `dispatch_id IS NULL` — without it the item page lists dispatches as purchases (badged with the raw sentinel "dispatched") and the price min/max/last mixes booked cost into supplier prices. Dispatch lines are reported separately by `GetItemDispatchHistory`
- FIFO lot consumption: always deduct from oldest `inventory` rows first
- Every movement of stock leaves a `stock_history` row, including manual lot create/edit/delete — the item history reconstructs on-hand from these, so a change without one makes the quantity disagree with the sum of its own history. Manual lots use type `manual_in` / `manual_out` (never `purchase`: there is no invoice, vendor or price behind them, and the item detail page groups its flow breakdown by this label) with `source_type = 'inventory'`. Use `handler.recordManualStockMove`
- Nothing writes `accounts.balance` outside `service.Post`, which refuses an unbalanced entry and routes unresolvable legs to the suspense account. `GET /api/accounts/trial-balance` reports per-account drift between the cached balance and the journal — a healthy system returns an empty list
- `go test ./...` runs the handler tests against the **real** database, so fixtures leave rows in the live Chart of Accounts. A fixture's own `t.Cleanup` cannot delete an account once a journal entry references it (FK, and the error is swallowed), which is how hundreds of fake accounts carrying real balances once accumulated. `handler.TestMain` sweeps them after each run: it deletes fixture-shaped rows (8-hex suffix; accounts additionally must have no COA number, no parent and no `is_system`), removes the journal entries touching them, rolls back the balance those entries gave any surviving shared account, and only commits when the equation and per-account drift are both still 0. Name new fixture accounts `<Prefix> <8 hex>` so the sweep can find them
- Stock opname corrections are append-only: a correction is a new `stock_opname_items` row with `is_correction = true`, never an edit of the original. An item counted down to 0 loses its inventory lots and disappears from the warehouse, so the correction UI lists the union of current lots **and** the opname's own items (the zeroed ones at system qty 0) — otherwise the rows most likely to need fixing are the ones you cannot reach. A correction that puts stock back is valued at the cost this opname wrote off (`handler.opnameNetWaste`), not at the latest purchase price, and carries a **negative** `waste_value` so the opname's net loss drops by what was restored. That is what keeps the write-off and its reversal from leaving a residue in `Selisih Persediaan`, and what stops a second correction reversing the same value twice. Only quantity beyond what the opname removed is a genuine surplus, priced from the last purchase
- Currency: BigInt cents throughout; never use NUMERIC/FLOAT for money
- Closing a payroll period does NOT write the ledger inline: it queues a `payroll_postings` row in the same transaction, and `service.PayrollPoster` writes one balanced journal entry per period in the background (`Beban Gaji - <cabang>` debited net + kasbon per branch, Kas credited net, Piutang Karyawan credited the kasbon repayments). The queue row is the durability guarantee — a restart mid-post is retried by the startup/5-min sweep, capped at `MaxPostingAttempts`. Never post payroll to the ledger from the frontend, and never re-add a manual "process to accounting" step
- `wage_components.type = 'daily_allowance'` (e.g. uang makan) is disbursed manually in cash each day, outside payroll. It is still snapshotted into `payroll_line_components` (honouring `calc_method`, so a `per_present_day` rate resolves to rate × present days) and printed on the payslip as information, but it must never enter `allowance_total` / `gross_pay` / `net_pay` — counting it would pay it twice. Use the `service.ComponentType*` constants when switching on the type

---

## API Endpoints (96 total)

**Auth** (3): POST /api/auth/login, /logout, /refresh

**Users** (4): GET/POST /api/users, PUT/DELETE /api/users/:id

**Warehouses** (4): CRUD /api/warehouses

**Vendors** (5): CRUD /api/vendors + GET /api/vendors/:id/history (vendor + invoices it appears on + per-item purchase breakdown with latest/avg price + payable summary — powers the vendor activity page)

**Items** (10): CRUD /api/items (list rows carry `stock_quantity` + `is_low_stock`; `?low_stock=true` keeps only the ones under their threshold) + GET /:id/last-price + GET /:id/history (purchase invoice lines) + GET /:id/stock-history + GET /:id/stock-detail (warehouse balances, purchases, dispatch usage, monthly/per-type flow — powers the stock item history page) + GET /:id/price-history (purchase price rolled up per unit, per vendor and per month — powers the "Riwayat Harga" tab on both item detail pages; optional `?from=`/`?to=` bound every rollup, and the tab defaults to the last 30 days, but the `range` field in the response reports the item's full data extent *outside* that window so the UI can offer to widen it)

**Accounts** (4): CRUD /api/accounts

**Inventory** (5): CRUD /api/inventory

**Stock History** (1): GET /api/stock-history/:itemId

**Stock Opname** (3): GET list, GET /:id, POST /api/stock-opname

**Stock Transfers** (3): GET list, POST, GET /group/:groupId — /api/stock-transfers

**Invoices** (8): CRUD /api/invoices + POST /:id/pay + POST/DELETE /:id/photo — the list takes `?status=` (show only one) *and* `?exclude_status=` (CSV, hide several: the page's status picker works by exclusion so dispatch mirror invoices and cancellations can be hidden while browsing everything else)

**Invoice Templates** (4): CRUD /api/invoice-templates

**Dispatch Templates** (5): GET list, GET /:id, POST, PUT /:id, DELETE /:id — /api/dispatch-templates

**Dispatches** (3): GET list, GET /:id, POST /api/dispatches

**Branches** (4): CRUD /api/branches

**Divisions** (4): CRUD /api/divisions

**Division Categories** (3): GET, POST, DELETE /:id — /api/division-categories (POS revenue labels)

**Expense Categories** (3): GET, POST, DELETE /:id — /api/expense-categories (division expense subaccounts)

**Recipes** (5): CRUD /api/recipes + GET /:id detail

**Productions** (2): GET list, POST /api/productions

**Sales** (3): GET, POST, DELETE /:id — /api/sales

**POS Import** (4): POST /parse, POST /confirm, GET list, DELETE /:id — /api/pos-import

**Account Adjustments** (3): GET, POST, POST /transfer — /api/account-adjustments

**Activity Log** (3): GET, GET /export, DELETE — /api/activity-log

**Enumerations** (3): GET, POST, DELETE /:id — /api/enumerations

**Reports** (7): GET /api/reports/financial, /daily, /inventory-value, /expense-summary + /profit-loss-by-branch (P&L split into one column per branch — see the rule below) + /price-changes (weighted fixed-basket purchase-price index per week over a range, plus per item/unit first-vs-last price and rupiah impact) + /usage-trend (daily item usage over a range: stock items via dispatch stock_history, non-stock items via invoice lines, with start-vs-end percentage changes)

**Stats** (3): GET /api/stats, /stats/daily-sales, /stats/stock-flow

**Expense Report** (1): GET /api/expense-report

**HR Employees & Positions** (11): CRUD /api/hr/employees + photo upload/delete; CRUD /api/hr/positions

**HR Wages** (6): GET/POST /api/hr/wage-components (CRUD); GET/POST /api/hr/employees/:id/wage + GET history

**HR Import** (3): GET template, POST parse, POST confirm — /api/hr/import

**HR Attendance — JWT** (15): GET/PUT /api/hr/attendance; POST reconcile; GET face/overview (enrollment coverage + device fleet health); fingerprint parse/confirm; work-schedules GET/POST; holidays GET/POST/DELETE; devices CRUD

**HR Attendance — Device key** (4): POST /api/hr/attendance/device/event, GET /api/hr/attendance/device/employees (roster + stored face embeddings), POST /api/hr/attendance/device/face (upload enrollment), POST /api/hr/attendance/device/face/sync (diff device's local face store vs server → to_download/to_upload/to_delete/to_reenroll)

**HR Performance** (9): policies CRUD; GET scores; GET employee performance; POST/DELETE violations; POST evaluate

**HR Leave** (11): leave-types CRUD; leave-requests GET/POST/cancel/approve/reject; employee leave-balance GET/PUT; employee leave-requests GET

**HR Kasbon** (8): GET/POST /api/hr/kasbons; GET/:id; PUT/:id; POST process/cancel/approve/reject

**HR Payroll** (12): periods GET/POST/:id/lines/regenerate-line/close/mark-paid/post-accounting; lines review/unreview/payslip; period payslips (single PDF, two slips per A4-landscape page — cut down the middle for two A5-portrait slips)

**HR Settings** (3): GET/PUT /api/hr/settings; POST /api/hr/settings/logo

**HR Documents** (5): POST /api/hr/documents/generate?format=docx|pdf (stateless render of PKWT/PKWTT/Surat Peringatan/Paklaring, legally-compliant Indonesian templates — see `service/hrdoc*.go`); GET/POST /api/hr/employees/:id/documents (list/upload signed docs); GET /api/hr/employees/:id/documents/:docId/download; DELETE /api/hr/employees/:id/documents/:docId — admin/manager

_(HR total: ~78 endpoints; grand total: ~175)_

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
