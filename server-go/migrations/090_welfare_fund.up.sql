-- 090: Dana Kesejahteraan Karyawan — a retirement fund, separate from the health
-- benefit of migration 085 (which keeps its own allowance, claims and pension sweep
-- and is now called "Tunjangan Kesehatan" in the UI).
--
-- A wage component of the new type 'welfare_fund' sets how much the company puts
-- aside for an employee every pay period. Like 'benefit_fund' it is an EMPLOYER
-- contribution: it never enters gross/net (every payroll switch only sums the types
-- it names) and is held as a liability until the employee leaves.
--
-- Money flow (all through service.Post):
--
--   payroll close, period P   Dr Beban Gaji - <cabang>        / Cr Dana Kesejahteraan Karyawan
--                             (the line's 'welfare_fund' component snapshot)
--   retirement / resignation  Dr Dana Kesejahteraan Karyawan  / Cr <kas>   (whole balance)
--
-- Per-employee balances are SUM(amount) over welfare_fund_movements, never a
-- counter. Kept out of benefit_fund_movements on purpose: the health fund's
-- idempotency and final-month checks count 'contribution' rows across that whole
-- table, and a welfare contribution there would make them skip health postings.

ALTER TABLE wage_components DROP CONSTRAINT wage_components_type_check;
ALTER TABLE wage_components
  ADD CONSTRAINT wage_components_type_check
  CHECK (type IN ('allowance', 'bonus', 'deduction', 'daily_allowance', 'benefit_fund', 'welfare_fund'));

-- ── The two wage components ──────────────────────────────────────────────────
-- Both are catalog rows: the per-employee amount is set on each wage structure.
--   Tunjangan Kesehatan  type benefit_fund  — the health allowance (migration 085)
--   Dana Kesejahteraan   type welfare_fund  — the retirement fund (this migration)
-- Before 090 the health component was itself called "Dana Kesejahteraan", so that
-- row is renamed (keeping its id, so every wage structure using it keeps paying
-- the health allowance), and the name is then free for the new fund.
-- A same-named row of another type (e.g. a hand-made "Tunjangan Kesehatan"
-- allowance) is never converted — that would move money in or out of gross pay.
-- It is renamed out of the way instead, "<name> (lama)": same id, type and
-- amounts, so nobody's pay changes, only its label. Each rename is reported as
-- a NOTICE.
DO $$
DECLARE
  conflict RECORD;
  free_name TEXT;
  n INT;
BEGIN
  FOR conflict IN
    SELECT id, name FROM wage_components
    WHERE (name = 'Tunjangan Kesehatan' AND type <> 'benefit_fund')
       -- A benefit_fund "Dana Kesejahteraan" is the pre-090 health component and
       -- is renamed below, unless "Tunjangan Kesehatan" already is one.
       OR (name = 'Dana Kesejahteraan' AND type <> 'welfare_fund' AND type <> 'benefit_fund')
       OR (name = 'Dana Kesejahteraan' AND type = 'benefit_fund'
           AND EXISTS (SELECT 1 FROM wage_components WHERE name = 'Tunjangan Kesehatan' AND type = 'benefit_fund'))
  LOOP
    free_name := conflict.name || ' (lama)';
    n := 1;
    WHILE EXISTS (SELECT 1 FROM wage_components WHERE name = free_name) LOOP
      n := n + 1;
      free_name := conflict.name || ' (lama ' || n || ')';
    END LOOP;
    UPDATE wage_components SET name = free_name WHERE id = conflict.id;
    RAISE NOTICE 'komponen gaji "%" diganti nama menjadi "%" (tipe dan nominal tidak berubah)', conflict.name, free_name;
  END LOOP;

  IF NOT EXISTS (SELECT 1 FROM wage_components WHERE name = 'Tunjangan Kesehatan') THEN
    UPDATE wage_components SET name = 'Tunjangan Kesehatan'
    WHERE name = 'Dana Kesejahteraan' AND type = 'benefit_fund';
  END IF;

  INSERT INTO wage_components (name, type, is_fixed, calc_method)
  VALUES ('Tunjangan Kesehatan', 'benefit_fund', true, 'fixed')
  ON CONFLICT (name) DO NOTHING;
  INSERT INTO wage_components (name, type, is_fixed, calc_method)
  VALUES ('Dana Kesejahteraan', 'welfare_fund', true, 'fixed')
  ON CONFLICT (name) DO NOTHING;
END $$;

DO $$
DECLARE
  liab_root UUID;
BEGIN
  IF EXISTS (SELECT 1 FROM accounts WHERE account_number = 21300 AND name <> 'Dana Kesejahteraan Karyawan') THEN
    RAISE EXCEPTION 'nomor akun 21300 sudah dipakai akun lain';
  END IF;

  SELECT id INTO liab_root FROM accounts WHERE account_number = 20000 LIMIT 1;

  INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
  SELECT gen_random_uuid(), 'Dana Kesejahteraan Karyawan', 21300, 'liability', liab_root, 0, true
  WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE account_number = 21300);
END $$;

CREATE TABLE welfare_fund_movements (
  id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id       UUID        NOT NULL REFERENCES employees(id),
  movement_type     TEXT        NOT NULL CHECK (movement_type IN (
                      'contribution',   -- +  payroll close
                      'payout',         -- −  retirement / resignation
                      'opening')),      -- ±  balance carried in from before the system
  amount            BIGINT      NOT NULL CHECK (amount <> 0),
  period_month      DATE,
  movement_date     DATE        NOT NULL,
  payroll_period_id UUID        REFERENCES payroll_periods(id) ON DELETE SET NULL,
  journal_entry_id  UUID        REFERENCES journal_entries(id) ON DELETE SET NULL,
  note              TEXT        NOT NULL DEFAULT '',
  created_by        UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_welfare_movements_emp ON welfare_fund_movements (employee_id);
-- One contribution per employee per payroll period: a retried posting can never
-- credit the same period twice.
CREATE UNIQUE INDEX uq_welfare_contribution_period
  ON welfare_fund_movements (payroll_period_id, employee_id)
  WHERE movement_type = 'contribution';
