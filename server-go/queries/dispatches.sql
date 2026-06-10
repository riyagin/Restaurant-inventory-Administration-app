-- name: ListDispatches :many
SELECT
    d.id, d.branch_id, d.division_id, d.warehouse_id,
    d.notes, d.dispatched_at,
    b.name AS branch_name,
    dv.name AS division_name,
    w.name AS warehouse_name,
    u.username AS dispatched_by_name
FROM dispatches d
JOIN branches b ON b.id = d.branch_id
LEFT JOIN divisions dv ON dv.id = d.division_id
JOIN warehouses w ON w.id = d.warehouse_id
LEFT JOIN users u ON u.id = d.dispatched_by
WHERE ($1::date IS NULL OR d.dispatched_at::date >= $1)
  AND ($2::date IS NULL OR d.dispatched_at::date <= $2)
ORDER BY d.dispatched_at DESC;

-- name: GetDispatchByID :one
SELECT
    d.id, d.branch_id, d.division_id, d.warehouse_id,
    d.notes, d.dispatched_at,
    b.name AS branch_name,
    dv.name AS division_name,
    w.name AS warehouse_name,
    u.username AS dispatched_by_name
FROM dispatches d
JOIN branches b ON b.id = d.branch_id
LEFT JOIN divisions dv ON dv.id = d.division_id
JOIN warehouses w ON w.id = d.warehouse_id
LEFT JOIN users u ON u.id = d.dispatched_by
WHERE d.id = $1;

-- name: GetDispatchItems :many
SELECT
    di.id, di.dispatch_id, di.item_id, di.quantity, di.unit_index, di.unit_name,
    i.name AS item_name, i.code AS item_code, i.units AS item_units
FROM dispatch_items di
JOIN items i ON i.id = di.item_id
WHERE di.dispatch_id = $1
ORDER BY i.name;

-- name: InsertDispatch :one
INSERT INTO dispatches (id, branch_id, division_id, warehouse_id, notes, dispatched_by)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
RETURNING id, dispatched_at;

-- name: InsertDispatchItem :exec
INSERT INTO dispatch_items (id, dispatch_id, item_id, quantity, unit_index, unit_name)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5);
