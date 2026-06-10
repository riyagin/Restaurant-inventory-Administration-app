-- name: ListWarehouses :many
SELECT w.id, w.name, w.inventory_account_id, a.name AS account_name
FROM warehouses w
LEFT JOIN accounts a ON a.id = w.inventory_account_id
ORDER BY w.name;

-- name: GetWarehouseByID :one
SELECT w.id, w.name, w.inventory_account_id, a.name AS account_name
FROM warehouses w
LEFT JOIN accounts a ON a.id = w.inventory_account_id
WHERE w.id = $1;

-- name: CreateWarehouse :one
INSERT INTO warehouses (id, name, inventory_account_id)
VALUES (gen_random_uuid(), $1, $2)
RETURNING id, name, inventory_account_id;

-- name: UpdateWarehouse :one
UPDATE warehouses SET name = $1 WHERE id = $2
RETURNING id, name, inventory_account_id;

-- name: DeleteWarehouse :exec
DELETE FROM warehouses WHERE id = $1;

-- name: CreateAccountForWarehouse :one
INSERT INTO accounts (id, name, account_number, account_type, balance, is_system)
VALUES (gen_random_uuid(), $1, $2, 'asset', 0, false)
RETURNING id;

-- name: GetNextInventoryAccountNumber :one
SELECT COALESCE(MAX(account_number), 10999) + 1 AS next_number
FROM accounts
WHERE account_number BETWEEN 11000 AND 19999;

-- name: GetWarehouseInventoryAccountID :one
SELECT inventory_account_id FROM warehouses WHERE id = $1;
