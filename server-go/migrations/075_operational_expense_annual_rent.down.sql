DROP TABLE IF EXISTS operational_expense_amortizations;

ALTER TABLE branches DROP COLUMN IF EXISTS prepaid_rent_account_id;

-- The per-branch prepaid accounts are left in place if they carry history;
-- only an unused parent is removed.
DELETE FROM accounts a
WHERE a.account_number = 10500
  AND NOT EXISTS (SELECT 1 FROM accounts c WHERE c.parent_id = a.id)
  AND NOT EXISTS (SELECT 1 FROM journal_lines jl WHERE jl.account_id = a.id);

ALTER TABLE operational_expenses
  DROP CONSTRAINT IF EXISTS operational_expense_term_check,
  DROP CONSTRAINT IF EXISTS operational_expense_billing_check,
  DROP COLUMN IF EXISTS term_months,
  DROP COLUMN IF EXISTS due_date,
  DROP COLUMN IF EXISTS billing;
