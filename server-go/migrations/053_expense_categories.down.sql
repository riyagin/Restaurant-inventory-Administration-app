-- Drops the category level. The child accounts themselves are left in place:
-- they may carry posted journal lines, and deleting an account that a balanced
-- entry references would tear the books. They simply become ordinary
-- subaccounts of the division expense account, which is what they already are.

DROP INDEX IF EXISTS idx_invoices_expense_category;
ALTER TABLE invoices DROP COLUMN IF EXISTS expense_category_id;

DROP INDEX IF EXISTS idx_expense_categories_division;
DROP TABLE IF EXISTS expense_categories;
