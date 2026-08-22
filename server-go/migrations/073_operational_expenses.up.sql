-- Beban Operasional — the branch overheads that are not goods.
--
-- Every branch already keeps an "Operasional" division: the bucket for the costs
-- that keep the doors open (listrik, air, sewa) rather than the costs of what it
-- sells. It was an ordinary division, hand-created per branch, which meant a new
-- branch silently had nowhere to book its electricity until somebody remembered
-- to add one — and the whole overhead of a branch landed on a single account
-- with no breakdown, so "why is this branch's Operasional 132 juta" had no
-- answer short of reading the journal.
--
-- This migration makes it structural:
--   * the Operasional division and its expense account are marked is_system, so
--     every branch has one and nobody can rename or delete it out from under the
--     postings;
--   * a standard set of sub-accounts is created under it, one per kind of
--     overhead, so the breakdown exists before the first bill arrives;
--   * `operational_expenses` records the bills themselves.
--
-- Nothing already booked moves. The parent account keeps its balance and the
-- P&L rolls parent + children up exactly as it did before (see `effectiveBalance`
-- / `totalOf`), so the pre-existing balance stays visible on the parent line
-- while new bills land on the child that names them.

-- ── The Operasional division is system-owned ────────────────────────────────
--
-- is_system on the division, not just on its account: the division row is what
-- the branch/division owner walks reach the account through, and a deleted
-- division would orphan the sub-accounts from the branch P&L.
ALTER TABLE divisions ADD COLUMN IF NOT EXISTS is_system BOOLEAN NOT NULL DEFAULT false;

DO $mig$
DECLARE
  b        RECORD;
  div_id   UUID;
  exp_id   UUID;
  rev_id   UUID;
  disc_id  UUID;
  next_num INT;
BEGIN
  FOR b IN SELECT id, name FROM branches ORDER BY name LOOP
    div_id := NULL;
    exp_id := NULL;
    SELECT id, expense_account_id INTO div_id, exp_id
    FROM divisions WHERE branch_id = b.id AND lower(name) = 'operasional' LIMIT 1;

    -- A branch with no Operasional division gets one, with the same three
    -- accounts handler.DivisionsHandler.Create would have given it.
    IF div_id IS NULL THEN
      SELECT COALESCE(MAX(account_number), 39999) + 1 INTO next_num
        FROM accounts WHERE account_number BETWEEN 40000 AND 49999;
      INSERT INTO accounts (name, account_number, account_type)
        VALUES ('Pendapatan - ' || b.name || ' - Operasional', next_num, 'revenue')
        RETURNING id INTO rev_id;

      SELECT COALESCE(MAX(account_number), 49999) + 1 INTO next_num
        FROM accounts WHERE account_number BETWEEN 50000 AND 59999;
      INSERT INTO accounts (name, account_number, account_type, is_system)
        VALUES ('Beban - ' || b.name || ' - Operasional', next_num, 'expense', true)
        RETURNING id INTO exp_id;

      SELECT COALESCE(MAX(account_number), 49999) + 1 INTO next_num
        FROM accounts WHERE account_number BETWEEN 50000 AND 59999;
      INSERT INTO accounts (name, account_number, account_type)
        VALUES ('Diskon - ' || b.name || ' - Operasional', next_num, 'expense')
        RETURNING id INTO disc_id;

      INSERT INTO divisions (branch_id, name, revenue_account_id, expense_account_id, discount_account_id, is_system)
        VALUES (b.id, 'Operasional', rev_id, exp_id, disc_id, true)
        RETURNING id INTO div_id;
    ELSE
      UPDATE divisions SET is_system = true WHERE id = div_id;
      IF exp_id IS NOT NULL THEN
        UPDATE accounts SET is_system = true WHERE id = exp_id;
      END IF;
    END IF;
  END LOOP;
END
$mig$;

-- ── The overhead breakdown ──────────────────────────────────────────────────
--
-- Deliberately not `expense_categories`. That table is the breakdown of what a
-- division *buys* — it is offered on the expense-invoice form and routes a
-- purchase's debit. These are the standing bills of running the place, entered
-- on their own screen, and mixing the two would put "Sewa" in the picker on
-- every purchase invoice.
CREATE TABLE IF NOT EXISTS operational_expense_categories (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  division_id UUID        NOT NULL REFERENCES divisions(id) ON DELETE CASCADE,
  name        TEXT        NOT NULL,
  -- The child account this category posts to. RESTRICT: an account carrying
  -- posted history must not vanish because a category row was removed.
  account_id  UUID        NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  -- Seeded rows are is_system: they exist in every branch by construction, which
  -- is what makes "listrik across all four branches" a question you can ask.
  -- Deleting one would make that comparison silently branch-dependent.
  is_system   BOOLEAN     NOT NULL DEFAULT false,
  sort_order  INT         NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (division_id, name)
);

