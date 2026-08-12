-- name: ListInventory :many
SELECT
    inv.id, inv.item_id, inv.warehouse_id, inv.quantity,
    inv.unit_index, inv.value, inv.date,
    i.name AS item_name, i.code AS item_code, i.units AS item_units,
    w.name AS warehouse_name
FROM inventory inv
JOIN items i ON i.id = inv.item_id
JOIN warehouses w ON w.id = inv.warehouse_id
WHERE ($1::uuid IS NULL OR inv.warehouse_id = $1)
  AND ($2::uuid IS NULL OR inv.item_id = $2)
ORDER BY i.name, w.name, inv.date ASC;

-- The inventory list.
--
-- Two things beyond the obvious. First, `include_empty` widens the list from
-- "lots with stock" to everything: depleted lots (which now survive their own
-- consumption) and items that have no lot at all. An item at zero is exactly the
-- one you need to see before a shopping run, so hiding it was the wrong default
-- to have had.
--
-- Second, ordering is chosen by the caller through `sort`/`dir` rather than
-- fixed. It has to happen here rather than in the client because the list is
-- paginated — sorting one page of 25 sorts nothing. The CASE ladder is verbose
-- but keeps the column list closed, so a sort key can never become an injection
-- point.
-- name: ListInventoryPage :many
WITH lots AS (
    SELECT
        inv.id, inv.item_id, inv.warehouse_id, inv.quantity,
        inv.unit_index, inv.date, inv.depleted_at,
        i.name AS item_name, i.code AS item_code, i.units AS item_units,
        w.name AS warehouse_name
    FROM inventory inv
    JOIN items i ON i.id = inv.item_id
    JOIN warehouses w ON w.id = inv.warehouse_id
    WHERE (sqlc.narg('warehouse_id')::uuid IS NULL OR inv.warehouse_id = sqlc.narg('warehouse_id'))
      AND (sqlc.narg('item_id')::uuid IS NULL OR inv.item_id = sqlc.narg('item_id'))
      AND (sqlc.narg('search')::text IS NULL OR i.name ILIKE '%' || sqlc.narg('search') || '%' OR i.code ILIKE '%' || sqlc.narg('search') || '%')
      AND (sqlc.narg('date_from')::date IS NULL OR inv.date >= sqlc.narg('date_from'))
      AND (sqlc.narg('date_to')::date IS NULL OR inv.date <= sqlc.narg('date_to'))
      AND (sqlc.arg('include_empty')::bool OR inv.quantity > 0)
),
-- Stock items with no lot at all in the selected warehouse. They carry a NULL
-- lot id, which is what tells the UI there is nothing to edit, delete or trace —
-- the row exists to say "we stock this and have none".
empty_items AS (
    SELECT
        NULL::uuid AS id, i.id AS item_id, NULL::uuid AS warehouse_id, 0::numeric AS quantity,
        0::int AS unit_index, NULL::date AS date, NULL::timestamptz AS depleted_at,
        i.name AS item_name, i.code AS item_code, i.units AS item_units,
        NULL::text AS warehouse_name
    FROM items i
    WHERE sqlc.arg('include_empty')::bool
      AND i.is_stock
      AND (sqlc.narg('item_id')::uuid IS NULL OR i.id = sqlc.narg('item_id'))
      AND (sqlc.narg('search')::text IS NULL OR i.name ILIKE '%' || sqlc.narg('search') || '%' OR i.code ILIKE '%' || sqlc.narg('search') || '%')
      AND sqlc.narg('date_from')::date IS NULL
      AND sqlc.narg('date_to')::date IS NULL
      AND NOT EXISTS (
        SELECT 1 FROM inventory x
        WHERE x.item_id = i.id
          AND (sqlc.narg('warehouse_id')::uuid IS NULL OR x.warehouse_id = sqlc.narg('warehouse_id'))
      )
)
SELECT id, item_id, warehouse_id, quantity, unit_index, date, depleted_at,
       item_name, item_code, item_units, warehouse_name
