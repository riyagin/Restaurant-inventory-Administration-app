-- 042: Seed the journal with an opening entry, and name the opening cash
-- injection for what it is.
--
-- 040 gave the system a journal and made every posting path write balanced
-- entries. It did nothing about the past: the balances carried into this
-- migration are the result of months of one-sided writes, and
-- A - (L + E + R - Exp) is a long way from zero.
--
-- This posts a single opening entry establishing every account's current balance
-- in the journal, and balances it against two equity accounts:
--
--   30100 Modal Pemilik    the cash the business started this system with. When
--                          the old system was replaced, cash balances were typed
--                          straight into accounts.balance with nothing on the
--                          credit side. That was never an error in the money —
--                          only in the bookkeeping: the money was real, the
--                          matching capital entry was missing. This supplies it.
--   30990 Selisih Migrasi  everything else — accumulated one-sided writes whose
--                          cause is not established. Named so it cannot be
--                          mistaken for a real equity balance.
--
-- The split is computed from the data, not hardcoded, so it produces the right
-- answer on production as well as on a dev copy.
--
-- HOW THE CASH INJECTION IS IDENTIFIED
--
-- For every cash account (a child of "Kas dan Setara Kas") the view
-- cash_reconciliation compares its balance against the transactions that should
-- explain it: POS cash lines and manual sales in, invoice payments and kasbon
-- disbursements out. An account whose balance exceeds what its transactions
-- explain is holding money that entered outside any recorded transaction —
-- which is the signature of an opening balance typed in at migration time.
--
-- This is evidence, not proof. Both destinations are equity and both are
-- visible in the CoA, so once the figures are checked against the old system
-- any amount can be moved between them with an ordinary journal entry. What
-- matters is that the total is right and nothing is hidden.
--
-- Why raw SQL rather than service.Post: Post also moves accounts.balance, which
-- is correct for a new transaction but would double every balance here. The
-- journal rows are written directly so that afterwards the journal agrees with
-- the cache for every account, and the only balances that change are the two
-- equity accounts'.
--
-- Idempotent: re-running is a no-op once an opening entry exists.

-- Reusable because it is needed in three places: this migration, the
-- reconciliation report an operator runs before trusting the split, and the
-- verification that the books still tie afterwards.
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
  v_entry_id     UUID;
  capital_acct   UUID;
  residual_acct  UUID;
  imbalance      BIGINT;
  injected_cash  BIGINT;
  capital_amount BIGINT;
  residual       BIGINT;
BEGIN
  IF EXISTS (SELECT 1 FROM journal_entries WHERE source_type = 'opening_balance') THEN
    RAISE NOTICE '042: opening entry already present, skipping';
    RETURN;
  END IF;

  SELECT id INTO capital_acct  FROM accounts WHERE account_number = 30100 LIMIT 1;
  SELECT id INTO residual_acct FROM accounts WHERE account_number = 30990 LIMIT 1;
  IF capital_acct IS NULL OR residual_acct IS NULL THEN
    RAISE EXCEPTION '042: akun ekuitas 30100/30990 tidak ditemukan — jalankan migrasi 040 lebih dulu';
  END IF;

  INSERT INTO journal_entries (entry_date, source_type, description)
  VALUES (
    CURRENT_DATE,
    'opening_balance',
    'Saldo awal jurnal — migrasi dari sistem tanpa buku besar'
  )
  RETURNING id INTO v_entry_id;

  -- One line per account, debit-positive, for the part of its balance the
  -- journal does not already explain.
  --
  -- Subtracting the existing journal balance matters: entries posted between 040
  -- and this migration already account for part of the cache. Booking the full
  -- balance on top of them would double-count those accounts, leaving cache and
  -- journal disagreeing by exactly the amount already posted.
  --
  -- Accounts the journal already explains in full contribute nothing and are
  -- skipped — journal_lines.amount is CHECKed non-zero.
  INSERT INTO journal_lines (entry_id, account_id, amount, memo)
  SELECT
    v_entry_id,
    a.id,
    (CASE WHEN a.account_type IN ('asset', 'expense') THEN a.balance ELSE -a.balance END)
      - COALESCE(j.posted, 0),
    'saldo awal'
  FROM accounts a
  LEFT JOIN (
    SELECT account_id, SUM(amount) AS posted FROM journal_lines GROUP BY account_id
  ) j ON j.account_id = a.id
  WHERE a.id NOT IN (capital_acct, residual_acct)
    AND (CASE WHEN a.account_type IN ('asset', 'expense') THEN a.balance ELSE -a.balance END)
        - COALESCE(j.posted, 0) <> 0;

  -- Whatever is left over is the accumulated imbalance, as a debit surplus.
  SELECT COALESCE(SUM(amount), 0) INTO imbalance
  FROM journal_lines WHERE entry_id = v_entry_id;

  IF imbalance = 0 THEN
    RAISE NOTICE '042: books already balance, no opening equity needed';
    RETURN;
  END IF;

  -- Cash that no transaction explains. Clamped into [0, imbalance]: never book
  -- more capital than the imbalance actually is, and never book negative capital
  -- (an account short of what its transactions imply is a different problem, and
  -- belongs in the residual).
  SELECT COALESCE(SUM(unexplained), 0) INTO injected_cash
  FROM cash_reconciliation WHERE unexplained > 0;

  capital_amount := GREATEST(0, LEAST(injected_cash, imbalance));
  residual       := imbalance - capital_amount;

  IF capital_amount <> 0 THEN
    INSERT INTO journal_lines (entry_id, account_id, amount, memo)
    VALUES (v_entry_id, capital_acct, -capital_amount,
            'setoran modal awal saat migrasi sistem');
    UPDATE accounts SET balance = balance + capital_amount WHERE id = capital_acct;
  END IF;

  IF residual <> 0 THEN
    INSERT INTO journal_lines (entry_id, account_id, amount, memo)
    VALUES (v_entry_id, residual_acct, -residual,
            'selisih akumulasi sebelum jurnal ada — belum diidentifikasi');
    UPDATE accounts SET balance = balance + residual WHERE id = residual_acct;
  END IF;

  RAISE NOTICE '042: modal awal %, selisih belum teridentifikasi %', capital_amount, residual;
END $$;
