-- The accounts themselves are deliberately left behind: once a journal entry
-- references one, deleting it destroys the audit trail that entry belongs to.
-- Rolling back only unlinks them.
ALTER TABLE branches DROP COLUMN IF EXISTS petty_cash_account_id;
