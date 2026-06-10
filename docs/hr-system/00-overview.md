# HR System — Master Plan & Shared Context

> **Read this file first in every session.** It is the shared context for all prompts in `docs/hr-system/`. Each numbered prompt is designed to be run by one agent session, **in order**. Do not skip ahead — later prompts depend on tables, endpoints, and pages created by earlier ones.

## Run Order

| # | Prompt | Depends on |
|---|---|---|
| 01 | `01-employees-and-roles.md` — Employee profiles, positions, manager role | — |
| 02 | `02-wage-module.md` — Wage structures, component catalog, versioning | 01 |
| 03 | `03-excel-bulk-import.md` — Bulk import employees + wages via Excel | 01, 02 |
| 04 | `04-attendance.md` — Attendance ingestion (face API + fingerprint import), dashboard, anomalies | 01 |
| 05 | `05-performance-scoring.md` — Monthly score engine + violation policies | 04 |
| 06 | `06-leave-management.md` — Leave types, quotas, requests, approval | 01, 04 |
| 07 | `07-kasbon.md` — Cash advance manager with approval + processing flow | 01, 02 |
| 08 | `08-payroll.md` — Payroll periods, review workflow, dashboard | 02, 04, 05, 06, 07 |
| 09 | `09-payslips.md` — PDF payslip generation | 08 |
| 10 | `10-integration-qa.md` — Navigation, access control audit, tests, final QA | all |

The Android face-recognition app has its own prompt series in `docs/hr-android-app/` (separate project, separate repo/folder). Backend prompt 04 defines the API contract the app consumes.

## Project Context

This HR system extends the existing **inventory-app** (see root `CLAUDE.md`):

- **Backend**: Go (`server-go/`) — chi router, pgx/v5, sqlc, golang-migrate, JWT auth. All HR work goes here. Never touch the legacy `server/` Node backend.
- **Frontend**: React 19 SPA (`client/`), Vite, Axios via `client/src/api.js`, local `useState` only (no Redux/Context), data fetched per-component in `useEffect`.
- **Database**: PostgreSQL. UUID PKs (`gen_random_uuid()`), TIMESTAMPTZ (UTC), **BigInt cents for all money** (never NUMERIC/FLOAT).
- **Locale**: All UI text in **Indonesian** (`id-ID`). Currency via `Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR' })`.
- **Existing entities reused**: `branches` (employees are assigned to branches), `accounts` (CoA — kasbon fund sources and payroll expense postings), `users` (auth), `activity_log` (every mutation must call the `logActivity` helper).

## HR-Wide Conventions

1. **Migrations**: every prompt that adds tables creates a new sequential migration pair in `server-go/migrations/` (`migrate create -ext sql -dir migrations -seq hr_<name>`). Never edit applied migrations.
2. **Queries**: raw SQL in `server-go/queries/`, regenerate with `sqlc generate`. Never hand-edit `internal/db/`.
3. **Handlers**: one file per domain in `server-go/internal/handler/`. Business logic in `server-go/internal/service/`.
4. **Routes**: all under `/api/hr/...`, wired in `cmd/api/main.go`, behind the existing JWT auth middleware.
5. **Roles**: `admin`, `manager` (added in prompt 01), `staff`. `manager` = all admin permissions **plus** approval rights (kasbon, leave). HR pages are admin/manager-only unless a prompt says otherwise. Use/extend `requireAdmin`-style middleware (`requireManager` = manager only, `requireAdminOrManager` = either).
6. **Wage history rule**: wage structures and payroll lines are **never edited in place** — versioned rows (effective_date/end_date) and snapshots. This intentionally differs from the app's "hard deletes only" rule; HR financial history is immutable.
7. **Frontend pages** go in `client/src/pages/hr/`, API functions appended to `client/src/api.js`, routes + nav links in `App.jsx` under an "HR" nav group, wrapped in `RequireAuth`.
8. **Activity logging**: every HR mutation logs to `activity_log` (action CREATE/UPDATE/DELETE, entity_type e.g. `hr_employee`, `kasbon`).
9. **Tests**: each prompt adds Go tests for its service-layer logic (`go test ./...` must pass before the session ends).
10. **Definition of done** (every prompt): migrations apply cleanly up *and* down, `sqlc generate` clean, `go build ./...` and `go test ./...` pass, `npm run lint` passes in `client/`, new pages reachable from nav, all UI text Indonesian, activity logging in place.

## Domain Glossary (Indonesian UI labels)

| Concept | UI label |
|---|---|
| Employee | Karyawan |
| Wage structure | Struktur Gaji |
| Allowance | Tunjangan |
| Bonus | Bonus |
| Deduction | Potongan |
| Attendance | Absensi / Kehadiran |
| Cash advance | Kasbon |
| Payroll | Penggajian |
| Payslip | Slip Gaji |
| Leave | Cuti |
| Overtime | Lembur |
| Public holiday | Hari Libur Nasional |
