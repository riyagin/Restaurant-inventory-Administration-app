-- Allow an employee to be marked as "resigned" (mengundurkan diri), distinct from
-- a plain inactive flag. resign_date records the effective resignation date.
ALTER TABLE employees DROP CONSTRAINT IF EXISTS employees_status_check;
ALTER TABLE employees
  ADD CONSTRAINT employees_status_check
  CHECK (status IN ('active', 'inactive', 'resigned'));

ALTER TABLE employees
  ADD COLUMN IF NOT EXISTS resign_date DATE;
