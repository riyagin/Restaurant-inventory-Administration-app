-- A punch on a day the employee is on approved leave.
--
-- Previously the punch silently turned the 'leave' record into 'present', so the
-- paid day off was scored as late / early with a pay cut and the leave quietly
-- disappeared. The record now keeps status 'leave' and its punches, and this flag
-- marks it as an anomaly for HR to review during evaluation. It carries no points
-- and no pay cut by itself.
ALTER TABLE attendance_records
  ADD COLUMN is_leave_punch BOOLEAN NOT NULL DEFAULT false;

-- Leave records that already hold a punch (approval does not overwrite a day that
-- has a check-in, so these can only come from a punch on the day itself).
UPDATE attendance_records
SET is_leave_punch = true
WHERE status = 'leave' AND (check_in IS NOT NULL OR check_out IS NOT NULL);
