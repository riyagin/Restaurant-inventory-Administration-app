-- HR Attendance: schedules, holidays, devices, records, and fingerprint imports.
-- Attendance has two sources: facial recognition (primary, pushed by the Android
-- app via a device key) and fingerprint scanner exports (backup, imported by an
-- admin). A nightly reconciliation job inserts 'absent' rows for scheduled work
-- days with no record.

-- Per-branch work schedule + anomaly thresholds.
CREATE TABLE work_schedules (
  id                  UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id           UUID    NOT NULL UNIQUE REFERENCES branches(id),
  work_start          TIME    NOT NULL DEFAULT '08:00',
  work_end            TIME    NOT NULL DEFAULT '17:00',
  grace_minutes       INT     NOT NULL DEFAULT 15,         -- late beyond this = anomaly
  early_leave_minutes INT     NOT NULL DEFAULT 30,         -- leaving earlier than work_end - this = anomaly
  work_days           INT[]   NOT NULL DEFAULT '{1,2,3,4,5,6}'  -- ISO weekday numbers (1=Mon..7=Sun)
);

-- National / company-wide holidays (no work, no anomaly).
CREATE TABLE public_holidays (
  id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  date DATE NOT NULL UNIQUE,
  name TEXT NOT NULL
);

-- Android devices / scanners allowed to push attendance events.
-- Only the SHA-256 hex hash of the API key is stored; the raw key is shown once
-- on creation and never again.
CREATE TABLE attendance_devices (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name         TEXT        NOT NULL,
  branch_id    UUID        REFERENCES branches(id),
  api_key_hash TEXT        NOT NULL,
  is_active    BOOLEAN     NOT NULL DEFAULT true,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_attendance_devices_api_key_hash ON attendance_devices (api_key_hash);

-- One attendance record per employee per day.
CREATE TABLE attendance_records (
  id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id         UUID        NOT NULL REFERENCES employees(id),
  date                DATE        NOT NULL,
  check_in            TIMESTAMPTZ,
  check_out           TIMESTAMPTZ,
  check_in_source     TEXT        CHECK (check_in_source IN ('face', 'fingerprint', 'manual')),
  check_out_source    TEXT        CHECK (check_out_source IN ('face', 'fingerprint', 'manual')),
  check_in_photo_path TEXT,                                  -- face check-in evidence
  device_id           UUID        REFERENCES attendance_devices(id),
  status              TEXT        NOT NULL DEFAULT 'present'
                        CHECK (status IN ('present', 'absent', 'leave', 'holiday')),
  is_late             BOOLEAN     NOT NULL DEFAULT false,
  late_minutes        INT         NOT NULL DEFAULT 0,
  is_early_leave      BOOLEAN     NOT NULL DEFAULT false,
  early_leave_minutes INT         NOT NULL DEFAULT 0,
  is_missing_checkout BOOLEAN     NOT NULL DEFAULT false,
  note                TEXT,
  UNIQUE (employee_id, date)
);

CREATE INDEX idx_attendance_records_date        ON attendance_records (date);
CREATE INDEX idx_attendance_records_emp_date    ON attendance_records (employee_id, date);
CREATE INDEX idx_attendance_records_status      ON attendance_records (status);

-- Fingerprint import batch header.
CREATE TABLE fingerprint_imports (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  filename      TEXT,
  imported_by   UUID        REFERENCES users(id),
  row_count     INT,
  matched_count INT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
