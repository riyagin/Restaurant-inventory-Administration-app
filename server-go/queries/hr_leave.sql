-- ── Leave Types ──────────────────────────────────────────────────────────────

-- name: ListLeaveTypes :many
SELECT id, name, is_paid, uses_quota, is_active
FROM leave_types
ORDER BY name;

-- name: ListActiveLeaveTypes :many
SELECT id, name, is_paid, uses_quota, is_active
FROM leave_types
WHERE is_active = true
ORDER BY name;

-- name: GetLeaveTypeByID :one
SELECT id, name, is_paid, uses_quota, is_active
FROM leave_types
WHERE id = $1;

-- name: CreateLeaveType :one
INSERT INTO leave_types (id, name, is_paid, uses_quota, is_active)
VALUES (gen_random_uuid(), $1, $2, $3, $4)
RETURNING id, name, is_paid, uses_quota, is_active;

-- name: UpdateLeaveType :one
UPDATE leave_types
SET name = $1, is_paid = $2, uses_quota = $3, is_active = $4
WHERE id = $5
RETURNING id, name, is_paid, uses_quota, is_active;

-- name: SetLeaveTypeActive :one
UPDATE leave_types
SET is_active = $1
WHERE id = $2
RETURNING id, name, is_paid, uses_quota, is_active;

-- name: DeleteLeaveType :exec
DELETE FROM leave_types WHERE id = $1;

-- name: CountLeaveTypeReferences :one
SELECT COUNT(*) FROM leave_requests WHERE leave_type_id = $1;

-- ── Leave Balances ───────────────────────────────────────────────────────────

-- name: GetLeaveBalance :one
SELECT id, employee_id, year, quota_days, used_days
FROM leave_balances
WHERE employee_id = $1 AND year = $2;

-- name: CreateLeaveBalance :one
INSERT INTO leave_balances (id, employee_id, year, quota_days, used_days)
VALUES (gen_random_uuid(), $1, $2, $3, 0)
RETURNING id, employee_id, year, quota_days, used_days;

-- name: SetLeaveBalanceQuota :one
UPDATE leave_balances
SET quota_days = $1
WHERE employee_id = $2 AND year = $3
RETURNING id, employee_id, year, quota_days, used_days;

-- name: IncrementLeaveBalanceUsed :one
UPDATE leave_balances
SET used_days = used_days + $1
WHERE employee_id = $2 AND year = $3
RETURNING id, employee_id, year, quota_days, used_days;

-- ── Leave Requests ───────────────────────────────────────────────────────────

-- name: GetLeaveRequestByID :one
SELECT id, employee_id, leave_type_id, start_date, end_date, day_count, reason,
       status, decided_by, decided_at, decision_note, created_by, created_at
FROM leave_requests
WHERE id = $1;

-- name: CreateLeaveRequest :one
INSERT INTO leave_requests (
    id, employee_id, leave_type_id, start_date, end_date, day_count, reason,
    status, created_by
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, 'pending', $7)
RETURNING id, employee_id, leave_type_id, start_date, end_date, day_count, reason,
          status, decided_by, decided_at, decision_note, created_by, created_at;

-- name: SetLeaveRequestStatus :one
UPDATE leave_requests
SET status = $1, decided_by = $2, decided_at = now(), decision_note = $3
WHERE id = $4
RETURNING id, employee_id, leave_type_id, start_date, end_date, day_count, reason,
          status, decided_by, decided_at, decision_note, created_by, created_at;

-- name: ListOverlappingRequests :many
SELECT id, employee_id, leave_type_id, start_date, end_date, day_count, reason,
       status, decided_by, decided_at, decision_note, created_by, created_at
FROM leave_requests
WHERE employee_id = $1
  AND status IN ('pending', 'approved')
  AND start_date <= $3
  AND end_date >= $2;

-- name: ListLeaveRequests :many
SELECT lr.id, lr.employee_id, lr.leave_type_id, lr.start_date, lr.end_date,
       lr.day_count, lr.reason, lr.status, lr.decided_by, lr.decided_at,
       lr.decision_note, lr.created_by, lr.created_at,
       e.full_name AS employee_name, e.employee_code, e.branch_id,
       lt.name AS leave_type_name, lt.is_paid, lt.uses_quota
FROM leave_requests lr
JOIN employees e ON e.id = lr.employee_id
JOIN leave_types lt ON lt.id = lr.leave_type_id
WHERE ($1::text = '' OR lr.status = $1)
  AND ($2::uuid IS NULL OR e.branch_id = $2)
  AND ($3::uuid IS NULL OR lr.employee_id = $3)
  AND ($4::int = 0 OR EXTRACT(YEAR FROM lr.start_date) = $4)
ORDER BY lr.created_at DESC;

-- name: ListLeaveRequestsByEmployee :many
SELECT lr.id, lr.employee_id, lr.leave_type_id, lr.start_date, lr.end_date,
       lr.day_count, lr.reason, lr.status, lr.decided_by, lr.decided_at,
       lr.decision_note, lr.created_by, lr.created_at,
       lt.name AS leave_type_name, lt.is_paid, lt.uses_quota
FROM leave_requests lr
JOIN leave_types lt ON lt.id = lr.leave_type_id
WHERE lr.employee_id = $1
ORDER BY lr.start_date DESC;

-- name: ListApprovedUnpaidLeaveOverlapping :many
SELECT lr.id, lr.employee_id, lr.start_date, lr.end_date
FROM leave_requests lr
JOIN leave_types lt ON lt.id = lr.leave_type_id
WHERE lr.employee_id = $1
  AND lr.status = 'approved'
  AND lt.is_paid = false
  AND lr.start_date <= $3
  AND lr.end_date >= $2;

-- name: ListHolidaysInRange :many
SELECT date
FROM public_holidays
WHERE date >= $1 AND date <= $2;

-- ── Attendance upsert for approved leave ─────────────────────────────────────

-- name: UpsertLeaveAttendance :exec
INSERT INTO attendance_records (id, employee_id, date, status)
VALUES (gen_random_uuid(), $1, $2, 'leave')
ON CONFLICT (employee_id, date)
DO UPDATE SET status = 'leave'
WHERE attendance_records.check_in IS NULL;

-- name: HasCheckInOnDate :one
SELECT EXISTS (
    SELECT 1 FROM attendance_records
    WHERE employee_id = $1 AND date = $2 AND check_in IS NOT NULL
) AS exists;

-- name: DeleteLeaveAttendanceWithoutCheckIn :exec
DELETE FROM attendance_records
WHERE employee_id = $1 AND date = $2 AND status = 'leave' AND check_in IS NULL;
