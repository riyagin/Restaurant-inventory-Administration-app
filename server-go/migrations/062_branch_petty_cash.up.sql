-- Petty cash, one box per branch.
--
-- A branch buys small things daily — ice, gas, a missing crate of eggs — out of
-- a physical cash box, and that box is a real asset that has to appear in the
-- chart of accounts or the balance sheet is short by whatever is sitting in it.
-- So each branch owns a "Kas Kecil - <Cabang>" account the same way it already
-- owns a revenue and an expense account, created with the branch and never by
-- hand.
--
-- Numbering: 11100-11199, under the system parent "Kas dan Setara Kas" (11000).
-- The range matters. `GetNextInventoryAccountNumber` allocates warehouse
-- inventory accounts as MAX(account_number) + 1 over the whole 11000-19999 span,
-- and inventory accounts already sit at 12001+. Putting petty cash *below* that
-- maximum leaves the warehouse sequence untouched; putting it above (13000, say)
-- would silently push every future warehouse account into a new range.

ALTER TABLE branches
  ADD COLUMN IF NOT EXISTS petty_cash_account_id UUID REFERENCES accounts(id) ON DELETE SET NULL;

-- Backfill: give every existing branch its box. Numbered in name order purely so
-- a re-run of this migration on a copy of the database produces the same layout.
WITH parent AS (
  SELECT id FROM accounts WHERE account_number = 11000 AND is_system LIMIT 1
),
needing AS (
  SELECT b.id, b.name, ROW_NUMBER() OVER (ORDER BY b.name) AS seq
  FROM branches b
  WHERE b.petty_cash_account_id IS NULL
),
created AS (
  INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
  SELECT gen_random_uuid(), 'Kas Kecil - ' || n.name, 11099 + n.seq, 'asset', (SELECT id FROM parent), 0, false
  FROM needing n
  RETURNING id, name
)
UPDATE branches b
SET petty_cash_account_id = c.id
FROM created c
WHERE c.name = 'Kas Kecil - ' || b.name
  AND b.petty_cash_account_id IS NULL;
