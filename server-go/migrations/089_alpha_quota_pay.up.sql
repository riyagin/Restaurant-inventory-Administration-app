-- Alpha (a working day missed without permission) now costs leave allowance,
-- and pay once the allowance can't cover it. See service/alpha.go.
--
-- The pay part is already INSIDE unpaid_leave_days / unpaid_leave_deduction,
-- so every net-pay formula and period total stays correct unchanged. These two
-- columns say how much of that figure was Alpha, so the payslip and the period
-- page can show it as its own line. Lines generated before this migration
-- carry 0 (Alpha cost no pay then).
ALTER TABLE payroll_lines
  ADD COLUMN alpha_unpaid_days INT    NOT NULL DEFAULT 0,
  ADD COLUMN alpha_deduction   BIGINT NOT NULL DEFAULT 0;
