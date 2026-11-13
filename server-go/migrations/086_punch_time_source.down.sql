DROP INDEX IF EXISTS idx_attendance_punches_client_event;

ALTER TABLE attendance_punches
  DROP COLUMN IF EXISTS client_event_id,
  DROP COLUMN IF EXISTS device_reported_at,
  DROP COLUMN IF EXISTS time_source;
