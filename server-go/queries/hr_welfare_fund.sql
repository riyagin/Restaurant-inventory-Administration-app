-- Dana Kesejahteraan Karyawan (retirement fund). See migration 090.

-- name: GetWelfareMonthlyAmount :one
-- The employee's per-period contribution: the sum of 'welfare_fund' components on
-- the wage structure in force on the given date. 0 when none.
SELECT COALESCE(SUM(ewc.amount), 0)::bigint AS monthly_amount
FROM wage_structures ws
JOIN employee_wage_components ewc ON ewc.wage_structure_id = ws.id
JOIN wage_components wc ON wc.id = ewc.wage_component_id AND wc.type = 'welfare_fund'
WHERE ws.id = (
  SELECT w2.id FROM wage_structures w2
  WHERE w2.employee_id = sqlc.arg(employee_id)
    AND w2.effective_date <= sqlc.arg(as_of)::date
    AND (w2.end_date IS NULL OR w2.end_date >= sqlc.arg(as_of)::date)
  ORDER BY w2.effective_date DESC
  LIMIT 1
);

-- name: GetWelfareBalance :one
SELECT COALESCE(SUM(amount), 0)::bigint AS balance
FROM welfare_fund_movements
WHERE employee_id = $1;

-- name: CreateWelfareMovement :one
INSERT INTO welfare_fund_movements (
  employee_id, movement_type, amount, period_month, movement_date,
  payroll_period_id, journal_entry_id, note, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListWelfareMovementsByEmployee :many
SELECT m.*, pp.period_month AS payroll_month
FROM welfare_fund_movements m
LEFT JOIN payroll_periods pp ON pp.id = m.payroll_period_id
WHERE m.employee_id = $1
ORDER BY m.movement_date DESC, m.created_at DESC;

-- name: CountWelfareContributionsForPeriod :one
SELECT COUNT(*)::int FROM welfare_fund_movements
WHERE payroll_period_id = $1 AND movement_type = 'contribution';

-- name: ListWelfareContributionsForPeriod :many
-- Per payroll line: the employee's welfare_fund contribution snapshotted at
-- generation (and possibly adjusted at review), with the branch the expense
-- belongs to.
SELECT pl.employee_id, e.full_name, e.branch_id, b.name AS branch_name,
       COALESCE(SUM(plc.amount), 0)::bigint AS contribution
FROM payroll_lines pl
JOIN employees e ON e.id = pl.employee_id
LEFT JOIN branches b ON b.id = e.branch_id
JOIN payroll_line_components plc ON plc.payroll_line_id = pl.id AND plc.type = 'welfare_fund'
WHERE pl.payroll_period_id = $1
GROUP BY pl.employee_id, e.full_name, e.branch_id, b.name;

-- name: CountUnsettledPayrollForEmployee :one
-- Payroll periods holding this employee whose fund contribution has not reached
-- the fund yet: still open, or closed but the ledger posting is not done.
SELECT COUNT(*)::int
FROM payroll_lines pl
JOIN payroll_periods pp ON pp.id = pl.payroll_period_id
LEFT JOIN payroll_postings ps ON ps.period_id = pp.id
WHERE pl.employee_id = $1
  AND (pp.status = 'open' OR ps.status IS DISTINCT FROM 'posted');
