-- 047: Clear the pre-043 POS delivery-fee plugs out of the suspense account.
--
-- Between the journal landing (040) and the delivery-fee reclassification (043),
-- POS imports posted "Biaya Tambahan" as an expense debit with no credit leg,
-- and the resulting imbalance was parked in "Akun Sementara" (19999) with the
-- memo "selisih impor POS (kas + beban ≠ pendapatan)".
--
-- 043 has since booked that money properly — the whole balance of the old
-- expense account was moved to "Piutang Ongkir DO" and "Pendapatan Ongkir DO",
-- and it included the amounts from these imports, because 043 read the account's
-- balance at the time it ran. The revenue and the receivable are therefore
-- already correct.
--
-- What is left over is only the suspense credit itself, whose matching debit 043
-- routed through "Selisih Migrasi" when it zeroed the old account. Those two
-- cancel, so this returns the suspense parking to the residual and leaves
-- everything else untouched.
--
-- Only the lines carrying the pre-043 memo are unwound. The suspense account
-- also holds entries with the memo "pembulatan impor POS", which are the current
-- (correct) behaviour — genuine POS rounding, a few rupiah per import — and
-- those stay where they are.
--
-- Idempotent via the marker source_type.

DO $$
DECLARE
  v_entry_id    UUID;
  suspense      UUID;
  residual_acct UUID;
  amount        BIGINT;
BEGIN
  IF EXISTS (SELECT 1 FROM journal_entries WHERE source_type = 'pos_suspense_unwind') THEN
    RAISE NOTICE '047: POS suspense already unwound, skipping';
    RETURN;
  END IF;

  SELECT id INTO suspense      FROM accounts WHERE account_number = 19999 LIMIT 1;
  SELECT id INTO residual_acct FROM accounts WHERE account_number = 30990 LIMIT 1;
  IF suspense IS NULL OR residual_acct IS NULL THEN
    RAISE EXCEPTION '047: akun 19999/30990 tidak ditemukan';
  END IF;

  -- The parked amount, as a debit that would bring these lines back to zero.
  SELECT -COALESCE(SUM(jl.amount), 0) INTO amount
  FROM journal_lines jl
  WHERE jl.account_id = suspense
    AND jl.memo = 'selisih impor POS (kas + beban ≠ pendapatan)';

  IF amount = 0 THEN
    RAISE NOTICE '047: tidak ada parkir impor POS lama di akun sementara';
    RETURN;
  END IF;

  INSERT INTO journal_entries (entry_date, source_type, description)
  VALUES (CURRENT_DATE, 'pos_suspense_unwind',
          'Penyelesaian parkir sementara impor POS sebelum reklasifikasi ongkir')
  RETURNING id INTO v_entry_id;

  INSERT INTO journal_lines (entry_id, account_id, amount, memo) VALUES
    (v_entry_id, suspense,       amount, 'pelunasan parkir impor POS lama'),
    (v_entry_id, residual_acct, -amount, 'imbangan parkir impor POS lama');

  -- Suspense is an asset (debit-normal), the residual is equity (credit-normal).
  UPDATE accounts SET balance = balance + amount WHERE id = suspense;
  UPDATE accounts SET balance = balance + amount WHERE id = residual_acct;

  RAISE NOTICE '047: % dikeluarkan dari akun sementara', amount;
END $$;
