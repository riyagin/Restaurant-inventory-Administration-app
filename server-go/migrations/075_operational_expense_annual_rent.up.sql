-- Sewa tahunan — rent paid a year in advance, expensed a month at a time.
--
-- A shop lease is commonly settled once a year, up front. Booking the whole
-- payment as that month's Beban Sewa makes one month carry twelve months of rent
-- and the other eleven look rent-free, which wrecks every month-on-month P&L
-- comparison the branch report exists for. The payment is instead an asset —
-- rent the branch has bought and not yet used — and one twelfth of it becomes
-- expense each month the lease runs:
--
--   on payment:   Dr Sewa Dibayar di Muka - <cabang>    Cr <whatever paid it>
--   every month:  Dr Beban - <cabang> - Operasional - Sewa
--                 Cr Sewa Dibayar di Muka - <cabang>
--
-- The monthly entries are not all written on the day of payment: future-dated
-- journal lines would move accounts.balance today, so the balance sheet would
-- show next year's rent as already used. Instead the schedule below is the
-- queue, and service.AmortizationPoster posts each row once its month starts —
-- the same durability arrangement as payroll_postings.

-- ── How a bill is billed ────────────────────────────────────────────────────
ALTER TABLE operational_expenses
  ADD COLUMN IF NOT EXISTS billing TEXT NOT NULL DEFAULT 'monthly',
  -- Tanggal jatuh tempo. Optional on a monthly bill (it is only information
  -- there); on an annual one it is the day the lease period starts, which is
  -- what the schedule and the next renewal are counted from.
  ADD COLUMN IF NOT EXISTS due_date DATE,
  ADD COLUMN IF NOT EXISTS term_months INT NOT NULL DEFAULT 1;

ALTER TABLE operational_expenses
  ADD CONSTRAINT operational_expense_billing_check CHECK (billing IN ('monthly', 'annual')),
  ADD CONSTRAINT operational_expense_term_check CHECK (
    (billing = 'monthly' AND term_months = 1) OR
    (billing = 'annual'  AND term_months = 12 AND due_date IS NOT NULL)
  );

-- ── Where prepaid rent sits ─────────────────────────────────────────────────
--
-- 10500, under Aset and outside 11000-19999: GetNextInventoryAccountNumber
-- allocates warehouse accounts as MAX + 1 over that whole span, so anything
-- placed inside it moves where the next warehouse account lands. Per-branch
-- children take 10501-10599 and are created on a branch's first annual rent —
-- most branches will never pay rent this way, and an empty account per branch
-- is noise in the chart of accounts.
INSERT INTO accounts (name, account_number, account_type, parent_id, is_system)
SELECT 'Sewa Dibayar di Muka', 10500, 'asset',
       (SELECT id FROM accounts WHERE account_number = 10000), true
WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE account_number = 10500);

ALTER TABLE branches
  ADD COLUMN IF NOT EXISTS prepaid_rent_account_id UUID REFERENCES accounts(id) ON DELETE SET NULL;

-- ── The amortization schedule ───────────────────────────────────────────────
--
-- One row per month of the lease, written when the payment is recorded. Both
-- accounts are frozen onto the row for the same reason operational_expenses
-- freezes its own: repointing a category or a branch later must not change
-- where a month that is already scheduled will land.
CREATE TABLE IF NOT EXISTS operational_expense_amortizations (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  expense_id         UUID NOT NULL REFERENCES operational_expenses(id) ON DELETE CASCADE,
  seq                INT  NOT NULL CHECK (seq >= 1),
  -- First day of the month this slice of rent is used up in.
  period_month       DATE NOT NULL,
  amount             BIGINT NOT NULL CHECK (amount > 0),
  expense_account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  prepaid_account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  -- scheduled → posted by the poster; scheduled → cancelled when the lease
  -- payment is cancelled. A posted row is never un-posted: cancellation reverses
  -- it with a new entry, like everything else in this ledger.
  status             TEXT NOT NULL DEFAULT 'scheduled'
                     CHECK (status IN ('scheduled', 'posted', 'cancelled')),
  journal_entry_id   UUID REFERENCES journal_entries(id) ON DELETE SET NULL,
  posted_at          TIMESTAMPTZ,
  attempts           INT  NOT NULL DEFAULT 0,
  last_error         TEXT NOT NULL DEFAULT '',
  UNIQUE (expense_id, seq)
);

-- The poster's question on every sweep: what is scheduled and already due.
CREATE INDEX IF NOT EXISTS idx_opex_amortizations_due
  ON operational_expense_amortizations (period_month) WHERE status = 'scheduled';
