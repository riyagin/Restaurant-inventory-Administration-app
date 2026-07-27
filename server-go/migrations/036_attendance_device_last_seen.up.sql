-- Device liveness. The face/device dashboard needs to answer "is this kiosk still
-- talking to us, and when did it last sync?" — previously the only timestamp on a
-- device was created_at, so a kiosk that died a month ago looked identical to one
-- that synced a minute ago.
--
-- last_seen_at is stamped by the DeviceAuth middleware on every device-key request
-- (event push, roster sync, face enrollment, sync-diff), so it tracks any contact,
-- not just successful check-ins.
ALTER TABLE attendance_devices
  ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;
