-- Add the 'superuser' role.
--
-- superuser passes every gate in middleware/auth.go, including RequireManager
-- (approvals), which no single other role holds. It exists for the owner of the
-- system, who otherwise has to keep two accounts to both run the business and
-- approve requests.
--
-- No superuser is seeded: an existing deployment already has an admin, and that
-- admin promotes whoever should hold the role. Seeding one here would create an
-- account with a known password and total access on every live database.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users
  ADD CONSTRAINT users_role_check
  CHECK (role IN ('superuser', 'admin', 'manager', 'staff', 'hr', 'store_manager'));
