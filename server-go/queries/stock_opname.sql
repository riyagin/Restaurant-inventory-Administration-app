-- name: ListStockOpname :many
SELECT
    so.id, so.warehouse_id, so.notes, so.performed_at,
    so.operator_name, so.pic_name,
    w.name AS warehouse_name,
    u.username AS performed_by_name
FROM stock_opname so
JOIN warehouses w ON w.id = so.warehouse_id
LEFT JOIN users u ON u.id = so.performed_by
ORDER BY so.performed_at DESC;

-- name: GetStockOpnameByID :one
SELECT
    so.id, so.warehouse_id, so.notes, so.performed_at,
    so.operator_name, so.pic_name,
    w.name AS warehouse_name,
    u.username AS performed_by_name
FROM stock_opname so
JOIN warehouses w ON w.id = so.warehouse_id
LEFT JOIN users u ON u.id = so.performed_by
WHERE so.id = $1;

-- name: GetStockOpnameItems :many
SELECT
    soi.id, soi.opname_id, soi.item_id, soi.unit_index, soi.unit_name,
    soi.recorded_quantity, soi.actual_quantity, soi.difference, soi.waste_value,
    i.name AS item_name
FROM stock_opname_items soi
JOIN items i ON i.id = soi.item_id
WHERE soi.opname_id = $1
ORDER BY i.name;

-- name: InsertStockOpname :one
INSERT INTO stock_opname (id, warehouse_id, notes, performed_by, operator_name, pic_name)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
RETURNING id, warehouse_id, notes, performed_at, operator_name, pic_name;

-- name: InsertStockOpnameItem :exec
INSERT INTO stock_opname_items (
    id, opname_id, item_id, unit_index, unit_name,
    recorded_quantity, actual_quantity, difference, waste_value
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetCurrentInventoryQuantity :one
SELECT COALESCE(SUM(quantity), '0'::numeric) AS total_quantity
FROM inventory
WHERE item_id = $1 AND warehouse_id = $2;

-- name: GetStockWasteAccountID :one
SELECT id FROM accounts WHERE name = 'Stock Waste' LIMIT 1;
