-- Attendance-driven wage components. A component's calc_method decides how its
-- per-employee `amount` (in employee_wage_components) is interpreted at payroll
-- generation:
--   'fixed'           → amount is the full period figure, used as-is (existing behaviour)
--   'per_present_day' → amount is a PER-DAY rate; the payroll multiplies it by the
--                       number of 'present' attendance days in the period.
-- Lets HR model things like a daily meal allowance (uang makan Rp15.000/hari) that
-- scales with how many days the employee actually showed up.
ALTER TABLE wage_components
  ADD COLUMN calc_method TEXT NOT NULL DEFAULT 'fixed'
  CHECK (calc_method IN ('fixed', 'per_present_day'));
