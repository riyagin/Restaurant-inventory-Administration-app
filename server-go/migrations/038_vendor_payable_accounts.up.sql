-- 038: Per-vendor Accounts Payable sub-accounts.
--
-- Unpaid invoices used to credit a single shared liability account
-- ("Utang Usaha", 20100). This splits the payable per vendor: every vendor gets
-- its own child account "Utang Usaha - <Vendor>" under 20100, numbered from
-- 20101 upward. Invoices without a vendor post to the shared bucket
-- "Utang Usaha - Lainnya" (20999, system-protected so it cannot be deleted).
--
-- Existing balance handling: each new vendor account is opened with that
-- vendor's outstanding amount (unpaid + partial invoices). Whatever is left of
-- the old 20100 balance after those moves — invoices with no vendor, plus any
-- historical drift — lands in "Utang Usaha - Lainnya", and 20100 itself is
-- zeroed. The COA UI rolls a parent up from its children, so the group total
-- stays exactly what it was before this migration.
--
-- Idempotent: vendors that already have account_id are skipped, so a re-run
-- only fills in gaps (moved = 0 and 20100 already 0 → no balance shuffling).

ALTER TABLE vendors ADD COLUMN IF NOT EXISTS account_id UUID REFERENCES accounts(id);

DO $$
DECLARE
  ap_id       UUID;
  ap_balance  BIGINT;
  other_id    UUID;
  v           RECORD;
  new_number  INT;
  acct_name   TEXT;
  acct_id     UUID;
  outstanding BIGINT;
  moved       BIGINT := 0;
BEGIN
  SELECT id, balance INTO ap_id, ap_balance
  FROM accounts
  WHERE account_number = 20100 AND is_system = true
  LIMIT 1;

  -- No default payable account (fresh/partial DB) → nothing to split.
  IF ap_id IS NULL THEN
    RETURN;
  END IF;

  -- Shared bucket for invoices that carry no vendor.
  SELECT id INTO other_id FROM accounts WHERE account_number = 20999 LIMIT 1;
  IF other_id IS NULL THEN
    INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
    VALUES (gen_random_uuid(), 'Utang Usaha - Lainnya', 20999, 'liability', ap_id, 0, true)
    RETURNING id INTO other_id;
  END IF;

  FOR v IN SELECT id, name FROM vendors WHERE account_id IS NULL ORDER BY name LOOP
    SELECT COALESCE(MAX(account_number), 20100) + 1 INTO new_number
    FROM accounts
    WHERE account_number BETWEEN 20101 AND 20998;

    -- accounts.name is UNIQUE; disambiguate on the rare clash.
    acct_name := 'Utang Usaha - ' || v.name;
    IF EXISTS (SELECT 1 FROM accounts WHERE name = acct_name) THEN
      acct_name := acct_name || ' (' || new_number || ')';
    END IF;

    SELECT COALESCE(SUM(t.total - i.amount_paid), 0) INTO outstanding
    FROM invoices i
    JOIN LATERAL (
      SELECT COALESCE(SUM(ii.price * ii.quantity), 0)::BIGINT AS total
      FROM invoice_items ii WHERE ii.invoice_id = i.id
    ) t ON TRUE
    WHERE i.vendor_id = v.id AND i.payment_status IN ('unpaid', 'partial');

    INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
    VALUES (gen_random_uuid(), acct_name, new_number, 'liability', ap_id, outstanding, false)
    RETURNING id INTO acct_id;

    UPDATE vendors SET account_id = acct_id WHERE id = v.id;
    moved := moved + outstanding;
  END LOOP;

  -- Residual keeps the group total unchanged; the parent now only rolls up.
  UPDATE accounts SET balance = balance + (ap_balance - moved) WHERE id = other_id;
  UPDATE accounts SET balance = 0 WHERE id = ap_id;
END $$;
