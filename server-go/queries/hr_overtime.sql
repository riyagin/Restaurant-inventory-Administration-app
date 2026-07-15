-- ── Overtime Requests ────────────────────────────────────────────────────────

-- name: CreateOvertimeRequest :one
INSERT INTO overtime_requests (id, employee_id, date, hours, reason, created_by)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
RETURNING id, employee_id, date, hours, reason, status, decided_by, decided_at, decision_note, created_by, created_at;

-- name: GetOvertimeRequestByID :one
SELECT id, employee_id, date, hours, reason, status, decided_by, decided_at, decision_note, created_by, created_at
FROM overtime_requests
WHERE id = $1;

-- name: DeleteOvertimeRequest :exec
DELETE FROM overtime_requests WHERE id = $1;

-- name: ApproveOvertimeRequest :one
UPDATE overtime_requests
SET status = 'approved', decided_by = $2, decided_at = now(), decision_note = $3
WHERE id = $1 AND status = 'pending'
RETURNING id, employee_id, date, hours, reason, status, decided_by, decided_at, decision_note, created_by, created_at;

-- name: RejectOvertimeRequest :one
UPDATE overtime_requests
SET status = 'rejected', decided_by = $2, decided_at = now(), decision_note = $3
WHERE id = $1 AND status = 'pending'
RETURNING id, employee_id, date, hours, reason, status, decided_by, decided_at, decision_note, created_by, created_at;

-- name: CancelOvertimeRequest :one
UPDATE overtime_requests
SET status = 'cancelled', decided_by = $2, decided_at = now(), decision_note = $3
WHERE id = $1 AND status IN ('pending', 'approved')
RETURNING id, employee_id, date, hours, reason, status, decided_by, decided_at, decision_note, created_by, created_at;

-- name: SumOvertimeHoursForEmployee :one
-- Only APPROVED overtime feeds payroll.
SELECT COALESCE(SUM(hours), 0)::float8 AS total_hours
FROM overtime_requests
WHERE employee_id = $1
  AND date >= $2 AND date <= $3
  AND status = 'approved';

-- name: ListOvertimeRequestsForEmployee :many
SELECT id, employee_id, date, hours, reason, created_by, created_at
FROM overtime_requests
WHERE employee_id = $1
  AND date >= $2 AND date <= $3
ORDER BY date;
