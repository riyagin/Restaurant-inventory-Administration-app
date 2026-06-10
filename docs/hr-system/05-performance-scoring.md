# Prompt 05 — Performance Scoring Engine

> Read `docs/hr-system/00-overview.md` first. Requires prompt 04 (attendance) completed.

## Goal

Monthly performance score per employee: **starts at 100 every month, violations deduct points**. Deductions come from **configurable policies** that automatically read attendance data (late by X minutes, early clock-out, missing checkout, absent without leave), plus manual deductions. Scores are reviewed during payroll (prompt 08) to inform bonus allocation.

## Database

Migration `hr_performance`:

```sql
performance_policies (
  id UUID PK,
  name TEXT NOT NULL,                     -- e.g. "Terlambat > 15 menit"
  rule_type TEXT NOT NULL CHECK (rule_type IN
    ('late','early_leave','missing_checkout','absent_no_leave','manual')),
  threshold_minutes INT,                  -- for late/early_leave: applies when minutes ≥ threshold
  points INT NOT NULL CHECK (points > 0), -- deducted per occurrence
  max_occurrences_per_month INT,          -- NULL = unlimited; cap total deductions from this policy
  is_active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

performance_violations (
  id UUID PK,
  employee_id UUID NOT NULL REFERENCES employees(id),
  policy_id UUID REFERENCES performance_policies(id),   -- NULL for ad-hoc manual entries
  attendance_record_id UUID REFERENCES attendance_records(id),
  date DATE NOT NULL,
  points INT NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('auto','manual')),
  note TEXT,
  created_by UUID REFERENCES users(id),   -- for manual
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (policy_id, attendance_record_id)   -- idempotent auto-evaluation
)

performance_scores (                       -- materialized monthly score
  id UUID PK,
  employee_id UUID NOT NULL REFERENCES employees(id),
  period_month DATE NOT NULL,              -- first day of month
  score INT NOT NULL DEFAULT 100,
  UNIQUE (employee_id, period_month)
)
```

## Engine (`internal/service/performance.go`)

1. `EvaluateDay(date)` — for each finalized attendance record of that date, match against active policies:
   - `late`: `is_late AND late_minutes >= threshold_minutes`. Multiple late policies may exist (e.g. ≥15 min = 2 pts, ≥60 min = 5 pts) — apply **only the highest matching threshold**, not all.
   - `early_leave`: same pattern with `early_leave_minutes`.
   - `missing_checkout`: `is_missing_checkout`.
   - `absent_no_leave`: record status `absent`.
   - Insert violations idempotently (the UNIQUE constraint makes re-runs safe). Respect `max_occurrences_per_month`.
2. Run from the same nightly reconciliation tick as prompt 04 (after absent rows are inserted), and expose `POST /api/hr/performance/evaluate?from=&to=` for backfills.
3. **Score** = `max(0, 100 − Σ points in month)`. Recompute and upsert `performance_scores` after every violation insert/delete. Score resets to 100 implicitly each new month.
4. If an attendance record is manually corrected (prompt 04 PUT), delete its auto violations and re-evaluate that record.

## Endpoints (admin/manager)

| Endpoint | Notes |
|---|---|
| CRUD `/api/hr/performance/policies` | Deactivate instead of delete when referenced |
| `GET /api/hr/performance/scores?month=&branch_id=&q=` | Scores list with employee info |
| `GET /api/hr/employees/:id/performance?month=` | Score + violation breakdown |
| `POST /api/hr/performance/violations` | Manual deduction (employee, date, points, note) |
| `DELETE /api/hr/performance/violations/:id` | Remove (logActivity with reason); triggers recompute |
| `POST /api/hr/performance/evaluate` | Manual backfill |

## Frontend

1. **PerformancePolicies** (`/hr/performance/policies`) — CRUD table: name, rule type (Terlambat / Pulang Awal / Tidak Absen Pulang / Absen Tanpa Cuti / Manual), threshold minutes, points, monthly cap, active toggle. Seed suggestion shown as empty-state examples.
2. **PerformanceDashboard** (`/hr/performance`) — month picker; table: employee, branch, score (color-coded: ≥90 green, 70–89 yellow, <70 red), violation count; row expands to violation list (date, policy, points, note). Button "Tambah Pelanggaran Manual".
3. EmployeeDetail gains score summary on the "Absensi" tab (current month score + link to breakdown).

## Definition of Done

Standard checklist + tests: highest-threshold-only selection, idempotent re-evaluation, monthly cap, score floor at 0, recompute on violation delete.
