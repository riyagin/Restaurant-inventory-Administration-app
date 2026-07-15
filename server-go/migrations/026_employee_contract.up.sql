-- Distinguish permanent staff (karyawan tetap / PKWTT) from contract staff
-- (karyawan kontrak / PKWT) and record a contract expiry date so HR can be
-- reminded when a contract enters its final month.
ALTER TABLE employees
  ADD COLUMN employment_type   TEXT NOT NULL DEFAULT 'permanent'
    CHECK (employment_type IN ('permanent', 'contract')),
  ADD COLUMN contract_end_date DATE;

-- Speeds up the "contracts ending soon" scan surfaced by the notifier.
CREATE INDEX idx_employees_contract_end
  ON employees (contract_end_date)
  WHERE employment_type = 'contract' AND contract_end_date IS NOT NULL;
