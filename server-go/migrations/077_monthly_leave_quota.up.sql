-- Employees work seven days a week; their days off are paid leave drawn from a
-- monthly allowance (default 4) in which a Saturday or Sunday costs 2 days.
-- Replaces the annual 12-day quota for every uses_quota leave type.

-- 1. Seven-day week. A day off is now a leave request, not a gap in the schedule,
--    so every branch schedule — and the default for new ones — covers all seven.
ALTER TABLE work_schedules ALTER COLUMN work_days SET DEFAULT '{1,2,3,4,5,6,7}';
UPDATE work_schedules SET work_days = '{1,2,3,4,5,6,7}';

-- 2. One row per paid-leave day actually taken, with what it cost against the
--    month's allowance. Written on approval, so a month's usage is a SUM over
--    rows rather than a counter that can drift. Cancelling deletes only the rows
--    for days that have not happened yet: a past day off stays consumed, so
--    cancelling old leave cannot inflate the unused balance that evaluation
--    converts to cash.
CREATE TABLE leave_quota_usage (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  leave_request_id UUID NOT NULL REFERENCES leave_requests(id) ON DELETE CASCADE,
  employee_id      UUID NOT NULL REFERENCES employees(id),
  date             DATE NOT NULL,
  cost             INT  NOT NULL CHECK (cost > 0),
  UNIQUE (employee_id, date)
);

CREATE INDEX idx_leave_quota_usage_request ON leave_quota_usage (leave_request_id);

-- 3. Per-employee, per-month override of the default allowance. Absent row =
--    the default (service.DefaultMonthlyLeaveQuota).
CREATE TABLE leave_monthly_quotas (
  employee_id UUID NOT NULL REFERENCES employees(id),
  month       DATE NOT NULL CHECK (EXTRACT(DAY FROM month) = 1),
  quota_days  INT  NOT NULL CHECK (quota_days >= 0),
  PRIMARY KEY (employee_id, month)
);

-- 4. A request whose start date was already in the past when it was submitted.
--    Frozen at creation; such requests must be approved individually by a manager.
ALTER TABLE leave_requests ADD COLUMN is_backdated BOOLEAN NOT NULL DEFAULT false;

-- 5. Carry approved quota leave from the current month onward into the new
--    ledger, so this month's balance reflects leave already granted. Earlier
--    months were taken under the six-day annual rules and are not re-scored.
INSERT INTO leave_quota_usage (leave_request_id, employee_id, date, cost)
SELECT lr.id, lr.employee_id, d::date,
       CASE WHEN EXTRACT(ISODOW FROM d) IN (6, 7) THEN 2 ELSE 1 END
FROM leave_requests lr
JOIN leave_types lt ON lt.id = lr.leave_type_id
CROSS JOIN LATERAL generate_series(
  GREATEST(lr.start_date, date_trunc('month', CURRENT_DATE)::date),
  lr.end_date, interval '1 day') AS d
WHERE lr.status = 'approved'
  AND lt.uses_quota
  AND NOT EXISTS (SELECT 1 FROM public_holidays ph WHERE ph.date = d::date)
ON CONFLICT (employee_id, date) DO NOTHING;
