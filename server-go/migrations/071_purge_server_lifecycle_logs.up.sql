-- One-time purge of server start/stop rows from the activity log.
--
-- A misconfigured deploy left the Go binary crash-looping on
-- 'bind: address already in use' for hundreds of restarts, and each restart
-- writes a 'start' row (service.LogSystemEvent, called from cmd/api/main.go).
-- The result buried every genuine entry — the audit trail is only useful if you
-- can see the user actions in it.
--
-- Scoped as tightly as the data allows: username, entity_type and action must
-- all match what LogSystemEvent writes, so no user-triggered row can be caught
-- by this. entity_id and user_id are NULL on these rows by construction, which
-- is asserted rather than assumed — a row carrying either is not one of ours.
--
-- Actions are lowercase because migration 048 normalised the log's casing;
-- service.ActionServerStart / ActionServerStop are 'start' / 'stop'.
DELETE FROM activity_log
WHERE username    = 'system'
  AND entity_type = 'system'
  AND action IN ('start', 'stop')
  AND user_id   IS NULL
  AND entity_id IS NULL;
