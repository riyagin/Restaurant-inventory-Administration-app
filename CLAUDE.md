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
| Dashboard | `/` | No (operational overview; `staff` gets StaffDashboard and `hr` gets HRDashboard instead) |
| StaffDashboard | `/` for the `staff` role | Shortcuts (purchasing, transfer, dispatch) + the pending daily-task board |
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
| Templates | `/templates?tab=pembelanjaan\|invoice\|pengiriman` | Yes |
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
| DailyPurchases | `/daily-purchases` | No (cancel: Yes) |
| DailyPurchaseForm | `/daily-purchases/new` | No |
| DailyPurchaseTemplates | `/daily-purchase-templates` | No |
| PettyCash | `/petty-cash` | No (recording counts: Yes) |
| CashTracking | `/cash-tracking` | No (recording counts: Yes) |
| Setoran | `/setoran` | No (recording: Yes) |
| ExpenseReport | `/expense-report` | Yes |
| ExpenseSummary | `/reports/expense-summary` | Yes |
| DailyReport | `/reports/daily` | Yes |
| FinancialReport | `/reports/financial` | Yes |
| FinancialStatement | `/reports/statement` | Yes — **not in the menu**: reached from the "Dokumen / Cetak" button on FinancialReport, which passes its period as `?start_date=&end_date=` |
| ProfitLossComparison | `/reports/profit-loss` | Yes (P&L with one column per month or year) |
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
| HRSettings | `/hr/settings` | HR roles (company data, signatory, document numbering, payslip/attendance config + quick links) |
| OnboardingWizard | `/hr/onboarding` | HR roles (animated multi-step: creates employee + uploads signed docs) |
| DocumentGenerator | `/hr/documents` | HR roles (generate PKWT/PKWTT/Surat Peringatan/Paklaring as DOCX/PDF) |
| HRDashboard | `/` for the `hr` role | Shortcut map of every HR page — replaces the operational dashboard |
| StaffKPI | `/hr/kpi` | HR roles (daily-task definitions + staff KPIs + monthly scorecards) |

### Templates page
All three template kinds — Pembelanjaan Harian, Invoice, Pengiriman — are managed on **one** page (`pages/Templates.jsx`, tabs in `pages/templates/`), not three. They were three pages under two different menus, which fit the API layout rather than the way they are used: a template is set up once and revised occasionally, and the same shopping run often exists as both a daily-purchase and an invoice template. The old paths (`/invoice-templates`, `/dispatch-templates`, `/daily-purchase-templates`) survive as redirects to `/templates?tab=…`, since they are linked from the pages that consume the templates.

The three lists stay three tables — an invoice template carries a type, a dispatch template a destination — but the chrome (`templates/shared.jsx`) and the master data are shared: items, warehouses, vendors and branches are fetched **once** by the shell and passed down, and each panel filters `is_stock` locally instead of issuing its own `getItems({is_stock})` calls. All three panels stay mounted (inactive ones `hidden`), so peeking at another tab cannot discard a half-filled form and the per-tab counts are real rather than "loaded so far".

### Components (`client/src/components/`)
- `CurrencyInput.jsx` — IDR currency input with formatting
- `Icon.jsx` — inline SVG glyphs (Lucide geometry, ISC) sized and coloured by prop/`currentColor`; used by the onboarding wizard. Prefer these over emoji, which render at platform-dependent sizes and cannot take a colour
- `ItemPriceBreakdown.jsx`, `UnitConversion.jsx`
- `SearchSelect.jsx` — type-to-filter picker (keyboard-navigable combobox) for lists too long for a `<select>`, e.g. the employee roster on the document generator
- `NotificationBell.jsx` — navbar bell (sits left of the user block); polls `/api/notifications` every 2 min and on every navigation

### HR navigation
The HR menu is grouped by **how often you touch it**, not by subsystem: Karyawan / Harian / Berkala / Pengaturan. It had grown past twenty flat entries, and cadence is the axis that predicts what someone is looking for when they open it. The HR-only role gets those four as its whole navbar; the operational navbar flattens them into one dropdown but keeps the group names as headings. `HRDashboard` uses the same four groups, so the vocabulary matches wherever you are.

