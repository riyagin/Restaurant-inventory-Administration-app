# Inventory App — Project Overview

A one-page map of the `inventory-app` folder: what the system does, how it is put together, and where to look next. For the detailed rules (accounting invariants, role gates, every endpoint) see [CLAUDE.md](CLAUDE.md). For deployment see [deploy/DEPLOY.md](deploy/DEPLOY.md).

> The older [README.md](README.md) still describes the original Node/Express version with two roles. That is out of date: the live backend is Go (`server-go/`) and there are seven roles. Treat this file and CLAUDE.md as current.

---

## What it is

An all-in-one back-office system for a multi-branch Indonesian F&B business. It has two halves that share one database and one web app:

| Area | Covers |
|---|---|
| **Operations & accounting** | Item catalogue with unit conversion, multi-warehouse stock (FIFO lots), purchase/expense invoices (including split "faktur distribusi"), daily market purchases (Pembelanjaan Harian), dispatch to branches, stock transfers, stock opname, recipes and production, sales and POS Excel import, petty cash and till counts, cash deposits (Setoran), operational expenses (including annual rent amortisation), a double-entry journal, chart of accounts, and reports (P&L by branch and by period, inventory value, price changes, usage trends, stock movement) |
| **HR** | Employees, positions, wage structures and components, Excel bulk import, attendance (fingerprint import, face-recognition devices, manual entry), shifts and manpower planning, performance scoring, leave (monthly allowance), overtime, kasbon (cash advances), payroll with automatic ledger posting, payslips (PDF), HR documents (PKWT/PKWTT/warning letters/Paklaring as DOCX/PDF), onboarding, daily tasks and staff KPIs |

All UI text is Indonesian, amounts are IDR stored as integer cents (BIGINT), and timestamps are UTC (`TIMESTAMPTZ`).

---

## Folder layout

```
inventory-app/
├── client/            React 19 + Vite single-page app (58 pages, plus an hr/ subfolder)
├── server-go/         Go backend: the live API
│   ├── cmd/           api (server), reset-password, seed-hr, seed-month-attendance,
│   │                  seed-attendance-cuts, test-attendance, vedit
│   ├── internal/
│   │   ├── handler/   HTTP handlers, one file per domain (~84 files incl. tests)
│   │   ├── service/   Business logic: FIFO, journal posting, payroll, attendance, leave… (~50 files)
│   │   ├── middleware/ JWT auth, role gates, device-key auth, rate limiting
│   │   ├── db/        sqlc-generated query code (do not edit by hand)
│   │   ├── config/, testutil/
│   ├── migrations/    golang-migrate SQL files (001 → 080, up + down)
│   ├── queries/       .sql files read by sqlc
│   └── sqlc.yaml, go.mod
├── server/            Legacy Node/Express backend (one ~3,300-line index.js). Reference only — no new work here
├── deploy/            PM2 config, Nginx config, deploy/migrate/backup scripts, DEPLOY.md
├── docs/hr-system/    Design specs for the HR module (00-overview … 10-integration-qa, access-matrix)
├── start.bat          Windows: starts the dev servers
└── *.xlsx, *.sql      Working data files (employee imports, salary data, DB dumps) — not part of the app
```

---

## Tech stack

| Layer | Technology |
|---|---|
| Frontend | React 19, React Router 7, Axios, Vite 8, ESLint 9, `xlsx` / `xlsx-js-style` for Excel |
| Backend | Go 1.26, `chi` router, `pgx/v5`, `sqlc`, `golang-migrate`, `golang-jwt`, `excelize`, `go-pdf/fpdf` |
| Database | PostgreSQL, UUID primary keys, hard deletes |
| Production | Ubuntu VPS, PM2 running the Go binary directly, Nginx reverse proxy plus static files, Let's Encrypt, daily `pg_dump` backups |

Request path: `React page → api.js (Axios + JWT, auto-refresh on 401) → chi router → middleware → handler → service → sqlc → PostgreSQL`.

The frontend reads its API base URL from `/config.json` at runtime, so a VPS can be repointed without rebuilding.

---

## Running locally

```bash
# Backend (http://localhost:5000). Runs migrations on start
cd server-go
go run ./cmd/api

# Frontend (http://localhost:5173)
cd client
npm install
npm run dev
```

