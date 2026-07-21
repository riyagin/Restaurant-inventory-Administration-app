-- ── Performance policies ────────────────────────────────────────────────────

-- name: ListPerformancePolicies :many
SELECT id, name, rule_type, threshold_minutes, points, max_occurrences_per_month, is_active, created_at
FROM performance_policies
ORDER BY rule_type, threshold_minutes NULLS FIRST, name;

-- name: ListActivePerformancePoliciesByRule :many
SELECT id, name, rule_type, threshold_minutes, points, max_occurrences_per_month, is_active, created_at
FROM performance_policies
WHERE is_active = true AND rule_type = $1
ORDER BY threshold_minutes NULLS FIRST;

-- name: GetPerformancePolicyByID :one
SELECT id, name, rule_type, threshold_minutes, points, max_occurrences_per_month, is_active, created_at
FROM performance_policies
WHERE id = $1;

-- name: CreatePerformancePolicy :one
INSERT INTO performance_policies (id, name, rule_type, threshold_minutes, points, max_occurrences_per_month, is_active)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
RETURNING id, name, rule_type, threshold_minutes, points, max_occurrences_per_month, is_active, created_at;

-- name: UpdatePerformancePolicy :one
UPDATE performance_policies
SET name = $1, rule_type = $2, threshold_minutes = $3, points = $4,
    max_occurrences_per_month = $5, is_active = $6
WHERE id = $7
RETURNING id, name, rule_type, threshold_minutes, points, max_occurrences_per_month, is_active, created_at;

-- name: SetPerformancePolicyActive :one
UPDATE performance_policies
SET is_active = $1
WHERE id = $2
RETURNING id, name, rule_type, threshold_minutes, points, max_occurrences_per_month, is_active, created_at;

-- name: DeletePerformancePolicy :exec
DELETE FROM performance_policies WHERE id = $1;

-- name: CountPolicyViolations :one
SELECT COUNT(*) FROM performance_violations WHERE policy_id = $1;

-- ── Performance violations ──────────────────────────────────────────────────

-- name: InsertAutoViolation :exec
INSERT INTO performance_violations (id, employee_id, policy_id, attendance_record_id, date, points, source, note)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, 'auto', $6)
ON CONFLICT (policy_id, attendance_record_id) DO NOTHING;

-- name: InsertManualViolation :one
INSERT INTO performance_violations (id, employee_id, policy_id, attendance_record_id, date, points, source, note, created_by)
VALUES (gen_random_uuid(), $1, $2, NULL, $3, $4, 'manual', $5, $6)
RETURNING id, employee_id, policy_id, attendance_record_id, date, points, source, note, created_by, created_at;

-- name: GetViolationByID :one
SELECT id, employee_id, policy_id, attendance_record_id, date, points, source, note, created_by, created_at
FROM performance_violations
WHERE id = $1;

-- name: DeleteViolation :exec
DELETE FROM performance_violations WHERE id = $1;

-- name: DeleteAutoViolationsByRecord :exec
DELETE FROM performance_violations
WHERE attendance_record_id = $1 AND source = 'auto';

-- name: DeleteAutoViolationsForRange :exec
DELETE FROM performance_violations
WHERE source = 'auto'
  AND date >= $1
  AND date <= $2;

-- name: CountPolicyOccurrencesInMonth :one
SELECT COUNT(*)
FROM performance_violations
WHERE policy_id = $1
  AND employee_id = $2
  AND date >= $3
  AND date < ($3::date + INTERVAL '1 month');

-- name: CountAbsentDaysBeforeInMonth :one
-- Number of 'absent' attendance days the employee already has in the month,
-- strictly before `before_date`. Used to apply the monthly absence grace: the
-- first N absent days each month carry no performance violation.
SELECT COUNT(*)
FROM attendance_records
WHERE employee_id = $1
  AND status = 'absent'
  AND date >= $2
  AND date < $3;

-- name: SumViolationPointsInMonth :one
SELECT COALESCE(SUM(points), 0)::int AS total
FROM performance_violations
WHERE employee_id = $1
  AND date >= $2
  AND date < ($2::date + INTERVAL '1 month');

-- name: ListViolationsForEmployeeMonth :many
SELECT v.id, v.employee_id, v.policy_id, v.attendance_record_id, v.date, v.points,
       v.source, v.note, v.created_by, v.created_at,
       p.name AS policy_name, p.rule_type AS rule_type
FROM performance_violations v
LEFT JOIN performance_policies p ON p.id = v.policy_id
WHERE v.employee_id = $1
  AND v.date >= $2
  AND v.date < ($2::date + INTERVAL '1 month')
ORDER BY v.date, v.created_at;

-- ── Finalized attendance records for a date (engine input) ──────────────────

-- name: ListAttendanceRecordsForDate :many
SELECT id, employee_id, date, check_in, check_out, check_in_source, check_out_source,
       check_in_photo_path, device_id, status, is_late, late_minutes,
       is_early_leave, early_leave_minutes, is_missing_checkout, note,
       is_half_day, half_day_lost_minutes, half_day_type, is_missing_checkin, is_no_punch
FROM attendance_records
WHERE date = $1;

-- ── Performance scores ──────────────────────────────────────────────────────

-- name: UpsertPerformanceScore :one
INSERT INTO performance_scores (id, employee_id, period_month, score)
VALUES (gen_random_uuid(), $1, $2, $3)
ON CONFLICT (employee_id, period_month) DO UPDATE SET score = EXCLUDED.score
RETURNING id, employee_id, period_month, score;

-- name: GetPerformanceScore :one
SELECT id, employee_id, period_month, score
FROM performance_scores
WHERE employee_id = $1 AND period_month = $2;
