-- ── Payroll Settings ─────────────────────────────────────────────────────────

-- name: GetPayrollSettings :one
SELECT id, overtime_multiplier, holiday_multiplier
FROM payroll_settings
WHERE id = 1;

-- ── Payroll Periods ──────────────────────────────────────────────────────────

-- name: CreatePayrollPeriod :one
INSERT INTO payroll_periods (id, period_month, start_date, end_date, status, created_by)
VALUES (gen_random_uuid(), $1, $2, $3, 'open', $4)
RETURNING id, period_month, start_date, end_date, status, created_by, closed_at, paid_at, created_at;

-- name: GetPayrollPeriodByID :one
SELECT id, period_month, start_date, end_date, status, created_by, closed_at, paid_at, created_at
FROM payroll_periods
WHERE id = $1;

-- name: GetPayrollPeriodByMonth :one
SELECT id, period_month, start_date, end_date, status, created_by, closed_at, paid_at, created_at
FROM payroll_periods
WHERE period_month = $1;

-- name: ListPayrollPeriods :many
SELECT
    p.id, p.period_month, p.start_date, p.end_date, p.status, p.created_by,
    p.closed_at, p.paid_at, p.created_at,
    COALESCE(SUM(l.net_pay), 0)::bigint        AS total_net,
    COALESCE(SUM(l.gross_pay), 0)::bigint       AS total_gross,
    COUNT(l.id)::int                            AS line_count,
    COUNT(l.id) FILTER (WHERE l.reviewed)::int  AS reviewed_count
FROM payroll_periods p
LEFT JOIN payroll_lines l ON l.payroll_period_id = p.id
GROUP BY p.id
ORDER BY p.period_month DESC;

-- name: GetPayrollPeriodSummary :one
SELECT
    COALESCE(SUM(l.gross_pay), 0)::bigint AS total_gross,
    COALESCE(SUM(l.net_pay), 0)::bigint AS total_net,
    COALESCE(SUM(l.component_deduction_total + l.kasbon_deduction + l.unpaid_leave_deduction), 0)::bigint AS total_deductions,
    COUNT(l.id)::int AS line_count,
    COUNT(l.id) FILTER (WHERE l.reviewed)::int AS reviewed_count
FROM payroll_lines l
WHERE l.payroll_period_id = $1;

-- name: ClosePayrollPeriod :one
UPDATE payroll_periods
SET status = 'closed', closed_at = now()
WHERE id = $1
RETURNING id, period_month, start_date, end_date, status, created_by, closed_at, paid_at, created_at;

-- name: MarkPayrollPeriodPaid :one
UPDATE payroll_periods
SET status = 'paid', paid_at = now()
WHERE id = $1
RETURNING id, period_month, start_date, end_date, status, created_by, closed_at, paid_at, created_at;

-- ── Payroll Lines ────────────────────────────────────────────────────────────

-- name: CreatePayrollLine :one
INSERT INTO payroll_lines (
    id, payroll_period_id, employee_id, wage_structure_id,
    base_salary, daily_rate, overtime_days, public_holiday_days,
    overtime_amount, public_holiday_amount, allowance_total, bonus_total,
    component_deduction_total, kasbon_deduction, unpaid_leave_days,
    unpaid_leave_deduction, gross_pay, net_pay, performance_score
)
VALUES (
    gen_random_uuid(), $1, $2, $3,
    $4, $5, $6, $7,
    $8, $9, $10, $11,
    $12, $13, $14,
    $15, $16, $17, $18
)
RETURNING id, payroll_period_id, employee_id, wage_structure_id, base_salary, daily_rate,
          overtime_days, public_holiday_days, overtime_amount, public_holiday_amount,
          allowance_total, bonus_total, component_deduction_total, kasbon_deduction,
          unpaid_leave_days, unpaid_leave_deduction, gross_pay, net_pay,
          performance_score, reviewed, reviewed_by, reviewed_at, review_note;

