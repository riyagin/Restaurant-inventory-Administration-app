DROP TABLE IF EXISTS fingerprint_imports;

DROP INDEX IF EXISTS idx_attendance_records_status;
DROP INDEX IF EXISTS idx_attendance_records_emp_date;
DROP INDEX IF EXISTS idx_attendance_records_date;
DROP TABLE IF EXISTS attendance_records;

DROP INDEX IF EXISTS idx_attendance_devices_api_key_hash;
DROP TABLE IF EXISTS attendance_devices;

DROP TABLE IF EXISTS public_holidays;

DROP TABLE IF EXISTS work_schedules;
