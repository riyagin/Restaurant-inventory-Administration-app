DROP TABLE IF EXISTS invoice_item_allocations;

ALTER TABLE invoices DROP COLUMN IF EXISTS is_distributed;
