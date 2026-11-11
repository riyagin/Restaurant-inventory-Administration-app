-- name: ListDailyAllowanceByBranchForPeriod :many
-- The uang makan (daily_allowance components) snapshotted on a payroll
-- period's lines, summed per branch: the month's expense for that cash.
SELECT e.branch_id, b.name AS branch_name,
       COALESCE(SUM(plc.amount), 0)::bigint AS total
FROM payroll_lines pl
JOIN employees e ON e.id = pl.employee_id
LEFT JOIN branches b ON b.id = e.branch_id
JOIN payroll_line_components plc ON plc.payroll_line_id = pl.id AND plc.type = 'daily_allowance'
WHERE pl.payroll_period_id = $1
GROUP BY e.branch_id, b.name
ORDER BY b.name;
