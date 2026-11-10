-- Hourly pay cut for attendance errors. Arriving after the grace window and
-- leaving before the early-leave tolerance cost the same thing: the pay for the
-- minutes not worked, each end rounded up to a 15-minute block and capped at one
-- day's pay. The cut is frozen onto the payroll line at generation, like every
-- other line amount, and the per-day breakdown is recomputed from attendance on
-- demand (service.AttendanceCutsForPeriod).
--
-- Half-day corrections keep their own deduction (half_day_deduction) and are
-- skipped here, so a reclassified day is never cut twice.
ALTER TABLE payroll_lines
  ADD COLUMN attendance_cut_minutes   INT    NOT NULL DEFAULT 0 CHECK (attendance_cut_minutes >= 0),
  ADD COLUMN attendance_cut_deduction BIGINT NOT NULL DEFAULT 0 CHECK (attendance_cut_deduction >= 0);