-- name: GetPayrollLineByID :one
SELECT id, payroll_period_id, employee_id, wage_structure_id, base_salary, daily_rate,
       overtime_days, public_holiday_days, overtime_amount, public_holiday_amount,
       allowance_total, bonus_total, component_deduction_total, kasbon_deduction,
       unpaid_leave_days, unpaid_leave_deduction, gross_pay, net_pay,
       performance_score, reviewed, reviewed_by, reviewed_at, review_note
FROM payroll_lines
WHERE id = $1;

-- name: GetPayrollLineByPeriodEmployee :one
SELECT id, payroll_period_id, employee_id, wage_structure_id, base_salary, daily_rate,
       overtime_days, public_holiday_days, overtime_amount, public_holiday_amount,
       allowance_total, bonus_total, component_deduction_total, kasbon_deduction,
       unpaid_leave_days, unpaid_leave_deduction, gross_pay, net_pay,
       performance_score, reviewed, reviewed_by, reviewed_at, review_note
FROM payroll_lines
WHERE payroll_period_id = $1 AND employee_id = $2;

-- name: ListPayrollLinesForPeriod :many
SELECT
    l.id, l.payroll_period_id, l.employee_id, l.wage_structure_id, l.base_salary, l.daily_rate,
    l.overtime_days, l.public_holiday_days, l.overtime_amount, l.public_holiday_amount,
    l.allowance_total, l.bonus_total, l.component_deduction_total, l.kasbon_deduction,
    l.unpaid_leave_days, l.unpaid_leave_deduction, l.gross_pay, l.net_pay,
    l.performance_score, l.reviewed, l.reviewed_by, l.reviewed_at, l.review_note,
    e.full_name AS employee_name, e.employee_code,
    e.position_id, pos.name AS position_name,
    e.branch_id, b.name AS branch_name
FROM payroll_lines l
JOIN employees e ON e.id = l.employee_id
LEFT JOIN positions pos ON pos.id = e.position_id
LEFT JOIN branches b ON b.id = e.branch_id
WHERE l.payroll_period_id = $1
  AND ($2::text = '' OR lower(e.full_name) LIKE '%' || lower($2) || '%'
       OR lower(e.employee_code) LIKE '%' || lower($2) || '%')
  AND ($3::uuid IS NULL OR e.position_id = $3)
  AND ($4::uuid IS NULL OR e.branch_id = $4)
ORDER BY
    CASE WHEN $5::text = 'net_pay' AND $6::text = 'asc'  THEN l.net_pay END ASC,
    CASE WHEN $5::text = 'net_pay' AND $6::text = 'desc' THEN l.net_pay END DESC,
    CASE WHEN $5::text = 'name'    AND $6::text = 'desc' THEN e.full_name END DESC,
    e.full_name ASC;

-- name: CountUnreviewedLines :one
SELECT COUNT(*) FROM payroll_lines
WHERE payroll_period_id = $1 AND reviewed = false;

-- name: ListPayrollLineBranchTotals :many
SELECT e.branch_id, COALESCE(SUM(l.gross_pay), 0)::bigint AS total_gross,
       COALESCE(SUM(l.net_pay), 0)::bigint AS total_net
FROM payroll_lines l
JOIN employees e ON e.id = l.employee_id
WHERE l.payroll_period_id = $1
GROUP BY e.branch_id;

-- name: UpdatePayrollLineReview :one
UPDATE payroll_lines
SET overtime_days = $1, public_holiday_days = $2,
    overtime_amount = $3, public_holiday_amount = $4,
    bonus_total = $5, gross_pay = $6, net_pay = $7,
    reviewed = true, reviewed_by = $8, reviewed_at = now(), review_note = $9
WHERE id = $10
RETURNING id, payroll_period_id, employee_id, wage_structure_id, base_salary, daily_rate,
          overtime_days, public_holiday_days, overtime_amount, public_holiday_amount,
          allowance_total, bonus_total, component_deduction_total, kasbon_deduction,
          unpaid_leave_days, unpaid_leave_deduction, gross_pay, net_pay,
          performance_score, reviewed, reviewed_by, reviewed_at, review_note;

