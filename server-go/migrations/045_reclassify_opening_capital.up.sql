-- 045: Move the opening cash injection out of "Selisih Migrasi" into
-- "Modal Pemilik", and restore the cash_reconciliation view.
--
-- WHY THIS IS NEEDED — two defects in 042, both visible once it ran against a
-- real production snapshot:
--
-- 1. The split never happened. 042 credits the unexplained-cash portion of the
--    imbalance to Modal Pemilik and only the remainder to Selisih Migrasi. On
--    this database it put the entire 991,245,483 in Selisih Migrasi and left
--    Modal Pemilik at zero, because the cash reconciliation returned nothing at
--    the moment it ran — the accounts table was not yet in the state the query
--    assumes (no children under 11000 resolves to an empty result, and
--    COALESCE(SUM(...), 0) turns that into "no injection" rather than an error).
--
--    Re-running 042 cannot fix it: its first statement is
--    "IF EXISTS (opening_balance) THEN RETURN". That guard makes the migration
--    safe to re-run but also makes a wrong result permanent — a one-shot,
--    data-dependent decision taken inside a migration that can never re-decide.
--    The correction therefore has to be an ordinary journal entry, which is what
--    this is.
--
-- 2. The view disappeared. 042 creates cash_reconciliation at the top of the
--    file, so it only exists if 042 itself runs. Once the database is at version
--    042+, anything that drops the view (a restore, a manual down-migration)
--    leaves it gone for good and GET /api/accounts/cash-reconciliation returns
--    500. Recreated here with CREATE OR REPLACE so it is re-established
--    independently of whether 042 ever ran.
--
-- ON THE AMOUNT: 1,044,158,349, being the sum of every cash account holding more
-- than its recorded transactions explain (Kas Besar 800,850,365 and Cash
-- 243,307,984). The other seven cash accounts reconcile to the rupiah, which is
-- what gives the figure its weight.
--
-- That is more than Selisih Migrasi holds, so the residual goes negative by
-- ~52.9M. That is not an error to hide: it says the remaining one-sided writes
-- net to a debit, which is consistent with the known ones still outstanding
-- (Selisih Persediaan booked as a negative asset, payroll closes that debited
-- expense with no credit). Those get corrected on their own terms.
--
-- Still worth checking the two figures against what the old system said cash was
-- at cutover. Both accounts are equity and both are visible, so moving amounts
-- between them afterwards is an ordinary entry.

CREATE OR REPLACE VIEW cash_reconciliation AS
WITH cash_accounts AS (
  SELECT id, name, balance
  FROM accounts
  WHERE parent_id = (SELECT id FROM accounts WHERE account_number = 11000 LIMIT 1)
),
pos_in AS (
  SELECT account_id, SUM(amount) AS amt
  FROM pos_import_lines WHERE line_type = 'cash' GROUP BY account_id
),
sales_in AS (
  SELECT account_id, SUM(amount) AS amt FROM sales GROUP BY account_id
),
invoice_out AS (
  SELECT i.account_id, SUM(COALESCE(t.total, 0)) AS amt
  FROM invoices i
  JOIN LATERAL (
    SELECT COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
    FROM invoice_items ii WHERE ii.invoice_id = i.id
  ) t ON TRUE
  WHERE i.payment_status <> 'unpaid' AND i.account_id IS NOT NULL
  GROUP BY i.account_id
),
kasbon_out AS (
  SELECT fund_source_account_id AS account_id, SUM(amount) AS amt
  FROM kasbons WHERE fund_source_account_id IS NOT NULL GROUP BY fund_source_account_id
)
SELECT
  c.id,
  c.name,
  c.balance,
  (COALESCE(p.amt, 0) + COALESCE(s.amt, 0)
   - COALESCE(i.amt, 0) - COALESCE(k.amt, 0))::BIGINT AS explained,
  (c.balance - (COALESCE(p.amt, 0) + COALESCE(s.amt, 0)
   - COALESCE(i.amt, 0) - COALESCE(k.amt, 0)))::BIGINT AS unexplained
FROM cash_accounts c
LEFT JOIN pos_in      p ON p.account_id = c.id
LEFT JOIN sales_in    s ON s.account_id = c.id
LEFT JOIN invoice_out i ON i.account_id = c.id
LEFT JOIN kasbon_out  k ON k.account_id = c.id;

COMMENT ON VIEW cash_reconciliation IS
  'Per cash account: balance vs what recorded transactions explain. A positive '
  '"unexplained" is money that entered outside any transaction — typically an '
  'opening balance carried over from the previous system.';

DO $$
DECLARE
  v_entry_id    UUID;
  capital_acct  UUID;
  residual_acct UUID;
  amount        BIGINT;
BEGIN
  IF EXISTS (SELECT 1 FROM journal_entries WHERE source_type = 'capital_reclass') THEN
    RAISE NOTICE '045: opening capital already reclassified, skipping';
    RETURN;
  END IF;

  SELECT id INTO capital_acct  FROM accounts WHERE account_number = 30100 LIMIT 1;
  SELECT id INTO residual_acct FROM accounts WHERE account_number = 30990 LIMIT 1;
  IF capital_acct IS NULL OR residual_acct IS NULL THEN
    RAISE EXCEPTION '045: akun ekuitas 30100/30990 tidak ditemukan';
  END IF;

  SELECT COALESCE(SUM(unexplained), 0) INTO amount
  FROM cash_reconciliation WHERE unexplained > 0;

  IF amount <= 0 THEN
    RAISE NOTICE '045: tidak ada kas tak terjelaskan, tidak ada yang direklasifikasi';
    RETURN;
  END IF;

  INSERT INTO journal_entries (entry_date, source_type, description)
  VALUES (CURRENT_DATE, 'capital_reclass',
          'Reklasifikasi setoran modal awal dari Selisih Migrasi ke Modal Pemilik')
  RETURNING id INTO v_entry_id;

  INSERT INTO journal_lines (entry_id, account_id, amount, memo) VALUES
    (v_entry_id, residual_acct,  amount, 'kas awal yang sebelumnya belum teridentifikasi'),
    (v_entry_id, capital_acct,  -amount, 'setoran modal awal saat migrasi sistem');

  -- Both are equity (credit-normal), so the cached balance moves opposite to the
  -- debit-positive journal amount.
  UPDATE accounts SET balance = balance - amount WHERE id = residual_acct;
  UPDATE accounts SET balance = balance + amount WHERE id = capital_acct;

  RAISE NOTICE '045: % direklasifikasi ke Modal Pemilik', amount;
END $$;