Or run `start.bat` on Windows. Configuration lives in `server-go/.env` (`DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`, `JWT_SECRET`, `PORT`).

Useful commands:

| Task | Command |
|---|---|
| Run tests | `cd server-go && go test ./...` (**these hit the real database**) |
| New migration | `migrate create -ext sql -dir migrations -seq <name>` |
| Reset a password | `go run ./server-go/cmd/reset-password <username> <new-password>` |
| Seed HR dev data | `go run ./server-go/cmd/seed-hr` |
| Seed a month of attendance and payroll | `go run ./server-go/cmd/seed-month-attendance -month=2026-07` |
| Lint frontend | `cd client && npm run lint` |

Default login after seeding: `admin` / `admin123`. Change it immediately.

---

## Roles

| Role | Access |
|---|---|
| `superuser` | Everything, including approvals. The navbar turns red to show the elevated login |
| `admin` | Every module |
| `manager` | Same as admin, plus approving leave, overtime and kasbon |
| `staff` | Same as admin except reports and the activity log, and cannot approve |
| `hr` | The HR module only, with its own navbar and dashboard |
| `store_manager` | Attendance entry and correction only |
| `branch_operator` | One branch's HR paperwork: attendance, leave/overtime/kasbon requests |
| device key | Attendance devices (face kiosks, fingerprint) using `X-Device-Key`, limited to `/api/hr/attendance/device/*` |

Role gates live in `server-go/internal/middleware/auth.go`, and `client/src/roles.js` mirrors them. Hiding a link in the UI is only a convenience. The server enforces access.

---

## Core rules to know before changing anything

These are summaries. CLAUDE.md has the full reasoning behind each one.

- **Money:** always integer cents. Never use FLOAT or NUMERIC.
- **Ledger:** only `service.Post` writes `accounts.balance`, and it refuses unbalanced entries. `GET /api/accounts/trial-balance` should return an empty list.
- **Stock:** stored in each item's base unit, which is the **last** entry in `items.units`. Deductions go oldest lot first (`service.FIFODeduct`). Empty lots are kept and stamped `depleted_at`. Every stock movement writes a `stock_history` row.
- **Frozen values:** conversion factors, petty-cash accounts, allocation values and variances are stored on the row when recorded. Reversals use the stored value, never today's.
- **Dispatches:** each one creates a mirror expense invoice, so a dispatch appears twice in the source tables. Any aggregate must count only one side.
- **P&L:** there is one definition (`handler.plActivityByAccount` / `plAmountSQL`), shared by the branch, periodic and financial reports. Tests enforce this. Do not add a second one.
- **Payroll:** closing a period queues a `payroll_postings` row, and a background poster writes the journal entry.
- **Leave:** employees work seven days a week. Days off come from 4 paid-leave days per month, and a weekend day costs 2. Backdated requests need explicit confirmation and approval one at a time.
- **Late arrival / early leave:** the missed time is rounded up to 15-minute blocks and deducted at the day's per-minute rate. The amount is frozen on the payroll line.

---

## How it connects to the mobile app

The Android face-recognition kiosk in `../mobile` calls these device endpoints:

| Endpoint | Purpose |
|---|---|
| `POST /api/auth/login`, `GET /api/branches` | Operator login and branch choice during device setup |
| `POST /api/hr/attendance/device/enroll` | Registers the device and returns its `device_key` |
| `GET  /api/hr/attendance/device/employees` | Branch roster plus stored face embeddings |
| `POST /api/hr/attendance/device/face` | Uploads a face enrollment |
| `POST /api/hr/attendance/device/event` | Check-in/out event (multipart, with photo; or manual with `recorded_by`) |
| `GET  /api/hr/employees?q=` | Cross-branch employee search for manual "Bantu Absen" |

Registered devices and enrollment coverage are managed on the web at `/hr/face` and `/hr/attendance/settings`.

---

## Where to look next

| Need | File |
|---|---|
| Full architecture, every DB rule, every endpoint | [CLAUDE.md](CLAUDE.md) |
| Production deployment | [deploy/DEPLOY.md](deploy/DEPLOY.md) |
| HR module design specs | [docs/hr-system/](docs/hr-system/) |
| Database schema | `server-go/migrations/` (the source of truth; `server/schema.sql` is legacy) |
| API client functions | `client/src/api.js` |
| Routes and page guards | `client/src/App.jsx` |
