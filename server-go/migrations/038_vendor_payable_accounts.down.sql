-- Roll the per-vendor payable balances back into the shared 20100 account,
-- then drop the sub-accounts and the vendors.account_id link.

DO $$
DECLARE
  ap_id UUID;
  total BIGINT;
BEGIN
  SELECT id INTO ap_id
  FROM accounts
  WHERE account_number = 20100 AND is_system = true
  LIMIT 1;

  IF ap_id IS NULL THEN
    RETURN;
  END IF;

  SELECT COALESCE(SUM(balance), 0) INTO total
  FROM accounts
  WHERE parent_id = ap_id AND account_number BETWEEN 20101 AND 20999;

  UPDATE accounts SET balance = balance + total WHERE id = ap_id;

  UPDATE vendors SET account_id = NULL;

  DELETE FROM accounts
  WHERE parent_id = ap_id AND account_number BETWEEN 20101 AND 20999;
END $$;

ALTER TABLE vendors DROP COLUMN IF EXISTS account_id;
