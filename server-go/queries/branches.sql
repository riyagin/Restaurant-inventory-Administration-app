-- name: ListBranches :many
SELECT
    b.id, b.name, b.created_at,
    b.revenue_account_id, ra.name AS revenue_account_name,
    b.expense_account_id, ea.name AS expense_account_name,
    b.petty_cash_account_id, pa.name AS petty_cash_account_name,
    COALESCE(pa.balance, 0)::bigint AS petty_cash_balance
FROM branches b
LEFT JOIN accounts ra ON ra.id = b.revenue_account_id
LEFT JOIN accounts ea ON ea.id = b.expense_account_id
LEFT JOIN accounts pa ON pa.id = b.petty_cash_account_id
ORDER BY b.name;

-- name: GetBranchByID :one
SELECT
    b.id, b.name, b.created_at,
    b.revenue_account_id, ra.name AS revenue_account_name,
    b.expense_account_id, ea.name AS expense_account_name,
    b.petty_cash_account_id, pa.name AS petty_cash_account_name,
    COALESCE(pa.balance, 0)::bigint AS petty_cash_balance
FROM branches b
LEFT JOIN accounts ra ON ra.id = b.revenue_account_id
LEFT JOIN accounts ea ON ea.id = b.expense_account_id
LEFT JOIN accounts pa ON pa.id = b.petty_cash_account_id
WHERE b.id = $1;

-- name: CreateBranch :one
INSERT INTO branches (id, name, revenue_account_id, expense_account_id, petty_cash_account_id)
VALUES (gen_random_uuid(), $1, $2, $3, $4)
RETURNING id, name, revenue_account_id, expense_account_id, created_at, petty_cash_account_id;

-- name: UpdateBranch :one
UPDATE branches SET name = $1 WHERE id = $2
RETURNING id, name, revenue_account_id, expense_account_id, created_at, petty_cash_account_id;

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

-- The petty cash box hangs under "Kas dan Setara Kas" (11000) rather than
-- standing alone, so the cash total on the balance sheet includes it without
-- anyone maintaining a list.
-- name: CreatePettyCashAccountForBranch :one
INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
VALUES (gen_random_uuid(), $1, $2, 'asset', $3, 0, false)
RETURNING id;

-- 11100-11199 sits below the 12001+ inventory accounts, so allocating here
-- leaves GetNextInventoryAccountNumber (MAX over 11000-19999) untouched.
-- name: GetNextPettyCashAccountNumber :one
SELECT COALESCE(MAX(account_number), 11099) + 1 AS next_number
FROM accounts WHERE account_number BETWEEN 11100 AND 11199;

-- name: GetBranchPettyCashAccountID :one
SELECT petty_cash_account_id FROM branches WHERE id = $1;

-- name: SetBranchPettyCashAccountID :exec
UPDATE branches SET petty_cash_account_id = $1 WHERE id = $2;

-- Branch + petty cash account + live balance, the trio every petty cash screen
-- needs. Branches whose account is somehow missing are still returned so the UI
-- can say so rather than silently dropping the branch.
-- name: ListBranchPettyCash :many
SELECT
    b.id AS branch_id, b.name AS branch_name,
    b.petty_cash_account_id,
    pa.name AS account_name,
    COALESCE(pa.balance, 0)::bigint AS balance
FROM branches b
LEFT JOIN accounts pa ON pa.id = b.petty_cash_account_id
ORDER BY b.name;
