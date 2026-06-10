-- name: ListStockTransfers :many
SELECT
    st.id, st.item_id, st.from_warehouse_id, st.to_warehouse_id,
    st.quantity, st.unit_index, st.unit_name, st.notes,
    st.transferred_by, st.transferred_at, st.group_id,
    i.name AS item_name,
    fw.name AS from_warehouse_name,
    tw.name AS to_warehouse_name,
    u.username AS transferred_by_name
FROM stock_transfers st
JOIN items i ON i.id = st.item_id
JOIN warehouses fw ON fw.id = st.from_warehouse_id
JOIN warehouses tw ON tw.id = st.to_warehouse_id
LEFT JOIN users u ON u.id = st.transferred_by
WHERE ($1::date IS NULL OR st.transferred_at::date >= $1)
  AND ($2::date IS NULL OR st.transferred_at::date <= $2)
ORDER BY st.transferred_at DESC;

-- name: ListStockTransfersByGroup :many
SELECT
    st.id, st.item_id, st.from_warehouse_id, st.to_warehouse_id,
    st.quantity, st.unit_index, st.unit_name, st.notes,
    st.transferred_by, st.transferred_at, st.group_id,
    i.name AS item_name,
    fw.name AS from_warehouse_name,
    tw.name AS to_warehouse_name
FROM stock_transfers st
JOIN items i ON i.id = st.item_id
JOIN warehouses fw ON fw.id = st.from_warehouse_id
JOIN warehouses tw ON tw.id = st.to_warehouse_id
WHERE st.group_id = $1
ORDER BY i.name;

-- name: InsertStockTransfer :one
INSERT INTO stock_transfers (
    id, item_id, from_warehouse_id, to_warehouse_id,
    quantity, unit_index, unit_name, notes, transferred_by, group_id
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, group_id, transferred_at;
