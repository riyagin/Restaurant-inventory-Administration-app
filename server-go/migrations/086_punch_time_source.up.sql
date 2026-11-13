-- Attendance punches: whose clock the time came from.
--
-- The Android kiosk used to send its own wall-clock time and the server stored
-- it as-is, so changing the tablet's clock moved a check-in. Face and manual
-- punches from the kiosk are now timed by the SERVER:
--
--   server  — stamped with the server clock when the event arrived (the normal,
--             online case).
--   queued  — the event was captured offline and sent later. The device sends
--             how long ago it was captured, measured on its monotonic clock
--             (elapsed time since boot, which changing the wall clock does not
--             affect), and the server subtracts that from its receive time.
--   device  — the device's own wall clock, which the server cannot verify: an
--             offline event whose device rebooted before it could be sent, a
--             legacy app build, and every fingerprint-file import (the scanner's
--             clock is all that file carries). Existing rows are this.
--
-- device_reported_at keeps what the device clock claimed for server/queued
-- punches, so a tampered tablet clock is visible after the fact.
--
-- client_event_id is the app's id for one captured event. A retry after a lost
-- response re-sends the same id and is recognised instead of logging a second
-- punch.

ALTER TABLE attendance_punches
  ADD COLUMN time_source TEXT NOT NULL DEFAULT 'device'
    CHECK (time_source IN ('server', 'queued', 'device')),
  ADD COLUMN device_reported_at TIMESTAMPTZ,
  ADD COLUMN client_event_id UUID;

CREATE UNIQUE INDEX idx_attendance_punches_client_event
  ON attendance_punches (client_event_id)
  WHERE client_event_id IS NOT NULL;
