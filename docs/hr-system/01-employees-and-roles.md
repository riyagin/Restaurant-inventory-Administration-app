# Prompt 01 — Employee Profiles & Manager Role

> Read `docs/hr-system/00-overview.md` and root `CLAUDE.md` first. This is the foundation session for the HR system — everything later depends on it.

## Goal

Add the `employees` domain (master records + profile pages, assigned to existing `branches`) and introduce the new `manager` user role.

## Part A — Manager Role

1. Migration: relax/extend the `users.role` constraint to allow `admin | manager | staff`.
2. Middleware (`internal/middleware/auth.go`):
   - `requireManager` — manager only.
   - `requireAdminOrManager` — admin or manager.
   - `manager` passes every existing `requireAdmin` check (manager = admin permissions + approval responsibilities). Update `requireAdmin` so role `manager` is also accepted, OR rename appropriately — keep backward compatible.
3. Users page (`client/src/pages/Users.jsx`): role dropdown gains "Manajer".
4. JWT claims unchanged in shape; role string just gains a new value. Verify refresh flow still works.

## Part B — Database

Migration `hr_employees`:

```sql
positions (
  id UUID PK DEFAULT gen_random_uuid(),
  name TEXT NOT NULL UNIQUE,
  is_active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

employees (
  id UUID PK DEFAULT gen_random_uuid(),
  employee_code TEXT NOT NULL UNIQUE,        -- user-friendly ID, e.g. EMP-0001, auto-generated, editable
  full_name TEXT NOT NULL,
  dob DATE,
  join_date DATE NOT NULL,
  position_id UUID NOT NULL REFERENCES positions(id),
  branch_id UUID NOT NULL REFERENCES branches(id),   -- existing inventory-app table
  phone TEXT, email TEXT, address TEXT,
  national_id TEXT,                          -- NIK / KTP
  bank_name TEXT, bank_account_number TEXT, bank_account_holder TEXT,
  photo_path TEXT,
  user_id UUID REFERENCES users(id) ON DELETE SET NULL,  -- optional link to a login account
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
```

Indexes on `branch_id`, `position_id`, `status`, and `lower(full_name)`.

## Part C — Backend

`queries/hr_employees.sql` + `internal/handler/hr_employees.go`:

| Endpoint | Notes |
|---|---|
| `GET /api/hr/employees` | Filters: `q` (name/code), `branch_id`, `position_id`, `status`; pagination |
| `POST /api/hr/employees` | Auto-generate next `employee_code` if blank |
| `GET /api/hr/employees/:id` | Full profile incl. branch + position names |
| `PUT /api/hr/employees/:id` | |
| `DELETE /api/hr/employees/:id` | Block if referenced by later HR tables (rely on FK RESTRICT); recommend setting status inactive instead |
| `POST /api/hr/employees/:id/photo` / `DELETE .../photo` | Mirror the invoice photo upload pattern (multipart → `server/uploads/`) |
| `GET /api/hr/positions`, `POST`, `PUT /:id`, `DELETE /:id` | Position master CRUD |

Access: admin/manager full CRUD; staff read-only list+detail. Log all mutations via `logActivity` (entity_type `hr_employee`, `hr_position`).

## Part D — Frontend

Pages in `client/src/pages/hr/` (UI text Indonesian):

1. **Employees** (`/hr/employees`) — table: photo thumb, code, name, position, branch, join date, status badge; search + branch/position/status filters; "Tambah Karyawan" button.
2. **EmployeeForm** (`/hr/employees/new`, `/hr/employees/:id/edit`) — all fields above; branch dropdown from existing `getBranches()`; position dropdown with inline "add new position" affordance; photo upload.
3. **EmployeeDetail** (`/hr/employees/:id`) — profile page with tabs scaffold: "Profil" (active now), plus placeholder tabs "Gaji", "Absensi", "Kasbon", "Cuti" (later prompts fill these — leave clearly-marked TODO stubs).
4. **Positions** (`/hr/positions`) — simple master CRUD (admin/manager).

Add an **"HR"** group to the nav in `App.jsx` with these routes (RequireAuth; hide admin/manager-only links from staff).

## Definition of Done

Standard checklist from `00-overview.md` §10. Additionally: creating an employee with auto code works; staff login cannot mutate; manager login passes all admin-gated routes.
