-- 040: Double-entry journal.
--
-- Until now `accounts.balance` was the only record of a financial event: every
-- handler mutated it in place and was individually responsible for remembering
-- both legs of the entry. Several forgot one (POS expense lines, payroll close,
-- THR close, single-account adjustments, kasbon repayments), and because nothing
-- recorded the *intended* entry those mistakes were neither detectable nor
-- reversible after the fact. The books drifted ~982M out of balance.
--
-- This adds the missing substrate:
--
--   journal_entries  one financial event (an invoice, a dispatch, a payroll close)
--   journal_lines    its legs — signed, debit-positive, must sum to zero
--
-- `accounts.balance` stays as a read cache so reports and handlers keep working,
-- but from here it is only ever written by service.Post, which writes the journal
-- and the cache together in one transaction. A deferred constraint trigger makes
-- an unbalanced entry impossible to commit, so a single-legged posting is now a
-- write error rather than silent corruption.
--
-- Sign convention: `amount` is debit-positive.
--     debit  (Dr) → amount > 0
--     credit (Cr) → amount < 0
-- The effect on the cached `accounts.balance`, which is natural-sign per type:
--     asset, expense              → balance += amount
--     liability, equity, revenue  → balance -= amount
-- so a balanced entry (sum = 0) always leaves A = L + E + R - Exp intact.

CREATE TABLE IF NOT EXISTS journal_entries (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  entry_date  DATE        NOT NULL,
  source_type TEXT        NOT NULL,
  source_id   UUID,
  description TEXT        NOT NULL DEFAULT '',
  created_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- Set when this entry exists to reverse an earlier one. Reversals are appended,
  -- never applied by rewriting the original — same convention the invoice
  -- edit/delete paths already follow.
  reverses_id UUID REFERENCES journal_entries(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS journal_lines (
  id         UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
  entry_id   UUID   NOT NULL REFERENCES journal_entries(id) ON DELETE CASCADE,
  account_id UUID   NOT NULL REFERENCES accounts(id),
  -- Debit-positive. Zero-amount legs carry no information and are rejected so
  -- they cannot pad an entry into looking two-sided.
  amount     BIGINT NOT NULL CHECK (amount <> 0),
  memo       TEXT   NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS journal_entries_date_idx   ON journal_entries (entry_date DESC);
CREATE INDEX IF NOT EXISTS journal_entries_source_idx ON journal_entries (source_type, source_id);
CREATE INDEX IF NOT EXISTS journal_lines_entry_idx    ON journal_lines (entry_id);
CREATE INDEX IF NOT EXISTS journal_lines_account_idx  ON journal_lines (account_id);

-- ── Balance enforcement ──────────────────────────────────────────────────────
--
-- Deferred so the entry and its lines can be inserted in any order within a
-- transaction; the check runs once at COMMIT. This is the guarantee that the
-- Go-side validation in service.Post is a convenience rather than the only thing
-- standing between the books and a single-legged write.

CREATE OR REPLACE FUNCTION assert_journal_entry_balanced() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
  eid UUID;
  net BIGINT;
  cnt INT;
BEGIN
  IF TG_TABLE_NAME = 'journal_entries' THEN
    eid := NEW.id;
  ELSIF TG_OP = 'DELETE' THEN
    eid := OLD.entry_id;
  ELSE
    eid := NEW.entry_id;
  END IF;

  -- The whole entry was deleted and its lines cascaded away: nothing to assert.
  IF NOT EXISTS (SELECT 1 FROM journal_entries WHERE id = eid) THEN
    RETURN NULL;
  END IF;

  SELECT COALESCE(SUM(amount), 0), COUNT(*) INTO net, cnt
  FROM journal_lines WHERE entry_id = eid;

  IF cnt < 2 THEN
    RAISE EXCEPTION 'jurnal %: entri harus memiliki minimal 2 baris (ditemukan %)', eid, cnt
      USING ERRCODE = 'check_violation';
  END IF;

  IF net <> 0 THEN
    RAISE EXCEPTION 'jurnal % tidak seimbang: selisih debit-kredit = %', eid, net
      USING ERRCODE = 'check_violation';
  END IF;

  RETURN NULL;
END $$;

DROP TRIGGER IF EXISTS journal_entries_balanced ON journal_entries;
CREATE CONSTRAINT TRIGGER journal_entries_balanced
  AFTER INSERT OR UPDATE ON journal_entries
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION assert_journal_entry_balanced();

DROP TRIGGER IF EXISTS journal_lines_balanced ON journal_lines;
CREATE CONSTRAINT TRIGGER journal_lines_balanced
  AFTER INSERT OR UPDATE OR DELETE ON journal_lines
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION assert_journal_entry_balanced();

-- ── Equity accounts ──────────────────────────────────────────────────────────
--
-- "Ekuitas" (30000) has always existed but never held a balance, which is why
-- cash carried over from the old system had nothing to be credited against.
--
--   30100 Modal Pemilik  permanent owner's capital
--   30900 Saldo Awal     migration suspense: the counter-leg for every opening
--                        balance. Once all opening balances are entered it holds
--                        net worth at cutover and is closed to 30100.
--   30990 Selisih Migrasi  quarantine for drift that predates the journal and
--                        cannot be attributed to a real transaction. Named so it
--                        is impossible to mistake for a real equity balance.
--
-- Created at zero here; 041 posts the opening entry that populates them.

DO $$
DECLARE
  equity_root UUID;
BEGIN
  SELECT id INTO equity_root
  FROM accounts WHERE account_number = 30000 AND is_system = true LIMIT 1;

  IF equity_root IS NULL THEN
    INSERT INTO accounts (id, name, account_number, account_type, balance, is_system)
    VALUES (gen_random_uuid(), 'Ekuitas', 30000, 'equity', 0, true)
    RETURNING id INTO equity_root;
  END IF;

  INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
  SELECT gen_random_uuid(), 'Modal Pemilik', 30100, 'equity', equity_root, 0, true
  WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE account_number = 30100);

  INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
  SELECT gen_random_uuid(), 'Saldo Awal', 30900, 'equity', equity_root, 0, true
  WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE account_number = 30900);

  INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
  SELECT gen_random_uuid(), 'Selisih Migrasi', 30990, 'equity', equity_root, 0, true
  WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE account_number = 30990);

  -- Counter-leg for single-account manual adjustments. That screen lets an admin
  -- move one account's balance without naming a counterparty, which is a
  -- one-sided entry by construction — the balancing half has to land somewhere,
  -- and equity is where an unexplained balance movement belongs. A growing
  -- balance here means the adjustment screen is being used as a crutch for
  -- something that should be a real transaction or a transfer.
  INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
  SELECT gen_random_uuid(), 'Penyesuaian Saldo Manual', 30800, 'equity', equity_root, 0, true
  WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE account_number = 30800);
END $$;

-- ── Suspense account ─────────────────────────────────────────────────────────
--
-- Where a leg lands when its account cannot be resolved at posting time — a
-- warehouse with no inventory account, a branch with no expense account. The
-- old code skipped those legs, which is precisely how the books came apart.
-- service.Post redirects them here instead, so the entry still balances and the
-- misconfiguration shows up as a non-zero balance in an account named for it.
-- A non-zero 19999 always means something needs reclassifying.

DO $$
DECLARE
  asset_root UUID;
BEGIN
  SELECT id INTO asset_root
  FROM accounts WHERE account_number = 10000 AND is_system = true LIMIT 1;

  INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
  SELECT gen_random_uuid(), 'Akun Sementara', 19999, 'asset', asset_root, 0, true
  WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE account_number = 19999);
END $$;
