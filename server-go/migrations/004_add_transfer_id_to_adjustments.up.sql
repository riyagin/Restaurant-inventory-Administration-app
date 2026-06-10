ALTER TABLE account_adjustments ADD COLUMN IF NOT EXISTS transfer_id UUID;
CREATE INDEX IF NOT EXISTS idx_adjustments_transfer_id ON account_adjustments(transfer_id);
