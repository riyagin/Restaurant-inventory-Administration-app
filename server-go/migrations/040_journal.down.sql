-- Reverses 040. The equity accounts are dropped only if nothing ever posted to
-- them; a non-zero balance means real entries exist and dropping them would
-- silently unbalance the books, so they are left in place.

DROP TRIGGER IF EXISTS journal_lines_balanced ON journal_lines;
DROP TRIGGER IF EXISTS journal_entries_balanced ON journal_entries;
DROP FUNCTION IF EXISTS assert_journal_entry_balanced();

DROP TABLE IF EXISTS journal_lines;
DROP TABLE IF EXISTS journal_entries;

DELETE FROM accounts
WHERE account_number IN (19999, 30100, 30800, 30900, 30990)
  AND balance = 0
  AND NOT EXISTS (SELECT 1 FROM accounts c WHERE c.parent_id = accounts.id);
