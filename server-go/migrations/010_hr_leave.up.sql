-- HR Leave Management: leave types (annual / sick / unpaid), per-employee annual
-- quotas, and leave requests with manager approval. Approved leave feeds the
-- attendance calendar (status='leave' rows) so the employee is not marked absent;
-- unpaid-leave working days are deducted at payroll (prompt 08).

-- Leave type catalog. uses_quota is true only for annual leave; is_paid=false for
-- unpaid leave (deducted at payroll).
CREATE TABLE leave_types (
  id         UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  name       TEXT    NOT NULL UNIQUE,
  is_paid    BOOLEAN NOT NULL,
  uses_quota BOOLEAN NOT NULL,
  is_active  BOOLEAN NOT NULL DEFAULT true
);

-- Seed the three standard leave types.
INSERT INTO leave_types (name, is_paid, uses_quota) VALUES
  ('Cuti Tahunan',     true,  true),
  ('Sakit',            true,  false),
  ('Izin Tanpa Gaji',  false, false);

-- Per-employee annual leave quota (only meaningful for quota leave types).
CREATE TABLE leave_balances (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id UUID NOT NULL REFERENCES employees(id),
  year        INT  NOT NULL,
  quota_days  INT  NOT NULL DEFAULT 12,
  used_days   INT  NOT NULL DEFAULT 0,
  UNIQUE (employee_id, year)
);

CREATE INDEX idx_leave_balances_emp_year ON leave_balances (employee_id, year);

-- Leave requests. day_count counts working days only (skips non-work days and
-- public holidays). status defaults to pending; approval is manager-only.
CREATE TABLE leave_requests (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id   UUID        NOT NULL REFERENCES employees(id),
  leave_type_id UUID        NOT NULL REFERENCES leave_types(id),
  start_date    DATE        NOT NULL,
  end_date      DATE        NOT NULL CHECK (end_date >= start_date),
  day_count     INT         NOT NULL,
  reason        TEXT,
  status        TEXT        NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
  decided_by    UUID        REFERENCES users(id),
  decided_at    TIMESTAMPTZ,
  decision_note TEXT,
  created_by    UUID        REFERENCES users(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_leave_requests_emp_status ON leave_requests (employee_id, status);
CREATE INDEX idx_leave_requests_dates      ON leave_requests (start_date, end_date);
CREATE INDEX idx_leave_requests_status     ON leave_requests (status);
