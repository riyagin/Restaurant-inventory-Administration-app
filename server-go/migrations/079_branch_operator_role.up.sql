-- Add the 'branch_operator' role.
--
-- A branch operator handles the HR paperwork of one branch on the floor:
-- checking and correcting attendance, and filing kasbon, leave and overtime
-- requests on behalf of that branch's employees. It approves nothing.
--
-- The role is meaningless without a branch, so users gain a branch_id and a
-- CHECK makes it mandatory for this role. Other roles are unscoped and must not
-- carry one, otherwise a leftover value would read as a restriction that is not
-- enforced anywhere.
--
-- ON DELETE RESTRICT: a branch with an operator cannot be deleted out from
-- under them — reassign or remove the account first, the same way employees
-- already pin their branch.

ALTER TABLE users
  ADD COLUMN branch_id UUID REFERENCES branches(id) ON DELETE RESTRICT;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users
  ADD CONSTRAINT users_role_check
  CHECK (role IN ('superuser', 'admin', 'manager', 'staff', 'hr', 'store_manager', 'branch_operator'));

ALTER TABLE users
  ADD CONSTRAINT users_branch_scope_check
  CHECK ((role = 'branch_operator') = (branch_id IS NOT NULL));
