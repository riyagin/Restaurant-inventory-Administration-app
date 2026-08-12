-- Per-lot stock history.
--
-- "What happened to this particular delivery?" could not be answered before.
-- `stock_history` records that 5 kg left the warehouse, not which of the three
-- open lots it came out of, and FIFODeduct **deleted** a lot the moment it was
-- used up — so a finished lot left no trace at all and its usages were mixed in
-- with the deliveries either side of it.
--
-- Two changes fix that. Depleted lots are now kept and stamped rather than
-- deleted, and every deduction writes a row saying which lot it came from. A
-- lot's page is then simply: the purchase that opened it, its consumption rows,
-- and whatever quantity is still on the shelf.

-- A lot at quantity 0 that is still a row. GetInventoryLotsForFIFO already
-- filters `quantity > 0`, so FIFO is unaffected by their sticking around.
ALTER TABLE inventory ADD COLUMN IF NOT EXISTS depleted_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS inventory_lot_consumptions (
  id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  -- ON DELETE CASCADE, not SET NULL: a consumption row with no lot is a fact
  -- about nothing. Manual lot deletion is the only thing that still removes a
  -- lot, and it should take its usage log with it.
  lot_id  UUID NOT NULL REFERENCES inventory(id) ON DELETE CASCADE,
  item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,

  -- Base units taken out of this lot, and the FIFO value that left with them.
  quantity NUMERIC NOT NULL,
  value    BIGINT  NOT NULL DEFAULT 0,

  -- What used it. Optional: a few deduction paths are corrections rather than
  -- usage (an invoice being edited, a cancelled purchase unwinding) and have no
  -- meaningful "user". The row is still written, so the lot's arithmetic always
  -- closes; it simply reads as an adjustment.
  source_type TEXT,
  source_id   UUID,
  reference   TEXT NOT NULL DEFAULT '',

  date       DATE        NOT NULL DEFAULT CURRENT_DATE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lot_consumptions_lot ON inventory_lot_consumptions (lot_id, date);
CREATE INDEX IF NOT EXISTS idx_lot_consumptions_item ON inventory_lot_consumptions (item_id, warehouse_id, date);
CREATE INDEX IF NOT EXISTS idx_lot_consumptions_source ON inventory_lot_consumptions (source_type, source_id);

-- Existing lots that are already at zero are historical: they were depleted at
-- some unknown time before this migration. Stamping them with their own date
-- rather than now() keeps them from all bunching up on the deploy date.
UPDATE inventory SET depleted_at = date::timestamptz
WHERE quantity <= 0 AND depleted_at IS NULL;

-- Nothing backfills `inventory_lot_consumptions`. Lots consumed before this
-- migration were deleted outright, so the data to reconstruct them does not
-- exist — a lot page for an old delivery will honestly show the purchase and no
-- usage rather than a guess. Item-level history (`stock_history`) is unchanged
-- and still covers that period in full.
