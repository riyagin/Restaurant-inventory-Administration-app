-- Revert any resigned rows to inactive before restoring the tighter constraint.
UPDATE employees SET status = 'inactive' WHERE status = 'resigned';

ALTER TABLE employees DROP CONSTRAINT IF EXISTS employees_status_check;
ALTER TABLE employees
  ADD CONSTRAINT employees_status_check
  CHECK (status IN ('active', 'inactive'));

ALTER TABLE employees DROP COLUMN IF EXISTS resign_date;
