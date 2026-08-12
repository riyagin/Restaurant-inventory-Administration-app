-- name: ListDailyPurchaseTemplates :many
SELECT
    t.id, t.name, t.branch_id, t.division_id, t.warehouse_id, t.vendor_id,
    t.expense_category_id, t.notes, t.created_at,
    b.name AS branch_name, d.name AS division_name,
    w.name AS warehouse_name, v.name AS vendor_name,
    (SELECT COUNT(*) FROM daily_purchase_template_items ti WHERE ti.template_id = t.id)::int AS line_count
FROM daily_purchase_templates t
LEFT JOIN branches b   ON b.id = t.branch_id
LEFT JOIN divisions d  ON d.id = t.division_id
LEFT JOIN warehouses w ON w.id = t.warehouse_id
LEFT JOIN vendors v    ON v.id = t.vendor_id
ORDER BY t.name;

-- name: GetDailyPurchaseTemplateItems :many
SELECT
    ti.id, ti.template_id, ti.item_id, ti.description, ti.unit_index, ti.sort_order,
    i.name AS item_name, i.code AS item_code, i.units AS item_units, i.is_stock
FROM daily_purchase_template_items ti
LEFT JOIN items i ON i.id = ti.item_id
WHERE ti.template_id = $1
ORDER BY ti.sort_order;

-- name: CreateDailyPurchaseTemplate :one
INSERT INTO daily_purchase_templates (
    id, name, branch_id, division_id, warehouse_id, vendor_id,
    expense_category_id, notes, created_by
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, name, branch_id, division_id, warehouse_id, vendor_id,
          expense_category_id, notes, created_by, created_at;

-- name: UpdateDailyPurchaseTemplate :one
UPDATE daily_purchase_templates
SET name = $1, branch_id = $2, division_id = $3, warehouse_id = $4,
    vendor_id = $5, expense_category_id = $6, notes = $7
WHERE id = $8
RETURNING id, name, branch_id, division_id, warehouse_id, vendor_id,
          expense_category_id, notes, created_by, created_at;

-- name: CreateDailyPurchaseTemplateItem :exec
INSERT INTO daily_purchase_template_items (id, template_id, item_id, description, unit_index, sort_order)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5);

-- Lines are replaced wholesale on save rather than diffed: a template is small,
-- and reconciling adds/moves/removes buys nothing but a chance to get it wrong.
-- name: DeleteDailyPurchaseTemplateItems :exec
DELETE FROM daily_purchase_template_items WHERE template_id = $1;

-- name: DeleteDailyPurchaseTemplate :exec
DELETE FROM daily_purchase_templates WHERE id = $1;
