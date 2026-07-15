-- name: ListWageComponents :many
SELECT id, name, type, is_fixed, is_active, created_at, calc_method, min_score
FROM wage_components
ORDER BY type, name;

-- name: ListActiveWageComponents :many
SELECT id, name, type, is_fixed, is_active, created_at, calc_method, min_score
FROM wage_components
WHERE is_active = true
ORDER BY type, name;

-- name: GetWageComponentByID :one
SELECT id, name, type, is_fixed, is_active, created_at, calc_method, min_score
FROM wage_components
WHERE id = $1;

-- name: CreateWageComponent :one
INSERT INTO wage_components (id, name, type, is_fixed, is_active, calc_method, min_score)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
RETURNING id, name, type, is_fixed, is_active, created_at, calc_method, min_score;

-- name: UpdateWageComponent :one
UPDATE wage_components
SET name = $1, type = $2, is_fixed = $3, is_active = $4, calc_method = $5, min_score = $6
WHERE id = $7
RETURNING id, name, type, is_fixed, is_active, created_at, calc_method, min_score;

-- name: SetWageComponentActive :one
UPDATE wage_components
SET is_active = $1
WHERE id = $2
RETURNING id, name, type, is_fixed, is_active, created_at, calc_method, min_score;

-- name: DeleteWageComponent :exec
DELETE FROM wage_components WHERE id = $1;

-- name: CountWageComponentReferences :one
SELECT COUNT(*) FROM employee_wage_components WHERE wage_component_id = $1;

-- name: GetCurrentOpenWageStructure :one
SELECT id, employee_id, base_salary, working_days_per_month, daily_rate,
       effective_date, end_date, created_by, created_at
FROM wage_structures
WHERE employee_id = $1 AND end_date IS NULL;

-- name: GetWageStructureAsOf :one
SELECT id, employee_id, base_salary, working_days_per_month, daily_rate,
       effective_date, end_date, created_by, created_at
FROM wage_structures
WHERE employee_id = $1
  AND effective_date <= $2
  AND (end_date IS NULL OR end_date >= $2)
ORDER BY effective_date DESC
LIMIT 1;

-- name: ListWageStructuresByEmployee :many
SELECT id, employee_id, base_salary, working_days_per_month, daily_rate,
       effective_date, end_date, created_by, created_at
FROM wage_structures
WHERE employee_id = $1
ORDER BY effective_date DESC;

-- name: CloseOpenWageStructure :exec
UPDATE wage_structures
SET end_date = $1
WHERE employee_id = $2 AND end_date IS NULL;

-- name: CreateWageStructure :one
INSERT INTO wage_structures (
    id, employee_id, base_salary, working_days_per_month, daily_rate,
    effective_date, end_date, created_by
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, NULL, $6)
RETURNING id, employee_id, base_salary, working_days_per_month, daily_rate,
          effective_date, end_date, created_by, created_at;

-- name: CreateEmployeeWageComponent :one
INSERT INTO employee_wage_components (id, wage_structure_id, wage_component_id, amount)
VALUES (gen_random_uuid(), $1, $2, $3)
RETURNING id, wage_structure_id, wage_component_id, amount;

-- name: ListEmployeeWageComponents :many
SELECT ewc.id, ewc.wage_structure_id, ewc.wage_component_id, ewc.amount,
       wc.name AS component_name, wc.type AS component_type, wc.is_fixed AS component_is_fixed,
       wc.calc_method AS component_calc_method, wc.min_score AS component_min_score
FROM employee_wage_components ewc
JOIN wage_components wc ON wc.id = ewc.wage_component_id
WHERE ewc.wage_structure_id = $1
ORDER BY wc.type, wc.name;
