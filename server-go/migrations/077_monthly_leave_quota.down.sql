-- Schedules are not restored to their previous per-branch work days (they were
-- overwritten); only the column default returns to Mon-Sat.
ALTER TABLE leave_requests DROP COLUMN IF EXISTS is_backdated;
DROP TABLE IF EXISTS leave_monthly_quotas;
DROP TABLE IF EXISTS leave_quota_usage;
ALTER TABLE work_schedules ALTER COLUMN work_days SET DEFAULT '{1,2,3,4,5,6}';
