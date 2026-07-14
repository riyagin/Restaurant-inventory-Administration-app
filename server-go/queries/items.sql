-- name: ListItems :many
SELECT id, name, code, units, is_stock FROM items ORDER BY name;

-- name: GetItemByID :one
SELECT id, name, code, units, is_stock FROM items WHERE id = $1;

-- name: CreateItem :one
INSERT INTO items (id, name, code, units, is_stock)
VALUES (gen_random_uuid(), $1, $2, $3, $4)
RETURNING id, name, code, units, is_stock;

-- name: UpdateItem :one
UPDATE items SET name = $1, code = $2, units = $3, is_stock = $4
WHERE id = $5
RETURNING id, name, code, units, is_stock;

-- name: DeleteItem :exec
DELETE FROM items WHERE id = $1;

-- name: GetItemLastPrice :one
SELECT ii.price, ii.unit_index, i.date
FROM invoice_items ii
JOIN invoices i ON i.id = ii.invoice_id
WHERE ii.item_id = $1
ORDER BY i.date DESC, i.created_at DESC
LIMIT 1;

-- name: GetItemStockHistory :many
SELECT
    sh.id, sh.quantity_change, sh.unit_name, sh.vendor,
    sh.type, sh.reference, sh.date, sh.created_at, sh.value,
    w.name AS warehouse_name
FROM stock_history sh
LEFT JOIN warehouses w ON w.id = sh.warehouse_id
WHERE sh.item_id = $1
  AND ($2::date IS NULL OR sh.date >= $2)
  AND ($3::date IS NULL OR sh.date <= $3)
ORDER BY sh.date DESC, sh.created_at DESC;
