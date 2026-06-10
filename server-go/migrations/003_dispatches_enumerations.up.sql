-- Make dispatches.division_id nullable (dispatch can go to a branch without a specific division)
ALTER TABLE dispatches ALTER COLUMN division_id DROP NOT NULL;

-- Add missing columns to enumerations
ALTER TABLE enumerations
    ADD COLUMN IF NOT EXISTS transferred_value BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS date DATE NOT NULL DEFAULT CURRENT_DATE;
