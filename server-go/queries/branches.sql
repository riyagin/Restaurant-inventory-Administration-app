-- name: ListBranches :many
SELECT
    b.id, b.name, b.created_at,
    b.revenue_account_id, ra.name AS revenue_account_name,
    b.expense_account_id, ea.name AS expense_account_name
FROM branches b
LEFT JOIN accounts ra ON ra.id = b.revenue_account_id
LEFT JOIN accounts ea ON ea.id = b.expense_account_id
ORDER BY b.name;

-- name: GetBranchByID :one
SELECT
    b.id, b.name, b.created_at,
    b.revenue_account_id, ra.name AS revenue_account_name,
    b.expense_account_id, ea.name AS expense_account_name
FROM branches b
LEFT JOIN accounts ra ON ra.id = b.revenue_account_id
LEFT JOIN accounts ea ON ea.id = b.expense_account_id
WHERE b.id = $1;

-- name: CreateBranch :one
INSERT INTO branches (id, name, revenue_account_id, expense_account_id)
VALUES (gen_random_uuid(), $1, $2, $3)
RETURNING id, name, revenue_account_id, expense_account_id, created_at;

-- name: UpdateBranch :one
UPDATE branches SET name = $1 WHERE id = $2
RETURNING id, name, revenue_account_id, expense_account_id, created_at;

-- name: DeleteBranch :exec
DELETE FROM branches WHERE id = $1;

-- name: CountDivisionsByBranch :one
SELECT COUNT(*) FROM divisions WHERE branch_id = $1;

-- name: CreateAccountForBranch :one
INSERT INTO accounts (id, name, account_number, account_type, balance, is_system)
VALUES (gen_random_uuid(), $1, $2, $3, 0, false)
RETURNING id;

-- name: GetNextRevenueAccountNumber :one
SELECT COALESCE(MAX(account_number), 39999) + 1 AS next_number
FROM accounts WHERE account_number BETWEEN 40000 AND 49999;

-- name: GetNextExpenseAccountNumber :one
SELECT COALESCE(MAX(account_number), 49999) + 1 AS next_number
FROM accounts WHERE account_number BETWEEN 50000 AND 59999;

-- name: GetBranchExpenseAccountID :one
SELECT expense_account_id FROM branches WHERE id = $1;

-- name: GetBranchRevenueAccountID :one
SELECT revenue_account_id FROM branches WHERE id = $1;
