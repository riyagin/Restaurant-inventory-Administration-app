-- Adds hourly overtime pay alongside the existing day-based overtime_days/overtime_amount.
-- overtime_hourly_rate is a snapshot (rupiah per hour, derived from daily_rate / standard
-- work hours from the employee's branch work_schedule at generation time) so the rate used
-- stays stable even if the schedule changes later; overtime_hours is entered manually during
-- review (mirrors overtime_days), and overtime_hourly_amount is the derived rupiah amount
-- that feeds into gross_pay alongside the day-based overtime_amount.
ALTER TABLE payroll_lines
  ADD COLUMN overtime_hours          NUMERIC(6,2) NOT NULL DEFAULT 0,
  ADD COLUMN overtime_hourly_rate    BIGINT       NOT NULL DEFAULT 0,
  ADD COLUMN overtime_hourly_amount  BIGINT       NOT NULL DEFAULT 0;
