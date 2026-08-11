DROP TABLE IF EXISTS staff_kpis;
DROP TABLE IF EXISTS daily_task_completions;
DROP TABLE IF EXISTS daily_task_definitions;

DROP INDEX IF EXISTS idx_invoices_created_by;
ALTER TABLE invoices DROP COLUMN IF EXISTS created_by;
