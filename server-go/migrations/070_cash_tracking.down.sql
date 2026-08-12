DROP VIEW IF EXISTS pos_settlement_by_branch;
DROP TABLE IF EXISTS cash_day_counts;
ALTER TABLE accounts DROP COLUMN IF EXISTS is_cash_drawer;
