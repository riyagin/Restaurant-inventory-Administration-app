-- name: NextCashDepositNumber :one
SELECT 'ST-' || LPAD(nextval('cash_deposit_number_seq')::text, 6, '0') AS number;

-- name: CreateCashDeposit :one
INSERT INTO cash_deposits (
    id, number, date, branch_id, movement_type, from_account_id, to_account_id,
    amount, reference, handed_to, notes, created_by
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, number, date, branch_id, movement_type, from_account_id, to_account_id,
          amount, reference, handed_to, notes, photo_path, status,
          created_by, created_at, cancelled_by, cancelled_at, cancel_reason;

-- name: GetCashDepositRow :one
SELECT id, number, date, branch_id, movement_type, from_account_id, to_account_id,
       amount, reference, handed_to, notes, photo_path, status,
       created_by, created_at, cancelled_by, cancelled_at, cancel_reason
FROM cash_deposits WHERE id = $1;

-- name: CancelCashDeposit :exec
UPDATE cash_deposits
SET status = 'cancelled', cancelled_by = $1, cancelled_at = now(), cancel_reason = $2
WHERE id = $3;

-- name: SetCashDepositPhoto :exec
UPDATE cash_deposits SET photo_path = $1 WHERE id = $2;

-- name: ListCashDeposits :many
SELECT
    cd.id, cd.number, cd.date, cd.movement_type, cd.amount, cd.reference,
    cd.handed_to, cd.notes, cd.photo_path, cd.status,
    cd.branch_id, b.name AS branch_name,
    cd.from_account_id, fa.name AS from_account_name,
    cd.to_account_id, ta.name AS to_account_name,
    cd.created_at, u.username AS created_by_name,
    cd.cancelled_at, cd.cancel_reason
FROM cash_deposits cd
LEFT JOIN branches b ON b.id = cd.branch_id
JOIN accounts fa     ON fa.id = cd.from_account_id
JOIN accounts ta     ON ta.id = cd.to_account_id
LEFT JOIN users u    ON u.id = cd.created_by
WHERE ($1::uuid IS NULL OR cd.branch_id = $1)
  AND ($2::date IS NULL OR cd.date >= $2)
  AND ($3::date IS NULL OR cd.date <= $3)
  AND ($4::text IS NULL OR cd.movement_type = $4)
  AND ($5::text IS NULL OR cd.status = $5)
ORDER BY cd.date DESC, cd.created_at DESC;

-- Movement through one account on one day, split by direction. This is the
-- top-up half of the petty cash reconciliation: the box should hold opening +
-- cash_in - cash_out - spending.
-- name: SumCashDepositsForAccountDay :one
SELECT
    COALESCE(SUM(amount) FILTER (WHERE to_account_id = $1), 0)::bigint   AS cash_in,
    COALESCE(SUM(amount) FILTER (WHERE from_account_id = $1), 0)::bigint AS cash_out
FROM cash_deposits
WHERE date = $2 AND status = 'posted'
  AND (to_account_id = $1 OR from_account_id = $1);
