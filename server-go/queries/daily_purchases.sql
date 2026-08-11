-- name: NextDailyPurchaseNumber :one
SELECT 'PH-' || LPAD(nextval('daily_purchase_number_seq')::text, 6, '0') AS number;

-- name: CreateDailyPurchase :one
INSERT INTO daily_purchases (
    id, number, date, branch_id, division_id, warehouse_id, expense_category_id,
    petty_cash_account_id, vendor_id, notes, created_by
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, number, date, branch_id, division_id, warehouse_id, expense_category_id,
          petty_cash_account_id, vendor_id, total_amount, notes, photo_path, status,
          created_by, created_at, cancelled_by, cancelled_at, cancel_reason;

-- name: CreateDailyPurchaseItem :exec
INSERT INTO daily_purchase_items (
    id, purchase_id, item_id, description, quantity, unit_index, price, conversion_factor
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7);

-- name: SetDailyPurchaseTotal :exec
UPDATE daily_purchases SET total_amount = $1 WHERE id = $2;

-- name: SetDailyPurchasePhoto :exec
UPDATE daily_purchases SET photo_path = $1 WHERE id = $2;

-- name: CancelDailyPurchase :exec
UPDATE daily_purchases
SET status = 'cancelled', cancelled_by = $1, cancelled_at = now(), cancel_reason = $2
WHERE id = $3;

-- The list the office works from. Every filter is optional so one query serves
-- the branch view, the date view and the "everything today" view.
-- name: ListDailyPurchases :many
SELECT
    dp.id, dp.number, dp.date, dp.total_amount, dp.status, dp.notes, dp.photo_path,
    dp.branch_id, b.name AS branch_name,
    dp.division_id, d.name AS division_name,
    dp.warehouse_id, w.name AS warehouse_name,
    dp.vendor_id, v.name AS vendor_name,
    dp.petty_cash_account_id, pa.name AS petty_cash_account_name,
    dp.created_at, u.username AS created_by_name,
    (SELECT COUNT(*) FROM daily_purchase_items i WHERE i.purchase_id = dp.id)::int AS line_count
FROM daily_purchases dp
JOIN branches b            ON b.id = dp.branch_id
LEFT JOIN divisions d      ON d.id = dp.division_id
LEFT JOIN warehouses w     ON w.id = dp.warehouse_id
LEFT JOIN vendors v        ON v.id = dp.vendor_id
LEFT JOIN accounts pa      ON pa.id = dp.petty_cash_account_id
LEFT JOIN users u          ON u.id = dp.created_by
WHERE ($1::uuid IS NULL OR dp.branch_id = $1)
  AND ($2::date IS NULL OR dp.date >= $2)
  AND ($3::date IS NULL OR dp.date <= $3)
  AND ($4::text IS NULL OR dp.status = $4)
ORDER BY dp.date DESC, dp.created_at DESC;

-- name: GetDailyPurchaseByID :one
SELECT
    dp.id, dp.number, dp.date, dp.total_amount, dp.status, dp.notes, dp.photo_path,
    dp.branch_id, b.name AS branch_name,
    dp.division_id, d.name AS division_name,
    dp.warehouse_id, w.name AS warehouse_name,
    dp.expense_category_id, ec.name AS expense_category_name,
    dp.vendor_id, v.name AS vendor_name,
    dp.petty_cash_account_id, pa.name AS petty_cash_account_name,
    dp.created_at, u.username AS created_by_name,
    dp.cancelled_at, cu.username AS cancelled_by_name, dp.cancel_reason
FROM daily_purchases dp
JOIN branches b                 ON b.id = dp.branch_id
LEFT JOIN divisions d           ON d.id = dp.division_id
LEFT JOIN warehouses w          ON w.id = dp.warehouse_id
LEFT JOIN expense_categories ec ON ec.id = dp.expense_category_id
LEFT JOIN vendors v             ON v.id = dp.vendor_id
LEFT JOIN accounts pa           ON pa.id = dp.petty_cash_account_id
LEFT JOIN users u               ON u.id = dp.created_by
LEFT JOIN users cu              ON cu.id = dp.cancelled_by
WHERE dp.id = $1;

-- name: GetDailyPurchaseRow :one
SELECT id, number, date, branch_id, division_id, warehouse_id, expense_category_id,
       petty_cash_account_id, vendor_id, total_amount, notes, photo_path, status,
       created_by, created_at, cancelled_by, cancelled_at, cancel_reason
FROM daily_purchases WHERE id = $1;

-- Lines carry the item's unit list so a reversal can unwind at the *stored*
-- conversion factor without a second round trip per line.
-- name: GetDailyPurchaseItems :many
SELECT
    i.id, i.purchase_id, i.item_id, i.description, i.quantity, i.unit_index,
    i.price, i.conversion_factor,
    it.name AS item_name, it.code AS item_code, it.is_stock, it.units AS item_units
FROM daily_purchase_items i
LEFT JOIN items it ON it.id = i.item_id
WHERE i.purchase_id = $1
ORDER BY i.id;

-- The spending side of the day's reconciliation: what left this branch's box.
-- Cancelled rows are excluded — their money went back in.
-- name: SumDailyPurchasesForDay :one
SELECT COALESCE(SUM(total_amount), 0)::bigint AS total,
       COUNT(*)::int AS count
FROM daily_purchases
WHERE branch_id = $1 AND date = $2 AND status = 'posted';

-- name: ListDailyPurchasesForDay :many
SELECT id, number, total_amount, notes, status
FROM daily_purchases
WHERE branch_id = $1 AND date = $2
ORDER BY created_at;
