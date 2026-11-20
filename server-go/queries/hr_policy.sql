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

-- ── Payslip: offense detail ─────────────────────────────────────────────────

-- name: ListViolationDetailsForRange :many
-- Every violation in [$2, $3], one row each, with the attendance it was judged
-- on (check-in/out and minutes), for the payslip's per-date offense table.
SELECT pv.date,
       COALESCE(pp.name, NULLIF(pv.note, ''), 'Pelanggaran')::text AS label,
       COALESCE(pp.rule_type, 'manual')::text AS rule_type,
       pv.points,
       ar.check_in,
       ar.check_out,
       COALESCE(ar.late_minutes, 0)::int AS late_minutes,
       COALESCE(ar.early_leave_minutes, 0)::int AS early_leave_minutes,
       COALESCE(ar.half_day_lost_minutes, 0)::int AS half_day_lost_minutes
FROM performance_violations pv
LEFT JOIN performance_policies pp ON pp.id = pv.policy_id
LEFT JOIN attendance_records ar ON ar.id = pv.attendance_record_id
WHERE pv.employee_id = $1
  AND pv.date >= $2
  AND pv.date <= $3
ORDER BY pv.date, pv.points DESC;

-- name: ListHalfDaysForRange :many
-- Half-day days in [$2, $3]: what payroll's half_day_deduction is made of.
SELECT date, half_day_type, half_day_lost_minutes, check_in, check_out
FROM attendance_records
WHERE employee_id = $1
  AND date >= $2
  AND date <= $3
  AND is_half_day
ORDER BY date;

-- name: ListPayslipKasbons :many
-- The kasbon a payroll line repays, for the payslip: what this line deducts from
-- each kasbon and what is still owed after it. On a closed period the installments
-- are linked to the line; on an open one they are the processed kasbon's pending
-- installments due by the period month (the rule payroll generation used).
-- "remaining" counts installments due after the month, so an old slip keeps
-- showing the balance as it stood then.
WITH on_line AS (
  SELECT ki.kasbon_id, ki.amount
  FROM kasbon_installments ki
  WHERE ki.payroll_line_id = $1
  UNION ALL
  SELECT ki.kasbon_id, ki.amount
  FROM kasbon_installments ki
  JOIN kasbons k ON k.id = ki.kasbon_id
  WHERE k.employee_id = $2
    AND k.status = 'processed'
    AND ki.status = 'pending'
    AND ki.payroll_line_id IS NULL
    AND ki.due_month <= $3
    AND NOT EXISTS (SELECT 1 FROM kasbon_installments z WHERE z.payroll_line_id = $1)
)
SELECT k.kasbon_number,
       k.amount AS kasbon_amount,
       SUM(o.amount)::bigint AS deducted,
       COALESCE((SELECT SUM(x.amount) FROM kasbon_installments x
                 WHERE x.kasbon_id = k.id AND x.due_month > $3), 0)::bigint AS remaining
FROM on_line o
JOIN kasbons k ON k.id = o.kasbon_id
GROUP BY k.id, k.kasbon_number, k.amount
ORDER BY k.kasbon_number;
