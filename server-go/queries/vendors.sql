-- name: ListVendors :many
SELECT id, name FROM vendors ORDER BY name;

-- name: GetVendorByID :one
SELECT id, name FROM vendors WHERE id = $1;

-- name: CreateVendor :one
INSERT INTO vendors (id, name)
VALUES (gen_random_uuid(), $1)
RETURNING id, name;

-- name: UpdateVendor :one
UPDATE vendors SET name = $1 WHERE id = $2
RETURNING id, name;

-- name: DeleteVendor :exec
DELETE FROM vendors WHERE id = $1;

-- name: GetVendorHistory :many
SELECT
    i.id,
    i.invoice_number,
    i.date,
    i.invoice_type,
    i.payment_status,
    COALESCE(SUM(ii.price * ii.quantity), 0)::BIGINT AS total_amount,
    i.amount_paid
FROM invoices i
LEFT JOIN invoice_items ii ON ii.invoice_id = i.id
WHERE i.vendor_id = $1
GROUP BY i.id, i.invoice_number, i.date, i.invoice_type, i.payment_status, i.amount_paid
ORDER BY i.date DESC;
