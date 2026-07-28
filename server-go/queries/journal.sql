-- name: CreateJournalEntry :one
INSERT INTO journal_entries (entry_date, source_type, source_id, description, created_by, reverses_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, entry_date, source_type, source_id, description, created_by, created_at, reverses_id;

-- name: InsertJournalLine :exec
INSERT INTO journal_lines (entry_id, account_id, amount, memo)
VALUES ($1, $2, $3, $4);

-- name: ListAccountTypes :many
SELECT id, account_type FROM accounts WHERE id = ANY($1::uuid[]);

-- name: ListJournalEntriesBySource :many
SELECT id, entry_date, source_type, source_id, description, created_by, created_at, reverses_id
FROM journal_entries
WHERE source_type = $1 AND source_id = $2
ORDER BY created_at;

-- name: ListJournalLinesForEntry :many
SELECT jl.id, jl.entry_id, jl.account_id, jl.amount, jl.memo, a.name AS account_name, a.account_number
FROM journal_lines jl
JOIN accounts a ON a.id = jl.account_id
WHERE jl.entry_id = $1
ORDER BY jl.amount DESC;

-- name: ListAccountLedger :many
SELECT jl.id, jl.amount, jl.memo,
       je.id AS entry_id, je.entry_date, je.source_type, je.source_id, je.description
FROM journal_lines jl
JOIN journal_entries je ON je.id = jl.entry_id
WHERE jl.account_id = $1
  AND ($2::date IS NULL OR je.entry_date >= $2)
  AND ($3::date IS NULL OR je.entry_date <= $3)
ORDER BY je.entry_date DESC, je.created_at DESC
LIMIT $4 OFFSET $5;

-- name: TrialBalance :many
-- Cached balance vs the balance implied by the journal, per account. `drift` is
-- non-zero only where something wrote accounts.balance outside service.Post, or
-- for history that predates the journal.
SELECT a.id, a.account_number, a.name, a.account_type, a.balance AS cached_balance,
       COALESCE(SUM(
         CASE WHEN a.account_type IN ('asset', 'expense') THEN jl.amount ELSE -jl.amount END
       ), 0)::BIGINT AS journal_balance,
       a.balance - COALESCE(SUM(
         CASE WHEN a.account_type IN ('asset', 'expense') THEN jl.amount ELSE -jl.amount END
       ), 0)::BIGINT AS drift
FROM accounts a
LEFT JOIN journal_lines jl ON jl.account_id = a.id
GROUP BY a.id, a.account_number, a.name, a.account_type, a.balance
ORDER BY a.account_number NULLS LAST, a.name;

-- name: AccountingEquation :one
-- The single number that says whether the books balance: assets minus
-- (liabilities + equity + revenue - expense). Zero is the only correct value.
SELECT
  COALESCE(SUM(balance) FILTER (WHERE account_type = 'asset'), 0)::BIGINT     AS assets,
  COALESCE(SUM(balance) FILTER (WHERE account_type = 'liability'), 0)::BIGINT AS liabilities,
  COALESCE(SUM(balance) FILTER (WHERE account_type = 'equity'), 0)::BIGINT    AS equity,
  COALESCE(SUM(balance) FILTER (WHERE account_type = 'revenue'), 0)::BIGINT   AS revenue,
  COALESCE(SUM(balance) FILTER (WHERE account_type = 'expense'), 0)::BIGINT   AS expense,
  (COALESCE(SUM(balance) FILTER (WHERE account_type = 'asset'), 0)
   - COALESCE(SUM(balance) FILTER (WHERE account_type = 'liability'), 0)
   - COALESCE(SUM(balance) FILTER (WHERE account_type = 'equity'), 0)
   - COALESCE(SUM(balance) FILTER (WHERE account_type = 'revenue'), 0)
   + COALESCE(SUM(balance) FILTER (WHERE account_type = 'expense'), 0))::BIGINT AS difference
FROM accounts;
