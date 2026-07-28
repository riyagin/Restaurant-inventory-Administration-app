-- Reverses 044. Only renames back if the old name is free, so it cannot collide
-- with an account someone created under the original name in the meantime.

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM accounts WHERE name = 'Stock Waste') THEN
    RETURN;
  END IF;

  UPDATE accounts SET name = 'Stock Waste' WHERE name = 'Selisih Persediaan';
END $$;
