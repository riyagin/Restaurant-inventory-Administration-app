# Prompt 07 — Kasbon (Cash Advance) Manager

> Read `docs/hr-system/00-overview.md` first. Requires prompts 01–02 completed (employees, wages, manager role).

## Goal

Cash advance (kasbon) workflow: request → **manager approval** → processing (money sent, optional photo evidence) → automatic wage deduction in the chosen resolution month(s). Integrates with the existing Chart of Accounts (`accounts`) for fund sources.

## Lifecycle

```
pending ──approve──▶ approved ──process──▶ processed ──payroll deducts──▶ resolved
   │  └─reject──▶ rejected                      (prompt 08 marks resolved when all
   └─cancel──▶ cancelled                         installments are paid out)
```

## Database

Migration `hr_kasbon`:

```sql
kasbons (
  id UUID PK,
  kasbon_number TEXT NOT NULL UNIQUE,      -- friendly ID: KSB-YYYY-NNNN, auto-generated
  employee_id UUID NOT NULL REFERENCES employees(id),
  amount BIGINT NOT NULL CHECK (amount > 0),          -- cents
  details TEXT NOT NULL,                   -- reason / details of request
  sending_method TEXT NOT NULL,            -- e.g. 'Transfer Bank', 'Tunai' (free text w/ suggestions)
  fund_source_account_id UUID NOT NULL REFERENCES accounts(id),  -- existing CoA
  request_date DATE NOT NULL DEFAULT CURRENT_DATE,
  resolution_month DATE NOT NULL,          -- first day of month; CHECK: within 2 months of request_date
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','approved','rejected','processed','resolved','cancelled')),
  approved_by UUID REFERENCES users(id), approved_at TIMESTAMPTZ, approval_note TEXT,
  processed_by UUID REFERENCES users(id), processed_at TIMESTAMPTZ,
  evidence_photo_path TEXT,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

kasbon_installments (        -- deduction plan, 1 or 2 rows per kasbon
  id UUID PK,
  kasbon_id UUID NOT NULL REFERENCES kasbons(id) ON DELETE CASCADE,
  due_month DATE NOT NULL,                 -- first day of month
  amount BIGINT NOT NULL CHECK (amount > 0),
  payroll_line_id UUID,                    -- set by payroll (prompt 08) when deducted
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','deducted')),
  UNIQUE (kasbon_id, due_month)
)
```

### Rules
1. `resolution_month` must be ≤ 2 calendar months after `request_date` (validate in service; enforce with trigger or app-level check).
2. **Split**: requester/approver may split the deduction across up to **2** monthly installments (both within the 2-month window; installment amounts must sum to `amount`). Default = single installment in `resolution_month`.
3. `kasbon_number`: `KSB-<year>-<zero-padded sequence>` per year.
4. **Approval is manager-only.** The approval view must display the employee's **last resolved kasbon date** (and amount) — informational only, never auto-reject.
5. **Processing** (after approval): user marks money as sent, optionally uploads photo evidence (multipart, invoice-photo pattern). In one transaction: status → `processed`, **debit the fund source account** — decrease `accounts.balance` by `amount` and follow the existing account-adjustment pattern so the cash movement appears in financial reports (post a corresponding entry to an "Piutang Karyawan" asset account; create it as a system account in the migration if absent).
6. When processed, installments become live deductions: payroll (prompt 08) reads pending `kasbon_installments` for the period's month and deducts them from net pay, sets `payroll_line_id` + status `deducted`, and marks the kasbon `resolved` once all installments are deducted. **This prompt only needs to expose** `GetPendingInstallments(employeeID, month)` and `MarkInstallmentDeducted(...)` in `internal/service/kasbon.go`.
7. Edits allowed only in `pending`. Cancel allowed in `pending`/`approved` (not after processing).

## Endpoints

| Endpoint | Access |
|---|---|
| `GET /api/hr/kasbons?status=&employee_id=&q=` | admin/manager |
| `POST /api/hr/kasbons` (incl. optional installment split) | admin/manager |
| `GET /api/hr/kasbons/:id` — incl. installments + employee's last resolved kasbon info | admin/manager |
| `PUT /api/hr/kasbons/:id` (pending only) | admin/manager |
| `POST /api/hr/kasbons/:id/approve` / `/reject` (body: note; approve may adjust split) | **manager only** |
| `POST /api/hr/kasbons/:id/process` (multipart, optional photo) | admin/manager |
| `POST /api/hr/kasbons/:id/cancel` | admin/manager |

logActivity on every transition (entity_type `kasbon`).

## Frontend

1. **KasbonDashboard** (`/hr/kasbon`) — main view:
   - **"Kasbon Berjalan"**: all ongoing kasbons (approved/processed, not yet resolved) — kasbon number, employee name, amount, remaining installments, resolution month(s), status chip.
   - **"Disetujui — Belum Diproses"** section: approved kasbons awaiting processing, each with a "Proses" button → modal: confirm sending method, optional photo evidence upload.
   - Pending-approval list for managers with Setujui/Tolak actions; approval modal shows: employee, amount, details, fund source, **"Kasbon terakhir lunas: <date> (Rp …)"** or "Belum pernah kasbon".
   - Filters: status, employee search.
2. **KasbonForm** (`/hr/kasbon/new`) — employee picker, amount (`CurrencyInput`), details, sending method, fund source (account picker filtered to asset/cash accounts), resolution month (dropdown limited to current+2 months), optional split UI (two month+amount rows that must sum to total).
3. **KasbonDetail** (`/hr/kasbon/:id`) — full record, timeline of transitions, installment table, evidence photo.
4. EmployeeDetail "Kasbon" tab (fill prompt-01 stub): history + outstanding balance.

## Definition of Done

Standard checklist + tests: 2-month window validation, split-sums-to-total, number generation per year, status transition guards, account balance debit on process, last-resolved-kasbon query.
