-- Track stock-transfer lifecycle so a transfer group can be edited or cancelled
-- while keeping its record. 'active' = normal, 'cancelled' = reversed (kept for
-- audit). Mirrors the dispatch status column added in migration 018.
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;
