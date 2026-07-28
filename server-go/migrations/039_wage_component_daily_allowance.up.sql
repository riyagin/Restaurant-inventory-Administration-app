-- 039: 'daily_allowance' wage component type.
--
-- A daily_allowance component (e.g. uang makan handed out in cash every day) is
-- part of the employee's wage structure and must appear on the payslip, but it is
-- NOT part of the monthly transfer: it is disbursed manually, day by day, outside
-- payroll. So it is excluded from allowance_total / gross_pay / net_pay and shown
-- as an informational block on the slip instead.
--
-- It is still snapshotted into payroll_line_components at generation time (with
-- calc_method honoured, so a per_present_day rate resolves to rate × present days)
-- so the slip can state how much was already handed out during the period.
ALTER TABLE wage_components DROP CONSTRAINT wage_components_type_check;

ALTER TABLE wage_components
  ADD CONSTRAINT wage_components_type_check
  CHECK (type IN ('allowance', 'bonus', 'deduction', 'daily_allowance'));
