DROP INDEX IF EXISTS idx_operational_expenses_period;
ALTER TABLE operational_expenses DROP COLUMN IF EXISTS period_month;
