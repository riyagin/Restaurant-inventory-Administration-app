-- name: ListAccounts :many
SELECT id, name, balance, account_number, account_type, parent_id, is_system
FROM accounts
ORDER BY account_number NULLS LAST, name;

-- name: GetAccountByID :one
SELECT id, name, balance, account_number, account_type, parent_id, is_system
FROM accounts WHERE id = $1;

-- name: CreateAccount :one
INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
VALUES (gen_random_uuid(), $1, $2, $3, $4, 0, false)
RETURNING id, name, balance, account_number, account_type, parent_id, is_system;

-- name: UpdateAccount :one
UPDATE accounts
SET name = $1, account_number = $2, account_type = $3, parent_id = $4
WHERE id = $5
RETURNING id, name, balance, account_number, account_type, parent_id, is_system;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = $1;

-- name: AddToAccountBalance :exec
UPDATE accounts SET balance = balance + $1::bigint WHERE id = $2;

-- name: GetSystemAccountByNumber :one
SELECT id, name, balance FROM accounts WHERE account_number = $1 AND is_system = true LIMIT 1;
