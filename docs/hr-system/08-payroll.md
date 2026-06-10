# Prompt 08 — Payroll Module

> Read `docs/hr-system/00-overview.md` first. Requires prompts 02 (wages), 04 (attendance), 05 (performance), 06 (leave), 07 (kasbon) completed. This is the integration heart of the HR system — read those prompts' service interfaces before starting.

## Goal

Monthly payroll: generate lines snapshotting each employee's wage structure, pull overtime/holiday pay, kasbon installments, unpaid-leave deductions, and require a **per-employee performance review before the period can be closed**. Dashboard filterable by name/position/branch, sortable by projected salary.

## Database

Migration `hr_payroll`:

```sql
payroll_periods (
  id UUID PK,
  period_month DATE NOT NULL UNIQUE,       -- first day of month
  start_date DATE NOT NULL, end_date DATE NOT NULL,
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed','paid')),
  created_by UUID REFERENCES users(id),
  closed_at TIMESTAMPTZ, paid_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

payroll_lines (                -- one row per employee per period; SNAPSHOT, immutable after close
  id UUID PK,
  payroll_period_id UUID NOT NULL REFERENCES payroll_periods(id) ON DELETE CASCADE,
  employee_id UUID NOT NULL REFERENCES employees(id),
  wage_structure_id UUID NOT NULL REFERENCES wage_structures(id),  -- snapshot ref
  base_salary BIGINT NOT NULL,             -- snapshot at generation time
  daily_rate BIGINT NOT NULL,              -- snapshot
  overtime_days NUMERIC(5,2) NOT NULL DEFAULT 0,
  public_holiday_days NUMERIC(5,2) NOT NULL DEFAULT 0,
  overtime_amount BIGINT NOT NULL DEFAULT 0,
  public_holiday_amount BIGINT NOT NULL DEFAULT 0,
  allowance_total BIGINT NOT NULL DEFAULT 0,
  bonus_total BIGINT NOT NULL DEFAULT 0,
  component_deduction_total BIGINT NOT NULL DEFAULT 0,
  kasbon_deduction BIGINT NOT NULL DEFAULT 0,
  unpaid_leave_days INT NOT NULL DEFAULT 0,
  unpaid_leave_deduction BIGINT NOT NULL DEFAULT 0,
  gross_pay BIGINT NOT NULL DEFAULT 0,
  net_pay BIGINT NOT NULL DEFAULT 0,
  performance_score INT,                   -- snapshot from performance_scores
  reviewed BOOLEAN NOT NULL DEFAULT false,
  reviewed_by UUID REFERENCES users(id), reviewed_at TIMESTAMPTZ, review_note TEXT,
  UNIQUE (payroll_period_id, employee_id)
)

payroll_line_components (      -- per-component snapshot (drives payslip breakdown)
  id UUID PK,
  payroll_line_id UUID NOT NULL REFERENCES payroll_lines(id) ON DELETE CASCADE,
  wage_component_id UUID REFERENCES wage_components(id),
  name TEXT NOT NULL, type TEXT NOT NULL,  -- denormalized snapshot
  amount BIGINT NOT NULL
)

payroll_settings (             -- singleton row
  id INT PK DEFAULT 1 CHECK (id = 1),
  overtime_multiplier NUMERIC(4,2) NOT NULL DEFAULT 1.5,
  holiday_multiplier NUMERIC(4,2) NOT NULL DEFAULT 2.0
)
```

## Calculation (`internal/service/payroll.go`)

For each active employee at generation time:

```
overtime_amount       = round(overtime_days × daily_rate × overtime_multiplier)
public_holiday_amount = round(public_holiday_days × daily_rate × holiday_multiplier)
gross_pay = base_salary + allowance_total + bonus_total
          + overtime_amount + public_holiday_amount
net_pay   = gross_pay − component_deduction_total
          − kasbon_deduction − unpaid_leave_deduction
```

- Components snapshot from `GetCurrentWage(employee, period.end_date)` (prompt 02).
- `overtime_days` / `public_holiday_days`: prefill `public_holiday_days` from attendance records on `public_holidays` dates where the employee was present; `overtime_days` entered/adjusted manually in review (no overtime clock data yet).
- `kasbon_deduction` = pending `kasbon_installments` for the period month via `GetPendingInstallments` (prompt 07). On **close**, call `MarkInstallmentDeducted` and resolve fully-deducted kasbons.
- `unpaid_leave_deduction` = `GetUnpaidLeaveDays(...)` (prompt 06) × daily_rate.
- `performance_score` snapshot from `performance_scores` for the period month. Bonus components are editable during review — score is **decision support**, deductions already flowed through the score; reviewer adjusts bonus amounts as needed.
- All money: BigInt cents; round half-up to whole cents.

## Workflow

1. `POST /api/hr/payroll/periods` — create period for a month (reject duplicates) and **generate lines** for all active employees (transaction). Employees without an open wage structure are listed in the response as skipped warnings.
2. While `open`: `POST /api/hr/payroll/lines/:id/review` — body: overtime_days, public_holiday_days, adjusted bonus/variable component amounts, review_note → recalculates the line, sets reviewed=true. `POST .../unreview` to reopen. `POST /api/hr/payroll/periods/:id/regenerate-line/:employeeId` re-snapshots one line (loses review).
3. `POST /api/hr/payroll/periods/:id/close` — **rejected unless every line is reviewed**. Locks lines, performs kasbon installment marking, posts total payroll expense to the branch expense accounts (group lines by employee branch, decrease the corresponding expense flow per the existing CoA pattern — follow how dispatches/sales post to `accounts`).
4. `POST /api/hr/payroll/periods/:id/mark-paid` — status `paid` (after actual disbursement).
5. Closed/paid periods and their lines are immutable.

## Endpoints

All admin/manager. `GET /api/hr/payroll/periods`, `GET /api/hr/payroll/periods/:id` (summary totals), `GET /api/hr/payroll/periods/:id/lines?q=&position_id=&branch_id=&sort=net_pay|name&order=` plus the workflow endpoints above. logActivity on every transition.

## Frontend

1. **PayrollDashboard** (`/hr/payroll`) — period list (month, status chip, total net, reviewed x/y) + "Buat Periode" button.
2. **PayrollPeriodDetail** (`/hr/payroll/:id`) — the main screen:
   - Filters: search name, position, branch; **sort by projected salary** (net_pay) asc/desc.
   - Table: employee, position, branch, base, allowances, bonus, overtime, deductions (kasbon + unpaid leave shown distinctly), net pay, **performance score badge**, reviewed ✓.
   - Row click → **review drawer**: attendance summary for the month (hadir/absen/terlambat/cuti counts), performance score + violation list (from prompt 05), kasbon installments due, editable overtime days / holiday days / bonus & variable component amounts, note, "Tandai Sudah Direview".
   - Header: progress "Direview 12/15", "Tutup Periode" button (disabled until all reviewed), then "Tandai Dibayar".
3. Totals footer (gross, deductions, net) formatted `id-ID`.

## Definition of Done

Standard checklist + tests: calculation math (incl. multipliers and rounding), close blocked until all reviewed, kasbon resolution on close, snapshot immutability after close, skipped-employee warnings.
