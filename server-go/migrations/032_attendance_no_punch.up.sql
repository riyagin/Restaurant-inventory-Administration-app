-- "No punch" correction + policy. The employee came to work but forgot to check
-- in AND check out, so there is no punch at all and reconciliation would mark the
-- day 'absent'. A manager corrects it to a present day with no punches
-- (is_no_punch), which gets its own configurable 'no_punch' performance policy.

ALTER TABLE attendance_records
  ADD COLUMN is_no_punch BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE performance_policies DROP CONSTRAINT performance_policies_rule_type_check;
ALTER TABLE performance_policies
  ADD CONSTRAINT performance_policies_rule_type_check
  CHECK (rule_type IN ('late', 'early_leave', 'missing_checkout', 'missing_checkin',
                       'no_punch', 'absent_no_leave', 'half_day_late', 'half_day_early', 'manual'));
