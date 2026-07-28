-- Reverses 042: drops the opening entry (its lines cascade) and returns both
-- equity accounts to what they held before it, putting the books back in their
-- pre-migration state — imbalance and all.
--
-- Safe only while the opening entry is still the whole story for those two
-- accounts. If real entries have since been posted against them (for example
-- reclassifying part of the residual to capital once the figures were confirmed
-- against the old system), unwinding here would discard them, so this refuses
-- to run in that case.

DO $$
DECLARE
  v_entry_id    UUID;
  capital_acct  UUID;
  residual_acct UUID;
  other_lines   INT;
BEGIN
  SELECT id INTO v_entry_id
  FROM journal_entries WHERE source_type = 'opening_balance'
  ORDER BY created_at LIMIT 1;

  IF v_entry_id IS NULL THEN
    DROP VIEW IF EXISTS cash_reconciliation;
    RETURN;
  END IF;

  SELECT id INTO capital_acct  FROM accounts WHERE account_number = 30100 LIMIT 1;
  SELECT id INTO residual_acct FROM accounts WHERE account_number = 30990 LIMIT 1;

  SELECT COUNT(*) INTO other_lines
  FROM journal_lines
  WHERE account_id IN (capital_acct, residual_acct) AND entry_id <> v_entry_id;

  IF other_lines > 0 THEN
    RAISE EXCEPTION
      '042 down: akun ekuitas memiliki % baris jurnal lain — batalkan entri tersebut lebih dulu', other_lines;
  END IF;

  -- Undo exactly what the opening entry credited, rather than assuming zero.
  UPDATE accounts a
  SET balance = a.balance + jl.amount
  FROM journal_lines jl
  WHERE jl.entry_id = v_entry_id
    AND jl.account_id = a.id
    AND a.id IN (capital_acct, residual_acct);

  DELETE FROM journal_entries WHERE id = v_entry_id;
END $$;

DROP VIEW IF EXISTS cash_reconciliation;
