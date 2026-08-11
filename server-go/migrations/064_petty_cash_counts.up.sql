-- The daily petty cash count.
--
-- Twice a day someone opens the box and counts what is in it: once before
-- trading, once after. Those two numbers bracket the day, and the difference
-- between them has to equal what the system says moved — top-ups in, Pembelanjaan
-- Harian out. When it doesn't, the gap is real money and the point of recording
-- both ends is to surface it the same day, while anyone still remembers.
--
-- Nothing here posts to the ledger. A count is an observation of a physical box,
-- not a financial event; treating a miscount as a journal entry would let a
-- typo rewrite the books. The variance is recorded, flagged and explained, and
-- correcting it stays a deliberate act on the Setoran or adjustment screen.

CREATE TABLE IF NOT EXISTS petty_cash_counts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id  UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  count_date DATE NOT NULL,

  -- Opening is required to create the row; closing arrives later the same day,
  -- so the day sits half-recorded in between and the board can say which half.
  opening_amount BIGINT      NOT NULL CHECK (opening_amount >= 0),
  opening_by     UUID        REFERENCES users(id) ON DELETE SET NULL,
  opening_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

  closing_amount BIGINT      CHECK (closing_amount IS NULL OR closing_amount >= 0),
  closing_by     UUID        REFERENCES users(id) ON DELETE SET NULL,
  closing_at     TIMESTAMPTZ,

  -- What the box should have held at close, frozen at the moment it was counted:
  -- opening + top-ups - spending, as the data stood then. Recomputing it on
  -- every read would let a spend backdated into a closed day quietly change a
  -- variance someone has already signed off on.
  expected_closing BIGINT,
  variance         BIGINT,
  variance_note    TEXT NOT NULL DEFAULT '',

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  -- A note is only meaningful against a variance, and a variance without one is
  -- exactly what this feature exists to prevent.
  CONSTRAINT petty_cash_variance_needs_note
    CHECK (variance IS NULL OR variance = 0 OR length(trim(variance_note)) > 0)
);

-- One count per branch per day: the second one is a correction of the first,
-- not a new observation.
CREATE UNIQUE INDEX IF NOT EXISTS idx_petty_cash_counts_branch_date
  ON petty_cash_counts (branch_id, count_date);

-- ── The count as a daily duty ──────────────────────────────────────────────
-- Same shape as the existing back-office duties: one instance per branch per
-- day, and completion is derived from whether both ends were counted rather
-- than ticked off by hand.
ALTER TABLE daily_task_definitions DROP CONSTRAINT IF EXISTS daily_task_definitions_task_type_check;
ALTER TABLE daily_task_definitions ADD CONSTRAINT daily_task_definitions_task_type_check
  CHECK (task_type IN ('purchasing', 'pos_import', 'manual', 'petty_cash'));

-- starts_on = today, unlike the seeded duties. Nobody should log in to a wall of
-- failures for a rule that did not exist yesterday, and there is no history to
-- score: the table is empty by construction.
INSERT INTO daily_task_definitions (name, description, task_type, scope, target_role, link_path, starts_on, sort_order)
SELECT 'Hitung Kas Kecil', 'Catat saldo kas kecil awal dan akhir hari untuk setiap cabang.',
       'petty_cash', 'per_branch', 'admin', '/petty-cash', CURRENT_DATE, 30
WHERE NOT EXISTS (SELECT 1 FROM daily_task_definitions WHERE task_type = 'petty_cash');