FROM (SELECT * FROM lots UNION ALL SELECT * FROM empty_items) rows
ORDER BY
    CASE WHEN sqlc.arg('sort')::text = 'item'      AND sqlc.arg('dir')::text = 'asc'  THEN item_name END ASC,
    CASE WHEN sqlc.arg('sort')::text = 'item'      AND sqlc.arg('dir')::text = 'desc' THEN item_name END DESC,
    CASE WHEN sqlc.arg('sort')::text = 'code'      AND sqlc.arg('dir')::text = 'asc'  THEN item_code END ASC,
    CASE WHEN sqlc.arg('sort')::text = 'code'      AND sqlc.arg('dir')::text = 'desc' THEN item_code END DESC,
    CASE WHEN sqlc.arg('sort')::text = 'quantity'  AND sqlc.arg('dir')::text = 'asc'  THEN quantity END ASC,
    CASE WHEN sqlc.arg('sort')::text = 'quantity'  AND sqlc.arg('dir')::text = 'desc' THEN quantity END DESC,
    CASE WHEN sqlc.arg('sort')::text = 'warehouse' AND sqlc.arg('dir')::text = 'asc'  THEN warehouse_name END ASC,
    CASE WHEN sqlc.arg('sort')::text = 'warehouse' AND sqlc.arg('dir')::text = 'desc' THEN warehouse_name END DESC,
    CASE WHEN sqlc.arg('sort')::text = 'date'      AND sqlc.arg('dir')::text = 'asc'  THEN date END ASC,
    CASE WHEN sqlc.arg('sort')::text = 'date'      AND sqlc.arg('dir')::text = 'desc' THEN date END DESC,
    -- Stable tail so equal keys never shuffle between pages.
    item_name, warehouse_name NULLS LAST, date
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountInventory :one
SELECT (
    (SELECT COUNT(*)
     FROM inventory inv
     JOIN items i ON i.id = inv.item_id
     WHERE (sqlc.narg('warehouse_id')::uuid IS NULL OR inv.warehouse_id = sqlc.narg('warehouse_id'))
       AND (sqlc.narg('item_id')::uuid IS NULL OR inv.item_id = sqlc.narg('item_id'))
       AND (sqlc.narg('search')::text IS NULL OR i.name ILIKE '%' || sqlc.narg('search') || '%' OR i.code ILIKE '%' || sqlc.narg('search') || '%')
       AND (sqlc.narg('date_from')::date IS NULL OR inv.date >= sqlc.narg('date_from'))
       AND (sqlc.narg('date_to')::date IS NULL OR inv.date <= sqlc.narg('date_to'))
       AND (sqlc.arg('include_empty')::bool OR inv.quantity > 0))
  + (SELECT COUNT(*)
     FROM items i
     WHERE sqlc.arg('include_empty')::bool
       AND i.is_stock
       AND (sqlc.narg('item_id')::uuid IS NULL OR i.id = sqlc.narg('item_id'))
       AND (sqlc.narg('search')::text IS NULL OR i.name ILIKE '%' || sqlc.narg('search') || '%' OR i.code ILIKE '%' || sqlc.narg('search') || '%')
       AND sqlc.narg('date_from')::date IS NULL
       AND sqlc.narg('date_to')::date IS NULL
       AND NOT EXISTS (
         SELECT 1 FROM inventory x
         WHERE x.item_id = i.id
           AND (sqlc.narg('warehouse_id')::uuid IS NULL OR x.warehouse_id = sqlc.narg('warehouse_id'))))
)::bigint AS count;

-- name: GetInventoryByID :one
SELECT
    inv.id, inv.item_id, inv.warehouse_id, inv.quantity,
    inv.unit_index, inv.value, inv.date,
    i.name AS item_name, i.code AS item_code, i.units AS item_units,
    w.name AS warehouse_name
FROM inventory inv
JOIN items i ON i.id = inv.item_id
JOIN warehouses w ON w.id = inv.warehouse_id
WHERE inv.id = $1;

-- name: GetInventoryLotsForFIFO :many
SELECT id, quantity, value, date, unit_index
FROM inventory
WHERE item_id = $1 AND warehouse_id = $2 AND quantity > 0
ORDER BY date ASC, created_at ASC, id ASC;

