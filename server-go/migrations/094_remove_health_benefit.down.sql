-- Brings back the type and an empty "Tunjangan Kesehatan" component. The
-- per-employee assignments deleted by 094 are not restored.
COMMENT ON TABLE benefit_fund_movements IS NULL;
COMMENT ON TABLE benefit_claims IS NULL;

UPDATE accounts SET is_system = true WHERE account_number IN (21100, 21200);

ALTER TABLE wage_components DROP CONSTRAINT wage_components_type_check;
ALTER TABLE wage_components
  ADD CONSTRAINT wage_components_type_check
  CHECK (type IN ('allowance', 'bonus', 'deduction', 'daily_allowance', 'benefit_fund', 'welfare_fund'));

INSERT INTO wage_components (name, type, is_fixed, calc_method)
VALUES ('Tunjangan Kesehatan', 'benefit_fund', true, 'fixed')
ON CONFLICT (name) DO NOTHING;
