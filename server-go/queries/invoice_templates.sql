-- name: ListInvoiceTemplates :many
SELECT
    t.id, t.name, t.invoice_type, t.vendor_id, t.warehouse_id, t.created_at,
    v.name AS vendor_name, w.name AS warehouse_name
FROM invoice_templates t
LEFT JOIN vendors v ON v.id = t.vendor_id
LEFT JOIN warehouses w ON w.id = t.warehouse_id
ORDER BY t.name;

-- name: GetInvoiceTemplateByID :one
SELECT
    t.id, t.name, t.invoice_type, t.vendor_id, t.warehouse_id, t.created_at,
    v.name AS vendor_name, w.name AS warehouse_name
FROM invoice_templates t
LEFT JOIN vendors v ON v.id = t.vendor_id
LEFT JOIN warehouses w ON w.id = t.warehouse_id
WHERE t.id = $1;

-- name: GetInvoiceTemplateItems :many
SELECT
    ti.id, ti.template_id, ti.item_id, ti.description, ti.unit_index, ti.sort_order,
    i.name AS item_name, i.units AS item_units
FROM invoice_template_items ti
LEFT JOIN items i ON i.id = ti.item_id
WHERE ti.template_id = $1
ORDER BY ti.sort_order;

-- name: CreateInvoiceTemplate :one
INSERT INTO invoice_templates (id, name, invoice_type, vendor_id, warehouse_id)
VALUES (gen_random_uuid(), $1, $2, $3, $4)
RETURNING id, name, invoice_type, vendor_id, warehouse_id, created_at;

-- name: CreateInvoiceTemplateItem :one
INSERT INTO invoice_template_items (id, template_id, item_id, description, unit_index, sort_order)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
RETURNING id, template_id, item_id, description, unit_index, sort_order;

-- name: DeleteInvoiceTemplateItems :exec
DELETE FROM invoice_template_items WHERE template_id = $1;

-- name: UpdateInvoiceTemplate :one
UPDATE invoice_templates
SET name = $1, invoice_type = $2, vendor_id = $3, warehouse_id = $4
WHERE id = $5
RETURNING id, name, invoice_type, vendor_id, warehouse_id, created_at;

-- name: DeleteInvoiceTemplate :exec
DELETE FROM invoice_templates WHERE id = $1;
