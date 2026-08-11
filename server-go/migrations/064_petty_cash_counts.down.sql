DELETE FROM daily_task_definitions WHERE task_type = 'petty_cash';

ALTER TABLE daily_task_definitions DROP CONSTRAINT IF EXISTS daily_task_definitions_task_type_check;
ALTER TABLE daily_task_definitions ADD CONSTRAINT daily_task_definitions_task_type_check
  CHECK (task_type IN ('purchasing', 'pos_import', 'manual'));

DROP TABLE IF EXISTS petty_cash_counts;
