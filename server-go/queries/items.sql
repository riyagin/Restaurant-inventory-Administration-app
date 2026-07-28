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

-- name: GetItemPurchaseHistory :many
SELECT
    ii.id, ii.quantity, ii.unit_index, ii.price,
    (ii.quantity * ii.price) AS line_total,
    ii.description,
    inv.id AS invoice_id, inv.invoice_number, inv.date,
    inv.invoice_type, inv.payment_status,
    v.name AS vendor_name,
    b.name AS branch_name,
    dv.name AS division_name,
    wh.name AS warehouse_name,
    it.units->ii.unit_index->>'name' AS unit_name
FROM invoice_items ii
JOIN invoices inv ON inv.id = ii.invoice_id
JOIN items it ON it.id = ii.item_id
LEFT JOIN vendors v     ON v.id  = COALESCE(ii.vendor_id, inv.vendor_id)
LEFT JOIN branches b    ON b.id  = inv.branch_id
LEFT JOIN divisions dv  ON dv.id = inv.division_id
LEFT JOIN warehouses wh ON wh.id = inv.warehouse_id
WHERE ii.item_id = $1
ORDER BY inv.date DESC, inv.created_at DESC;

-- Price breakdown per unit: prices for different units of the same item are not
-- comparable, so every rollup below keeps unit_index in the grouping key.
-- name: GetItemPriceByUnit :many
SELECT
    ii.unit_index,
    it.units->ii.unit_index->>'name' AS unit_name,
    SUM(ii.quantity)::numeric           AS total_quantity,
    SUM(ii.quantity * ii.price)::bigint AS total_spend,
    COUNT(*)::int                       AS purchase_count,
    COUNT(DISTINCT COALESCE(ii.vendor_id, inv.vendor_id))::int AS vendor_count,
    MIN(ii.price)::bigint               AS min_price,
    MAX(ii.price)::bigint               AS max_price,
    MIN(inv.date)                       AS first_purchase_date,
    MAX(inv.date)                       AS last_purchase_date,
    (ARRAY_AGG(ii.price ORDER BY inv.date DESC, inv.created_at DESC))[1]::bigint AS last_price,
    (ARRAY_AGG(ii.price ORDER BY inv.date ASC,  inv.created_at ASC))[1]::bigint  AS first_price
FROM invoice_items ii
JOIN invoices inv ON inv.id = ii.invoice_id
JOIN items it     ON it.id = ii.item_id
WHERE ii.item_id = $1
GROUP BY ii.unit_index, it.units->ii.unit_index->>'name'
ORDER BY total_spend DESC;

-- name: GetItemPriceByVendor :many
SELECT
    v.id   AS vendor_id,
    v.name AS vendor_name,
    ii.unit_index,
    it.units->ii.unit_index->>'name' AS unit_name,
    SUM(ii.quantity)::numeric           AS total_quantity,
    SUM(ii.quantity * ii.price)::bigint AS total_spend,
    COUNT(*)::int                       AS purchase_count,
    MIN(ii.price)::bigint               AS min_price,
    MAX(ii.price)::bigint               AS max_price,
    MIN(inv.date)                       AS first_purchase_date,
    MAX(inv.date)                       AS last_purchase_date,
    (ARRAY_AGG(ii.price ORDER BY inv.date DESC, inv.created_at DESC))[1]::bigint AS last_price,
    (ARRAY_AGG(ii.price ORDER BY inv.date ASC,  inv.created_at ASC))[1]::bigint  AS first_price
FROM invoice_items ii
JOIN invoices inv   ON inv.id = ii.invoice_id
JOIN items it       ON it.id  = ii.item_id
LEFT JOIN vendors v ON v.id   = COALESCE(ii.vendor_id, inv.vendor_id)
WHERE ii.item_id = $1
GROUP BY v.id, v.name, ii.unit_index, it.units->ii.unit_index->>'name'
ORDER BY MAX(inv.date) DESC;

-- name: GetItemPriceTrend :many
SELECT
    to_char(date_trunc('month', inv.date), 'YYYY-MM') AS month,
    ii.unit_index,
    it.units->ii.unit_index->>'name' AS unit_name,
    SUM(ii.quantity)::numeric           AS total_quantity,
    SUM(ii.quantity * ii.price)::bigint AS total_spend,
    COUNT(*)::int                       AS purchase_count,
    MIN(ii.price)::bigint               AS min_price,
    MAX(ii.price)::bigint               AS max_price,
    (ARRAY_AGG(ii.price ORDER BY inv.date DESC, inv.created_at DESC))[1]::bigint AS last_price
FROM invoice_items ii
JOIN invoices inv ON inv.id = ii.invoice_id
JOIN items it     ON it.id = ii.item_id
WHERE ii.item_id = $1
GROUP BY 1, ii.unit_index, 3
ORDER BY 1 DESC;

-- name: GetItemStockByWarehouse :many
SELECT
    w.id   AS warehouse_id,
    w.name AS warehouse_name,
    SUM(inv.quantity)::numeric AS quantity,
    SUM(inv.value)::bigint     AS value,
    COUNT(*)::int              AS lot_count,
    MIN(inv.date)              AS oldest_lot_date
FROM inventory inv
JOIN warehouses w ON w.id = inv.warehouse_id
WHERE inv.item_id = $1
GROUP BY w.id, w.name
ORDER BY w.name;

-- name: GetItemUsageByDestination :many
SELECT
    b.name  AS branch_name,
    dv.name AS division_name,
    di.unit_name,
    SUM(di.quantity)::numeric      AS quantity,
    COUNT(DISTINCT d.id)::int      AS dispatch_count,
    MAX(d.dispatched_at)           AS last_dispatched_at
FROM dispatch_items di
JOIN dispatches d ON d.id = di.dispatch_id
LEFT JOIN branches b   ON b.id  = d.branch_id
LEFT JOIN divisions dv ON dv.id = d.division_id
WHERE di.item_id = $1
  AND COALESCE(d.status, 'active') <> 'cancelled'
GROUP BY b.name, dv.name, di.unit_name
ORDER BY quantity DESC;

-- name: GetItemMonthlyFlow :many
SELECT
    to_char(date_trunc('month', sh.date), 'YYYY-MM') AS month,
    SUM(CASE WHEN sh.quantity_change > 0 THEN sh.quantity_change ELSE 0 END)::numeric AS qty_in,
    SUM(CASE WHEN sh.quantity_change < 0 THEN -sh.quantity_change ELSE 0 END)::numeric AS qty_out,
    COALESCE(SUM(CASE WHEN sh.value > 0 THEN sh.value ELSE 0 END), 0)::bigint  AS value_in,
    COALESCE(SUM(CASE WHEN sh.value < 0 THEN -sh.value ELSE 0 END), 0)::bigint AS value_out,
    COUNT(*)::int AS movement_count
FROM stock_history sh
WHERE sh.item_id = $1
GROUP BY 1
ORDER BY 1 DESC;

-- name: GetItemFlowByType :many
SELECT
    sh.type,
    SUM(CASE WHEN sh.quantity_change > 0 THEN sh.quantity_change ELSE 0 END)::numeric AS qty_in,
    SUM(CASE WHEN sh.quantity_change < 0 THEN -sh.quantity_change ELSE 0 END)::numeric AS qty_out,
    COALESCE(SUM(sh.value), 0)::bigint AS value_net,
    COUNT(*)::int AS movement_count
FROM stock_history sh
WHERE sh.item_id = $1
GROUP BY sh.type
ORDER BY movement_count DESC;

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
