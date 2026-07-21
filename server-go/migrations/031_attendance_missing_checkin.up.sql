-- "No check-in" anomaly + policy. A record can end up with only an evening punch
-- (a check-out but no check-in) — e.g. the morning scan was missed and even after
-- fingerprint consolidation the only event is in the evening. This mirrors the
-- existing is_missing_checkout anomaly in the opposite direction, and gets its own
-- configurable performance policy (rule_type 'missing_checkin').

ALTER TABLE attendance_records
  ADD COLUMN is_missing_checkin BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE performance_policies DROP CONSTRAINT performance_policies_rule_type_check;
ALTER TABLE performance_policies
  ADD CONSTRAINT performance_policies_rule_type_check
  CHECK (rule_type IN ('late', 'early_leave', 'missing_checkout', 'missing_checkin',
                       'absent_no_leave', 'half_day_late', 'half_day_early', 'manual'));