CREATE INDEX IF NOT EXISTS idx_op_expense_categories_division
  ON operational_expense_categories (division_id);

DO $mig$
DECLARE
  d           RECORD;
  seed        TEXT;
  seeds       TEXT[] := ARRAY['Listrik','Air','Sewa','Internet','Telepon','Gas','Kebersihan','Keamanan','Perbaikan','Lain-lain'];
  i           INT;
  next_num    INT;
  acct_id     UUID;
  acct_name   TEXT;
BEGIN
  FOR d IN
    SELECT dv.id, dv.expense_account_id, a.name AS parent_name
    FROM divisions dv JOIN accounts a ON a.id = dv.expense_account_id
    WHERE dv.is_system AND lower(dv.name) = 'operasional'
    ORDER BY a.account_number
  LOOP
    FOR i IN 1 .. array_length(seeds, 1) LOOP
      seed := seeds[i];
      CONTINUE WHEN EXISTS (
        SELECT 1 FROM operational_expense_categories
        WHERE division_id = d.id AND name = seed
      );

      -- Fully qualified, matching the expense-category convention: the tree view
      -- makes the parent obvious, but the CoA export is a flat list sorted by
      -- number, where "Listrik" alone would be ambiguous across branches.
      acct_name := d.parent_name || ' - ' || seed;
      SELECT COALESCE(MAX(account_number), 49999) + 1 INTO next_num
        FROM accounts WHERE account_number BETWEEN 50000 AND 59999;

      INSERT INTO accounts (name, account_number, account_type, parent_id, is_system)
        VALUES (acct_name, next_num, 'expense', d.expense_account_id, true)
        RETURNING id INTO acct_id;

      INSERT INTO operational_expense_categories (division_id, name, account_id, is_system, sort_order)
        VALUES (d.id, seed, acct_id, true, i * 10);
    END LOOP;
  END LOOP;
END
$mig$;

-- ── The bills ───────────────────────────────────────────────────────────────
--
-- Its own table rather than an expense invoice with a flag, for the same reason
-- Pembelanjaan Harian is: what differs is not the debit but the settlement. A
-- bill has no line items, receives no goods, and is usually paid the moment it
-- is recorded — so there are no lines, no unit conversion and no stock path at
-- all. One row, one balanced entry: Dr the category's account, Cr whatever paid
-- it.
CREATE TABLE IF NOT EXISTS operational_expenses (
  id     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  number TEXT NOT NULL UNIQUE,
  date   DATE NOT NULL,

  -- The branch that incurred it. RESTRICT, not SET NULL: an overhead belonging
  -- to no branch cannot be compared against any other branch's.
  branch_id   UUID NOT NULL REFERENCES branches(id) ON DELETE RESTRICT,
  category_id UUID NOT NULL REFERENCES operational_expense_categories(id) ON DELETE RESTRICT,

  -- The account actually debited and the account actually credited, both frozen
  -- onto the row rather than resolved at read time. Repointing a branch at a new
  -- cash box, or a category at a new account, must not rewrite where last
  -- month's electricity came from — the same rule
  -- daily_purchases.petty_cash_account_id follows.
  debit_account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  credit_account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,

  amount BIGINT NOT NULL CHECK (amount > 0),

  vendor_id UUID REFERENCES vendors(id) ON DELETE SET NULL,
  -- Meter number, bill period, receipt number — what makes a disputed bill
  -- resolvable a month later.
  reference  TEXT NOT NULL DEFAULT '',
  notes      TEXT NOT NULL DEFAULT '',
  photo_path TEXT,

  -- Reverse-and-keep, never rewrite: a month that was reported on once must keep
  -- reporting the same way.
  status TEXT NOT NULL DEFAULT 'posted' CHECK (status IN ('posted', 'cancelled')),

  created_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  cancelled_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
  cancelled_at  TIMESTAMPTZ,
  cancel_reason TEXT        NOT NULL DEFAULT '',

  -- Paying an account out of itself is a typo that would post a balanced,
  -- meaningless entry.
  CONSTRAINT operational_expense_distinct_accounts CHECK (debit_account_id <> credit_account_id)
);

CREATE INDEX IF NOT EXISTS idx_operational_expenses_branch_date
  ON operational_expenses (branch_id, date);
CREATE INDEX IF NOT EXISTS idx_operational_expenses_date ON operational_expenses (date);
CREATE INDEX IF NOT EXISTS idx_operational_expenses_category ON operational_expenses (category_id);

-- BO-000001 upward, independent of the invoice and Pembelanjaan Harian
-- sequences so the three never look like each other's neighbours.
CREATE SEQUENCE IF NOT EXISTS operational_expense_number_seq START 1;
