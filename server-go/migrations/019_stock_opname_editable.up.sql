-- Make stock opname sessions editable. Following the accounting-edit
-- convention, an edit never rewrites the already-posted rows: each correction
-- is appended as a NEW stock_opname_items row (is_correction = true) with its
-- own stock_history row and balance moves. Track when a session was last edited.
ALTER TABLE stock_opname ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;
ALTER TABLE stock_opname_items ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE stock_opname_items ADD COLUMN IF NOT EXISTS is_correction BOOLEAN NOT NULL DEFAULT false;
