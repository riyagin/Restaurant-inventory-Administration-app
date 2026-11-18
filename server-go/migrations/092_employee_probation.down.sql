-- Probation employees fall back to permanent (probation is the start of a PKWTT
-- relationship); their probation end date is cleared with it.
UPDATE employees
SET employment_type = 'permanent', contract_end_date = NULL, updated_at = now()
WHERE employment_type = 'probation';

ALTER TABLE employees DROP CONSTRAINT IF EXISTS employees_employment_type_check;
ALTER TABLE employees
  ADD CONSTRAINT employees_employment_type_check
  CHECK (employment_type IN ('permanent', 'contract'));
