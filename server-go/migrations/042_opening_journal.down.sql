-- Reverses 042: drops the opening entry (its lines cascade) and returns the
-- plug account to zero, putting the books back in their pre-migration state —
-- imbalance and all.
--
-- Safe only while the opening entry is still the plug's whole story. If real
-- entries have since been posted against 30990 (for example moving part of the
-- plug to Saldo Awal once the opening balances were confirmed), zeroing it here
-- would discard them, so this refuses to run in that case.

DO $$
DECLARE
  v_entry_id   UUID;
  plug_account UUID;
  other_lines  INT;
BEGIN
  SELECT id INTO v_entry_id
  FROM journal_entries WHERE source_type = 'opening_balance'
  ORDER BY created_at LIMIT 1;

  IF v_entry_id IS NULL THEN
    RETURN;
  END IF;

  SELECT id INTO plug_account FROM accounts WHERE account_number = 30990 LIMIT 1;

  SELECT COUNT(*) INTO other_lines
  FROM journal_lines
  WHERE account_id = plug_account AND entry_id <> v_entry_id;

  IF other_lines > 0 THEN
    RAISE EXCEPTION
      '042 down: Selisih Migrasi memiliki % baris jurnal lain — batalkan entri tersebut lebih dulu', other_lines;
  END IF;

  DELETE FROM journal_entries WHERE id = v_entry_id;
  UPDATE accounts SET balance = 0 WHERE id = plug_account;
END $$;
