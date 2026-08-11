-- Setoran — cash leaving one place and arriving in another.
--
-- Two movements the business makes constantly, which until now had no home:
--   * the branch hands its takings to the owner, cash out of the till and into a
--     bank account;
--   * the office refills a branch's petty cash box.
-- Both are the same event — money moves between two accounts, attributed to a
-- branch, with a slip somebody signed — so they are one table with a type, not
-- two features.
--
-- The generic account-transfer screen could express either, but it does not ask
-- which branch, does not carry a reference number or a photo of the slip, and
-- does not produce a list anyone can hand to the owner at the end of the month.
-- That is the whole reason this exists separately.

CREATE TABLE IF NOT EXISTS cash_deposits (
  id     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  number TEXT NOT NULL UNIQUE,
  date   DATE NOT NULL,

  -- Which branch the movement belongs to. NULL is head office moving its own
  -- money around, which is rare but real.
  branch_id UUID REFERENCES branches(id) ON DELETE SET NULL,

  --   setoran                 branch cash → owner's bank
  --   pengisian_kas_kecil     till/bank → the branch's petty cash box
  --   pengembalian_kas_kecil  the box gives money back (closing down, over-filled)
  --   lainnya                 anything else, spelled out in notes
  movement_type TEXT NOT NULL
    CHECK (movement_type IN ('setoran', 'pengisian_kas_kecil', 'pengembalian_kas_kecil', 'lainnya')),

  from_account_id UUID   NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  to_account_id   UUID   NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  amount          BIGINT NOT NULL CHECK (amount > 0),

  -- Bank reference / slip number, and the person who physically took the money.
  -- Both are what makes a disputed setoran resolvable a month later.
  reference  TEXT NOT NULL DEFAULT '',
  handed_to  TEXT NOT NULL DEFAULT '',
  notes      TEXT NOT NULL DEFAULT '',
  photo_path TEXT,

  status TEXT NOT NULL DEFAULT 'posted' CHECK (status IN ('posted', 'cancelled')),

  created_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  cancelled_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
  cancelled_at  TIMESTAMPTZ,
  cancel_reason TEXT        NOT NULL DEFAULT '',

  -- A transfer to itself is a typo that would post a balanced, meaningless entry.
  CONSTRAINT cash_deposit_distinct_accounts CHECK (from_account_id <> to_account_id)
);

CREATE INDEX IF NOT EXISTS idx_cash_deposits_branch_date ON cash_deposits (branch_id, date);
CREATE INDEX IF NOT EXISTS idx_cash_deposits_date ON cash_deposits (date);
CREATE INDEX IF NOT EXISTS idx_cash_deposits_to_account ON cash_deposits (to_account_id, date);

CREATE SEQUENCE IF NOT EXISTS cash_deposit_number_seq START 1;
