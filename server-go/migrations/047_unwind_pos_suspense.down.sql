-- Reverses 047, putting the parked amount back in the suspense account.

DO $$
DECLARE
  v_entry_id    UUID;
  suspense      UUID;
  residual_acct UUID;
  amount        BIGINT;
BEGIN
  SELECT id INTO v_entry_id
  FROM journal_entries WHERE source_type = 'pos_suspense_unwind'
  ORDER BY created_at LIMIT 1;

  IF v_entry_id IS NULL THEN
    RETURN;
  END IF;

  SELECT id INTO suspense      FROM accounts WHERE account_number = 19999 LIMIT 1;
  SELECT id INTO residual_acct FROM accounts WHERE account_number = 30990 LIMIT 1;

  SELECT jl.amount INTO amount
  FROM journal_lines jl WHERE jl.entry_id = v_entry_id AND jl.account_id = suspense;

  DELETE FROM journal_entries WHERE id = v_entry_id;

  UPDATE accounts SET balance = balance - amount WHERE id = suspense;
  UPDATE accounts SET balance = balance - amount WHERE id = residual_acct;
END $$;
