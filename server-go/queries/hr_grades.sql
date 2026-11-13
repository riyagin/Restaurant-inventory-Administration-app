-- ── Performance grade scale (migration 087) ─────────────────────────────────

-- name: ListPerformanceGrades :many
SELECT id, label, min_score, pay_percent, created_at
FROM performance_grades
ORDER BY min_score DESC;

-- name: DeleteAllPerformanceGrades :exec
DELETE FROM performance_grades;

-- name: CreatePerformanceGrade :one
INSERT INTO performance_grades (label, min_score, pay_percent)
VALUES ($1, $2, $3)
RETURNING id, label, min_score, pay_percent, created_at;

-- name: SetPayrollLineGrade :exec
UPDATE payroll_lines
SET performance_grade = $2, grade_pay_percent = $3
WHERE id = $1;

-- name: ListPayrollLineGradesForPeriod :many
SELECT id, performance_grade, grade_pay_percent
FROM payroll_lines
WHERE payroll_period_id = $1;

-- name: GetPayrollLineGrade :one
SELECT id, performance_grade, grade_pay_percent
FROM payroll_lines
WHERE id = $1;

-- ── Position (role) wage templates ──────────────────────────────────────────

-- name: ListPositionWageTemplates :many
-- Every position, with its template when one exists, and how many active
-- employees currently hold it (the reach of a bulk apply).
SELECT p.id AS position_id, p.name AS position_name, p.is_active AS position_is_active,
       (t.position_id IS NOT NULL)::boolean AS has_template,
       COALESCE(t.base_salary, 0)::bigint AS base_salary,
       COALESCE(t.working_days_per_month, 0)::int AS working_days_per_month,
       t.updated_at,
       (SELECT COUNT(*) FROM employees e
         WHERE e.position_id = p.id AND e.status = 'active')::int AS active_employees
FROM positions p
LEFT JOIN position_wage_templates t ON t.position_id = p.id
ORDER BY p.name;

-- name: GetPositionWageTemplate :one
SELECT position_id, base_salary, working_days_per_month, updated_by, updated_at
FROM position_wage_templates
WHERE position_id = $1;

-- name: ListPositionWageTemplateComponents :many
SELECT tc.wage_component_id, tc.amount,
       wc.name AS component_name, wc.type AS component_type,
       wc.calc_method AS component_calc_method, wc.grade_linked AS component_grade_linked,
       wc.is_active AS component_is_active
FROM position_wage_template_components tc
JOIN wage_components wc ON wc.id = tc.wage_component_id
WHERE tc.position_id = $1
ORDER BY wc.type, wc.name;

-- name: UpsertPositionWageTemplate :one
INSERT INTO position_wage_templates (position_id, base_salary, working_days_per_month, updated_by, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (position_id) DO UPDATE
SET base_salary = EXCLUDED.base_salary,
    working_days_per_month = EXCLUDED.working_days_per_month,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING position_id, base_salary, working_days_per_month, updated_by, updated_at;

-- name: DeletePositionWageTemplateComponents :exec
DELETE FROM position_wage_template_components WHERE position_id = $1;

-- name: CreatePositionWageTemplateComponent :exec
INSERT INTO position_wage_template_components (position_id, wage_component_id, amount)
VALUES ($1, $2, $3);

-- name: DeletePositionWageTemplate :exec
DELETE FROM position_wage_templates WHERE position_id = $1;

-- name: ListActiveEmployeesByPosition :many
-- Who a bulk apply reaches, with the effective date of their open wage version
-- (NULL when they have none yet).
SELECT e.id, e.employee_code, e.full_name,
       ws.effective_date AS current_effective_date,
       ws.base_salary AS current_base_salary
FROM employees e
LEFT JOIN wage_structures ws ON ws.employee_id = e.id AND ws.end_date IS NULL
WHERE e.position_id = $1 AND e.status = 'active'
ORDER BY e.full_name;
