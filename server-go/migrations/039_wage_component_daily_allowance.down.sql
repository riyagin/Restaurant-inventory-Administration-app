-- Revert to the three original component types. Any daily_allowance component is
-- folded back into a plain allowance (it would otherwise violate the constraint),
-- which puts it back inside gross pay.
UPDATE wage_components SET type = 'allowance' WHERE type = 'daily_allowance';

ALTER TABLE wage_components DROP CONSTRAINT wage_components_type_check;

ALTER TABLE wage_components
  ADD CONSTRAINT wage_components_type_check
  CHECK (type IN ('allowance', 'bonus', 'deduction'));
