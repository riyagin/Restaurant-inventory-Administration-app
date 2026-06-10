-- name: GetInventoryValueReport :many
SELECT
    i.id AS item_id, i.name AS item_name, i.code AS item_code, i.units,
    w.id AS warehouse_id, w.name AS warehouse_name,
    SUM(inv.quantity) AS total_quantity,
    SUM(inv.value) AS total_value,
    MIN(inv.unit_index) AS unit_index
FROM inventory inv
JOIN items i ON i.id = inv.item_id
JOIN warehouses w ON w.id = inv.warehouse_id
GROUP BY i.id, i.name, i.code, i.units, w.id, w.name
ORDER BY i.name, w.name;
