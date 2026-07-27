-- 037: Per-branch wage (payroll) expense accounts.
--
-- Creates one expense account "Beban Gaji - <Branch>" for every branch, nested
-- under that branch's existing expense parent (branches.expense_account_id).
-- The account number is the parent's number + 900 (e.g. 51000 -> 51900), nudged
-- upward on the rare chance that number is already taken. If a branch has no
-- expense parent, the account is placed under the root "Beban" (50000).
--
-- The payroll "Proses ke Akuntansi" action credits cash and debits these
-- accounts, so they must exist before payroll can be posted to the ledger.
--
-- Idempotent: safe to run more than once. Branches that already have their
-- "Beban Gaji - <name>" account are skipped, so re-running only fills in gaps
-- (e.g. after a new branch is added).

DO $$
DECLARE
  b            RECORD;
  root_beban   UUID;
  parent_id_v  UUID;
  parent_num_v INT;
  new_number   INT;
BEGIN
  SELECT id INTO root_beban
  FROM accounts
  WHERE account_number = 50000 AND is_system = true
  LIMIT 1;

  FOR b IN SELECT id, name, expense_account_id FROM branches ORDER BY name LOOP
    -- Already seeded for this branch → skip.
    IF EXISTS (SELECT 1 FROM accounts WHERE name = 'Beban Gaji - ' || b.name) THEN
      CONTINUE;
    END IF;

    parent_id_v  := NULL;
    parent_num_v := NULL;

    IF b.expense_account_id IS NOT NULL THEN
      SELECT id, account_number INTO parent_id_v, parent_num_v
      FROM accounts WHERE id = b.expense_account_id;
    END IF;

    -- Fall back to the root "Beban" parent if the branch has no expense parent.
    IF parent_id_v IS NULL THEN
      parent_id_v  := root_beban;
      parent_num_v := 50000;
    END IF;

    new_number := COALESCE(parent_num_v, 50000) + 900;
    WHILE EXISTS (SELECT 1 FROM accounts WHERE account_number = new_number) LOOP
      new_number := new_number + 1;
    END LOOP;

    INSERT INTO accounts (id, name, account_number, account_type, parent_id, balance, is_system)
    VALUES (gen_random_uuid(), 'Beban Gaji - ' || b.name, new_number, 'expense', parent_id_v, 0, false);
  END LOOP;
END $$;