-- name: UnreviewPayrollLine :one
UPDATE payroll_lines
SET reviewed = false, reviewed_by = NULL, reviewed_at = NULL
WHERE id = $1
RETURNING id, payroll_period_id, employee_id, wage_structure_id, base_salary, daily_rate,
          overtime_days, public_holiday_days, overtime_amount, public_holiday_amount,
          allowance_total, bonus_total, component_deduction_total, kasbon_deduction,
          unpaid_leave_days, unpaid_leave_deduction, gross_pay, net_pay,
          performance_score, reviewed, reviewed_by, reviewed_at, review_note;

-- name: DeletePayrollLine :exec
DELETE FROM payroll_lines WHERE id = $1;

-- ── Payroll Line Components ──────────────────────────────────────────────────

-- name: CreatePayrollLineComponent :one
INSERT INTO payroll_line_components (id, payroll_line_id, wage_component_id, name, type, amount)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
RETURNING id, payroll_line_id, wage_component_id, name, type, amount;

-- name: ListPayrollLineComponents :many
SELECT id, payroll_line_id, wage_component_id, name, type, amount
FROM payroll_line_components
WHERE payroll_line_id = $1
ORDER BY type, name;

-- name: UpdatePayrollLineComponentAmount :exec
UPDATE payroll_line_components
SET amount = $1
WHERE id = $2;

-- name: DeletePayrollLineComponents :exec
DELETE FROM payroll_line_components WHERE payroll_line_id = $1;

-- ── Attendance helper: present-on-holiday prefill ────────────────────────────

-- name: CountPresentOnHolidays :one
SELECT COUNT(*)::int AS cnt
FROM attendance_records ar
JOIN public_holidays ph ON ph.date = ar.date
WHERE ar.employee_id = $1
  AND ar.date >= $2 AND ar.date <= $3
  AND ar.status = 'present';

-- name: GetAttendanceSummaryForMonth :one
SELECT
    COUNT(*) FILTER (WHERE status = 'present')::int AS hadir,
    COUNT(*) FILTER (WHERE status = 'absent')::int  AS absen,
    COUNT(*) FILTER (WHERE is_late)::int             AS terlambat,
    COUNT(*) FILTER (WHERE status = 'leave')::int    AS cuti
FROM attendance_records
WHERE employee_id = $1
  AND date >= $2 AND date <= $3;

-- name: ListActiveEmployeesForPayroll :many
SELECT id, employee_code, full_name, branch_id, position_id
FROM employees
WHERE status = 'active'
ORDER BY full_name;

-- ── Payslip rendering (prompt 09) ────────────────────────────────────────────

-- name: GetPayrollLineForPayslip :one
-- One denormalised row with everything the PDF payslip needs: the line snapshot
-- plus employee identity (name/code/join date), position name, branch name and the
-- period month. Components are fetched separately via ListPayrollLineComponents.
SELECT
    l.id, l.payroll_period_id, l.employee_id,
    l.base_salary, l.daily_rate,
    l.overtime_days, l.public_holiday_days, l.overtime_amount, l.public_holiday_amount,
    l.allowance_total, l.bonus_total, l.component_deduction_total, l.kasbon_deduction,
    l.unpaid_leave_days, l.unpaid_leave_deduction, l.gross_pay, l.net_pay, l.review_note,
    e.full_name AS employee_name, e.employee_code, e.join_date,
    pos.name AS position_name,
    b.name   AS branch_name,
    p.period_month, p.status AS period_status
FROM payroll_lines l
JOIN payroll_periods p ON p.id = l.payroll_period_id
JOIN employees e       ON e.id = l.employee_id
LEFT JOIN positions pos ON pos.id = e.position_id
LEFT JOIN branches  b   ON b.id  = e.branch_id
WHERE l.id = $1;

-- name: ListLineKasbonNumbers :many
-- Kasbon numbers whose installments were deducted on this payroll line. Populated
-- only after the period is closed (installments are marked deducted at close), so a
-- payslip may legitimately show none and fall back to a generic "Kasbon" label.
SELECT DISTINCT k.kasbon_number
FROM kasbon_installments ki
JOIN kasbons k ON k.id = ki.kasbon_id
WHERE ki.payroll_line_id = $1
ORDER BY k.kasbon_number;
