-- Rolling back drops a role the constraint no longer allows. A branch operator
-- is demoted to store_manager — the closest remaining role (attendance entry
-- only) and strictly narrower, so no account gains access by the rollback.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_branch_scope_check;

UPDATE users SET role = 'store_manager' WHERE role = 'branch_operator';

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users
  ADD CONSTRAINT users_role_check
  CHECK (role IN ('superuser', 'admin', 'manager', 'staff', 'hr', 'store_manager'));

ALTER TABLE users DROP COLUMN IF EXISTS branch_id;
