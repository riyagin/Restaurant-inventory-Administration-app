-- name: GetPettyCashCount :one
SELECT id, branch_id, count_date, opening_amount, opening_by, opening_at,
       closing_amount, closing_by, closing_at, expected_closing, variance,
       variance_note, created_at
FROM petty_cash_counts
WHERE branch_id = $1 AND count_date = $2;

-- The opening count. Re-recording the same day is a correction of a miscount,
-- not a second observation, so it overwrites — and it clears the closing figures
-- with it, because a closing computed against the old opening is now wrong.
-- name: UpsertPettyCashOpening :one
INSERT INTO petty_cash_counts (id, branch_id, count_date, opening_amount, opening_by)
VALUES (gen_random_uuid(), $1, $2, $3, $4)
ON CONFLICT (branch_id, count_date) DO UPDATE
SET opening_amount = EXCLUDED.opening_amount,
    opening_by     = EXCLUDED.opening_by,
    opening_at     = now(),
    closing_amount = NULL,
    closing_by     = NULL,
    closing_at     = NULL,
    expected_closing = NULL,
    variance         = NULL,
    variance_note    = ''
RETURNING id, branch_id, count_date, opening_amount, opening_by, opening_at,
          closing_amount, closing_by, closing_at, expected_closing, variance,
          variance_note, created_at;

-- name: SetPettyCashClosing :one
UPDATE petty_cash_counts
SET closing_amount = $1, closing_by = $2, closing_at = now(),
    expected_closing = $3, variance = $4, variance_note = $5
WHERE branch_id = $6 AND count_date = $7
RETURNING id, branch_id, count_date, opening_amount, opening_by, opening_at,
          closing_amount, closing_by, closing_at, expected_closing, variance,
          variance_note, created_at;

-- name: ListPettyCashCounts :many
SELECT
    c.id, c.branch_id, b.name AS branch_name, c.count_date,
    c.opening_amount, ou.username AS opening_by_name, c.opening_at,
    c.closing_amount, cu.username AS closing_by_name, c.closing_at,
    c.expected_closing, c.variance, c.variance_note
FROM petty_cash_counts c
JOIN branches b   ON b.id = c.branch_id
LEFT JOIN users ou ON ou.id = c.opening_by
LEFT JOIN users cu ON cu.id = c.closing_by
WHERE ($1::uuid IS NULL OR c.branch_id = $1)
  AND ($2::date IS NULL OR c.count_date >= $2)
  AND ($3::date IS NULL OR c.count_date <= $3)
ORDER BY c.count_date DESC, b.name;

-- The previous day's closing figure, offered as the default opening so the two
-- ends of consecutive days are visibly linked rather than typed twice.
-- name: GetPreviousPettyCashClosing :one
SELECT count_date, closing_amount
FROM petty_cash_counts
WHERE branch_id = $1 AND count_date < $2 AND closing_amount IS NOT NULL
ORDER BY count_date DESC
LIMIT 1;

-- Which branches have counted which end of a given day. Feeds both the petty
-- cash board and the derived daily task.
-- name: ListPettyCashDayStatus :many
SELECT
    b.id AS branch_id, b.name AS branch_name,
    b.petty_cash_account_id,
    COALESCE(pa.balance, 0)::bigint AS ledger_balance,
    c.opening_amount, c.closing_amount, c.expected_closing, c.variance, c.variance_note
FROM branches b
LEFT JOIN accounts pa ON pa.id = b.petty_cash_account_id
LEFT JOIN petty_cash_counts c ON c.branch_id = b.id AND c.count_date = $1
ORDER BY b.name;
