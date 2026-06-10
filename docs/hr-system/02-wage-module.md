# Prompt 02 — Wage Module (Structures, Components, Versioning)

> Read `docs/hr-system/00-overview.md` first. Requires prompt 01 (employees) completed.

## Goal

Versioned wage structures per employee: fixed monthly salary + custom allowance/bonus/deduction components from a master catalog + auto-calculated daily rate for overtime/holiday pay.

## Business Rules

1. **Daily rate** is auto-calculated as `base_salary ÷ working_days_per_month`, rounded to whole IDR cents, and **stored** on the record (not derived at read time).
2. **Versioning**: wage structures are **never edited in place**. Saving changes inserts a new version with `effective_date`, and sets the previous open version's `end_date = effective_date - 1 day`. Exactly one open version (`end_date IS NULL`) per employee at any time. Deleting versions is not allowed (admin may delete only a version that has never been referenced by a payroll line).
3. **Components**: master catalog of reusable components (`allowance | bonus | deduction`); each employee's wage structure links to components with a per-employee amount. No per-component tax rules.
4. All amounts BigInt IDR cents.

## Database

Migration `hr_wages`:

```sql
wage_components (              -- master catalog
  id UUID PK,
  name TEXT NOT NULL UNIQUE,
  type TEXT NOT NULL CHECK (type IN ('allowance','bonus','deduction')),
  is_fixed BOOLEAN NOT NULL DEFAULT true,   -- fixed monthly vs variable per period
  is_active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

wage_structures (              -- versioned, one open version per employee
  id UUID PK,
  employee_id UUID NOT NULL REFERENCES employees(id),
  base_salary BIGINT NOT NULL,             -- cents
  working_days_per_month INT NOT NULL CHECK (working_days_per_month BETWEEN 1 AND 31),
  daily_rate BIGINT NOT NULL,              -- cents, computed & stored
  effective_date DATE NOT NULL,
  end_date DATE,                           -- NULL = current
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (employee_id, effective_date)
)
-- partial unique index: one open version per employee
CREATE UNIQUE INDEX ON wage_structures (employee_id) WHERE end_date IS NULL;

employee_wage_components (
  id UUID PK,
  wage_structure_id UUID NOT NULL REFERENCES wage_structures(id) ON DELETE CASCADE,
  wage_component_id UUID NOT NULL REFERENCES wage_components(id),
  amount BIGINT NOT NULL,                  -- cents
  UNIQUE (wage_structure_id, wage_component_id)
)
```

## Backend

Service `internal/service/wages.go`:
- `CreateWageVersion(tx, employeeID, baseSalary, workingDays, effectiveDate, components[])` — computes daily_rate, closes previous open version, inserts new + components, in one transaction. Reject `effective_date` ≤ current open version's effective_date.
- `GetCurrentWage(employeeID, asOfDate)` — version where `effective_date <= d AND (end_date IS NULL OR end_date >= d)`. Used later by payroll/kasbon.

Handlers `hr_wages.go` (admin/manager only; staff no access):

| Endpoint | Notes |
|---|---|
| `GET/POST/PUT/DELETE /api/hr/wage-components` | Catalog CRUD; DELETE only if unreferenced, else toggle `is_active` |
| `GET /api/hr/employees/:id/wage` | Current structure + components |
| `GET /api/hr/employees/:id/wage/history` | All versions, newest first |
| `POST /api/hr/employees/:id/wage` | Create new version (body: base_salary, working_days_per_month, effective_date, components[{component_id, amount}]) |

Activity log: entity_type `wage_structure`, `wage_component`.

## Frontend

1. **WageComponents** (`/hr/wage-components`) — catalog CRUD table: name, type (Tunjangan/Bonus/Potongan), fixed/variable, active toggle.
2. **Employee "Gaji" tab** (fill the stub on EmployeeDetail from prompt 01):
   - Current structure card: base salary, working days, daily rate (read-only, shown computed live in the form too), effective date, component list with amounts, total monthly projection (base + fixed allowances − fixed deductions).
   - "Ubah Struktur Gaji" → form to create a **new version** (explain in UI that history is preserved); component picker from active catalog with `CurrencyInput` amounts.
   - History table of past versions (expandable to see that version's components).
3. Use existing `CurrencyInput.jsx` everywhere amounts are entered.

## Definition of Done

Standard checklist + service tests: daily_rate rounding, version close/open invariant (partial unique index holds), `GetCurrentWage` boundary dates.
