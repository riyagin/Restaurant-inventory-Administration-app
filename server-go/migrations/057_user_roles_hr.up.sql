-- Widen users.role to cover the roles the application actually accepts.
--
-- 005 pinned the CHECK to admin|manager|staff. store_manager was later added to
-- handler.validUserRoles but never to the constraint, so creating one failed at
-- the database with a 500; 'hr' would have hit the same wall. Both are added
-- here so the constraint and validUserRoles agree.
--
--   hr            — confined to the HR module (middleware.RestrictHRRole)
--   store_manager — attendance record entry/correction only

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users
  ADD CONSTRAINT users_role_check
  CHECK (role IN ('admin', 'manager', 'staff', 'hr', 'store_manager'));
