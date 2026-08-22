-- The month a bill is *for*, which is not the date it was paid.
--
-- Electricity for July is settled some time in August, and the two dates answer
-- different questions: `date` is when the money moved and is what the ledger is
-- keyed on, while `period_month` is what makes "how much was July's electricity"
-- and "did we ever pay August's rent" answerable at all. Without it, a bill paid
-- late lands in the wrong month for every comparison anyone would want to make,
-- and a missed month is invisible because nothing records which months were
-- supposed to exist.
--
-- Stored as the first day of the month rather than as text: it sorts, it ranges,
-- and date_trunc makes the normalisation a one-liner on the way in. The handler
-- accepts "YYYY-MM" and truncates.
ALTER TABLE operational_expenses ADD COLUMN IF NOT EXISTS period_month DATE;

-- Rows booked before this column existed carry no separate period, and the only
-- honest reading of them is that the bill was for the month it was paid in.
UPDATE operational_expenses SET period_month = date_trunc('month', date)::date
WHERE period_month IS NULL;

ALTER TABLE operational_expenses ALTER COLUMN period_month SET NOT NULL;

-- A guard, not a constraint: the same bill paid twice in the same month is a
-- real possibility (two meters, a corrected re-entry), so this is deliberately
-- not UNIQUE. The index is here because the input form looks up "what has this
-- branch already recorded for this category and month" on every submission, and
-- the month report groups by exactly this key.
CREATE INDEX IF NOT EXISTS idx_operational_expenses_period
  ON operational_expenses (branch_id, category_id, period_month);
