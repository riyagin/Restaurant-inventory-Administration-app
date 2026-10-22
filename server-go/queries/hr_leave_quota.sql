-- Monthly paid-leave allowance (migration 077). The Go in
-- internal/db/hr_leave_quota.sql.go is hand-written to match this file.

-- name: InsertLeaveQuotaUsage :exec
INSERT INTO leave_quota_usage (leave_request_id, employee_id, date, cost)
VALUES ($1, $2, $3, $4)
ON CONFLICT (employee_id, date) DO NOTHING;

-- name: DeleteLeaveQuotaUsageAfter :exec
DELETE FROM leave_quota_usage
WHERE leave_request_id = $1 AND date > $2;

-- name: SumLeaveQuotaUsage :one
SELECT COALESCE(SUM(cost), 0)::int
FROM leave_quota_usage
WHERE employee_id = $1 AND date >= $2 AND date < $3;

-- name: GetLeaveMonthlyQuota :one
SELECT quota_days FROM leave_monthly_quotas
WHERE employee_id = $1 AND month = $2;

-- name: UpsertLeaveMonthlyQuota :exec
INSERT INTO leave_monthly_quotas (employee_id, month, quota_days)
VALUES ($1, $2, $3)
ON CONFLICT (employee_id, month) DO UPDATE SET quota_days = EXCLUDED.quota_days;

-- name: ListLeaveQuotaMonths :many
-- Usage and override per month in [$2, $3) for one employee. Months with
-- neither are absent; the caller fills them with the default.
SELECT m.month::date AS month,
       COALESCE(u.used, 0)::int AS used_days,
       q.quota_days
FROM (
  SELECT date_trunc('month', date)::date AS month FROM leave_quota_usage
  WHERE employee_id = $1 AND date >= $2 AND date < $3
  UNION
  SELECT month FROM leave_monthly_quotas
  WHERE employee_id = $1 AND month >= $2 AND month < $3
) m
LEFT JOIN (
  SELECT date_trunc('month', date)::date AS month, SUM(cost) AS used
  FROM leave_quota_usage
  WHERE employee_id = $1 AND date >= $2 AND date < $3
  GROUP BY 1
) u ON u.month = m.month
LEFT JOIN leave_monthly_quotas q ON q.employee_id = $1 AND q.month = m.month
ORDER BY m.month;

-- name: ListMonthlyLeaveBalances :many
-- Every active employee's allowance and usage for the month starting $1;
-- $2 is the default allowance for employees without an override.
SELECT e.id, e.full_name, e.employee_code, COALESCE(b.name, 'Tanpa Cabang')::text AS branch_name,
       COALESCE(q.quota_days, $2::int)::int AS quota_days,
       (q.quota_days IS NOT NULL) AS overridden,
       COALESCE((SELECT SUM(u.cost) FROM leave_quota_usage u
                 WHERE u.employee_id = e.id AND u.date >= $1
                   AND u.date < ($1::date + interval '1 month')), 0)::int AS used_days
FROM employees e
LEFT JOIN branches b ON b.id = e.branch_id
LEFT JOIN leave_monthly_quotas q ON q.employee_id = e.id AND q.month = $1
WHERE e.status = 'active'
ORDER BY COALESCE(b.name, 'Tanpa Cabang'), e.full_name;
