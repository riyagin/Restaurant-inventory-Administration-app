-- name: ListStockHistoryByItem :many
SELECT
    sh.id, sh.item_id, sh.warehouse_id, sh.quantity_change,
    sh.unit_name, sh.vendor, sh.type, sh.reference,
    sh.date, sh.created_at, sh.value,
    w.name AS warehouse_name
FROM stock_history sh
LEFT JOIN warehouses w ON w.id = sh.warehouse_id
WHERE sh.item_id = $1
  AND ($2::date IS NULL OR sh.date >= $2)
  AND ($3::date IS NULL OR sh.date <= $3)
ORDER BY sh.date DESC, sh.created_at DESC;

-- name: DeleteStockHistoryBySource :exec
DELETE FROM stock_history WHERE source_id = $1 AND source_type = $2;

-- name: InsertStockHistory :one
INSERT INTO stock_history (
    id, item_id, warehouse_id, quantity_change, unit_name,
    vendor, type, reference, date, value, source_id, source_type
)
VALUES (
    gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING id;
