-- The POS settlement layer, one row per payment method. Cash is the one that
-- gets counted; the rest are tracked so a branch's day adds up to its takings
-- and an EDC or platform settlement can be chased when it does not arrive.
-- name: GetPOSSettlementForDay :many
SELECT branch_id, account_id, account_name, is_cash_drawer, amount
FROM pos_settlement_by_branch
WHERE date = $1
ORDER BY is_cash_drawer DESC, account_name;

-- name: GetPOSSettlementRange :many
SELECT branch_id, date, account_id, account_name, is_cash_drawer, amount
FROM pos_settlement_by_branch
WHERE date BETWEEN $1 AND $2
  AND ($3::uuid IS NULL OR branch_id = $3)
ORDER BY date DESC, is_cash_drawer DESC, account_name;

-- Cash paid straight out of a drawer account on a given day: an invoice settled
-- in notes rather than by transfer. Pembelanjaan Harian is deliberately absent —
-- it comes out of Kas Kecil, which is its own reconciliation.
--
-- Matched on `payment_date`, not on the invoice date: the till was emptied on
-- the day the notes were handed over, and an invoice dated last Friday but
-- settled today belongs to today's count. Falls back to the invoice date for
-- rows predating that column.
-- name: SumCashInvoicesForBranchDay :one
SELECT COALESCE(SUM(i.amount_paid), 0)::bigint AS total
FROM invoices i
JOIN accounts a ON a.id = i.account_id
WHERE a.is_cash_drawer
  AND i.branch_id = $1
  AND COALESCE(i.payment_date, i.date) = $2
  AND i.payment_status IN ('paid', 'partial');

-- name: ListCashDrawerAccounts :many
SELECT id, name, account_number, balance
FROM accounts
WHERE is_cash_drawer
ORDER BY account_number NULLS LAST, name;

-- name: SetAccountCashDrawer :exec
UPDATE accounts SET is_cash_drawer = $1 WHERE id = $2;

-- Movements through every drawer account on a day, for one branch's setoran.
-- name: SumCashDrawerMovementsForDay :one
SELECT
    COALESCE(SUM(cd.amount) FILTER (WHERE ta.is_cash_drawer), 0)::bigint AS cash_in,
    COALESCE(SUM(cd.amount) FILTER (WHERE fa.is_cash_drawer), 0)::bigint AS cash_out
FROM cash_deposits cd
LEFT JOIN accounts fa ON fa.id = cd.from_account_id
LEFT JOIN accounts ta ON ta.id = cd.to_account_id
WHERE cd.date = $2 AND cd.status = 'posted'
  AND cd.branch_id = $1
  AND (fa.is_cash_drawer OR ta.is_cash_drawer);

-- ── Counts ─────────────────────────────────────────────────────────────────
-- name: GetCashDayCount :one
SELECT id, branch_id, count_date, opening_amount, opening_by, opening_at,
       closing_amount, closing_by, closing_at, expected_closing, variance,
       variance_note, cash_sales, cash_in, cash_out, cash_expenses, created_at
FROM cash_day_counts
WHERE branch_id = $1 AND count_date = $2;

-- Re-recording an opening is a correction of a miscount, so it overwrites — and
-- clears the closing with it, because a variance measured against the old
-- opening no longer means anything.
-- name: UpsertCashDayOpening :one
INSERT INTO cash_day_counts (id, branch_id, count_date, opening_amount, opening_by)
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
          variance_note, cash_sales, cash_in, cash_out, cash_expenses, created_at;

-- name: SetCashDayClosing :one
UPDATE cash_day_counts
SET closing_amount = $1, closing_by = $2, closing_at = now(),
    expected_closing = $3, variance = $4, variance_note = $5,
    cash_sales = $6, cash_in = $7, cash_out = $8, cash_expenses = $9
WHERE branch_id = $10 AND count_date = $11
RETURNING id, branch_id, count_date, opening_amount, opening_by, opening_at,
          closing_amount, closing_by, closing_at, expected_closing, variance,
          variance_note, cash_sales, cash_in, cash_out, cash_expenses, created_at;

-- name: ListCashDayCounts :many
SELECT
    c.id, c.branch_id, b.name AS branch_name, c.count_date,
    c.opening_amount, c.closing_amount, c.expected_closing, c.variance, c.variance_note,
    c.cash_sales, c.cash_in, c.cash_out, c.cash_expenses,
    ou.username AS opening_by_name, cu.username AS closing_by_name
FROM cash_day_counts c
JOIN branches b    ON b.id = c.branch_id
LEFT JOIN users ou ON ou.id = c.opening_by
LEFT JOIN users cu ON cu.id = c.closing_by
WHERE ($1::uuid IS NULL OR c.branch_id = $1)
  AND ($2::date IS NULL OR c.count_date >= $2)
  AND ($3::date IS NULL OR c.count_date <= $3)
ORDER BY c.count_date DESC, b.name;

-- The previous day's closing, offered as this morning's default so the two ends
-- of consecutive days are linked rather than typed twice.
-- name: GetPreviousCashDayClosing :one
SELECT count_date, closing_amount
FROM cash_day_counts
WHERE branch_id = $1 AND count_date < $2 AND closing_amount IS NOT NULL
ORDER BY count_date DESC
LIMIT 1;

-- name: ListCashDayStatus :many
SELECT
    b.id AS branch_id, b.name AS branch_name,
    c.opening_amount, c.closing_amount, c.expected_closing, c.variance, c.variance_note
FROM branches b
LEFT JOIN cash_day_counts c ON c.branch_id = b.id AND c.count_date = $1
ORDER BY b.name;
