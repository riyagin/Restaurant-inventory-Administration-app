-- Vendor transfer accounts.
--
-- A vendor is paid by bank transfer, and one vendor routinely has more than one
-- destination: a company account and the owner's personal one, BCA for some
-- deliveries and Mandiri for others, a new number after a bank switch while the
-- old one is still on unpaid invoices. Modelling it as a column on `vendors`
-- would force the office to overwrite the old number to record a new one, so it
-- is its own table from the start.
--
-- Nothing here touches the ledger: these are payment instructions, not accounts
-- in the chart of accounts. The vendor's payable sub-account (20101+) remains
-- the only accounting object a vendor owns.

CREATE TABLE IF NOT EXISTS vendor_bank_accounts (
  id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  vendor_id      UUID        NOT NULL REFERENCES vendors(id) ON DELETE CASCADE,
  bank_name      TEXT        NOT NULL,
  account_number TEXT        NOT NULL,
  -- The name the account is registered under. Often not the vendor's own name
  -- (a supplier billing through its owner's personal account), which is exactly
  -- why it has to be recorded rather than assumed.
  account_holder TEXT        NOT NULL DEFAULT '',
  -- Bank branch / KCP, printed on some invoices and occasionally required.
  bank_branch    TEXT        NOT NULL DEFAULT '',
  is_primary     BOOLEAN     NOT NULL DEFAULT false,
  note           TEXT        NOT NULL DEFAULT '',
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_vendor_bank_accounts_vendor
  ON vendor_bank_accounts (vendor_id);

-- At most one default destination per vendor, so "which number do I pay?" has a
-- single answer. Enforced in the database rather than the handler because the
-- flag is flipped from more than one place.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vendor_bank_accounts_primary
  ON vendor_bank_accounts (vendor_id) WHERE is_primary;

-- The same number typed twice for one vendor is a duplicate, not a second
-- account — compare on digits alone so "1234 5678 90" and "1234567890" collide.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vendor_bank_accounts_unique
  ON vendor_bank_accounts (vendor_id, lower(bank_name), regexp_replace(account_number, '[^0-9]', '', 'g'));
