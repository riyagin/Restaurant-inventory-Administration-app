DROP INDEX IF EXISTS idx_employees_contract_end;
ALTER TABLE employees DROP COLUMN IF EXISTS contract_end_date;
ALTER TABLE employees DROP COLUMN IF EXISTS employment_type;
