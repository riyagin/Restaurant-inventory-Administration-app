-- Add ON DELETE SET NULL to every FK on users(id) that is still using the
-- default NO ACTION / RESTRICT rule. Skips constraints already set to SET NULL
-- so the migration is safe to retry and idempotent.
DO $$
DECLARE
  r RECORD;
BEGIN
  FOR r IN
    SELECT
      tc.table_name,
      kcu.column_name,
      tc.constraint_name
    FROM information_schema.table_constraints      AS tc
    JOIN information_schema.key_column_usage       AS kcu
      ON  kcu.constraint_name   = tc.constraint_name
      AND kcu.constraint_schema = tc.constraint_schema
    JOIN information_schema.referential_constraints AS rc
      ON  rc.constraint_name   = tc.constraint_name
      AND rc.constraint_schema = tc.constraint_schema
    JOIN information_schema.table_constraints      AS tc2
      ON  tc2.constraint_name   = rc.unique_constraint_name
      AND tc2.constraint_schema = rc.unique_constraint_schema
    WHERE tc.constraint_type = 'FOREIGN KEY'
      AND tc2.table_name     = 'users'
      AND rc.delete_rule     = 'NO ACTION'
      AND tc.table_name IN (
        'productions', 'pos_imports', 'account_adjustments', 'enumerations',
        'wage_structures', 'hr_import_batches', 'fingerprint_imports',
        'performance_violations', 'leave_requests', 'kasbons',
        'payroll_periods', 'payroll_lines', 'stock_opname_drafts'
      )
  LOOP
    EXECUTE format(
      'ALTER TABLE %I DROP CONSTRAINT %I',
      r.table_name, r.constraint_name
    );
    EXECUTE format(
      'ALTER TABLE %I ADD CONSTRAINT %I FOREIGN KEY (%I) REFERENCES users(id) ON DELETE SET NULL',
      r.table_name, r.constraint_name, r.column_name
    );
  END LOOP;
END $$;
