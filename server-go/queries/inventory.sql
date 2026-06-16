-- name: ListInventory :many
SELECT
    inv.id, inv.item_id, inv.warehouse_id, inv.quantity,
    inv.unit_index, inv.value, inv.date,
    i.name AS item_name, i.code AS item_code, i.units AS item_units,
    w.name AS warehouse_name
FROM inventory inv
JOIN items i ON i.id = inv.item_id
JOIN warehouses w ON w.id = inv.warehouse_id
WHERE ($1::uuid IS NULL OR inv.warehouse_id = $1)
  AND ($2::uuid IS NULL OR inv.item_id = $2)
ORDER BY i.name, w.name, inv.date ASC;

-- name: ListInventoryPage :many
SELECT
    inv.id, inv.item_id, inv.warehouse_id, inv.quantity,
    inv.unit_index, inv.date,
    i.name AS item_name, i.code AS item_code, i.units AS item_units,
    w.name AS warehouse_name
FROM inventory inv
JOIN items i ON i.id = inv.item_id
JOIN warehouses w ON w.id = inv.warehouse_id
WHERE (sqlc.narg('warehouse_id')::uuid IS NULL OR inv.warehouse_id = sqlc.narg('warehouse_id'))
  AND (sqlc.narg('item_id')::uuid IS NULL OR inv.item_id = sqlc.narg('item_id'))
  AND (sqlc.narg('search')::text IS NULL OR i.name ILIKE '%' || sqlc.narg('search') || '%' OR i.code ILIKE '%' || sqlc.narg('search') || '%')
  AND (sqlc.narg('date_from')::date IS NULL OR inv.date >= sqlc.narg('date_from'))
  AND (sqlc.narg('date_to')::date IS NULL OR inv.date <= sqlc.narg('date_to'))
ORDER BY i.name, w.name, inv.date ASC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountInventory :one
SELECT COUNT(*)
FROM inventory inv
JOIN items i ON i.id = inv.item_id
WHERE (sqlc.narg('warehouse_id')::uuid IS NULL OR inv.warehouse_id = sqlc.narg('warehouse_id'))
  AND (sqlc.narg('item_id')::uuid IS NULL OR inv.item_id = sqlc.narg('item_id'))
  AND (sqlc.narg('search')::text IS NULL OR i.name ILIKE '%' || sqlc.narg('search') || '%' OR i.code ILIKE '%' || sqlc.narg('search') || '%')
  AND (sqlc.narg('date_from')::date IS NULL OR inv.date >= sqlc.narg('date_from'))
  AND (sqlc.narg('date_to')::date IS NULL OR inv.date <= sqlc.narg('date_to'));

-- name: GetInventoryByID :one
SELECT
    inv.id, inv.item_id, inv.warehouse_id, inv.quantity,
    inv.unit_index, inv.value, inv.date,
    i.name AS item_name, i.code AS item_code, i.units AS item_units,
    w.name AS warehouse_name
FROM inventory inv
JOIN items i ON i.id = inv.item_id
JOIN warehouses w ON w.id = inv.warehouse_id
WHERE inv.id = $1;

-- name: GetInventoryLotsForFIFO :many
SELECT id, quantity, value, date, unit_index
FROM inventory
WHERE item_id = $1 AND warehouse_id = $2 AND quantity > 0
ORDER BY date ASC, id ASC;

-- name: CreateInventoryLot :one
INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
RETURNING id, item_id, warehouse_id, quantity, unit_index, value, date;

-- name: UpdateInventoryLotQuantity :exec
UPDATE inventory SET quantity = $1, value = $2 WHERE id = $3;

-- name: UpdateInventoryLot :exec
UPDATE inventory SET quantity = $1, value = $2, date = $3 WHERE id = $4;

-- name: DeleteInventoryLot :exec
DELETE FROM inventory WHERE id = $1;

-- name: GetInventoryLotValue :one
SELECT value FROM inventory WHERE id = $1;

-- name: GetItemInventorySummary :many
SELECT
    item_id, warehouse_id,
    SUM(quantity) AS total_quantity,
    SUM(value) AS total_value
FROM inventory
WHERE item_id = $1
GROUP BY item_id, warehouse_id;
