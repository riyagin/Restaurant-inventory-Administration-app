-- name: ListSales :many
SELECT
    s.id, s.account_id, s.amount, s.description, s.date, s.created_at,
    s.branch_id, s.division_id,
    a.name AS account_name,
    b.name AS branch_name,
    d.name AS division_name,
    u.username AS created_by_name
FROM sales s
LEFT JOIN accounts a ON a.id = s.account_id
LEFT JOIN branches b ON b.id = s.branch_id
LEFT JOIN divisions d ON d.id = s.division_id
LEFT JOIN users u ON u.id = s.created_by
WHERE ($1::date IS NULL OR s.date >= $1)
  AND ($2::date IS NULL OR s.date <= $2)
  AND ($3::uuid IS NULL OR s.branch_id = $3)
ORDER BY s.date DESC, s.created_at DESC;

-- name: GetSaleByID :one
SELECT id, account_id, amount, description, date, branch_id, division_id, created_by, created_at
FROM sales WHERE id = $1;

-- name: InsertSale :one
INSERT INTO sales (id, account_id, amount, description, date, branch_id, division_id, created_by)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at;

-- name: DeleteSale :exec
DELETE FROM sales WHERE id = $1;
