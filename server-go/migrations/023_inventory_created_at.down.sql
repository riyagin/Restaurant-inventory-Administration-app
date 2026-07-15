DROP INDEX IF EXISTS idx_inventory_fifo;
ALTER TABLE inventory DROP COLUMN IF EXISTS created_at;
