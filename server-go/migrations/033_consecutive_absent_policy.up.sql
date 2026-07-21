-- "Consecutive absence" performance rule. Penalizes being absent multiple
-- scheduled work days in a row (e.g. 2 days absent back-to-back). The rule reuses
-- the threshold_minutes column to hold the number of consecutive absent days that
-- triggers the deduction (default 2 when unset), and fires once — on the day the
-- streak first reaches that count.

ALTER TABLE performance_policies DROP CONSTRAINT performance_policies_rule_type_check;
ALTER TABLE performance_policies
  ADD CONSTRAINT performance_policies_rule_type_check
  CHECK (rule_type IN ('late', 'early_leave', 'missing_checkout', 'missing_checkin',
                       'no_punch', 'absent_no_leave', 'consecutive_absent',
                       'half_day_late', 'half_day_early', 'manual'));
