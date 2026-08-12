DROP TABLE IF EXISTS inventory_lot_consumptions;
-- Depleted lots are deliberately NOT re-deleted: FIFO ignores them anyway, and
-- destroying rows on a rollback loses more than it restores.
ALTER TABLE inventory DROP COLUMN IF EXISTS depleted_at;
