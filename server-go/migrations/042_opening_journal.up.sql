-- 042: Seed the journal with an opening entry and make the books balance.
--
-- 040 gave the system a journal and made every posting path write balanced
-- entries. It did nothing about the past: the balances carried into this
-- migration are the result of three months of one-sided writes, and
-- A - (L + E + R - Exp) is a long way from zero.
--
-- This posts a single opening entry, dated at cutover, that establishes every
-- account's current balance in the journal. The entry is made to balance by a
-- plug against "Selisih Migrasi" (30990) — the equity account named precisely so
-- that this number cannot be mistaken for a real one.
--
-- Deliberately NOT done here, because each is a decision about real money that
-- needs a human to confirm the amount first:
--
--   * splitting the plug into genuine opening capital (30900 Saldo Awal) vs
--     error. Roughly 1.09B of it is cash that the old system carried in and that
--     no transaction in this database explains — but those figures were derived
--     as residuals, not read off the old system, so they are not yet trustworthy
--     enough to book as capital.
--   * "Stock Waste": still typed `asset` with a negative balance, because the
--     legacy opname wrote `balance = balance - waste` on an account created with
--     no account_type. Reclassifying it to `expense` and flipping the sign moves
--     ~245M through the P&L.
--   * the inventory overstatement (Persediaan vs the value of the actual lots),
--     which the same legacy opname bug caused by never crediting inventory.
--   * ~327M of wage expense debited with no credit by payroll closes that ran
--     before 040 landed.
--
-- All four are visible in GET /api/accounts/trial-balance and can be corrected
-- with ordinary journal entries once the amounts are agreed. Until then they sit
-- inside the plug, quarantined and named, rather than spread invisibly across
-- the chart of accounts.
--
-- Why raw SQL rather than service.Post: Post also moves accounts.balance, which
-- is correct for a new transaction but would double every balance here. The
-- journal rows are written directly so that after this migration the journal
-- agrees with the cache for every account, and the only balance that changes is
-- the plug account's.
--
-- Idempotent: re-running is a no-op once an opening entry exists.

DO $$
DECLARE
  v_entry_id   UUID;
  plug_account UUID;
  plug_amount  BIGINT;
BEGIN
  IF EXISTS (SELECT 1 FROM journal_entries WHERE source_type = 'opening_balance') THEN
    RAISE NOTICE '042: opening entry already present, skipping';
    RETURN;
  END IF;

  SELECT id INTO plug_account FROM accounts WHERE account_number = 30990 LIMIT 1;
  IF plug_account IS NULL THEN
    RAISE EXCEPTION '042: akun 30990 (Selisih Migrasi) tidak ditemukan — jalankan migrasi 040 lebih dulu';
  END IF;

  INSERT INTO journal_entries (entry_date, source_type, description)
  VALUES (
    CURRENT_DATE,
    'opening_balance',
    'Saldo awal jurnal — hasil migrasi dari sistem tanpa buku besar'
  )
  RETURNING id INTO v_entry_id;

  -- One line per account, debit-positive, for the part of its balance the
  -- journal does not already explain.
  --
  -- Subtracting the existing journal balance matters: entries posted between 040
  -- and this migration (or by an integration test run against this database)
  -- already account for part of the cache. Booking the full balance on top of
  -- them would double-count those accounts, leaving cache and journal disagreeing
  -- by exactly the amount already posted.
  --
  -- Accounts where the journal already explains the balance in full contribute
  -- nothing and are skipped — journal_lines.amount is CHECKed non-zero.
  INSERT INTO journal_lines (entry_id, account_id, amount, memo)
  SELECT
    v_entry_id,
    a.id,
    (CASE WHEN a.account_type IN ('asset', 'expense') THEN a.balance ELSE -a.balance END)
      - COALESCE(j.posted, 0),
    'saldo awal'
  FROM accounts a
  LEFT JOIN (
    SELECT account_id, SUM(amount) AS posted
    FROM journal_lines
    GROUP BY account_id
  ) j ON j.account_id = a.id
  WHERE a.id <> plug_account
    AND (CASE WHEN a.account_type IN ('asset', 'expense') THEN a.balance ELSE -a.balance END)
        - COALESCE(j.posted, 0) <> 0;

  -- Whatever is left over is the accumulated imbalance.
  SELECT -COALESCE(SUM(amount), 0) INTO plug_amount
  FROM journal_lines WHERE entry_id = v_entry_id;

  IF plug_amount = 0 THEN
    RAISE NOTICE '042: books already balance, no plug needed';
  ELSE
    INSERT INTO journal_lines (entry_id, account_id, amount, memo)
    VALUES (
      v_entry_id,
      plug_account,
      plug_amount,
      'selisih akumulasi sebelum jurnal ada — belum diidentifikasi'
    );

    -- The plug is the one balance this migration changes. accounts.balance is
    -- natural-sign per type and 30990 is equity (credit-normal), so the cached
    -- balance is the negation of the debit-positive journal amount. After this,
    -- cache and journal agree for every account and the equation closes.
    UPDATE accounts SET balance = -plug_amount WHERE id = plug_account;

    RAISE NOTICE '042: plug of % posted to Selisih Migrasi (30990)', -plug_amount;
  END IF;
END $$;
