-- name: ListInvoices :many
SELECT
    i.id, i.invoice_number, i.date, i.due_date, i.invoice_type,
    i.payment_method, i.payment_status, i.amount_paid, i.reference_number,
    i.photo_path, i.created_at,
    i.vendor_id, v.name AS vendor_name,
    i.warehouse_id, w.name AS warehouse_name,
    i.branch_id, b.name AS branch_name,
    i.account_id, a.name AS account_name,
    i.dispatch_id,
    COALESCE(SUM(ii.price * ii.quantity), 0)::BIGINT AS total_amount
FROM invoices i
LEFT JOIN vendors v ON v.id = i.vendor_id
LEFT JOIN warehouses w ON w.id = i.warehouse_id
LEFT JOIN branches b ON b.id = i.branch_id
LEFT JOIN accounts a ON a.id = i.account_id
LEFT JOIN invoice_items ii ON ii.invoice_id = i.id
WHERE (NULLIF($1, '')::text IS NULL OR i.invoice_type = $1)
  AND (NULLIF($2, '')::text IS NULL OR i.payment_status = $2)
  AND ($3::date IS NULL OR i.date >= $3)
  AND ($4::date IS NULL OR i.date <= $4)
  AND ($5::uuid IS NULL OR i.vendor_id = $5)
GROUP BY i.id, v.name, w.name, b.name, a.name
ORDER BY i.date DESC, i.created_at DESC;

-- name: GetInvoiceByID :one
SELECT
    i.id, i.invoice_number, i.date, i.due_date, i.invoice_type,
    i.payment_method, i.payment_status, i.amount_paid, i.reference_number,
    i.photo_path, i.created_at, i.account_id,
    i.vendor_id, v.name AS vendor_name,
    i.warehouse_id, w.name AS warehouse_name,
    i.branch_id, b.name AS branch_name,
    i.division_id, d.name AS division_name,
    i.dispatch_id
FROM invoices i
LEFT JOIN vendors v ON v.id = i.vendor_id
LEFT JOIN warehouses w ON w.id = i.warehouse_id
LEFT JOIN branches b ON b.id = i.branch_id
LEFT JOIN divisions d ON d.id = i.division_id
WHERE i.id = $1;

-- name: GetInvoiceWithTotal :one
SELECT
    i.id, i.invoice_number, i.invoice_type, i.payment_status,
    i.amount_paid, i.account_id, i.warehouse_id, i.branch_id, i.division_id, i.dispatch_id,
    COALESCE(SUM(ii.price * ii.quantity), 0)::BIGINT AS total_amount
FROM invoices i
LEFT JOIN invoice_items ii ON ii.invoice_id = i.id
WHERE i.id = $1
GROUP BY i.id;

-- name: GetInvoiceItems :many
SELECT
    ii.id, ii.invoice_id, ii.item_id, ii.vendor_id,
    ii.quantity, ii.unit_index, ii.price, ii.description,
    it.name AS item_name, it.units AS item_units,
    v.name AS vendor_name
FROM invoice_items ii
LEFT JOIN items it ON it.id = ii.item_id
LEFT JOIN vendors v ON v.id = ii.vendor_id
WHERE ii.invoice_id = $1
ORDER BY ii.id;

-- name: GetNextInvoiceNumber :one
SELECT 'INV-' || LPAD(nextval('invoice_seq')::text, 5, '0') AS invoice_number;

-- name: CreateInvoice :one
INSERT INTO invoices (
    id, invoice_number, date, due_date, invoice_type, payment_method,
    payment_status, amount_paid, account_id, warehouse_id, branch_id,
    division_id, vendor_id, reference_number
)
VALUES (
    gen_random_uuid(), $1, $2, $3, $4, $5, 'unpaid', 0, $6, $7, $8, $9, $10, $11
)
RETURNING id, invoice_number, date, due_date, invoice_type, payment_status, created_at;

-- name: CreateInvoiceItem :one
INSERT INTO invoice_items (id, invoice_id, item_id, vendor_id, quantity, unit_index, price, description)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: UpdateInvoice :one
UPDATE invoices
SET date = $1, due_date = $2, payment_method = $3, account_id = $4,
    vendor_id = $5, reference_number = $6, warehouse_id = $7, branch_id = $8, division_id = $9
WHERE id = $10
RETURNING id;

-- name: DeleteInvoiceItems :exec
DELETE FROM invoice_items WHERE invoice_id = $1;

-- name: DeleteInvoice :exec
DELETE FROM invoices WHERE id = $1;

-- name: UpdateInvoicePayment :one
UPDATE invoices
SET amount_paid = $1, payment_status = $2, account_id = $3
WHERE id = $4
RETURNING id, invoice_number, amount_paid, payment_status;

-- name: UpdateInvoicePhotoPath :exec
UPDATE invoices SET photo_path = $1 WHERE id = $2;

-- name: SetInvoiceDispatchID :exec
UPDATE invoices SET dispatch_id = $1 WHERE id = $2;
