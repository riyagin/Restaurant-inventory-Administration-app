-- Deliberately empty.
--
-- The up migration deletes rows; there is nothing to restore them from. Rolling
-- back to 070 is still legal — it simply leaves the log purged. Recovery, if it
-- is ever wanted, comes from a pg_dump taken before the migration ran
-- (deploy/backup.sh), not from here.
SELECT 1;
