DROP INDEX IF EXISTS idx_adjustments_transfer_id;
ALTER TABLE account_adjustments DROP COLUMN IF EXISTS transfer_id;
