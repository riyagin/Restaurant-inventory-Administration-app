-- 085: Dana Kesejahteraan Karyawan — health benefit + pension fund.
--
-- A wage component of the new type 'benefit_fund' sets how much the company sets
-- aside for an employee every month. It is an EMPLOYER contribution: it never
-- enters gross/net pay (every payroll switch ignores unknown types), and it is
-- held as a liability, not paid out with the salary.
--
-- Money flow (all through service.Post):
--
--   sick claim, month M     Dr Dana Kesehatan Karyawan  / Cr <kas>            (covered part)
--                           the part above M's remaining amount becomes a
--                           PENDING kasbon, so the manager approval rule holds
--   payroll close, month M  Dr Beban Gaji - <cabang>    / Cr Dana Kesehatan   (contribution)
--                           Dr Dana Kesehatan           / Cr Dana Pensiun     (unused → pension)
--   resignation / retire    Dr Dana Pensiun Karyawan    / Cr <kas>            (payout)
--
-- So the health fund nets back to zero every month and only the pension fund
-- accumulates. Between a claim and that month's payroll close the health fund
-- sits in debit: the claim is paid when the cash moves, the contribution lands on
-- payday.
--
-- Per-employee balances are NOT a counter: they are SUM(amount) over
-- benefit_fund_movements, one signed row per event (+ = more owed to employee).

ALTER TABLE wage_components DROP CONSTRAINT wage_components_type_check;
ALTER TABLE wage_components
  ADD CONSTRAINT wage_components_type_check
  CHECK (type IN ('allowance', 'bonus', 'deduction', 'daily_allowance', 'benefit_fund'));

-- ── Liability accounts ───────────────────────────────────────────────────────
DO $$
DECLARE
  liab_root UUID;
BEGIN
  IF EXISTS (SELECT 1 FROM accounts WHERE account_number = 21100 AND name <> 'Dana Kesehatan Karyawan') THEN
    RAISE EXCEPTION 'nomor akun 21100 sudah dipakai akun lain';
  END IF;
  IF EXISTS (SELECT 1 FROM accounts WHERE account_number = 21200 AND name <> 'Dana Pensiun Karyawan') THEN
    RAISE EXCEPTION 'nomor akun 21200 sudah dipakai akun lain';
  END IF;

  SELECT id INTO liab_root FROM accounts WHERE account_number = 20000 LIMIT 1;

  INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
  SELECT gen_random_uuid(), 'Dana Kesehatan Karyawan', 21100, 'liability', liab_root, 0, true
  WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE account_number = 21100);

  INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
  SELECT gen_random_uuid(), 'Dana Pensiun Karyawan', 21200, 'liability', liab_root, 0, true
  WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE account_number = 21200);
END $$;

-- ── Claims ───────────────────────────────────────────────────────────────────
CREATE TABLE benefit_claims (
  id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  claim_number           TEXT        NOT NULL UNIQUE,              -- KLM-YYYY-NNNN
  employee_id            UUID        NOT NULL REFERENCES employees(id),
  claim_date             DATE        NOT NULL,
  period_month           DATE        NOT NULL,                     -- first of claim_date's month
  total_amount           BIGINT      NOT NULL CHECK (total_amount > 0),
  covered_amount         BIGINT      NOT NULL CHECK (covered_amount >= 0),
  excess_amount          BIGINT      NOT NULL CHECK (excess_amount >= 0),
  -- The monthly allowance and what remained of it when the claim was entered,
  -- frozen so the split can be explained later even if the wage changes.
  monthly_amount         BIGINT      NOT NULL,
  available_before       BIGINT      NOT NULL,
  fund_source_account_id UUID        NOT NULL REFERENCES accounts(id),
  kasbon_id              UUID        REFERENCES kasbons(id),
  leave_request_id       UUID        REFERENCES leave_requests(id) ON DELETE SET NULL,
  description            TEXT        NOT NULL,
  status                 TEXT        NOT NULL DEFAULT 'paid' CHECK (status IN ('paid', 'cancelled')),
  journal_entry_id       UUID        REFERENCES journal_entries(id) ON DELETE SET NULL,
  cancelled_by           UUID        REFERENCES users(id) ON DELETE SET NULL,
  cancelled_at           TIMESTAMPTZ,
  cancel_note            TEXT,
  created_by             UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (covered_amount + excess_amount = total_amount),
  CHECK ((excess_amount > 0) = (kasbon_id IS NOT NULL))
);

CREATE INDEX idx_benefit_claims_emp_month ON benefit_claims (employee_id, period_month);
CREATE INDEX idx_benefit_claims_month     ON benefit_claims (period_month);

-- ── Movements (the per-employee ledger) ──────────────────────────────────────
CREATE TABLE benefit_fund_movements (
  id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id       UUID        NOT NULL REFERENCES employees(id),
  fund              TEXT        NOT NULL CHECK (fund IN ('health', 'pension')),
  movement_type     TEXT        NOT NULL CHECK (movement_type IN (
                      'contribution',    -- health  +  payday
                      'claim',           -- health  −  sick claim paid
                      'claim_reversal',  -- health  +  claim cancelled
                      'sweep_out',       -- health  −  unused at month end
                      'sweep_in',        -- pension +  unused at month end
                      'payout',          -- pension −  resignation / retirement
                      'opening')),       -- either  ±  balance carried in (migration / simulation)
  amount            BIGINT      NOT NULL CHECK (amount <> 0),
  period_month      DATE,
  movement_date     DATE        NOT NULL,
  payroll_period_id UUID        REFERENCES payroll_periods(id) ON DELETE SET NULL,
  claim_id          UUID        REFERENCES benefit_claims(id) ON DELETE SET NULL,
  journal_entry_id  UUID        REFERENCES journal_entries(id) ON DELETE SET NULL,
  note              TEXT        NOT NULL DEFAULT '',
  created_by        UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_benefit_movements_emp_fund ON benefit_fund_movements (employee_id, fund);
CREATE INDEX idx_benefit_movements_period   ON benefit_fund_movements (payroll_period_id);
