DROP TABLE IF EXISTS benefit_fund_movements;
DROP TABLE IF EXISTS benefit_claims;

-- Accounts are left in place when they carry journal lines (FK); otherwise removed.
DELETE FROM accounts a
WHERE a.account_number IN (21100, 21200)
  AND NOT EXISTS (SELECT 1 FROM journal_lines jl WHERE jl.account_id = a.id);

UPDATE wage_components SET type = 'allowance' WHERE type = 'benefit_fund';
ALTER TABLE wage_components DROP CONSTRAINT wage_components_type_check;
ALTER TABLE wage_components
  ADD CONSTRAINT wage_components_type_check
  CHECK (type IN ('allowance', 'bonus', 'deduction', 'daily_allowance'));
