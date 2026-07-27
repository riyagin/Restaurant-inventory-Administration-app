-- 037 (down): remove per-branch wage accounts created by the up migration.
--
-- Only deletes accounts that carry no postings, so any wage account that has
-- already been used by a payroll journal (has account_adjustments) is preserved
-- to protect financial history. Wage accounts with a live balance are also kept.

DELETE FROM accounts a
WHERE a.name LIKE 'Beban Gaji - %'
  AND a.account_type = 'expense'
  AND a.balance = 0
  AND NOT EXISTS (
    SELECT 1 FROM account_adjustments aa WHERE aa.account_id = a.id
  );
