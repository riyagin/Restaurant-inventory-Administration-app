ALTER TABLE payroll_lines
  DROP COLUMN IF EXISTS half_day_deduction,
  DROP COLUMN IF EXISTS half_day_hours;

ALTER TABLE performance_policies DROP CONSTRAINT performance_policies_rule_type_check;
ALTER TABLE performance_policies
  ADD CONSTRAINT performance_policies_rule_type_check
  CHECK (rule_type IN ('late', 'early_leave', 'missing_checkout', 'absent_no_leave', 'manual'));

ALTER TABLE attendance_records
  DROP COLUMN IF EXISTS half_day_lost_minutes,
  DROP COLUMN IF EXISTS is_half_day;
