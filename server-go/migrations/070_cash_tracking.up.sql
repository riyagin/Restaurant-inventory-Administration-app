-- Pelacakan Kas — the branch till, as distinct from the petty cash box.
--
-- Two different piles of money, two different reconciliations, which is why this
-- does not replace `petty_cash_counts`:
--
--   Kas Kecil   a small float the branch buys from. Filled by top-up, emptied by
--               Pembelanjaan Harian. Nothing is sold out of it.
--   Kas laci    the till. Filled by customers paying cash, emptied by setoran to
--               the owner and by anything paid out of the drawer.
--
-- The till's day is:
--
--   opening + penjualan tunai (POS) + setoran masuk
--           - setoran keluar - pengeluaran tunai  =  seharusnya
--
-- and the counted closing is measured against that. Everything on the income
-- side comes from data that already exists — POS imports and cash_deposits — so
-- the only things anyone types are the two counts.

-- ── Which accounts are physical cash ───────────────────────────────────────
-- A POS import settles into one account per payment method: Cash, EDC BCA,
-- TRANSFER BCA, GoFood, ShopeeFood. Only some of those arrive as notes in a
-- drawer, and only those can be counted at the end of the day. Rather than
-- hard-coding account 11001, the property is marked on the account — a second
-- till, or a rename, then needs no code change.
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS is_cash_drawer BOOLEAN NOT NULL DEFAULT false;

UPDATE accounts SET is_cash_drawer = true
WHERE account_number = 11001 OR lower(name) IN ('cash', 'kas', 'tunai');

COMMENT ON COLUMN accounts.is_cash_drawer IS
  'Physical cash that can be counted at the end of a day. Drives Pelacakan Kas.';

-- ── The daily till count ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cash_day_counts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id  UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  count_date DATE NOT NULL,

  opening_amount BIGINT      NOT NULL CHECK (opening_amount >= 0),
  opening_by     UUID        REFERENCES users(id) ON DELETE SET NULL,
  opening_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

  closing_amount BIGINT      CHECK (closing_amount IS NULL OR closing_amount >= 0),
  closing_by     UUID        REFERENCES users(id) ON DELETE SET NULL,
  closing_at     TIMESTAMPTZ,

  -- Frozen when the closing is taken, for the same reason petty_cash_counts
  -- freezes its own: a POS import that lands after someone has signed off on a
  -- variance must not silently rewrite the figure they signed off on.
  expected_closing BIGINT,
  variance         BIGINT,
  variance_note    TEXT NOT NULL DEFAULT '',

  -- The income and outgoing sides as they stood at that moment, kept so the
  -- signed-off day can be re-read later without recomputing it from data that
  -- has since moved.
  cash_sales    BIGINT NOT NULL DEFAULT 0,
  cash_in       BIGINT NOT NULL DEFAULT 0,
  cash_out      BIGINT NOT NULL DEFAULT 0,
  cash_expenses BIGINT NOT NULL DEFAULT 0,

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT cash_day_variance_needs_note
    CHECK (variance IS NULL OR variance = 0 OR length(trim(variance_note)) > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cash_day_counts_branch_date
  ON cash_day_counts (branch_id, count_date);

-- ── The POS settlement layer ───────────────────────────────────────────────
-- A view rather than a table: it is a different reading of pos_import_lines, not
-- new information, and duplicating it into a table would create a second copy
-- that can disagree with the first.
--
-- `pos_imports` carries no branch column — the branch is implied by the accounts
-- its *revenue* lines post to — so the branch is resolved by walking down the
-- chart of accounts from each branch's own accounts, exactly as the branch P&L
-- and the daily task board do. The payment lines are then attributed to whatever
-- branch that import belonged to: a payment line posts to a shared account
-- ("Cash", "EDC BCA") and carries no branch of its own.
CREATE OR REPLACE VIEW pos_settlement_by_branch AS
WITH RECURSIVE direct AS (
  SELECT b.id AS branch_id, b.revenue_account_id AS account_id FROM branches b WHERE b.revenue_account_id IS NOT NULL
  UNION SELECT b.id, b.expense_account_id  FROM branches b  WHERE b.expense_account_id  IS NOT NULL
  UNION SELECT d.branch_id, d.revenue_account_id  FROM divisions d WHERE d.revenue_account_id  IS NOT NULL AND d.branch_id IS NOT NULL
  UNION SELECT d.branch_id, d.expense_account_id  FROM divisions d WHERE d.expense_account_id  IS NOT NULL AND d.branch_id IS NOT NULL
  UNION SELECT d.branch_id, d.discount_account_id FROM divisions d WHERE d.discount_account_id IS NOT NULL AND d.branch_id IS NOT NULL
),
owned AS (
  SELECT account_id, branch_id, 0 AS depth FROM direct
  UNION ALL
  SELECT a.id, o.branch_id, o.depth + 1
  FROM accounts a JOIN owned o ON a.parent_id = o.account_id
),
owner AS (
  SELECT DISTINCT ON (account_id) account_id, branch_id FROM owned ORDER BY account_id, depth
),
-- One branch per import, from its revenue lines. An import whose revenue lands
-- on more than one branch is not a thing the POS produces, but DISTINCT ON keeps
-- it deterministic if it ever happens.
import_branch AS (
  SELECT DISTINCT ON (pi.id) pi.id AS import_id, pi.date, owner.branch_id
  FROM pos_imports pi
  JOIN pos_import_lines pil ON pil.import_id = pi.id
  JOIN owner ON owner.account_id = pil.account_id
  ORDER BY pi.id, owner.branch_id
)
SELECT
  ib.branch_id,
  ib.date,
  pil.account_id,
  a.name        AS account_name,
  a.is_cash_drawer,
  SUM(pil.amount)::bigint AS amount
FROM import_branch ib
JOIN pos_import_lines pil ON pil.import_id = ib.import_id
JOIN accounts a ON a.id = pil.account_id
WHERE pil.line_type = 'cash'   -- the payment-method side of the import
GROUP BY ib.branch_id, ib.date, pil.account_id, a.name, a.is_cash_drawer;
