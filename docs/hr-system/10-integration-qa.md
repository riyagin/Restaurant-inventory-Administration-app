# Prompt 10 — Final Integration & QA Pass

> Read `docs/hr-system/00-overview.md` first. Requires ALL prompts 01–09 completed. This session makes no large new features — it audits, wires up loose ends, and verifies the whole HR system end to end.

## Checklist

### 1. Navigation & Routing Audit
- `App.jsx`: HR nav group contains (visibility per role): Karyawan, Jabatan, Struktur Gaji/Komponen Gaji, Impor Data, Absensi, Impor Sidik Jari, Kinerja, Cuti, Kasbon, Penggajian, Pengaturan HR.
- Every HR route wrapped in `RequireAuth`; admin/manager-only routes hidden from staff and rejected server-side (test both).
- EmployeeDetail tabs all filled (Profil, Gaji, Absensi, Kasbon, Cuti) — no leftover TODO stubs.

### 2. Access Control Audit
Build a table (commit as `docs/hr-system/access-matrix.md`) of every `/api/hr/*` endpoint × role (admin / manager / staff / device-key) and verify middleware matches. Pay attention to: manager-only approvals (kasbon, leave), device endpoints rejecting JWT-only calls, staff read-only surfaces.

### 3. Cross-Module Flows (manual + automated where possible)
Run these scenarios against a seeded dev database; fix anything broken:
1. Import 5 employees via Excel → wage structures created → appear in payroll generation.
2. Device check-in (face) then fingerprint import for the same day → face data preserved, source badges correct.
3. Late check-in beyond grace → anomaly flag → auto performance violation → score drops → visible in payroll review drawer.
4. Approve unpaid leave overlapping a period → attendance shows Cuti → payroll deducts daily_rate × days.
5. Kasbon: create (split 2 months) → manager approves (sees last-resolved info) → process with photo → fund-source account balance decreased → next two payroll closes deduct installments → kasbon auto-resolves.
6. Close payroll with one unreviewed line → blocked. Review all → close → lines immutable → payslip PDF + batch ZIP download correctly.
7. New wage version mid-month → payroll generated after uses the correct version per `GetCurrentWage`.

### 4. Data & Code Hygiene
- All migrations run `up` then fully `down` then `up` again cleanly on a fresh DB.
- `sqlc generate` produces no diff; `go vet ./...`, `go test ./...`, `go build ./...` pass.
- `npm run lint` and `npm run build` pass in `client/`.
- Every mutation writes to `activity_log` — grep handlers for missing `logActivity` calls.
- No `console.log`/debug prints; all UI strings Indonesian; all money paths BigInt cents (grep for float math on amounts).

### 5. Deployment Updates
- `deploy/backup.sh`: confirm pg_dump covers new tables (it does — full DB) and that `server/uploads/` subfolders (employee photos, kasbon evidence, payslip cache) are included in the tarball.
- `deploy/nginx.conf`: `/uploads/` alias serves the new subfolders; consider auth implications for sensitive payslip files (move payslip cache OUT of the public uploads alias — serve only through the authenticated API).
- Update root `CLAUDE.md`: add HR tables, routes, pages, the `manager` role, and the device API to the relevant sections.

### 6. Seed Script
Add `server-go/cmd/seed-hr/main.go` (dev only): 2 branches' worth of employees, positions, wage components, work schedules, sample policies, a public-holiday set for the current year. Document usage in CLAUDE.md.

## Definition of Done

All scenario tests pass, access matrix committed, CLAUDE.md updated, fresh-DB bootstrap (migrate up + seed + login + run scenario 1) works start to finish.
