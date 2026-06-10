-- name: ListProductions :many
SELECT
    p.id, p.recipe_id, p.warehouse_id, p.batches, p.output_quantity,
    p.date, p.notes, p.created_at,
    r.name AS recipe_name,
    r.output_item_id,
    i.name AS output_item_name,
    w.name AS warehouse_name,
    u.username AS created_by_name
FROM productions p
JOIN recipes r ON r.id = p.recipe_id
JOIN items i ON i.id = r.output_item_id
JOIN warehouses w ON w.id = p.warehouse_id
LEFT JOIN users u ON u.id = p.created_by
WHERE ($1::date IS NULL OR p.date >= $1)
  AND ($2::date IS NULL OR p.date <= $2)
ORDER BY p.date DESC, p.created_at DESC;

-- name: InsertProduction :one
INSERT INTO productions (id, recipe_id, warehouse_id, batches, output_quantity, date, notes, created_by)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at;
