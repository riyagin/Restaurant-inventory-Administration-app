-- Extend half-day corrections to cover early departures. A half day now has a
-- TYPE: 'late'  = arrived past the max late threshold (lost time at the START of
-- the day) or 'early' = came in on time but left in the afternoon (lost time at
-- the END of the day). Either way half_day_lost_minutes holds the lost working
-- time that drives the wage deduction, so payroll is unchanged.
--
-- Each type has its own performance policy so the point deduction can differ:
-- the old single 'half_day' rule becomes 'half_day_late' and a new 'half_day_early'
-- rule is added.

ALTER TABLE attendance_records
  ADD COLUMN half_day_type TEXT CHECK (half_day_type IN ('late', 'early'));

-- Existing half-day rows were all late arrivals.
UPDATE attendance_records SET half_day_type = 'late' WHERE is_half_day = true AND half_day_type IS NULL;

-- Split the half_day performance rule into late/early variants.
ALTER TABLE performance_policies DROP CONSTRAINT performance_policies_rule_type_check;
UPDATE performance_policies SET rule_type = 'half_day_late' WHERE rule_type = 'half_day';
ALTER TABLE performance_policies
  ADD CONSTRAINT performance_policies_rule_type_check
  CHECK (rule_type IN ('late', 'early_leave', 'missing_checkout', 'absent_no_leave',
                       'half_day_late', 'half_day_early', 'manual'));
