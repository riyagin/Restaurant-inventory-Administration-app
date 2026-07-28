-- name: ListVendors :many
SELECT id, name, account_id FROM vendors ORDER BY name;

-- name: GetVendorByID :one
SELECT id, name, account_id FROM vendors WHERE id = $1;

-- name: CreateVendor :one
INSERT INTO vendors (id, name)
VALUES (gen_random_uuid(), $1)
RETURNING id, name, account_id;

-- name: UpdateVendor :one
UPDATE vendors SET name = $1 WHERE id = $2
RETURNING id, name, account_id;

-- name: SetVendorAccountID :exec
UPDATE vendors SET account_id = $1 WHERE id = $2;

-- name: DeleteVendor :exec
DELETE FROM vendors WHERE id = $1;

-- An invoice belongs to a vendor either through its own vendor_id or through a
-- line that overrides it, so both paths are covered here. total_amount is the
-- whole invoice; vendor_amount is only the part actually billed by this vendor.
-- name: GetVendorHistory :many
SELECT
    i.id,
    i.invoice_number,
    i.reference_number,
    i.date,
    i.due_date,
    i.invoice_type,
    i.payment_status,
    i.payment_method,
    i.amount_paid,
    a.name AS account_name,
    w.name AS warehouse_name,
    COALESCE(i.vendor_id = $1, FALSE) AS is_primary,
    COALESCE(SUM(ii.price * ii.quantity), 0)::BIGINT AS total_amount,
    COALESCE(SUM(ii.price * ii.quantity)
        FILTER (WHERE COALESCE(ii.vendor_id, i.vendor_id) = $1), 0)::BIGINT AS vendor_amount,
    COUNT(ii.id)::int AS line_count
FROM invoices i
LEFT JOIN invoice_items ii ON ii.invoice_id = i.id
LEFT JOIN accounts a       ON a.id = i.account_id
LEFT JOIN warehouses w     ON w.id = i.warehouse_id
WHERE i.vendor_id = $1
   OR EXISTS (SELECT 1 FROM invoice_items x WHERE x.invoice_id = i.id AND x.vendor_id = $1)
GROUP BY i.id, a.name, w.name
ORDER BY i.date DESC, i.created_at DESC;

-- Everything this vendor has ever sold us, one row per item + unit so prices
-- stay comparable (a price per dus must never be averaged with a price per pcs).
-- name: GetVendorItemSummary :many
SELECT
    it.id      AS item_id,
    it.name    AS item_name,
    it.code    AS item_code,
    it.is_stock,
    ii.unit_index,
    it.units->ii.unit_index->>'name' AS unit_name,
    SUM(ii.quantity)::numeric              AS total_quantity,
    SUM(ii.quantity * ii.price)::bigint    AS total_spend,
    COUNT(*)::int                          AS purchase_count,
    MIN(ii.price)::bigint                  AS min_price,
    MAX(ii.price)::bigint                  AS max_price,
    MIN(inv.date)                          AS first_purchase_date,
    MAX(inv.date)                          AS last_purchase_date,
    (ARRAY_AGG(ii.price ORDER BY inv.date DESC, inv.created_at DESC))[1]::bigint AS last_price,
    (ARRAY_AGG(ii.price ORDER BY inv.date ASC,  inv.created_at ASC))[1]::bigint  AS first_price
FROM invoice_items ii
JOIN invoices inv ON inv.id = ii.invoice_id
JOIN items it     ON it.id = ii.item_id
WHERE COALESCE(ii.vendor_id, inv.vendor_id) = $1
GROUP BY it.id, it.name, it.code, it.is_stock, ii.unit_index, it.units->ii.unit_index->>'name'
ORDER BY total_spend DESC;
