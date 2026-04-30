-- ============================================================
--  PRODUCTION RESET
--  Wipes all data and dummy records.
--  Keeps only system accounts (root COA structure).
--  Creates a single admin user — change password on first login.
-- ============================================================

SET session_replication_role = replica;

TRUNCATE TABLE
  activity_log,
  account_adjustments,
  pos_import_lines,
  pos_imports,
  sales,
  stock_opname_items,
  stock_opname,
  dispatch_items,
  dispatches,
  productions,
  recipe_ingredients,
  recipes,
  stock_transfers,
  stock_history,
  invoice_items,
  invoices,
  inventory,
  items,
  vendors,
  divisions,
  branches,
  warehouses,
  token_blocklist,
  users
RESTART IDENTITY CASCADE;

-- Delete all non-system accounts (dummy cash, inventory, branch accounts)
DELETE FROM accounts WHERE is_system = false;

-- Reset all system account balances to zero
UPDATE accounts SET balance = 0 WHERE is_system = true;

SET session_replication_role = DEFAULT;

-- Reset invoice sequence
ALTER SEQUENCE IF EXISTS invoice_seq RESTART WITH 1;

-- ============================================================
--  ADMIN USER
--  Default password: admin123
--  Change this immediately after first login.
-- ============================================================
INSERT INTO users (username, password_hash, role)
VALUES ('admin', '$2b$10$105VinpgjzGk8sUNf6XHOeWGfeYKfpqza9.9qjuE9WYOeVoXBA1vS', 'admin');

-- ============================================================
--  VERIFICATION
-- ============================================================
SELECT 'users'    AS tbl, COUNT(*) FROM users
UNION ALL SELECT 'accounts',  COUNT(*) FROM accounts
UNION ALL SELECT 'warehouses', COUNT(*) FROM warehouses
UNION ALL SELECT 'vendors',   COUNT(*) FROM vendors
UNION ALL SELECT 'branches',  COUNT(*) FROM branches
UNION ALL SELECT 'items',     COUNT(*) FROM items
UNION ALL SELECT 'invoices',  COUNT(*) FROM invoices
ORDER BY 1;

SELECT account_number, name, account_type, balance
FROM accounts ORDER BY account_number;
