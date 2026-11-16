ALTER TABLE payroll_lines
  DROP COLUMN IF EXISTS alpha_deduction,
  DROP COLUMN IF EXISTS alpha_unpaid_days;
