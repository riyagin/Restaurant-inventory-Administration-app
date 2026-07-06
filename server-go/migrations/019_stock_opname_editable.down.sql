ALTER TABLE stock_opname_items DROP COLUMN IF EXISTS is_correction;
ALTER TABLE stock_opname_items DROP COLUMN IF EXISTS created_at;
ALTER TABLE stock_opname DROP COLUMN IF EXISTS updated_at;
