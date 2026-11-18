DROP TABLE IF EXISTS welfare_fund_movements;

-- The account is left in place when it carries journal lines (FK); otherwise removed.
DELETE FROM accounts a
WHERE a.account_number = 21300
  AND NOT EXISTS (SELECT 1 FROM journal_lines jl WHERE jl.account_id = a.id);

-- welfare_fund components cannot outlive the type. They become inactive allowances
-- (inactive so a wage structure still holding one does not suddenly start paying
-- it as salary), and the seeded one steps aside so the health component can take
-- back its pre-090 name.
UPDATE wage_components SET type = 'allowance', is_active = false WHERE type = 'welfare_fund';
UPDATE wage_components SET name = 'Dana Kesejahteraan (090 dibatalkan)'
WHERE name = 'Dana Kesejahteraan' AND type = 'allowance';
UPDATE wage_components SET name = 'Dana Kesejahteraan'
WHERE name = 'Tunjangan Kesehatan' AND type = 'benefit_fund'
  AND NOT EXISTS (SELECT 1 FROM wage_components WHERE name = 'Dana Kesejahteraan');

ALTER TABLE wage_components DROP CONSTRAINT wage_components_type_check;
ALTER TABLE wage_components
  ADD CONSTRAINT wage_components_type_check
  CHECK (type IN ('allowance', 'bonus', 'deduction', 'daily_allowance', 'benefit_fund'));
