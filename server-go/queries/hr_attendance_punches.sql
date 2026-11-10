-- Raw punch log, attendance permits and review settings (migration 084).

-- name: InsertAttendancePunch :exec
-- Idempotent: a re-imported cumulative attlog.dat re-sends every punch.
INSERT INTO attendance_punches (employee_id, punched_at, work_date, source, direction_hint, device_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (employee_id, punched_at, source) DO NOTHING;

-- name: ListPunchesForWorkDay :many
SELECT punched_at, source, direction_hint
FROM attendance_punches
WHERE employee_id = $1 AND work_date = $2
ORDER BY punched_at;

-- name: GetShiftHoursForEmployeeDate :one
-- The rostered shift's segments for an employee on a date (no row = not rostered).
SELECT sd.start1, sd.end1, sd.start2, sd.end2
FROM shift_assignments sa
JOIN shift_definitions sd ON sd.id = sa.shift_id
WHERE sa.employee_id = $1 AND sa.date = $2;

-- name: ListApprovedPermissionTypes :many
SELECT type
FROM attendance_permissions
WHERE employee_id = $1 AND date = $2 AND status = 'approved';

-- name: CreateAttendancePermission :one
INSERT INTO attendance_permissions (employee_id, date, type, reason, is_backdated, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAttendancePermission :one
SELECT * FROM attendance_permissions WHERE id = $1;

-- name: ApproveAttendancePermission :one
UPDATE attendance_permissions
SET status = 'approved', decided_by = $2, decided_at = now(), decision_note = $3
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: RejectAttendancePermission :one
UPDATE attendance_permissions
SET status = 'rejected', decided_by = $2, decided_at = now(), decision_note = $3
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: CancelAttendancePermission :one
UPDATE attendance_permissions
SET status = 'cancelled', decided_by = $2, decided_at = now(), decision_note = $3
WHERE id = $1 AND status IN ('pending', 'approved')
RETURNING *;

-- name: GetAttendanceReviewSettings :one
SELECT review_late_minutes, review_early_minutes, allow_backdated_permission
FROM hr_settings WHERE id = 1;

-- name: UpdateAttendanceReviewSettings :one
UPDATE hr_settings
SET review_late_minutes = $1, review_early_minutes = $2, allow_backdated_permission = $3, updated_at = now()
WHERE id = 1
RETURNING review_late_minutes, review_early_minutes, allow_backdated_permission;

-- name: SetAttendanceReviewResolved :one
UPDATE attendance_records SET review_resolved = $2 WHERE id = $1
RETURNING id, employee_id, date;
