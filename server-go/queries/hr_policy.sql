-- Kebijakan HRD alignment (migration 088).

-- ── Policy settings (singleton) ─────────────────────────────────────────────

-- name: GetHRPolicySettings :one
SELECT * FROM hr_policy_settings WHERE id = 1;

-- name: UpdateHRPolicySettings :one
INSERT INTO hr_policy_settings (
  id, payroll_cutoff_day, kasbon_window_start_day, kasbon_window_end_day,
  kasbon_max_amount, kasbon_max_installments, kasbon_permanent_only,
  kasbon_min_score, kasbon_priority_score, updated_by, updated_at)
VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (id) DO UPDATE
SET payroll_cutoff_day = EXCLUDED.payroll_cutoff_day,
    kasbon_window_start_day = EXCLUDED.kasbon_window_start_day,
    kasbon_window_end_day = EXCLUDED.kasbon_window_end_day,
    kasbon_max_amount = EXCLUDED.kasbon_max_amount,
    kasbon_max_installments = EXCLUDED.kasbon_max_installments,
    kasbon_permanent_only = EXCLUDED.kasbon_permanent_only,
    kasbon_min_score = EXCLUDED.kasbon_min_score,
    kasbon_priority_score = EXCLUDED.kasbon_priority_score,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING *;

-- ── Per-component grade percentages ─────────────────────────────────────────

-- name: ListGradeComponentPercents :many
SELECT gcp.grade_id, gcp.wage_component_id, gcp.pay_percent
FROM performance_grade_component_percents gcp;

-- name: CreateGradeComponentPercent :exec
INSERT INTO performance_grade_component_percents (grade_id, wage_component_id, pay_percent)
VALUES ($1, $2, $3);

-- ── Payroll period cut-off ──────────────────────────────────────────────────

-- name: GetLatestPayrollPeriodBefore :one
-- The period immediately before a month, so a new period never overlaps the
-- dates its predecessor already paid (matters on the switch to a cut-off).
SELECT id, period_month, start_date, end_date
FROM payroll_periods
WHERE period_month < $1
ORDER BY period_month DESC
LIMIT 1;

-- ── Holiday incentive ───────────────────────────────────────────────────────

-- name: CountPresentOnWeekdayHolidays :one
-- Insentif Libur Nasional applies only to holidays falling Monday–Friday.
SELECT COUNT(*)::int AS cnt
FROM attendance_records ar
JOIN public_holidays ph ON ph.date = ar.date
WHERE ar.employee_id = @employee_id
  AND ar.date >= @date_from::date AND ar.date <= @date_to::date
  AND ar.status = 'present'
  AND EXTRACT(ISODOW FROM ar.date) BETWEEN 1 AND 5;

-- name: SetPayrollLineHolidayRate :exec
UPDATE payroll_lines SET public_holiday_rate = $2 WHERE id = $1;

-- name: GetPayrollLineHolidayRate :one
SELECT public_holiday_rate FROM payroll_lines WHERE id = $1;

-- ── Kasbon eligibility ──────────────────────────────────────────────────────

-- name: CountOpenKasbonsForEmployee :one
-- Kasbon still running for an employee (not yet repaid, or awaiting a
-- decision). The one being edited is excluded via $2 (pass a zero UUID on create).
SELECT COUNT(*)::int AS cnt
FROM kasbons
WHERE employee_id = @employee_id
  AND id <> @exclude_id
  AND status IN ('pending', 'approved', 'processed');

-- name: SetKasbonPolicyCheck :exec
UPDATE kasbons SET score_at_request = $2, approval_track = $3 WHERE id = $1;

-- name: GetKasbonPolicyCheck :one
SELECT score_at_request, approval_track FROM kasbons WHERE id = $1;

-- name: ListKasbonPolicyChecks :many
SELECT id, score_at_request, approval_track FROM kasbons
WHERE approval_track IS NOT NULL;

-- ── Leave: doctor's note ────────────────────────────────────────────────────

-- name: ListLeaveTypeDoctorNoteFlags :many
SELECT id, requires_doctor_note FROM leave_types;

-- name: SetLeaveTypeRequiresDoctorNote :exec
UPDATE leave_types SET requires_doctor_note = $2 WHERE id = $1;

-- name: SetLeaveRequestDoctorNote :exec
UPDATE leave_requests SET has_doctor_note = $2 WHERE id = $1;

-- name: ListLeaveRequestDoctorNotes :many
SELECT id, has_doctor_note FROM leave_requests WHERE id = ANY($1::uuid[]);

-- name: GetApprovedLeaveOnDate :one
-- The approved leave covering an employee's day, with what the policy needs
-- to judge it. Points are per day of leave the employee was actually off.
SELECT lr.id, lt.is_paid, lt.uses_quota, lt.requires_doctor_note, lr.has_doctor_note
FROM leave_requests lr
JOIN leave_types lt ON lt.id = lr.leave_type_id
WHERE lr.employee_id = @employee_id
  AND lr.status = 'approved'
  AND lr.start_date <= @day::date AND lr.end_date >= @day::date
ORDER BY lr.created_at DESC
LIMIT 1;

-- name: ListApprovedPayCutLeaveOverlapping :many
-- Approved leave that costs pay: unpaid leave, and sick leave filed without a
-- doctor's note.
SELECT lr.id, lr.start_date, lr.end_date, lt.is_paid,
       (lt.requires_doctor_note AND NOT lr.has_doctor_note)::boolean AS missing_doctor_note
FROM leave_requests lr
JOIN leave_types lt ON lt.id = lr.leave_type_id
WHERE lr.employee_id = @employee_id
  AND lr.status = 'approved'
  AND (lt.is_paid = false OR (lt.requires_doctor_note AND NOT lr.has_doctor_note))
  AND lr.start_date <= @window_to::date
  AND lr.end_date >= @window_from::date;
