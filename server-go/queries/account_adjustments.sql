-- name: ListAccountAdjustments :many
SELECT
    aa.id, aa.account_id, aa.amount, aa.description, aa.created_at,
    aa.created_by_name,
    a.name AS account_name
FROM account_adjustments aa
LEFT JOIN accounts a ON a.id = aa.account_id
WHERE ($1::uuid IS NULL OR aa.account_id = $1)
  AND ($2::date IS NULL OR aa.created_at::date >= $2)
  AND ($3::date IS NULL OR aa.created_at::date <= $3)
ORDER BY aa.created_at DESC;

-- name: InsertAccountAdjustment :one
INSERT INTO account_adjustments (id, account_id, amount, description, created_by, created_by_name)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
RETURNING id, created_at;

-- name: InsertAccountAdjustmentWithTransfer :one
INSERT INTO account_adjustments (id, account_id, amount, description, created_by, created_by_name, transfer_id)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
RETURNING id, created_at;
