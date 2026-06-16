DROP INDEX IF EXISTS idx_kasbon_installments_due;
DROP INDEX IF EXISTS idx_kasbon_installments_kasbon;
DROP TABLE IF EXISTS kasbon_installments;

DROP INDEX IF EXISTS idx_kasbons_number;
DROP INDEX IF EXISTS idx_kasbons_status;
DROP INDEX IF EXISTS idx_kasbons_emp_status;
DROP TABLE IF EXISTS kasbons;

-- Remove the seeded system asset account. Guarded on is_system + name + number so
-- a manually-created account is left untouched. account_type='asset' has zero
-- balance after the kasbon tables (its only writers) are dropped above.
DELETE FROM accounts
WHERE account_number = 10300
  AND name = 'Piutang Karyawan'
  AND is_system = true;
