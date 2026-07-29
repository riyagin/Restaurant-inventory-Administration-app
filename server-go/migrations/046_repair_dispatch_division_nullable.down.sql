-- Reverses 046 only if the data still allows it. Restoring NOT NULL while
-- division-less dispatches exist would fail anyway; checking first turns that
-- into a clear message instead of a constraint error.

DO $$
DECLARE
  without_division INT;
BEGIN
  SELECT COUNT(*) INTO without_division FROM dispatches WHERE division_id IS NULL;

  IF without_division > 0 THEN
    RAISE EXCEPTION
      '046 down: % pengiriman tidak memiliki divisi — tidak dapat mengembalikan NOT NULL', without_division;
  END IF;

  ALTER TABLE dispatches ALTER COLUMN division_id SET NOT NULL;
END $$;
