-- Add a monotonic insertion timestamp so FIFO deduction can break ties between
-- lots received on the same calendar date. Without this, lots sharing a `date`
-- were ordered by their random gen_random_uuid() id, letting a newer lot be
-- consumed before an older one.
ALTER TABLE inventory ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Backfill existing rows to their date (midnight) so ordering stays date-correct.
-- True intra-day insertion order cannot be recovered for historical rows.
UPDATE inventory SET created_at = date::timestamptz;

CREATE INDEX idx_inventory_fifo ON inventory (item_id, warehouse_id, date, created_at, id);
