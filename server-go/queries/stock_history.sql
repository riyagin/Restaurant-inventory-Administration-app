-- The running balance is computed over the item's *whole* history before the
-- date filter is applied, so a bounded window still reports the true on-hand
-- figure after each movement rather than one that restarts at zero.
-- name: ListStockHistoryByItem :many
WITH h AS (
    SELECT
        sh.id, sh.item_id, sh.warehouse_id, sh.quantity_change,
        sh.unit_name, sh.vendor, sh.type, sh.reference,
        sh.date, sh.created_at, sh.value,
        sh.source_id, sh.source_type,
        SUM(sh.quantity_change) OVER (
            ORDER BY sh.date, sh.created_at, sh.id
            ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
        ) AS balance_after
    FROM stock_history sh
    WHERE sh.item_id = $1
)
SELECT
    h.id, h.item_id, h.warehouse_id, h.quantity_change,
    h.unit_name, h.vendor, h.type, h.reference,
    h.date, h.created_at, h.value,
    h.source_id, h.source_type,
    w.name AS warehouse_name,
    h.balance_after::numeric AS balance_after,
    -- Where the stock went, for the movements that have a destination beyond
    -- the warehouse column: a dispatch names a branch/division, a transfer
    -- names the receiving warehouse.
    CASE
        WHEN h.source_type = 'dispatch' THEN NULLIF(CONCAT_WS(' / ', b.name, dv.name), '')
        WHEN h.source_type = 'transfer' AND h.quantity_change < 0 THEN tw.name
    END AS destination
FROM h
LEFT JOIN warehouses w ON w.id = h.warehouse_id
LEFT JOIN dispatches d ON h.source_type = 'dispatch' AND d.id = h.source_id
LEFT JOIN branches b   ON b.id  = d.branch_id
LEFT JOIN divisions dv ON dv.id = d.division_id
LEFT JOIN LATERAL (
    SELECT tw2.name
    FROM stock_transfers st
    JOIN warehouses tw2 ON tw2.id = st.to_warehouse_id
    WHERE h.source_type = 'transfer'
      AND st.group_id = h.source_id
      AND st.item_id  = h.item_id
    LIMIT 1
) tw ON TRUE
WHERE ($2::date IS NULL OR h.date >= $2)
  AND ($3::date IS NULL OR h.date <= $3)
ORDER BY h.date DESC, h.created_at DESC;

-- name: DeleteStockHistoryBySource :exec
DELETE FROM stock_history WHERE source_id = $1 AND source_type = $2;

-- name: InsertStockHistory :one
INSERT INTO stock_history (
    id, item_id, warehouse_id, quantity_change, unit_name,
    vendor, type, reference, date, value, source_id, source_type
)
VALUES (
    gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING id;
