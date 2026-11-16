-- Alpha: a working day the employee missed without permission, i.e. an
-- attendance record left 'absent' (approved leave writes 'leave', holidays are
-- never reconciled, an off-site permit makes the day present). Derived, never
-- stored: a later correction or backdated leave changes the answer on its own.

-- name: ListAbsentDatesForEmployee :many
SELECT date
FROM attendance_records
WHERE employee_id = $1
  AND date >= sqlc.arg(date_from)
  AND date <  sqlc.arg(date_to)
  AND status = 'absent'
ORDER BY date;

-- name: ListAbsentDatesInRange :many
-- Every employee's absent dates in [date_from, date_to), for the all-employee
-- leave balance list.
SELECT employee_id, date
FROM attendance_records
WHERE date >= sqlc.arg(date_from)
  AND date <  sqlc.arg(date_to)
  AND status = 'absent'
ORDER BY employee_id, date;

-- name: SetPayrollLineAlpha :exec
-- The Alpha share of a line's unpaid_leave_days / unpaid_leave_deduction.
UPDATE payroll_lines
SET alpha_unpaid_days = sqlc.arg(alpha_unpaid_days),
    alpha_deduction   = sqlc.arg(alpha_deduction)
WHERE id = sqlc.arg(id);

-- name: GetPayrollLineAlpha :one
SELECT alpha_unpaid_days, alpha_deduction
FROM payroll_lines
WHERE id = $1;

-- name: ListPayrollLineAlphaForPeriod :many
SELECT id, alpha_unpaid_days, alpha_deduction
FROM payroll_lines
WHERE payroll_period_id = $1;
