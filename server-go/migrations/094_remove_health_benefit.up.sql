-- 094: Remove Tunjangan Kesehatan (the health-claim fund of migration 085).
--
-- Health claims no longer exist, and the employer retirement fund is Dana
-- Kesejahteraan (migration 090). The 'benefit_fund' component type goes away
-- with every component of that type; payroll snapshots keep their own name/type
-- copy, so past payroll lines are untouched (they only lose the catalog link).
--
-- Money already in the fund is NOT moved here — it is left for the accountant:
--   * accounts 21100 Dana Kesehatan Karyawan / 21200 Dana Pensiun Karyawan keep
--     their balances and stop being system accounts, so they can be cleared and
--     closed by hand from the chart of accounts;
--   * benefit_claims and benefit_fund_movements stay as a read-only archive (the
--     per-employee pension owed is SUM(amount) WHERE fund = 'pension'); no code
--     reads or writes them any more.

DELETE FROM employee_wage_components
WHERE wage_component_id IN (SELECT id FROM wage_components WHERE type = 'benefit_fund');
DELETE FROM position_wage_template_components
WHERE wage_component_id IN (SELECT id FROM wage_components WHERE type = 'benefit_fund');
DELETE FROM performance_grade_component_percents
WHERE wage_component_id IN (SELECT id FROM wage_components WHERE type = 'benefit_fund');
UPDATE payroll_line_components SET wage_component_id = NULL
WHERE wage_component_id IN (SELECT id FROM wage_components WHERE type = 'benefit_fund');
DELETE FROM wage_components WHERE type = 'benefit_fund';  -- service_charge_settings: ON DELETE SET NULL

ALTER TABLE wage_components DROP CONSTRAINT wage_components_type_check;
ALTER TABLE wage_components
  ADD CONSTRAINT wage_components_type_check
  CHECK (type IN ('allowance', 'bonus', 'deduction', 'daily_allowance', 'welfare_fund'));

UPDATE accounts SET is_system = false WHERE account_number IN (21100, 21200);

COMMENT ON TABLE benefit_claims IS
  'ARSIP (migrasi 094): klaim Tunjangan Kesehatan sudah dihapus dari aplikasi. Hanya untuk referensi.';
COMMENT ON TABLE benefit_fund_movements IS
  'ARSIP (migrasi 094): saldo dana pensiun per karyawan = SUM(amount) WHERE fund = ''pension''. Diselesaikan manual oleh akuntan.';
