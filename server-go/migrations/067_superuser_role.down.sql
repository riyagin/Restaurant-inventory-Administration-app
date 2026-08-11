-- Rolling back drops a role the constraint no longer allows, so any superuser
-- is demoted to admin first — the closest thing left, and it keeps the account
-- usable rather than orphaning it.

UPDATE users SET role = 'admin' WHERE role = 'superuser';

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users
  ADD CONSTRAINT users_role_check
  CHECK (role IN ('admin', 'manager', 'staff', 'hr', 'store_manager'));
