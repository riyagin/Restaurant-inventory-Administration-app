-- Reverses 045: moves the opening capital back into Selisih Migrasi.
--
-- The view is left in place. It is a read-only projection with no state of its
-- own, and dropping it here would reintroduce exactly the failure this migration
-- was written to fix.

DO $$
DECLARE
  v_entry_id    UUID;
  capital_acct  UUID;
  residual_acct UUID;
  amount        BIGINT;
BEGIN
  SELECT id INTO v_entry_id
  FROM journal_entries WHERE source_type = 'capital_reclass'
  ORDER BY created_at LIMIT 1;

  IF v_entry_id IS NULL THEN
    RETURN;
  END IF;

  SELECT id INTO capital_acct  FROM accounts WHERE account_number = 30100 LIMIT 1;
  SELECT id INTO residual_acct FROM accounts WHERE account_number = 30990 LIMIT 1;

  SELECT jl.amount INTO amount
  FROM journal_lines jl
  WHERE jl.entry_id = v_entry_id AND jl.account_id = residual_acct;

  DELETE FROM journal_entries WHERE id = v_entry_id;

  UPDATE accounts SET balance = balance + amount WHERE id = residual_acct;
  UPDATE accounts SET balance = balance - amount WHERE id = capital_acct;
END $$;
