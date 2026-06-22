-- ── Overtime Requests ────────────────────────────────────────────────────────

-- name: CreateOvertimeRequest :one
INSERT INTO overtime_requests (id, employee_id, date, hours, reason, created_by)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
RETURNING id, employee_id, date, hours, reason, created_by, created_at;

-- name: GetOvertimeRequestByID :one
SELECT id, employee_id, date, hours, reason, created_by, created_at
FROM overtime_requests
WHERE id = $1;

-- name: DeleteOvertimeRequest :exec
DELETE FROM overtime_requests WHERE id = $1;

-- name: SumOvertimeHoursForEmployee :one
SELECT COALESCE(SUM(hours), 0)::float8 AS total_hours
FROM overtime_requests
WHERE employee_id = $1
  AND date >= $2 AND date <= $3;

-- name: ListOvertimeRequestsForEmployee :many
SELECT id, employee_id, date, hours, reason, created_by, created_at
FROM overtime_requests
WHERE employee_id = $1
  AND date >= $2 AND date <= $3
ORDER BY date;
