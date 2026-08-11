-- Some duties cannot be done on the day they belong to.
--
-- POS sales for a given day only arrive the following morning, so "import POS
-- for 2 August" is not work anyone can do on 2 August — it becomes actionable on
-- 3 August. Without this the board naggs about a task that is impossible, and
-- the completion rate counts a day that was never anyone's to miss.
--
-- due_offset_days is how many days after the task's own date the work becomes
-- actionable. It is distinct from grace_days, which is the slack allowed *after*
-- that point before the task is called overdue:
--
--   actionable from  task_date + due_offset_days
--   overdue when     today > task_date + due_offset_days + grace_days
--
-- Instances that are not yet actionable are not shown at all.

ALTER TABLE daily_task_definitions
  ADD COLUMN IF NOT EXISTS due_offset_days INT NOT NULL DEFAULT 0 CHECK (due_offset_days >= 0);

-- POS data is always a day behind.
UPDATE daily_task_definitions SET due_offset_days = 1 WHERE task_type = 'pos_import';
