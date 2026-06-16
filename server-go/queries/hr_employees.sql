-- name: ListPositions :many
SELECT id, name, is_active, created_at
FROM positions
ORDER BY name;

-- name: GetPositionByID :one
SELECT id, name, is_active, created_at
FROM positions
WHERE id = $1;

-- name: CreatePosition :one
INSERT INTO positions (id, name, is_active)
VALUES (gen_random_uuid(), $1, $2)
RETURNING id, name, is_active, created_at;

-- name: UpdatePosition :one
UPDATE positions
SET name = $1, is_active = $2
WHERE id = $3
RETURNING id, name, is_active, created_at;

-- name: DeletePosition :exec
DELETE FROM positions WHERE id = $1;

-- name: CountEmployeesByPosition :one
SELECT COUNT(*) FROM employees WHERE position_id = $1;

-- name: GetMaxEmployeeCodeSeq :one
-- Returns the highest numeric suffix among auto-generated EMP-#### codes.
SELECT COALESCE(MAX(CAST(SUBSTRING(employee_code FROM 5) AS INTEGER)), 0)::int AS max_seq
FROM employees
WHERE employee_code ~ '^EMP-[0-9]+$';

-- name: GetEmployeeByID :one
SELECT
    e.id, e.employee_code, e.full_name, e.dob, e.join_date,
    e.position_id, p.name AS position_name,
    e.branch_id, b.name AS branch_name,
    e.phone, e.email, e.address, e.national_id,
    e.bank_name, e.bank_account_number, e.bank_account_holder,
    e.photo_path, e.user_id, e.status,
    e.created_at, e.updated_at
FROM employees e
JOIN positions p ON p.id = e.position_id
JOIN branches  b ON b.id = e.branch_id
WHERE e.id = $1;

-- name: GetEmployeePhotoPath :one
SELECT photo_path FROM employees WHERE id = $1;

-- name: CreateEmployee :one
INSERT INTO employees (
    id, employee_code, full_name, dob, join_date,
    position_id, branch_id, phone, email, address, national_id,
    bank_name, bank_account_number, bank_account_holder,
    user_id, status
)
VALUES (
    gen_random_uuid(), $1, $2, $3, $4,
    $5, $6, $7, $8, $9, $10,
    $11, $12, $13,
    $14, $15
)
RETURNING id;

-- name: UpdateEmployee :one
UPDATE employees SET
    employee_code = $1,
    full_name = $2,
    dob = $3,
    join_date = $4,
    position_id = $5,
    branch_id = $6,
    phone = $7,
    email = $8,
    address = $9,
    national_id = $10,
    bank_name = $11,
    bank_account_number = $12,
    bank_account_holder = $13,
    user_id = $14,
    status = $15,
    updated_at = now()
WHERE id = $16
RETURNING id;

-- name: UpdateEmployeePhotoPath :exec
UPDATE employees SET photo_path = $1, updated_at = now() WHERE id = $2;

-- name: DeleteEmployee :exec
DELETE FROM employees WHERE id = $1;
