-- Attendance: raw punch log, shift-window day assignment, and attendance permits.
--
-- 1. attendance_punches keeps EVERY punch a device sends (fingerprint import,
--    face/kiosk event, manager-assisted visitor entry). attendance_records keeps
--    the day's derived check-in / check-out, rebuilt from this log, so a
--    mis-classified day can always be re-derived and HR can see exactly what the
--    device recorded. work_date is the work day the punch was assigned to by the
--    employee's shift window (a 00:20 check-out belongs to the previous day).
--
-- 2. attendance_permissions are "izin" requests for part of a day: arriving late
--    (Izin Masuk Siang), leaving early (Izin Pulang Awal) or working off site
--    (Dinas Luar). An approved permit is applied when the day is rebuilt: a late
--    arrival / early departure becomes a half day, off-site removes the
--    late/early/missing flags (and turns a punch-less day into present).
--
-- 3. attendance_records.excused_by records which permit shaped the day, so a
--    permit-made half day can be undone when the permit is cancelled without
--    touching a half day a manager set by hand. review_resolved marks a flagged
--    day (very late / very early without a permit) as reviewed.
--
-- 4. hr_settings gets the review thresholds and whether a permit may be granted
--    after the day (marked backdated, like leave).

CREATE TABLE attendance_punches (
  id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id    UUID        NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
  punched_at     TIMESTAMPTZ NOT NULL,
  work_date      DATE        NOT NULL,
  source         TEXT        NOT NULL CHECK (source IN ('fingerprint', 'face', 'manual')),
  direction_hint TEXT        CHECK (direction_hint IN ('check_in', 'check_out')),
  device_id      UUID        REFERENCES attendance_devices(id) ON DELETE SET NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (employee_id, punched_at, source)
);
CREATE INDEX idx_attendance_punches_emp_day ON attendance_punches (employee_id, work_date);

-- Seed the log with the punches already summarised in attendance_records, so a
-- rebuild of an existing day reproduces it. Manual values are corrections, not
-- punches: they stay on the record as overrides and are not logged.
INSERT INTO attendance_punches (employee_id, punched_at, work_date, source, direction_hint, device_id)
SELECT employee_id, check_in, date, check_in_source, 'check_in', device_id
FROM attendance_records
WHERE check_in IS NOT NULL AND check_in_source IN ('fingerprint', 'face')
ON CONFLICT DO NOTHING;
INSERT INTO attendance_punches (employee_id, punched_at, work_date, source, direction_hint, device_id)
SELECT employee_id, check_out, date, check_out_source, 'check_out', device_id
FROM attendance_records
WHERE check_out IS NOT NULL AND check_out_source IN ('fingerprint', 'face')
ON CONFLICT DO NOTHING;

CREATE TABLE attendance_permissions (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id   UUID        NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
  date          DATE        NOT NULL,
  type          TEXT        NOT NULL CHECK (type IN ('late_arrival', 'early_departure', 'off_site')),
  reason        TEXT        NOT NULL,
  status        TEXT        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
  is_backdated  BOOLEAN     NOT NULL DEFAULT false,
  created_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  decided_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
  decided_at    TIMESTAMPTZ,
  decision_note TEXT
);
CREATE INDEX idx_attendance_permissions_emp_date ON attendance_permissions (employee_id, date);
CREATE INDEX idx_attendance_permissions_status ON attendance_permissions (status);
-- One live permit per employee, day and type.
CREATE UNIQUE INDEX uq_attendance_permissions_live
  ON attendance_permissions (employee_id, date, type)
  WHERE status IN ('pending', 'approved');

ALTER TABLE attendance_records
  ADD COLUMN excused_by TEXT CHECK (excused_by IN ('late_arrival', 'early_departure', 'off_site')),
  ADD COLUMN review_resolved BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE hr_settings
  ADD COLUMN review_late_minutes  INTEGER NOT NULL DEFAULT 120 CHECK (review_late_minutes >= 0),
  ADD COLUMN review_early_minutes INTEGER NOT NULL DEFAULT 120 CHECK (review_early_minutes >= 0),
  ADD COLUMN allow_backdated_permission BOOLEAN NOT NULL DEFAULT true;
