DROP INDEX IF EXISTS idx_employees_full_name;
DROP INDEX IF EXISTS idx_employees_status;
DROP INDEX IF EXISTS idx_employees_position_id;
DROP INDEX IF EXISTS idx_employees_branch_id;

DROP TABLE IF EXISTS employees;
DROP TABLE IF EXISTS positions;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
