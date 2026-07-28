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
