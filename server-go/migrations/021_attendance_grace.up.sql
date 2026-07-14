-- Monthly absence grace for the attendance performance score.
--
-- Each employee is expected to attend at least (scheduled work days in the month
-- − grace) days. The first `absence_grace_days` unexcused-absence days each month
-- therefore carry no performance deduction; only absences beyond the grace feed
-- the `absent_no_leave` violation and lower the monthly score.
--
-- Stored on the hr_settings singleton so managers can tune it from the HR
-- Settings page. Default 4 matches the "max work days − 4" rule.

ALTER TABLE hr_settings
  ADD COLUMN absence_grace_days INT NOT NULL DEFAULT 4 CHECK (absence_grace_days >= 0);
