ALTER TABLE invoice_items  DROP COLUMN IF EXISTS conversion_factor;
ALTER TABLE dispatch_items DROP COLUMN IF EXISTS conversion_factor;