### API Layer (`client/src/api.js`)
- Axios instance with `Authorization: Bearer <token>` header
- Base URL loaded from `/config.json` at runtime (allows VPS config without rebuild)
- Auto-refresh: on 401, queues in-flight requests, refreshes token, replays queue
- On refresh failure: clears localStorage, redirects to `/login`
- `getAllEmployees()` pages through the roster: `GET /api/hr/employees` is paginated and **caps `limit` at 100**, so a single call silently truncates. Any picker that needs the whole roster must use it — a plain `getEmployees()` returns only the first 25 by default. Note the response is an envelope, `{data, page, limit, total}`, not a bare array
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
| `users` | Auth — username, password_hash, role (superuser\|admin\|manager\|staff\|hr\|store_manager) |
| `token_blocklist` | Revoked JWT jti values with expiry |
| `accounts` | Chart of accounts — hierarchical (parent_id), types: asset/liability/equity/revenue/expense, system-protected |
| `warehouses` | Physical storage locations, linked to an inventory CoA account |
| `branches` | Business branches, each has revenue + expense + petty cash accounts |
| `divisions` | Sub-units of a branch, each has revenue + expense + discount accounts |
| `division_categories` | POS revenue labels per division — matched by name against POS import lines to auto-fill revenue accounts. Despite the name, nothing to do with expense |
| `expense_categories` | Subaccount breakdown under a division's expense account — name + the child COA account it posts to |
| `vendors` | Supplier master |
| `vendor_bank_accounts` | Transfer destinations per vendor — bank_name, account_number, account_holder, bank_branch, is_primary, note. A vendor may hold any number; a partial unique index enforces at most one default. **Not** part of the chart of accounts: these are payment instructions, and a vendor's only accounting object remains its payable sub-account |
| `daily_purchases` | Pembelanjaan Harian header — branch, division, warehouse, expense_category, petty_cash_account_id (frozen), vendor, total_amount, status (posted/cancelled) |
| `daily_purchase_items` | Lines — item_id nullable (free-text lines are normal here), quantity, unit_index, price, conversion_factor |
| `petty_cash_counts` | One row per branch per date — opening/closing counts, the expected closing frozen at the moment of counting, variance and its mandatory note |
| `cash_deposits` | Setoran — branch cash → owner's bank, and petty cash top-ups. movement_type, from/to account, amount, reference, handed_to, status |
| `daily_purchase_templates` | Skeleton of a repeating shopping run — branch/division/warehouse/vendor defaults. **No quantities, no prices**, same rule as dispatch_templates |
| `daily_purchase_template_items` | Template lines — item (nullable, free-text allowed) + unit + sort order |
| `cash_day_counts` | Pelacakan Kas — the branch **till**, one row per branch per date. Opening/closing counts plus the frozen income and outgoing figures they were measured against |
| `inventory_lot_consumptions` | Which lot each deduction came out of — lot, quantity, FIFO value, source. Written by `service.FIFODeduct` |
| `pos_settlement_by_branch` | **View**, not a table. POS payment lines (`line_type = 'cash'`) resolved to a branch through the same recursive chart-of-accounts walk the branch P&L uses, split by payment method |
| `items` | Item master — name, code, units (JSONB array of {name, ratio}), is_stock, min_stock (low-stock threshold, in the base unit; 0 = unset) |
| `inventory` | Current stock lots per item+warehouse — quantity, unit_index, value (cents) |
| `stock_history` | Immutable movement log — type, quantity_change, value, source_id/type |
| `stock_transfers` | Warehouse-to-warehouse transfers with group_id for batch |
| `stock_opname` | Physical count header — warehouse, operator |
| `stock_opname_items` | Per-item count results — recorded vs actual, waste_value |
| `dispatches` | Warehouse-to-branch/division dispatch header |
| `dispatch_items` | Items in a dispatch |
| `invoices` | Purchase & expense invoices — payment_status, amount_paid, payment_date (latest settlement), photo_path |
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
| `daily_task_definitions` | Recurring back-office duties — task_type (purchasing/pos_import/manual), scope (global/per_branch), target_role, link_path, starts_on, due_offset_days, grace_days |
| `daily_task_completions` | Manual-type completions only; derived types are answered by the data |
| `staff_kpis` | Staff targets measured against a daily task — metric (completion_rate/same_day_rate/completed_count), target_value, weight |
| `hr_contract_templates` | Reusable PKWT/PKWTT condition presets — position, place of work, wage, job description, contract_months. Company-level fields are deliberately absent; they live in `hr_settings` |
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
| `hr_settings` | Company-level HR config (singleton, id=1) — company_name, address, logo_path, payslip footer, absence grace days, **plus the document-generation defaults**: company_phone/email/city, signatory_name/position/national_id, doc_number_format + doc_number_counter, doc_working_hours, doc_payment_info, doc_probation_months |
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
- A branch's slice of the P&L is a question about the **chart of accounts**, not about the journal: `journal_lines` has no branch dimension and does not need one, because every revenue/expense account is branch-scoped by construction. `handler.accountBranchOwnerSQL` resolves it — a branch owns its own revenue/expense accounts, a division's revenue/expense/discount accounts belong to `divisions.branch_id`, and everything hung *underneath* those inherits the owner via a recursive descent (that is how expense categories and `Beban Gaji - <cabang>` land on the right branch without their own link). Accounts no branch owns (e.g. `Pendapatan Ongkir DO` 49900) go to a separate **Umum** column — never spread across branches, so a branch's profit only carries costs it actually owns. **Both** `GET /api/reports/profit-loss-by-branch` and the org-wide `GET /api/reports/financial` take their period amounts from the journal, through the one shared `handler.plActivityByAccount` — so the branch columns sum to the combined figure by construction, and ticking "Rincian per cabang" re-slices the same money instead of re-deriving it. `financial` used to sum the source tables (sales, pos_import_lines, invoices, account_adjustments) for its period figures, and the two disagreed on the same period: that path misses everything reaching a P&L account without passing through those tables (payroll, Pembelanjaan Harian, opname write-offs), values dispatch consumption at the mirror invoice's booked price rather than FIFO cost, and never filtered cancelled invoices out of its expense total. **Do not reintroduce a second definition of period P&L activity** — `handler.TestFinancialAgreesWithBranchSplit` fails per-account if either report grows its own. Note this is only about the *income statement*: `financial`'s balance-sheet figures are still `accounts.balance` (correct — nothing writes it outside `service.Post`), and `total_adjustments` is still an informational column read from `account_adjustments`
- The **periodic** P&L (`GET /api/reports/profit-loss-periodic`, page `/reports/profit-loss`) is that same money bucketed into columns, and it composes the shared `handler.plAmountSQL` / `plActivityFromSQL` fragments rather than writing its own aggregate — the third spelling of "what a P&L account did" would have been undetectable from any single report's totals, since each still adds up internally while disagreeing with the others. `handler.TestPeriodicBucketsSumToPeriodTotal` asserts per account that the buckets sum to the unbucketed figure over the same range. Columns are bounded on purpose: 6 months, year-to-date, or 2-5 whole years (`maxPeriodColumns` = 12)
- **Divisions are compared across branches by name.** Each branch owns its own `divisions` rows, so "Dapur" is several ids sharing a name, and the periodic report's `division` parameter takes the *name*. `handler.accountDivisionOwnerSQL` is the division-level twin of `accountBranchOwnerSQL` — same recursive descent from a division's revenue/expense/discount accounts, so expense categories inherit their division. A branch with no division of that name produces **no group at all** and is listed in `excluded_branches`: a column of zeros for a division that does not exist reads exactly like a bad month. Note a branch's *own* accounts belong to no division and correctly drop out of a division-filtered view, and no **Umum** group can appear there either
- Every dispatch auto-creates a mirror **expense invoice** (`handler.CreateDispatch`, `invoices.dispatch_id` set, `payment_status = 'dispatched'`) — that invoice *is* how the consumption reaches the division's expense account, and it carries the same items as the dispatch's `stock_history` rows. So the dispatch is visible twice in the source tables at slightly different values (invoice at booked price, `stock_history` at FIFO cost). **Any aggregate must pick exactly one side.** Reading `stock_history WHERE source_type = 'dispatch'` and `invoices WHERE invoice_type = 'expense'` in the same total doubles every dispatch — that was the dashboard chart's bug (`handler.StockFlow`), which reported ~2× spend on every day. Aggregates that read invoices *only* (financial report, expense summary, expense report) are correct as they stand and must keep including dispatch invoices, since that is where dispatch consumption enters. Cash reports are the exception: `handler.CashSummary` filters `dispatch_id IS NULL`, because the cash for those goods already left as a purchase invoice. The journal is safe by construction — the dispatch posts one `service.Post` entry and the auto-invoice posts none. Per-item views of *purchase* history are the other side of the same coin: an `invoice_items` row on a dispatch's mirror invoice is stock leaving at cost, not a purchase, so `GetItemLastPrice`, `GetItemPurchaseHistory` and the three `GetItemPrice*` rollups all filter `dispatch_id IS NULL` — without it the item page lists dispatches as purchases (badged with the raw sentinel "dispatched") and the price min/max/last mixes booked cost into supplier prices. Dispatch lines are reported separately by `GetItemDispatchHistory`
- **A payment is dated when the cash moved, not when it was keyed in.** `POST /api/invoices/:id/pay` takes an optional `payment_date` (defaulting to today, future dates refused) and that date becomes the settlement entry's journal date — an invoice paid on Friday and recorded on Monday lands in Friday's books. `invoices.payment_date` caches the *latest* settlement so the list and detail can show it without a join; the per-payment history stays where it already was, one `journal_entries` row per payment (`source_type = 'invoice_payment'`), which is what a partial invoice's earlier instalments are read from. Creating an invoice already marked paid/partial also accepts `payment_date`, defaulting to the invoice date — its pre-existing meaning
- FIFO lot consumption: always deduct from oldest `inventory` rows first
- Every movement of stock leaves a `stock_history` row, including manual lot create/edit/delete — the item history reconstructs on-hand from these, so a change without one makes the quantity disagree with the sum of its own history. Manual lots use type `manual_in` / `manual_out` (never `purchase`: there is no invoice, vendor or price behind them, and the item detail page groups its flow breakdown by this label) with `source_type = 'inventory'`. Use `handler.recordManualStockMove`
- Every branch owns a **"Kas Kecil - &lt;Cabang&gt;"** asset account, numbered 11100-11199 under the system parent `Kas dan Setara Kas` (11000), created together with the branch in `handler.createBranchPettyCashAccount` and backfilled for existing branches by migration 062. The range is not arbitrary: `GetNextInventoryAccountNumber` allocates warehouse inventory accounts as `MAX(account_number) + 1` over the whole 11000-19999 span and those already sit at 12001+, so petty cash must stay *below* that maximum or every future warehouse account silently jumps range. `branches.petty_cash_account_id` is the link; deleting a branch whose box is non-zero is refused rather than stranding the money in an unlinked account
- **Pembelanjaan Harian is its own table, not an invoice with a flag.** It is a purchase mechanically — FIFO lots, `stock_history`, unit conversion, expense-category routing all behave exactly as on an invoice — but settlement differs: cash changes hands at the stall, so there is no payable, no due date, no payment status, and it must never touch `Utang Usaha`. One entry does the lot: `Dr Persediaan - <Gudang>` (stock lines) + `Dr Beban - <Divisi>/<Kategori>` (non-stock lines) `Cr Kas Kecil - <Cabang>`. The credit is what makes the day's count checkable. **The cost of the separate table is that invoice-derived aggregates do not see these rows**: `GetItemPurchaseHistory`, `GetItemLastPrice`, the price-history rollups, the financial report's period figures and the expense report all read `invoice_items` / `invoices` and therefore exclude daily purchases. Stock quantities and the journal *are* correct, because both go through the shared services. Any new aggregate that means "everything we bought" must union both sources — and, per the dispatch rule above, must still pick exactly one side per source
- The petty cash account credited is **frozen onto `daily_purchases.petty_cash_account_id`**, not looked up from the branch at read time. Repointing a branch at a new box must not rewrite where last month's money came from. The same applies to `daily_purchase_items.conversion_factor`: a cancellation unwinds at the factor that was booked, never today's catalogue figure
- Recording a spend is **never blocked for want of a recorded top-up**. The money left the box whether or not Setoran knows about it, so refusing would make the spend invisible — strictly worse than a negative `Kas Kecil` balance, which is itself the signal that a top-up is missing. The create response carries the resulting balance so the UI can say so. Setoran is the opposite case and *does* check the source balance: it is recorded by the person moving the money, so getting it right at entry is both possible and cheaper
- **A petty cash count never posts to the ledger.** It is an observation of a physical box; letting a typo write a journal entry would mean a miscount could rewrite the books. `expected_closing` and `variance` are computed at the moment the closing is recorded and frozen onto the row — recomputing them on read would let a spend backdated into a closed day quietly change a variance someone has already signed off on. The board shows both the frozen pair and a live recomputation, and the difference between them is worth seeing. A non-zero variance cannot be saved without a note (enforced by a table CHECK, not just the handler). Re-recording an opening clears that day's closing, because a variance measured against the old opening is now meaningless
- **Kas Kecil and Pelacakan Kas are two different piles of money**, which is why they are two pages and two tables. `Kas Kecil` is a float the branch *buys* from — filled by top-up, emptied by Pembelanjaan Harian, nothing is ever sold out of it. The *till* (`cash_day_counts`) is filled by customers paying cash and emptied by setoran and by invoices settled in notes. Merging them would let a shortfall in one be covered by a surplus in the other, which is precisely the error the counts exist to catch. So `SumCashInvoicesForBranchDay` must never include Pembelanjaan Harian, and the petty cash day must never include POS takings
- Which accounts count as physical cash is `accounts.is_cash_drawer`, **not** a hard-coded account number. A POS import settles into one account per payment method (Cash, EDC BCA, TRANSFER BCA, GoFood, ShopeeFood); only the flagged ones are reconciled against a drawer count, and the rest are reported so the day's takings can be checked against what the EDC and the platforms actually settled. A second till needs a checkbox, not a deploy
- `pos_imports` carries no branch column, so `pos_settlement_by_branch` resolves one by walking down the chart of accounts from each branch's own accounts — the same recursive descent as `handler.accountBranchOwnerSQL` and `service.posImportDoneByBranch`. It has to be done from the **revenue** lines: a payment line posts to a shared account ("Cash" 11001) that no branch owns, so attributing it directly would put every branch's cash in one bucket
- Cash paid out of a drawer is matched on `COALESCE(invoices.payment_date, invoices.date)`, not the invoice date. The till was emptied on the day the notes changed hands; an invoice dated last Friday and settled today belongs to today's count
- **A depleted inventory lot is kept, not deleted.** `service.FIFODeduct` stamps `inventory.depleted_at` and zeroes the row, and writes one `inventory_lot_consumptions` row per lot it took from. Deleting the lot used to destroy the only evidence the delivery ever existed, which is why per-lot history was impossible. `GetInventoryLotsForFIFO` filters `quantity > 0`, so the surviving rows are invisible to FIFO itself. `LotSource` is passed **variadically** so the fourteen pre-existing call sites still compile and the paths with no meaningful user (an invoice being edited, a purchase being cancelled) can omit it — the consumption row is still written either way, so a lot's arithmetic always closes. Lots consumed before migration 068 cannot be reconstructed and the lot page says so rather than showing an empty list
- The inventory list shows **zero-stock rows by default** (`include_empty`, default on): depleted lots, and stocked items with no lot at all, which carry a NULL lot id and support no actions. Ordering is server-side through a closed `sort`/`dir` CASE ladder because the list is paginated — sorting one page of 25 sorts nothing
- The `petty_cash` daily task is satisfied only when **both** ends are counted — an opening with no closing is precisely the omission the duty exists to catch — and is attributed to whoever recorded the closing, since that is the person who owns the variance
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

