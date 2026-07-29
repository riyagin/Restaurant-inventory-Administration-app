-- name: ListDispatchTemplates :many
SELECT
    t.id, t.name, t.warehouse_id, t.branch_id, t.division_id, t.notes, t.created_at,
    w.name AS warehouse_name, b.name AS branch_name, d.name AS division_name
FROM dispatch_templates t
LEFT JOIN warehouses w ON w.id = t.warehouse_id
LEFT JOIN branches b ON b.id = t.branch_id
LEFT JOIN divisions d ON d.id = t.division_id
ORDER BY t.name;

-- name: GetDispatchTemplateByID :one
SELECT
    t.id, t.name, t.warehouse_id, t.branch_id, t.division_id, t.notes, t.created_at,
    w.name AS warehouse_name, b.name AS branch_name, d.name AS division_name
FROM dispatch_templates t
LEFT JOIN warehouses w ON w.id = t.warehouse_id
LEFT JOIN branches b ON b.id = t.branch_id
LEFT JOIN divisions d ON d.id = t.division_id
WHERE t.id = $1;

-- name: GetDispatchTemplateItems :many
SELECT
    ti.id, ti.template_id, ti.item_id, ti.unit_index, ti.sort_order,
    i.name AS item_name, i.units AS item_units
FROM dispatch_template_items ti
LEFT JOIN items i ON i.id = ti.item_id
WHERE ti.template_id = $1
ORDER BY ti.sort_order;

-- name: CreateDispatchTemplate :one
INSERT INTO dispatch_templates (id, name, warehouse_id, branch_id, division_id, notes)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
RETURNING id, name, warehouse_id, branch_id, division_id, notes, created_at;

-- name: CreateDispatchTemplateItem :one
INSERT INTO dispatch_template_items (id, template_id, item_id, unit_index, sort_order)
VALUES (gen_random_uuid(), $1, $2, $3, $4)
RETURNING id, template_id, item_id, unit_index, sort_order;

-- name: DeleteDispatchTemplateItems :exec
DELETE FROM dispatch_template_items WHERE template_id = $1;

-- name: UpdateDispatchTemplate :one
UPDATE dispatch_templates
SET name = $1, warehouse_id = $2, branch_id = $3, division_id = $4, notes = $5
WHERE id = $6
RETURNING id, name, warehouse_id, branch_id, division_id, notes, created_at;

-- name: DeleteDispatchTemplate :exec
DELETE FROM dispatch_templates WHERE id = $1;
