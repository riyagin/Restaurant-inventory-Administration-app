-- Rolling back drops the roles the constraint no longer allows, so any user
-- holding one is moved to staff first rather than leaving the table in a state
-- the new constraint would reject.
UPDATE users SET role = 'staff' WHERE role IN ('hr', 'store_manager');

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users
  ADD CONSTRAINT users_role_check CHECK (role IN ('admin', 'manager', 'staff'));
