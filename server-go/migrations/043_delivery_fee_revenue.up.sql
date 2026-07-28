-- 043: "Biaya Tambahan" (delivery fee) is revenue, not expense.
--
-- Every POS import carries a "Biaya Tambahan" column that was mapped to an
-- account named "Pendapatan Ongkir DO" — but that account was created with
-- account_type 'expense' and numbered 59999, in the expense range. So delivery
-- income was posted as a debit, reading as a cost, and with no credit leg at all
-- it also left the books short by its own value on every import.
--
-- Two things have to be true for this to be right:
--
--   1. The account is revenue. Retyped and renumbered 59999 -> 49900.
--   2. The debit has somewhere to go. It is NOT cash: the parser computes
--      Net = gross - disc (service/pos_import.go), Biaya Tambahan is excluded
--      from Net, and the POS payment breakdown sums to Net — verified across
--      100 of 103 existing imports, where cash equals revenue to within a few
--      rupiah. The fee is earned at the sale but arrives later in a platform
--      payout, so it is a receivable: 10400 "Piutang Ongkir DO".
--
-- If those payouts are never recorded, this receivable grows without ever being
-- cleared — which is an accurate signal, not a bug. It means money earned is not
-- being matched to money received, and the balance is the size of the gap.
--
-- The historical 6.8M is corrected in three steps below so that each one is a
-- balanced entry and none of them silently moves the totals.

DO $$
DECLARE
  v_entry_id  UUID;
  ongkir      UUID;
  ongkir_bal  BIGINT;
  residual    UUID;
  receivable  UUID;
  asset_root  UUID;
BEGIN
  SELECT id, balance INTO ongkir, ongkir_bal
  FROM accounts WHERE name = 'Pendapatan Ongkir DO' LIMIT 1;

  IF ongkir IS NULL THEN
    RAISE NOTICE '043: akun Pendapatan Ongkir DO tidak ada, tidak ada yang diperbaiki';
    RETURN;
  END IF;

  SELECT id INTO residual FROM accounts WHERE account_number = 30990 LIMIT 1;
  SELECT id INTO asset_root FROM accounts WHERE account_number = 10000 AND is_system LIMIT 1;

  -- The receivable the fee is earned into.
  SELECT id INTO receivable FROM accounts WHERE account_number = 10400 LIMIT 1;
  IF receivable IS NULL THEN
    INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
    VALUES (gen_random_uuid(), 'Piutang Ongkir DO', 10400, 'asset', asset_root, 0, true)
    RETURNING id INTO receivable;
  END IF;

  -- Step 1: unwind the bogus expense debit against the migration residual, so
  -- the account reaches zero while it is still typed 'expense'. Retyping an
  -- account that holds a balance would silently flip that balance's meaning.
  IF ongkir_bal <> 0 THEN
    INSERT INTO journal_entries (entry_date, source_type, description)
    VALUES (CURRENT_DATE, 'correction',
            'Pembalikan Biaya Tambahan yang salah dicatat sebagai beban')
    RETURNING id INTO v_entry_id;

    INSERT INTO journal_lines (entry_id, account_id, amount, memo) VALUES
      (v_entry_id, ongkir,   -ongkir_bal, 'nol-kan akun sebelum reklasifikasi'),
      (v_entry_id, residual,  ongkir_bal, 'koreksi klasifikasi ongkir DO');

    UPDATE accounts SET balance = 0 WHERE id = ongkir;
    UPDATE accounts SET balance = balance - ongkir_bal WHERE id = residual;
  END IF;

  -- Step 2: reclassify. Safe now that the balance is zero — no equation impact.
  UPDATE accounts
  SET account_type   = 'revenue',
      account_number = 49900,
      parent_id      = (SELECT id FROM accounts WHERE account_number = 40000 AND is_system LIMIT 1)
  WHERE id = ongkir;

  -- Step 3: re-post the same money the right way round — earned as revenue,
  -- receivable until the platform settles it.
  IF ongkir_bal <> 0 THEN
    INSERT INTO journal_entries (entry_date, source_type, description)
    VALUES (CURRENT_DATE, 'correction',
            'Pencatatan ulang Biaya Tambahan sebagai pendapatan ongkir DO')
    RETURNING id INTO v_entry_id;

    INSERT INTO journal_lines (entry_id, account_id, amount, memo) VALUES
      (v_entry_id, receivable, ongkir_bal, 'ongkir DO belum diterima dari platform'),
      (v_entry_id, ongkir,    -ongkir_bal, 'pendapatan ongkir DO');

    UPDATE accounts SET balance = balance + ongkir_bal WHERE id = receivable;
    UPDATE accounts SET balance = balance + ongkir_bal WHERE id = ongkir;
  END IF;

  RAISE NOTICE '043: ongkir DO % direklasifikasi ke pendapatan + piutang', ongkir_bal;
END $$;