**Vendors** (9): CRUD /api/vendors + GET /api/vendors/:id/history (vendor + invoices it appears on + per-item purchase breakdown with latest/avg price + payable summary — powers the vendor activity page) + CRUD /api/vendors/:id/bank-accounts (transfer destinations; the list endpoint returns each vendor's accounts inline so the vendor page needs one round trip)

**Pembelanjaan Harian** (8): GET /api/daily-purchases (`?branch_id=&from=&to=&status=`), GET /:id, POST (admin + staff), POST /:id/cancel (admin) + CRUD /api/daily-purchase-templates

**Pelacakan Kas** (7): GET /api/cash-tracking?date= (per branch: counts, POS settlement split by payment method, live expected/variance), GET /history, GET /settlement (the payment layer on its own), GET+PUT /drawer-accounts, POST /opening, POST /closing (writes admin only)

**Kas Kecil** (5): GET /api/petty-cash?date= (one row per branch: counts, the movements that should explain them, live expected/variance), GET /history, GET /accounts, POST /opening, POST /closing (both admin only)

**Setoran** (3): GET /api/cash-deposits (`?branch_id=&from=&to=&type=&status=`), POST, POST /:id/cancel (both admin only)

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

**Reports** (8): GET /api/reports/financial, /daily, /inventory-value, /expense-summary + /profit-loss-by-branch (P&L split into one column per branch — see the rule below) + /profit-loss-periodic (the same P&L with one column per **period**: `granularity=month|year`, `range=6m|ytd`, `years=2..5`, optional `branch_id` and `division` — see the rule below) + /price-changes (weighted fixed-basket purchase-price index per week over a range, plus per item/unit first-vs-last price and rupiah impact) + /usage-trend (daily item usage over a range: stock items via dispatch stock_history, non-stock items via invoice lines, with start-vs-end percentage changes)

**Stats** (3): GET /api/stats, /stats/daily-sales, /stats/stock-flow

**Daily Tasks** (7): GET /api/tasks/daily (the board over a date window), /api/tasks/pending, /api/tasks/definitions; POST /api/tasks/definitions, /api/tasks/daily/complete (manual types only); PUT/DELETE /api/tasks/definitions/:id — core roles only

**Notifications** (1): GET /api/notifications — role-assembled feed behind the navbar bell

**HR KPI** (5): GET/POST /api/hr/kpi, PUT/DELETE /api/hr/kpi/:id, GET /api/hr/kpi/scores?month=YYYY-MM

**HR Contract Templates** (4): GET/POST /api/hr/contract-templates (`?type=pkwt|pkwtt`; a template with a blank `doc_type` is offered for both), PUT/DELETE /api/hr/contract-templates/:id

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

**HR Settings** (3): GET/PUT /api/hr/settings; POST /api/hr/settings/logo — HR roles (this is where the company data feeding payslips *and* generated documents lives)

**HR Documents** (7): POST /api/hr/documents/generate?format=docx|pdf (render of PKWT/PKWTT/Surat Peringatan/Paklaring, legally-compliant Indonesian templates — see `service/hrdoc*.go`); GET/POST /api/hr/documents/next-number (peek / reserve the running letter number); GET/POST /api/hr/employees/:id/documents (list/upload signed docs); GET /api/hr/employees/:id/documents/:docId/download; DELETE /api/hr/employees/:id/documents/:docId

_(HR total: ~80 endpoints; grand total: ~177)_

### Daily tasks, notifications, and staff KPIs
The back office owns a few duties that must happen every day — record the day's purchasing, import each branch's POS sales — and they are **shared**: whoever gets to one does it for everyone, so a task belongs to the organisation and a date, not a person.

**Completion is derived, never ticked.** A purchase invoice dated D (type `purchase`, not cancelled) satisfies D's purchasing task; a POS import satisfies branch B's task for its date. `pos_imports` carries no `branch_id` — the branch is implied by the accounts its lines post to — so `service.posImportDoneByBranch` resolves it with the same recursive walk down the chart of accounts that `handler.accountBranchOwnerSQL` uses for the branch P&L. Deriving is what keeps the board honest: it cannot report "done" when the work wasn't, needs no backfilling, and scores existing history the first time it is read. Only `manual` definitions write `daily_task_completions`; `service.CompleteManualTask` **rejects** a tick on a derived type rather than letting it contradict the data.

**`due_offset_days` is not the same as `grace_days`.** A duty is actionable from `task_date + due_offset_days`, and overdue once `today > task_date + due_offset_days + grace_days`. POS import carries an offset of 1 because the sales data only lands the following morning — so today's board shows *yesterday's* POS imports as due, and today's are not listed at all. Instances that are not yet actionable are omitted entirely rather than shown pending: nagging about work that cannot be done is noise, and counting it as missed would drag the completion rate down for a day nobody could have worked.

`scope = per_branch` expands to one instance per branch at query time, so opening a branch immediately carries its duties. `starts_on` is the first date a duty applies — NULL for the seeded ones (purchasing and POS import have always been expected, so history is judged), and defaulted to today for a duty someone invents now, since nobody should log in to a backlog of failures for a rule that did not exist yesterday.

`invoices.created_by` was added by migration 058 for per-person attribution and backfilled from `activity_log`; note the comparison is **case-insensitive** because 048 normalised the log's action casing to lowercase. KPIs split deliberately between team and personal metrics: `completion_rate` measures the desk (identical for everyone — pinning a shared missed day on one individual would be fiction), while `same_day_rate` and `completed_count` are personal. A person is reached through `employees.user_id`; unlinked employees simply have no scorecard.

The bell (`GET /api/notifications`) is assembled per role: operational roles get overdue tasks and low stock, managers additionally get the approval queues, HR roles get expiring contracts and unreviewed payroll lines. The badge counts `alert` and `warn` only. `RestrictHRRole` allowlists `/api/notifications` (so the HR role gets its own queues) but not `/api/tasks/*`.

### HR document generation
Reusable **contract condition templates** (`hr_contract_templates`) preset the *ketentuan* half of a PKWT/PKWTT — position, place of work, wage, job description, term length — so hiring several people into the same role doesn't mean retyping terms that then disagree. Applying one fills the form and leaves every field editable. The term is stored as `contract_months`, not an end date, because a template is reused months apart from whatever start date the contract gets. Company-level values (letterhead, signatory, working hours, payment wording) are deliberately **not** duplicated here — they come from `hr_settings`, and a second copy is how the two would drift.

Only per-letter fields live on the generator form. Everything that belongs to the *company* — letterhead, contact details, signing city, signatory, standard working hours, payment wording, PKWTT probation length — is configured once in `hr_settings` and merged server-side by `handler.applySettingsDefaults`; a value the request does send still wins, so one-off overrides remain possible. Letter numbers come from `doc_number_format` + `doc_number_counter`: `GET /api/hr/documents/next-number` previews without advancing, `POST` claims the value and increments in a single UPDATE (so two people generating at once cannot collide). The frontend previews on load and claims only after the first successful download of a letter, which is why downloading both DOCX and PDF of the same letter burns one number, not two. `service.FormatDocNumber` owns the placeholder vocabulary; `HRSettings.jsx` mirrors it for the live preview only.

---

## Role-Based Access

- **superuser**: Every capability without exception, approvals included — the one role that is both admin and manager. Not seeded: an existing deployment already has an admin, and that admin promotes whoever should hold it (migration 067 only widens the CHECK). The navbar turns dark red (`.navbar-superuser`) for the whole session so the elevated login is visible on every page rather than only on the users screen.
- **admin**: Full access to every module.
- **manager**: Same as admin, **plus** the exclusive right to approve/reject kasbon, leave and overtime requests. Approval is the one capability no other role has.
- **staff**: Same reach as admin — items, inventory, invoices, accounts, branches, users, HR — **except** reports (`/api/reports/*`, `/api/expense-report`) and the activity log. Cannot approve requests.
- **hr**: Confined to the HR module. Gets its own navbar and its own dashboard (`HRDashboard`, a shortcut map of every HR page) instead of the operational one. Cannot approve requests.
- **store_manager**: Attendance record entry/correction only (`RequireAttendanceAccess`); rejected everywhere else.
- **device-key**: Machine accounts for fingerprint/face attendance devices; authenticated via `X-Device-Key` header (no JWT); access only to `/api/hr/attendance/device/*` endpoints.
- Roles are named as constants in `middleware/auth.go` and every gate is built from one `gate(...)` helper, so adding a role means touching that file and nothing else. **`superuser` is not listed in any gate** — `hasRole` short-circuits on it, so a role defined as "all capabilities" cannot drift out of a gate added later. The exported `middleware.HasRole` carries the same bypass for the few handlers that check a role inline instead of behind a gate. The gates are `RequireAdmin` / `RequireAdminOrManager` (admin+manager), `RequireCoreAccess` (admin+manager+staff), `RequireHRAccess` (admin+manager+staff+hr), `RequireReportsAccess` (admin+manager), `RequireManager` (approvals), `RequireAttendanceAccess`, and `DeviceAuth`.
- **`RestrictHRRole` is the exception to per-route gating**: most non-HR routes are deliberately open to every authenticated user, so scoping the `hr` role by adding middleware to each of them would mean getting it right on every future route. Instead it sits once at the top of the protected group and denies the `hr` role anything outside an allowlist (`/api/hr/*`, `/api/auth/*`, branches, divisions, accounts) — closed by default. Do not "fix" this by scattering per-route checks.
- The frontend mirrors all of this in `client/src/roles.js` (`canUseHR`, `canViewReports`, `canUseCore`, `canApprove`, `isAdminRole`, `isHROnly`, `isSuperuser`) — used by the `RequireHR` / `RequireReports` / `RequireCore` route guards and to build the navbar. Pages that gate a single button (delete invoice, cancel a daily purchase or setoran, record a petty cash count, clear the activity log, approve a request) must call `isAdminRole()` / `canApprove()` rather than comparing `role === 'admin'` inline — the inline form is how superuser silently loses a capability. Hidden links are a courtesy; the server is the control.

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
