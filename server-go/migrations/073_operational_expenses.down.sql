-- Reversing this drops the bills and the breakdown, but deliberately does NOT
-- delete the sub-accounts it created: once anything has posted to one it carries
-- journal lines, and deleting an account a balanced entry references would tear
-- the books apart. The untouched ones are left too — telling them apart would
-- mean guessing, and a stray zero-balance account under the right parent is a
-- far smaller problem than a hole in the journal.
DROP TABLE IF EXISTS operational_expenses;
DROP SEQUENCE IF EXISTS operational_expense_number_seq;
DROP TABLE IF EXISTS operational_expense_categories;

ALTER TABLE divisions DROP COLUMN IF EXISTS is_system;
