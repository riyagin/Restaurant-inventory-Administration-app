-- Track dispatch lifecycle so a dispatch can be edited or cancelled while
-- keeping its record. 'active' = normal, 'cancelled' = reversed (kept for audit).
ALTER TABLE dispatches ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE dispatches ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;