-- A used-up lot is stamped and zeroed, never deleted: the row is the only
-- surviving record that the delivery existed, and its usage log hangs off it.
-- name: DepleteInventoryLot :exec
UPDATE inventory
SET quantity = 0, value = 0, depleted_at = COALESCE(depleted_at, now())
WHERE id = $1;

-- name: InsertLotConsumption :exec
INSERT INTO inventory_lot_consumptions (
    id, lot_id, item_id, warehouse_id, quantity, value,
    source_type, source_id, reference, date
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9);

-- Everything that came out of one lot, oldest first — the body of a lot's
-- history page. Joined out to a human label per source so the page can say
-- "Pengiriman ke Cimanggu" rather than a bare UUID.
-- name: GetLotConsumptions :many
SELECT
    c.id, c.quantity, c.value, c.source_type, c.source_id, c.reference, c.date, c.created_at,
    COALESCE(b.name, dv.name, w.name, '')::text AS destination
FROM inventory_lot_consumptions c
LEFT JOIN dispatches d   ON c.source_type = 'dispatch' AND d.id = c.source_id
LEFT JOIN branches b     ON b.id = d.branch_id
LEFT JOIN divisions dv   ON dv.id = d.division_id
LEFT JOIN stock_transfers st ON c.source_type = 'stock_transfer' AND st.id = c.source_id
LEFT JOIN warehouses w   ON w.id = st.to_warehouse_id
WHERE c.lot_id = $1
ORDER BY c.date, c.created_at;

-- The lot itself: what arrived, when, from whom, and what is left of it.
-- name: GetInventoryLotDetail :one
SELECT
    inv.id, inv.item_id, inv.warehouse_id, inv.quantity, inv.unit_index,
    inv.value, inv.date, inv.created_at, inv.depleted_at,
    i.name AS item_name, i.code AS item_code, i.units AS item_units, i.is_stock,
    w.name AS warehouse_name,
    -- The purchase that opened the lot, matched through the stock_history row
    -- written alongside it. Absent for a manually created lot, which is the
    -- honest answer rather than a fabricated origin.
    (SELECT sh.reference FROM stock_history sh
      WHERE sh.item_id = inv.item_id AND sh.warehouse_id = inv.warehouse_id
        AND sh.quantity_change > 0 AND sh.date = inv.date
      ORDER BY sh.created_at LIMIT 1)::text AS opening_reference,
    (SELECT sh.vendor FROM stock_history sh
      WHERE sh.item_id = inv.item_id AND sh.warehouse_id = inv.warehouse_id
        AND sh.quantity_change > 0 AND sh.date = inv.date
      ORDER BY sh.created_at LIMIT 1)::text AS opening_vendor,
    COALESCE((SELECT SUM(c.quantity) FROM inventory_lot_consumptions c WHERE c.lot_id = inv.id), 0)::numeric AS consumed_quantity,
    COALESCE((SELECT SUM(c.value)    FROM inventory_lot_consumptions c WHERE c.lot_id = inv.id), 0)::bigint  AS consumed_value
FROM inventory inv
JOIN items i ON i.id = inv.item_id
JOIN warehouses w ON w.id = inv.warehouse_id
WHERE inv.id = $1;

-- name: CreateInventoryLot :one
INSERT INTO inventory (id, item_id, warehouse_id, quantity, unit_index, value, date)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
RETURNING id, item_id, warehouse_id, quantity, unit_index, value, date;

-- name: UpdateInventoryLotQuantity :exec
UPDATE inventory SET quantity = $1, value = $2 WHERE id = $3;

-- name: UpdateInventoryLot :exec
UPDATE inventory SET quantity = $1, value = $2, date = $3 WHERE id = $4;

-- name: DeleteInventoryLot :exec
DELETE FROM inventory WHERE id = $1;

-- name: GetInventoryLotValue :one
SELECT value FROM inventory WHERE id = $1;

-- name: GetItemInventorySummary :many
SELECT
    item_id, warehouse_id,
    SUM(quantity) AS total_quantity,
    SUM(value) AS total_value
FROM inventory
WHERE item_id = $1
GROUP BY item_id, warehouse_id;
