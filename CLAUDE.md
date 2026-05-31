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
    middleware/
      auth.go              # JWT validation, requireAdmin
      ratelimit.go
    service/               # Business logic (FIFO deduction, CoA updates, unit conversion)
      inventory.go
      accounts.go
      pos_import.go
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

### Components (`client/src/components/`)
- `CurrencyInput.jsx` — IDR currency input with formatting

### API Layer (`client/src/api.js`)
- Axios instance with `Authorization: Bearer <token>` header
- Base URL loaded from `/config.json` at runtime (allows VPS config without rebuild)
- Auto-refresh: on 401, queues in-flight requests, refreshes token, replays queue
- On refresh failure: clears localStorage, redirects to `/login`
- **67 exported functions** across 23 domains (auth, users, items, inventory, warehouses, vendors, accounts, stock-history, stock-opname, invoices, transfers, branches, divisions, division-categories, dispatches, sales, pos-import, recipes, productions, invoice-templates, activity-log, stats, reports, account-adjustments, enumerations)

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
| `users` | Auth — username, password_hash, role (admin\|staff) |
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

---

## Role-Based Access

- **admin**: Full access to all routes
- **staff**: Read-only on most resources; blocked from items CRUD, warehouses, vendors, accounts, users, activity log, reports, account adjustments, invoice templates, branches, divisions
- Enforced at the route level via `requireAdmin` middleware; also reflected in frontend navigation

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
