-- name: ListDivisions :many
SELECT
    d.id, d.name, d.branch_id, d.created_at,
    b.name AS branch_name,
    d.revenue_account_id, ra.name AS revenue_account_name,
    d.expense_account_id, ea.name AS expense_account_name,
    d.discount_account_id, da.name AS discount_account_name
FROM divisions d
JOIN branches b ON b.id = d.branch_id
LEFT JOIN accounts ra ON ra.id = d.revenue_account_id
LEFT JOIN accounts ea ON ea.id = d.expense_account_id
LEFT JOIN accounts da ON da.id = d.discount_account_id
WHERE ($1::uuid IS NULL OR d.branch_id = $1)
ORDER BY b.name, d.name;

-- name: GetDivisionByID :one
SELECT
    d.id, d.name, d.branch_id, d.created_at,
    b.name AS branch_name,
    d.revenue_account_id, ra.name AS revenue_account_name,
    d.expense_account_id, ea.name AS expense_account_name,
    d.discount_account_id, da.name AS discount_account_name
FROM divisions d
JOIN branches b ON b.id = d.branch_id
LEFT JOIN accounts ra ON ra.id = d.revenue_account_id
LEFT JOIN accounts ea ON ea.id = d.expense_account_id
LEFT JOIN accounts da ON da.id = d.discount_account_id
WHERE d.id = $1;

-- name: CreateDivision :one
INSERT INTO divisions (id, branch_id, name, revenue_account_id, expense_account_id, discount_account_id)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
RETURNING id, branch_id, name, revenue_account_id, expense_account_id, discount_account_id, created_at;

-- name: UpdateDivision :one
UPDATE divisions SET name = $1 WHERE id = $2
RETURNING id, branch_id, name, revenue_account_id, expense_account_id, discount_account_id, created_at;

-- name: DeleteDivision :exec
DELETE FROM divisions WHERE id = $1;

-- name: GetDivisionExpenseAccountID :one
SELECT expense_account_id FROM divisions WHERE id = $1;

-- name: GetDivisionRevenueAccountID :one
SELECT revenue_account_id FROM divisions WHERE id = $1;
