# Prompt 06 — Leave Management (Basic)

> Read `docs/hr-system/00-overview.md` first. Requires prompts 01 and 04 completed.

## Goal

Basic leave management: leave types (annual / sick / unpaid), annual quota per employee, request + manager approval, approved leave feeds the attendance calendar (so the employee isn't marked absent), unpaid leave deducts `daily_rate` per day at payroll (prompt 08 consumes this).

## Database

Migration `hr_leave`:

```sql
leave_types (
  id UUID PK,
  name TEXT NOT NULL UNIQUE,          -- seed: 'Cuti Tahunan', 'Sakit', 'Izin Tanpa Gaji'
  is_paid BOOLEAN NOT NULL,
  uses_quota BOOLEAN NOT NULL,        -- true only for annual leave
  is_active BOOLEAN NOT NULL DEFAULT true
)

leave_balances (
  id UUID PK,
  employee_id UUID NOT NULL REFERENCES employees(id),
  year INT NOT NULL,
  quota_days INT NOT NULL DEFAULT 12,
  used_days INT NOT NULL DEFAULT 0,
  UNIQUE (employee_id, year)
)

leave_requests (
  id UUID PK,
  employee_id UUID NOT NULL REFERENCES employees(id),
  leave_type_id UUID NOT NULL REFERENCES leave_types(id),
  start_date DATE NOT NULL,
  end_date DATE NOT NULL CHECK (end_date >= start_date),
  day_count INT NOT NULL,             -- working days only (skip non-work days & public holidays)
  reason TEXT,
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','approved','rejected','cancelled')),
  decided_by UUID REFERENCES users(id),
  decided_at TIMESTAMPTZ,
  decision_note TEXT,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
```

Seed the three leave types in the migration.

## Rules (`internal/service/leave.go`)

1. `day_count` counts only branch work days (reuse `work_schedules.work_days`) excluding `public_holidays`.
2. Quota types: on **approve**, check `quota_days - used_days >= day_count` (reject in API with clear Indonesian error if not), then increment `used_days`. On cancel of an approved future request, decrement.
3. Overlapping requests for the same employee (pending or approved) are rejected at create.
4. On approval, upsert `attendance_records` for each covered work day with `status='leave'` (don't overwrite days that already have a check-in — flag those in the response). Prompt 05's `absent_no_leave` policy then naturally skips these days.
5. **Approval is manager-only** (`requireManager`). Creation: admin/manager create for anyone (entered on behalf of employees — staff have no self-service portal in this phase).
6. Unpaid leave days in a payroll period → prompt 08 deducts `daily_rate × days`. Expose `GetUnpaidLeaveDays(employeeID, from, to)` for payroll to call.

## Endpoints

| Endpoint | Access |
|---|---|
| CRUD `/api/hr/leave-types` | admin/manager |
| `GET /api/hr/leave-requests?status=&branch_id=&employee_id=&year=` | admin/manager |
| `POST /api/hr/leave-requests` | admin/manager |
| `POST /api/hr/leave-requests/:id/approve` / `/reject` (body: note) | **manager only** |
| `POST /api/hr/leave-requests/:id/cancel` | admin/manager |
| `GET /api/hr/employees/:id/leave-balance?year=` | admin/manager |
| `PUT /api/hr/employees/:id/leave-balance` (set quota) | admin/manager |

logActivity on every mutation (entity_type `leave_request`, etc.).

## Frontend

1. **LeaveRequests** (`/hr/leave`) — tabs: "Menunggu Persetujuan" (pending, with Setujui/Tolak buttons visible to managers), "Semua Pengajuan" (filterable table). Create form modal: employee picker, type, date range (live computed day_count + remaining quota shown), reason.
2. EmployeeDetail "Cuti" tab (fill prompt-01 stub): balance card (kuota / terpakai / sisa) + request history.
3. AttendanceDashboard (prompt 04) already renders status `leave` as "Cuti" — verify.

## Definition of Done

Standard checklist + tests: working-day count across holidays/weekends, quota enforcement, overlap rejection, attendance upsert without overwriting check-ins.
