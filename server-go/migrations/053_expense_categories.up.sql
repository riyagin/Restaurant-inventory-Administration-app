-- Expense categories: a real COA level under a division's operational expense
-- account.
--
-- Until now every expense invoice for a division landed on the single account
-- "Beban - <cabang> - <divisi>" (handler.invoiceExpenseAccountID). That answers
-- "how much did this division spend" and nothing else — listrik, sewa, ATK and
-- perbaikan are one undifferentiated number.
--
-- A category is a named child account under that division expense account. The
-- invoice picks a category; the journal debits the child instead of the parent.
-- Because the COA rolls a parent up from its children, the division total is
-- unchanged — it is only now broken down.
--
-- Deliberately NOT reusing division_categories: that table is the POS import's
-- revenue-label list (client SalesImport matches POS labels against those names
-- to auto-fill revenue accounts). Same shape, opposite side of the books.

CREATE TABLE expense_categories (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  division_id UUID        NOT NULL REFERENCES divisions(id) ON DELETE CASCADE,
  name        TEXT        NOT NULL,
  -- The child account this category posts to. RESTRICT, not CASCADE: an account
  -- carrying posted history must not vanish because a category row was removed.
  account_id  UUID        NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  created_at  TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE (division_id, name)
);

CREATE INDEX idx_expense_categories_division ON expense_categories(division_id);

-- Nullable by design. Expenses booked before this migration have no category,
-- and a division with no categories defined keeps posting to its parent account
-- exactly as before. SET NULL rather than CASCADE — deleting a category must
-- never delete an invoice.
ALTER TABLE invoices
  ADD COLUMN expense_category_id UUID REFERENCES expense_categories(id) ON DELETE SET NULL;

CREATE INDEX idx_invoices_expense_category ON invoices(expense_category_id);
