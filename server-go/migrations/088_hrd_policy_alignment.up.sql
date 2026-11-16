-- 088: align the HR rules with Kebijakan HRD AA (HRD-AA-001 v1.1, berlaku
-- 13 November 2026).
--
-- 1. Per-component tier percentages. The grade scale (087) paid ONE percentage
--    of every grade-linked component. The policy needs different shares per
--    component in the same tier: Tier B pays Service Charge at 50% but Uang
--    Kerajinan and Insentif Jabatan at 0%. A row here overrides the tier's
--    default percentage for one component; no row = the tier's own percentage.
--    Rows hang off the grade, so replacing the scale replaces them too.
CREATE TABLE performance_grade_component_percents (
  grade_id          UUID NOT NULL REFERENCES performance_grades(id) ON DELETE CASCADE,
  wage_component_id UUID NOT NULL REFERENCES wage_components(id) ON DELETE CASCADE,
  pay_percent       INT  NOT NULL CHECK (pay_percent BETWEEN 0 AND 100),
  PRIMARY KEY (grade_id, wage_component_id)
);

-- 2. Policy settings (singleton). Kept apart from hr_settings, which is
--    company/document data.
--    payroll_cutoff_day: a period for month M runs from the day after the
--      cut-off in M-1 to the cut-off day in M (policy: 26 → 25). 0 = calendar
--      month (the previous behaviour).
--    kasbon_*: the Kebijakan Kasbon conditions. kasbon_max_amount 0 = no cap.
CREATE TABLE hr_policy_settings (
  id                      INT         PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  payroll_cutoff_day      INT         NOT NULL DEFAULT 25 CHECK (payroll_cutoff_day BETWEEN 0 AND 28),
  kasbon_window_start_day INT         NOT NULL DEFAULT 10 CHECK (kasbon_window_start_day BETWEEN 1 AND 31),
  kasbon_window_end_day   INT         NOT NULL DEFAULT 20 CHECK (kasbon_window_end_day BETWEEN 1 AND 31),
  kasbon_max_amount       BIGINT      NOT NULL DEFAULT 500000 CHECK (kasbon_max_amount >= 0),
  kasbon_max_installments INT         NOT NULL DEFAULT 2 CHECK (kasbon_max_installments BETWEEN 1 AND 12),
  kasbon_permanent_only   BOOLEAN     NOT NULL DEFAULT true,
  kasbon_min_score        INT         NOT NULL DEFAULT 70 CHECK (kasbon_min_score BETWEEN 0 AND 100),
  kasbon_priority_score   INT         NOT NULL DEFAULT 90 CHECK (kasbon_priority_score BETWEEN 0 AND 100),
  updated_by              UUID        REFERENCES users(id) ON DELETE SET NULL,
  updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (kasbon_window_start_day <= kasbon_window_end_day),
  CHECK (kasbon_min_score <= kasbon_priority_score)
);
INSERT INTO hr_policy_settings (id) VALUES (1);

-- 3. Kasbon: the score the request was judged on and the track it took
--    (priority ≥ 90, evaluation 70–89). Frozen at request time.
ALTER TABLE kasbons
  ADD COLUMN score_at_request INT,
  ADD COLUMN approval_track   TEXT CHECK (approval_track IN ('priority', 'evaluation'));

-- 4. Sick leave without a doctor's note costs points and a day's pay.
ALTER TABLE leave_types
  ADD COLUMN requires_doctor_note BOOLEAN NOT NULL DEFAULT false;
UPDATE leave_types SET requires_doctor_note = true WHERE name = 'Sakit';

ALTER TABLE leave_requests
  ADD COLUMN has_doctor_note BOOLEAN NOT NULL DEFAULT false;
-- Sick leave filed before this rule existed is not penalised retroactively.
UPDATE leave_requests lr SET has_doctor_note = true
FROM leave_types lt
WHERE lt.id = lr.leave_type_id AND lt.requires_doctor_note;

-- 5. Insentif Libur Nasional is 2× the daily uang makan, not 2× the daily
--    wage. The per-day rate is frozen on the line like the hourly overtime
--    rate; lines generated before this keep pricing at their daily rate.
ALTER TABLE payroll_lines
  ADD COLUMN public_holiday_rate BIGINT;
UPDATE payroll_lines SET public_holiday_rate = daily_rate;
ALTER TABLE payroll_lines
  ALTER COLUMN public_holiday_rate SET NOT NULL,
  ALTER COLUMN public_holiday_rate SET DEFAULT 0;

-- 6. Policy rules: 'unpaid_leave' (Izin Satu Hari) and 'sick_no_note'.
ALTER TABLE performance_policies DROP CONSTRAINT performance_policies_rule_type_check;
ALTER TABLE performance_policies
  ADD CONSTRAINT performance_policies_rule_type_check
  CHECK (rule_type IN ('late', 'early_leave', 'missing_checkout', 'missing_checkin',
                       'no_punch', 'absent_no_leave', 'consecutive_absent',
                       'half_day_late', 'half_day_early', 'unpaid_leave',
                       'sick_no_note', 'manual'));

-- Late tiers per the Tabel Pengurangan Poin (11–20 → 3, 21–30 → 4). Only rows
-- still holding the previous value are touched, so an HR edit made since is
-- left alone (same guard as 083).
UPDATE performance_policies SET points = 3
WHERE rule_type = 'late' AND threshold_minutes = 11 AND points = 4;
UPDATE performance_policies SET points = 4
WHERE rule_type = 'late' AND threshold_minutes = 21 AND points = 8;

INSERT INTO performance_policies (name, rule_type, threshold_minutes, points, is_active)
SELECT 'Izin Satu Hari', 'unpaid_leave', NULL, 8, true
WHERE NOT EXISTS (SELECT 1 FROM performance_policies WHERE rule_type = 'unpaid_leave');

INSERT INTO performance_policies (name, rule_type, threshold_minutes, points, is_active)
SELECT 'Izin sakit tanpa surat keterangan dokter', 'sick_no_note', NULL, 8, true
WHERE NOT EXISTS (SELECT 1 FROM performance_policies WHERE rule_type = 'sick_no_note');

INSERT INTO performance_policies (name, rule_type, threshold_minutes, points, is_active)
SELECT 'Tidak check-in tanpa konfirmasi', 'missing_checkin', NULL, 10, true
WHERE NOT EXISTS (SELECT 1 FROM performance_policies WHERE rule_type = 'missing_checkin' AND is_active);

INSERT INTO performance_policies (name, rule_type, threshold_minutes, points, is_active)
SELECT 'Meninggalkan tempat kerja pada jam kerja tanpa izin', 'manual', NULL, 10, true
WHERE NOT EXISTS (
  SELECT 1 FROM performance_policies
  WHERE rule_type = 'manual' AND name ILIKE '%tanpa izin%'
    AND (name ILIKE '%meninggalkan%' OR name ILIKE '%keluar%'));
