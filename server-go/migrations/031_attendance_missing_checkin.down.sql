ALTER TABLE performance_policies DROP CONSTRAINT performance_policies_rule_type_check;
UPDATE performance_policies SET rule_type = 'manual' WHERE rule_type = 'missing_checkin';
ALTER TABLE performance_policies
  ADD CONSTRAINT performance_policies_rule_type_check
  CHECK (rule_type IN ('late', 'early_leave', 'missing_checkout', 'absent_no_leave',
                       'half_day_late', 'half_day_early', 'manual'));

ALTER TABLE attendance_records DROP COLUMN IF EXISTS is_missing_checkin;
