-- name: ListExpenseCategories :many
SELECT
    ec.id, ec.division_id, ec.name, ec.account_id, ec.created_at,
    a.account_number, a.balance AS account_balance,
    d.name AS division_name,
    b.name AS branch_name
FROM expense_categories ec
JOIN accounts a ON a.id = ec.account_id
JOIN divisions d ON d.id = ec.division_id
JOIN branches b ON b.id = d.branch_id
WHERE ($1::uuid IS NULL OR ec.division_id = $1)
ORDER BY b.name, d.name, ec.name;

-- name: GetExpenseCategoryByID :one
SELECT
    ec.id, ec.division_id, ec.name, ec.account_id, ec.created_at,
    a.account_number, a.balance AS account_balance,
    d.name AS division_name,
    b.name AS branch_name
FROM expense_categories ec
JOIN accounts a ON a.id = ec.account_id
JOIN divisions d ON d.id = ec.division_id
JOIN branches b ON b.id = d.branch_id
WHERE ec.id = $1;

-- name: CreateExpenseCategory :one
INSERT INTO expense_categories (id, division_id, name, account_id)
VALUES (gen_random_uuid(), $1, $2, $3)
RETURNING id, division_id, name, account_id, created_at;

-- name: DeleteExpenseCategory :exec
DELETE FROM expense_categories WHERE id = $1;

-- name: CreateExpenseCategoryAccount :one
INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
VALUES (gen_random_uuid(), $1, $2, 'expense', $3, 0, false)
RETURNING id;

-- name: CountJournalLinesForAccount :one
SELECT COUNT(*) FROM journal_lines WHERE account_id = $1;

-- name: GetDivisionExpenseParent :one
SELECT d.expense_account_id, d.name AS division_name, b.name AS branch_name
FROM divisions d
JOIN branches b ON b.id = d.branch_id
WHERE d.id = $1;
