-- ── Work schedules ──────────────────────────────────────────────────────────

-- name: ListWorkSchedules :many
SELECT id, branch_id, work_start, work_end, grace_minutes, early_leave_minutes, work_days
FROM work_schedules
ORDER BY branch_id;

-- name: GetWorkScheduleByBranch :one
SELECT id, branch_id, work_start, work_end, grace_minutes, early_leave_minutes, work_days
FROM work_schedules
WHERE branch_id = $1;

-- name: UpsertWorkSchedule :one
INSERT INTO work_schedules (id, branch_id, work_start, work_end, grace_minutes, early_leave_minutes, work_days)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
ON CONFLICT (branch_id) DO UPDATE SET
    work_start = EXCLUDED.work_start,
    work_end = EXCLUDED.work_end,
    grace_minutes = EXCLUDED.grace_minutes,
    early_leave_minutes = EXCLUDED.early_leave_minutes,
    work_days = EXCLUDED.work_days
RETURNING id, branch_id, work_start, work_end, grace_minutes, early_leave_minutes, work_days;

-- name: DeleteWorkSchedule :exec
DELETE FROM work_schedules WHERE id = $1;

-- ── Public holidays ─────────────────────────────────────────────────────────

-- name: ListPublicHolidays :many
SELECT id, date, name
FROM public_holidays
ORDER BY date DESC;

-- name: CreatePublicHoliday :one
INSERT INTO public_holidays (id, date, name)
VALUES (gen_random_uuid(), $1, $2)
RETURNING id, date, name;

-- name: DeletePublicHoliday :exec
DELETE FROM public_holidays WHERE id = $1;

-- name: IsPublicHoliday :one
SELECT EXISTS (SELECT 1 FROM public_holidays WHERE date = $1) AS exists;

-- ── Attendance devices ──────────────────────────────────────────────────────

-- name: ListAttendanceDevices :many
SELECT d.id, d.name, d.branch_id, d.api_key_hash, d.is_active, d.created_at,
       b.name AS branch_name
FROM attendance_devices d
LEFT JOIN branches b ON b.id = d.branch_id
ORDER BY d.created_at DESC;

-- name: GetActiveDeviceByKeyHash :one
SELECT id, name, branch_id, api_key_hash, is_active, created_at
FROM attendance_devices
WHERE api_key_hash = $1 AND is_active = true;

-- name: CreateAttendanceDevice :one
INSERT INTO attendance_devices (id, name, branch_id, api_key_hash, is_active)
VALUES (gen_random_uuid(), $1, $2, $3, true)
RETURNING id, name, branch_id, api_key_hash, is_active, created_at;

-- name: SetAttendanceDeviceActive :one
UPDATE attendance_devices
SET is_active = $1
WHERE id = $2
RETURNING id, name, branch_id, api_key_hash, is_active, created_at;

-- name: DeleteAttendanceDevice :exec
DELETE FROM attendance_devices WHERE id = $1;

-- ── Employees lookup (device + reconcile) ───────────────────────────────────

-- name: GetEmployeeByCode :one
SELECT id, employee_code, full_name, branch_id, status, photo_path
FROM employees
WHERE employee_code = $1;

-- name: ListDeviceRosterByBranch :many
SELECT employee_code, full_name, photo_path
FROM employees
WHERE branch_id = $1 AND status = 'active'
ORDER BY full_name;

-- name: ListActiveEmployeesForReconcile :many
SELECT id, employee_code, full_name, branch_id
FROM employees
WHERE status = 'active';

-- ── Attendance records ──────────────────────────────────────────────────────

-- name: GetAttendanceRecordByEmployeeDate :one
SELECT id, employee_id, date, check_in, check_out, check_in_source, check_out_source,
       check_in_photo_path, device_id, status, is_late, late_minutes,
       is_early_leave, early_leave_minutes, is_missing_checkout, note
FROM attendance_records
WHERE employee_id = $1 AND date = $2;

-- name: GetAttendanceRecordByID :one
SELECT id, employee_id, date, check_in, check_out, check_in_source, check_out_source,
       check_in_photo_path, device_id, status, is_late, late_minutes,
       is_early_leave, early_leave_minutes, is_missing_checkout, note
FROM attendance_records
WHERE id = $1;

-- name: InsertAttendanceRecord :one
INSERT INTO attendance_records (
    id, employee_id, date, check_in, check_out, check_in_source, check_out_source,
    check_in_photo_path, device_id, status, is_late, late_minutes,
    is_early_leave, early_leave_minutes, is_missing_checkout, note
)
VALUES (
    gen_random_uuid(), $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15
)
RETURNING id, employee_id, date, check_in, check_out, check_in_source, check_out_source,
          check_in_photo_path, device_id, status, is_late, late_minutes,
          is_early_leave, early_leave_minutes, is_missing_checkout, note;

-- name: UpdateAttendanceRecord :one
UPDATE attendance_records SET
    check_in = $1,
    check_out = $2,
    check_in_source = $3,
    check_out_source = $4,
    check_in_photo_path = $5,
    device_id = $6,
    status = $7,
    is_late = $8,
    late_minutes = $9,
    is_early_leave = $10,
    early_leave_minutes = $11,
    is_missing_checkout = $12,
    note = $13
WHERE id = $14
RETURNING id, employee_id, date, check_in, check_out, check_in_source, check_out_source,
          check_in_photo_path, device_id, status, is_late, late_minutes,
          is_early_leave, early_leave_minutes, is_missing_checkout, note;

-- name: InsertAbsentRecord :exec
INSERT INTO attendance_records (id, employee_id, date, status)
VALUES (gen_random_uuid(), $1, $2, 'absent')
ON CONFLICT (employee_id, date) DO NOTHING;

-- ── Fingerprint imports ─────────────────────────────────────────────────────

-- name: CreateFingerprintImport :one
INSERT INTO fingerprint_imports (id, filename, imported_by, row_count, matched_count)
VALUES (gen_random_uuid(), $1, $2, $3, $4)
RETURNING id, filename, imported_by, row_count, matched_count, created_at;
