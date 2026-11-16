DELETE FROM performance_violations
WHERE policy_id IN (SELECT id FROM performance_policies WHERE rule_type IN ('unpaid_leave', 'sick_no_note'));
DELETE FROM performance_policies WHERE rule_type IN ('unpaid_leave', 'sick_no_note');

ALTER TABLE performance_policies DROP CONSTRAINT performance_policies_rule_type_check;
ALTER TABLE performance_policies
  ADD CONSTRAINT performance_policies_rule_type_check
  CHECK (rule_type IN ('late', 'early_leave', 'missing_checkout', 'missing_checkin',
                       'no_punch', 'absent_no_leave', 'consecutive_absent',
                       'half_day_late', 'half_day_early', 'manual'));

ALTER TABLE payroll_lines DROP COLUMN public_holiday_rate;
ALTER TABLE leave_requests DROP COLUMN has_doctor_note;
ALTER TABLE leave_types DROP COLUMN requires_doctor_note;
ALTER TABLE kasbons DROP COLUMN approval_track, DROP COLUMN score_at_request;
DROP TABLE hr_policy_settings;
DROP TABLE performance_grade_component_percents;
