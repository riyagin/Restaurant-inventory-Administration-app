-- Half-day attendance correction. A manager can reclassify a day where the
-- employee arrived past the maximum late threshold as a "half day": the person
-- started work from a certain hour, so wage is reduced by the lost working hours
-- (scheduled work start → actual entry) and the performance score is deducted by a
-- separate, configurable 'half_day' policy instead of the normal late rule.
--
-- The day still counts as a present day (per-present-day allowances and monthly
-- present counts are unaffected); the only wage impact is the lost-hours deduction.

-- Per-record half-day flag + the lost minutes (entry beyond scheduled start).
ALTER TABLE attendance_records
  ADD COLUMN is_half_day           BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN half_day_lost_minutes INT     NOT NULL DEFAULT 0;

-- Allow a dedicated 'half_day' performance policy rule.
ALTER TABLE performance_policies DROP CONSTRAINT performance_policies_rule_type_check;
ALTER TABLE performance_policies
  ADD CONSTRAINT performance_policies_rule_type_check
  CHECK (rule_type IN ('late', 'early_leave', 'missing_checkout', 'absent_no_leave', 'half_day', 'manual'));

-- Payroll: snapshot the lost hours and the resulting deduction on each line.
ALTER TABLE payroll_lines
  ADD COLUMN half_day_hours     NUMERIC(5,2) NOT NULL DEFAULT 0,
  ADD COLUMN half_day_deduction BIGINT       NOT NULL DEFAULT 0;
