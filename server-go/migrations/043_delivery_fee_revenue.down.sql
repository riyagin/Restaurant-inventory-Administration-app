-- Reverses 043: puts "Pendapatan Ongkir DO" back in the expense range with its
-- original balance and unwinds the receivable.
--
-- Refuses to run once real activity has been posted against the receivable
-- (a platform payout being recorded, say) — reversing then would discard it.

DO $$
DECLARE
  ongkir      UUID;
  receivable  UUID;
  residual    UUID;
  amount      BIGINT;
  later_lines INT;
BEGIN
  SELECT id INTO ongkir     FROM accounts WHERE name = 'Pendapatan Ongkir DO' LIMIT 1;
  SELECT id INTO receivable FROM accounts WHERE account_number = 10400 LIMIT 1;
  SELECT id INTO residual   FROM accounts WHERE account_number = 30990 LIMIT 1;

  IF ongkir IS NULL OR receivable IS NULL THEN
    RETURN;
  END IF;

  SELECT COUNT(*) INTO later_lines
  FROM journal_lines jl
  JOIN journal_entries je ON je.id = jl.entry_id
  WHERE jl.account_id = receivable AND je.source_type <> 'correction';

  IF later_lines > 0 THEN
    RAISE EXCEPTION
      '043 down: Piutang Ongkir DO sudah memiliki % transaksi lain — batalkan entri tersebut lebih dulu', later_lines;
  END IF;

  SELECT balance INTO amount FROM accounts WHERE id = receivable;

  DELETE FROM journal_entries WHERE source_type = 'correction';

  UPDATE accounts
  SET account_type   = 'expense',
      account_number = 59999,
      parent_id      = (SELECT id FROM accounts WHERE account_number = 50000 AND is_system LIMIT 1),
      balance        = amount
  WHERE id = ongkir;

  UPDATE accounts SET balance = balance + amount WHERE id = residual;
  UPDATE accounts SET balance = 0 WHERE id = receivable;
END $$;
