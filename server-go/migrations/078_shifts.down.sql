DROP TABLE IF EXISTS shift_change_requests;
DROP TABLE IF EXISTS shift_assignments;
ALTER TABLE employees
  DROP COLUMN IF EXISTS is_delivery_staff,
  DROP COLUMN IF EXISTS division_id;
DROP TABLE IF EXISTS shift_definitions;
