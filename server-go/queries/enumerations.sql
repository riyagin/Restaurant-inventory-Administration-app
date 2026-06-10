-- name: ListEnumerations :many
SELECT
    e.id, e.warehouse_id, e.source_item_id, e.output_item_id,
    e.source_qty, e.source_unit_idx, e.output_qty, e.output_unit_idx,
    e.transferred_value, e.date, e.notes, e.created_at,
    w.name AS warehouse_name,
    si.name AS source_item_name,
    oi.name AS output_item_name,
    u.username AS created_by_name
FROM enumerations e
JOIN warehouses w ON w.id = e.warehouse_id
JOIN items si ON si.id = e.source_item_id
JOIN items oi ON oi.id = e.output_item_id
LEFT JOIN users u ON u.id = e.created_by
WHERE ($1::uuid IS NULL OR e.warehouse_id = $1)
  AND ($2::date IS NULL OR e.date >= $2)
  AND ($3::date IS NULL OR e.date <= $3)
ORDER BY e.date DESC, e.created_at DESC;

-- name: GetEnumerationByID :one
SELECT
    e.id, e.warehouse_id, e.source_item_id, e.output_item_id,
    e.source_qty, e.source_unit_idx, e.output_qty, e.output_unit_idx,
    e.transferred_value, e.date, e.notes, e.created_at,
    w.name AS warehouse_name,
    si.name AS source_item_name, si.units AS source_item_units,
    oi.name AS output_item_name, oi.units AS output_item_units
FROM enumerations e
JOIN warehouses w ON w.id = e.warehouse_id
JOIN items si ON si.id = e.source_item_id
JOIN items oi ON oi.id = e.output_item_id
WHERE e.id = $1;

-- name: InsertEnumeration :one
INSERT INTO enumerations (
    id, warehouse_id, source_item_id, output_item_id,
    source_qty, source_unit_idx, output_qty, output_unit_idx,
    transferred_value, date, notes, created_by
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, created_at;

-- name: DeleteEnumeration :exec
DELETE FROM enumerations WHERE id = $1;
