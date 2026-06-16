-- HR Kasbon (cash advance) manager. Workflow: pending → approved → processed →
-- resolved. On processing the fund-source account is debited (balance decreased)
-- and a matching entry is posted to the system asset account "Piutang Karyawan"
-- (employee receivable) so the movement appears in financial reports. Payroll
-- (prompt 08) deducts pending installments and marks the kasbon resolved once all
-- installments are paid out.

-- Seed the "Piutang Karyawan" system asset account if it does not already exist.
-- account_number 10300 sits in the asset range (10xxx) and does not collide with
-- any existing seed (no system accounts are seeded by earlier migrations; invoices
-- reference 20100 for Hutang Usaha which is created elsewhere). Guarded so re-runs
-- and pre-existing installs are idempotent.
INSERT INTO accounts (name, account_number, account_type, balance, is_system)
SELECT 'Piutang Karyawan', 10300, 'asset', 0, true
WHERE NOT EXISTS (
  SELECT 1 FROM accounts WHERE account_number = 10300
)
AND NOT EXISTS (
  SELECT 1 FROM accounts WHERE name = 'Piutang Karyawan'
);

CREATE TABLE kasbons (
  id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  kasbon_number          TEXT        NOT NULL UNIQUE,
  employee_id            UUID        NOT NULL REFERENCES employees(id),
  amount                 BIGINT      NOT NULL CHECK (amount > 0),
  details                TEXT        NOT NULL,
  sending_method         TEXT        NOT NULL,
  fund_source_account_id UUID        NOT NULL REFERENCES accounts(id),
  request_date           DATE        NOT NULL DEFAULT CURRENT_DATE,
  resolution_month       DATE        NOT NULL,
  status                 TEXT        NOT NULL DEFAULT 'pending'
                           CHECK (status IN ('pending','approved','rejected','processed','resolved','cancelled')),
  approved_by            UUID        REFERENCES users(id),
  approved_at            TIMESTAMPTZ,
  approval_note          TEXT,
  processed_by           UUID        REFERENCES users(id),
  processed_at           TIMESTAMPTZ,
  evidence_photo_path    TEXT,
  created_by             UUID        REFERENCES users(id),
  created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_kasbons_emp_status ON kasbons (employee_id, status);
CREATE INDEX idx_kasbons_status     ON kasbons (status);
CREATE INDEX idx_kasbons_number     ON kasbons (kasbon_number);

CREATE TABLE kasbon_installments (
  id              UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
  kasbon_id       UUID   NOT NULL REFERENCES kasbons(id) ON DELETE CASCADE,
  due_month       DATE   NOT NULL,
  amount          BIGINT NOT NULL CHECK (amount > 0),
  payroll_line_id UUID,
  status          TEXT   NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','deducted')),
  UNIQUE (kasbon_id, due_month)
);

CREATE INDEX idx_kasbon_installments_kasbon ON kasbon_installments (kasbon_id);
CREATE INDEX idx_kasbon_installments_due    ON kasbon_installments (due_month, status);
