-- ============================================================
-- FULL RESET + SEED
-- ============================================================

-- Disable triggers temporarily to avoid FK issues during truncation
SET session_replication_role = replica;

TRUNCATE TABLE
  activity_log,
  sales,
  stock_opname_items,
  stock_opname,
  dispatch_items,
  dispatches,
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
  accounts,
  users
RESTART IDENTITY CASCADE;

SET session_replication_role = DEFAULT;

-- Reset sequences
ALTER SEQUENCE IF EXISTS invoice_seq RESTART WITH 1;

-- ============================================================
-- USERS
-- ============================================================
INSERT INTO users (username, password_hash, role)
VALUES ('admin', crypt('admin', gen_salt('bf')), 'admin');

-- ============================================================
-- CHART OF ACCOUNTS
-- ============================================================

-- Root categories
INSERT INTO accounts (account_number, name, account_type, balance, is_system) VALUES
  (10000, 'Aset',       'asset',     0, true),
  (20000, 'Kewajiban',  'liability', 0, true),
  (30000, 'Ekuitas',    'equity',    0, true),
  (40000, 'Pendapatan', 'revenue',   0, true),
  (50000, 'Beban',      'expense',   0, true);

-- Sub-groups under Assets
INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 11000, 'Kas dan Setara Kas', 'asset', 0, id, true FROM accounts WHERE account_number = 10000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 12000, 'Persediaan',         'asset', 0, id, true FROM accounts WHERE account_number = 10000;

-- Liabilities
INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 20100, 'Utang Usaha', 'liability', 0, id, true FROM accounts WHERE account_number = 20000;

-- ── Cash accounts (under 11000) ──────────────────────────────
INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 11100, 'Kas Besar', 'asset', 50000000, id, false FROM accounts WHERE account_number = 11000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 11200, 'Bank BCA', 'asset', 200000000, id, false FROM accounts WHERE account_number = 11000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 11300, 'Bank Mandiri', 'asset', 150000000, id, false FROM accounts WHERE account_number = 11000;

-- ── Inventory accounts per warehouse (under 12000) ───────────
INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 12100, 'Persediaan - Gudang Utama', 'asset', 0, id, false FROM accounts WHERE account_number = 12000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 12200, 'Persediaan - Gudang Cabang', 'asset', 0, id, false FROM accounts WHERE account_number = 12000;

-- ── Revenue accounts (under 40000) ───────────────────────────
INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 41000, 'Pendapatan - Jakarta', 'revenue', 0, id, false FROM accounts WHERE account_number = 40000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 41100, 'Pendapatan - Jakarta Operasional', 'revenue', 0, id, false FROM accounts WHERE account_number = 41000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 41200, 'Pendapatan - Jakarta Proyek',      'revenue', 0, id, false FROM accounts WHERE account_number = 41000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 42000, 'Pendapatan - Surabaya', 'revenue', 0, id, false FROM accounts WHERE account_number = 40000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 42100, 'Pendapatan - Surabaya Operasional', 'revenue', 0, id, false FROM accounts WHERE account_number = 42000;

-- ── Expense accounts (under 50000) ───────────────────────────
INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 51000, 'Beban - Jakarta', 'expense', 0, id, false FROM accounts WHERE account_number = 50000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 51100, 'Beban - Jakarta Operasional', 'expense', 0, id, false FROM accounts WHERE account_number = 51000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 51200, 'Beban - Jakarta Proyek',      'expense', 0, id, false FROM accounts WHERE account_number = 51000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 52000, 'Beban - Surabaya', 'expense', 0, id, false FROM accounts WHERE account_number = 50000;

INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
SELECT 52100, 'Beban - Surabaya Operasional', 'expense', 0, id, false FROM accounts WHERE account_number = 52000;

-- ============================================================
-- WAREHOUSES  (linked to inventory accounts)
-- ============================================================
INSERT INTO warehouses (name, inventory_account_id)
SELECT 'Gudang Utama', id FROM accounts WHERE account_number = 12100;

INSERT INTO warehouses (name, inventory_account_id)
SELECT 'Gudang Cabang', id FROM accounts WHERE account_number = 12200;

-- ============================================================
-- VENDORS
-- ============================================================
INSERT INTO vendors (name) VALUES
  ('PT Sumber Makmur'),
  ('CV Jaya Abadi'),
  ('UD Mitra Setia');

-- ============================================================
-- BRANCHES & DIVISIONS  (with linked revenue/expense accounts)
-- ============================================================

-- Branch: Jakarta
INSERT INTO branches (name, revenue_account_id, expense_account_id)
SELECT 'Jakarta',
  (SELECT id FROM accounts WHERE account_number = 41000),
  (SELECT id FROM accounts WHERE account_number = 51000);

-- Divisions under Jakarta
INSERT INTO divisions (branch_id, name, revenue_account_id, expense_account_id)
SELECT b.id, 'Operasional',
  (SELECT id FROM accounts WHERE account_number = 41100),
  (SELECT id FROM accounts WHERE account_number = 51100)
FROM branches b WHERE b.name = 'Jakarta';

INSERT INTO divisions (branch_id, name, revenue_account_id, expense_account_id)
SELECT b.id, 'Proyek',
  (SELECT id FROM accounts WHERE account_number = 41200),
  (SELECT id FROM accounts WHERE account_number = 51200)
FROM branches b WHERE b.name = 'Jakarta';

-- Branch: Surabaya
INSERT INTO branches (name, revenue_account_id, expense_account_id)
SELECT 'Surabaya',
  (SELECT id FROM accounts WHERE account_number = 42000),
  (SELECT id FROM accounts WHERE account_number = 52000);

INSERT INTO divisions (branch_id, name, revenue_account_id, expense_account_id)
SELECT b.id, 'Operasional',
  (SELECT id FROM accounts WHERE account_number = 42100),
  (SELECT id FROM accounts WHERE account_number = 52100)
FROM branches b WHERE b.name = 'Surabaya';

-- ============================================================
-- ITEMS
-- ============================================================
-- units JSON: array of { name, perPrev } where perPrev is how many of this unit make 1 of the previous unit
-- The first unit has no perPrev (or perPrev=1). The last unit is the lowest/base.

INSERT INTO items (name, code, units, is_stock) VALUES
  ('Cat Tembok',   'CTK-001',
   '[{"name":"Kaleng 5L"},{"name":"Liter","perPrev":5}]'::jsonb,  true),

  ('Pipa PVC 4"',  'PVC-001',
   '[{"name":"Batang"},{"name":"Meter","perPrev":4}]'::jsonb, true),

  ('Kabel NYM 2x2.5',  'KBL-001',
   '[{"name":"Roll 100m"},{"name":"Meter","perPrev":100}]'::jsonb, true),

  ('Semen Portland', 'SMN-001',
   '[{"name":"Sak 40kg"},{"name":"Kg","perPrev":40}]'::jsonb,  true),

  ('Besi Beton 10mm', 'BSI-001',
   '[{"name":"Batang 12m"},{"name":"Meter","perPrev":12}]'::jsonb, true),

  ('Sarung Tangan Kerja', 'SGT-001',
   '[{"name":"Lusin"},{"name":"Pcs","perPrev":12}]'::jsonb, false),

  ('Solar Industri', 'SOL-001',
   '[{"name":"Drum 200L"},{"name":"Liter","perPrev":200}]'::jsonb, false);

-- ============================================================
-- INVOICES  (purchases spread over the past 4 weeks)
-- NOTE: We insert invoice items, update inventory lots, and
--       update account balances manually to be consistent.
-- ============================================================

DO $$
DECLARE
  wh_main   UUID := (SELECT id FROM warehouses WHERE name = 'Gudang Utama');
  wh_branch UUID := (SELECT id FROM warehouses WHERE name = 'Gudang Cabang');

  v_sumber  UUID := (SELECT id FROM vendors WHERE name = 'PT Sumber Makmur');
  v_jaya    UUID := (SELECT id FROM vendors WHERE name = 'CV Jaya Abadi');
  v_mitra   UUID := (SELECT id FROM vendors WHERE name = 'UD Mitra Setia');

  acct_bca     UUID := (SELECT id FROM accounts WHERE account_number = 11200);
  acct_mandiri UUID := (SELECT id FROM accounts WHERE account_number = 11300);
  acct_ap      UUID := (SELECT id FROM accounts WHERE account_number = 20100);
  acct_inv_main   UUID := (SELECT id FROM accounts WHERE account_number = 12100);
  acct_inv_branch UUID := (SELECT id FROM accounts WHERE account_number = 12200);

  item_cat    UUID := (SELECT id FROM items WHERE code = 'CTK-001');
  item_pvc    UUID := (SELECT id FROM items WHERE code = 'PVC-001');
  item_kabel  UUID := (SELECT id FROM items WHERE code = 'KBL-001');
  item_semen  UUID := (SELECT id FROM items WHERE code = 'SMN-001');
  item_besi   UUID := (SELECT id FROM items WHERE code = 'BSI-001');

  inv_id   UUID;
  total    BIGINT;
BEGIN

  -- ── Invoice 1 · 4 weeks ago · Purchase · Paid (BCA) ─────────
  -- Cat Tembok: 20 kaleng → 100 Liter @ Rp 85.000/kaleng  = 1.700.000
  -- Semen:      50 sak    → 2000 Kg  @ Rp 62.000/sak      = 3.100.000
  total := 20*85000 + 50*62000;
  INSERT INTO invoices (invoice_number, date, warehouse_id, vendor_id, payment_status, account_id, invoice_type)
  VALUES ('INV-0001', CURRENT_DATE - 28, wh_main, v_sumber, 'paid', acct_bca, 'purchase')
  RETURNING id INTO inv_id;

  INSERT INTO invoice_items (invoice_id, item_id, vendor_id, unit_index, quantity, price)
  VALUES (inv_id, item_cat,   v_sumber, 0, 20, 85000),
         (inv_id, item_semen, v_sumber, 0, 50, 62000);

  -- Inventory lots (stored at lowest unit index)
  -- Cat: 20 kaleng → lowestIdx=1, 20×5=100 Liter
  INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date)
  VALUES (item_cat,   wh_main, 100,  1, 20*85000,  CURRENT_DATE - 28),
         (item_semen, wh_main, 2000, 1, 50*62000,  CURRENT_DATE - 28);

  UPDATE accounts SET balance = balance - total WHERE id = acct_bca;
  UPDATE accounts SET balance = balance + total WHERE id = acct_inv_main;

  -- ── Invoice 2 · 3.5 weeks ago · Purchase · Paid (Mandiri) ───
  -- Pipa PVC: 30 batang → 120 Meter @ Rp 45.000/batang = 1.350.000
  -- Kabel:    5 roll    → 500 Meter @ Rp 320.000/roll  = 1.600.000
  total := 30*45000 + 5*320000;
  INSERT INTO invoices (invoice_number, date, warehouse_id, vendor_id, payment_status, account_id, invoice_type)
  VALUES ('INV-0002', CURRENT_DATE - 25, wh_main, v_jaya, 'paid', acct_mandiri, 'purchase')
  RETURNING id INTO inv_id;

  INSERT INTO invoice_items (invoice_id, item_id, vendor_id, unit_index, quantity, price)
  VALUES (inv_id, item_pvc,   v_jaya, 0, 30, 45000),
         (inv_id, item_kabel, v_jaya, 0, 5,  320000);

  INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date)
  VALUES (item_pvc,   wh_main, 120,  1, 30*45000,   CURRENT_DATE - 25),
         (item_kabel, wh_main, 500,  1, 5*320000,   CURRENT_DATE - 25);

  UPDATE accounts SET balance = balance - total WHERE id = acct_mandiri;
  UPDATE accounts SET balance = balance + total WHERE id = acct_inv_main;

  -- ── Invoice 3 · 3 weeks ago · Purchase · Unpaid ──────────────
  -- Besi Beton: 40 batang → 480 Meter @ Rp 95.000/batang = 3.800.000
  total := 40*95000;
  INSERT INTO invoices (invoice_number, date, warehouse_id, vendor_id, payment_status, invoice_type)
  VALUES ('INV-0003', CURRENT_DATE - 21, wh_main, v_mitra, 'unpaid', 'purchase')
  RETURNING id INTO inv_id;

  INSERT INTO invoice_items (invoice_id, item_id, vendor_id, unit_index, quantity, price)
  VALUES (inv_id, item_besi, v_mitra, 0, 40, 95000);

  INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date)
  VALUES (item_besi, wh_main, 480, 1, 40*95000, CURRENT_DATE - 21);

  UPDATE accounts SET balance = balance + total WHERE id = acct_ap;
  UPDATE accounts SET balance = balance + total WHERE id = acct_inv_main;

  -- ── Invoice 4 · 2 weeks ago · Purchase · Paid (BCA) ─────────
  -- Cat Tembok: 15 kaleng → 75 Liter @ Rp 87.000/kaleng = 1.305.000  (price up)
  -- Semen:      30 sak    → 1200 Kg  @ Rp 63.000/sak   = 1.890.000
  total := 15*87000 + 30*63000;
  INSERT INTO invoices (invoice_number, date, warehouse_id, vendor_id, payment_status, account_id, invoice_type)
  VALUES ('INV-0004', CURRENT_DATE - 14, wh_main, v_sumber, 'paid', acct_bca, 'purchase')
  RETURNING id INTO inv_id;

  INSERT INTO invoice_items (invoice_id, item_id, vendor_id, unit_index, quantity, price)
  VALUES (inv_id, item_cat,   v_sumber, 0, 15, 87000),
         (inv_id, item_semen, v_sumber, 0, 30, 63000);

  -- New FIFO lots at higher price
  INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date)
  VALUES (item_cat,   wh_main, 75,   1, 15*87000,  CURRENT_DATE - 14),
         (item_semen, wh_main, 1200, 1, 30*63000,  CURRENT_DATE - 14);

  UPDATE accounts SET balance = balance - total WHERE id = acct_bca;
  UPDATE accounts SET balance = balance + total WHERE id = acct_inv_main;

  -- ── Invoice 5 · 10 days ago · Purchase · Paid (Mandiri) · Gudang Cabang ──
  -- Pipa PVC: 20 batang → 80 Meter @ Rp 46.000/batang = 920.000
  -- Kabel:    3 roll    → 300 Meter @ Rp 325.000/roll = 975.000
  total := 20*46000 + 3*325000;
  INSERT INTO invoices (invoice_number, date, warehouse_id, vendor_id, payment_status, account_id, invoice_type)
  VALUES ('INV-0005', CURRENT_DATE - 10, wh_branch, v_jaya, 'paid', acct_mandiri, 'purchase')
  RETURNING id INTO inv_id;

  INSERT INTO invoice_items (invoice_id, item_id, vendor_id, unit_index, quantity, price)
  VALUES (inv_id, item_pvc,   v_jaya, 0, 20, 46000),
         (inv_id, item_kabel, v_jaya, 0, 3,  325000);

  INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date)
  VALUES (item_pvc,   wh_branch, 80,  1, 20*46000,   CURRENT_DATE - 10),
         (item_kabel, wh_branch, 300, 1, 3*325000,   CURRENT_DATE - 10);

  UPDATE accounts SET balance = balance - total WHERE id = acct_mandiri;
  UPDATE accounts SET balance = balance + total WHERE id = acct_inv_branch;

  -- ── Invoice 6 · 1 week ago · Purchase · Unpaid ───────────────
  -- Besi Beton: 20 batang → 240 Meter @ Rp 96.000/batang = 1.920.000
  total := 20*96000;
  INSERT INTO invoices (invoice_number, date, warehouse_id, vendor_id, payment_status, invoice_type)
  VALUES ('INV-0006', CURRENT_DATE - 7, wh_main, v_mitra, 'unpaid', 'purchase')
  RETURNING id INTO inv_id;

  INSERT INTO invoice_items (invoice_id, item_id, vendor_id, unit_index, quantity, price)
  VALUES (inv_id, item_besi, v_mitra, 0, 20, 96000);

  INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date)
  VALUES (item_besi, wh_main, 240, 1, 20*96000, CURRENT_DATE - 7);

  UPDATE accounts SET balance = balance + total WHERE id = acct_ap;
  UPDATE accounts SET balance = balance + total WHERE id = acct_inv_main;

END $$;

-- ============================================================
-- STOCK HISTORY  (matching the purchases above)
-- ============================================================
INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_type, value)
SELECT i.id, w.id, 100,  'Liter',    'PT Sumber Makmur', 'invoice', 'INV-0001', CURRENT_DATE-28, 'invoice', 1700000
FROM items i, warehouses w WHERE i.code='CTK-001' AND w.name='Gudang Utama';

INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_type, value)
SELECT i.id, w.id, 2000, 'Kg',       'PT Sumber Makmur', 'invoice', 'INV-0001', CURRENT_DATE-28, 'invoice', 3100000
FROM items i, warehouses w WHERE i.code='SMN-001' AND w.name='Gudang Utama';

INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_type, value)
SELECT i.id, w.id, 120,  'Meter',    'CV Jaya Abadi',    'invoice', 'INV-0002', CURRENT_DATE-25, 'invoice', 1350000
FROM items i, warehouses w WHERE i.code='PVC-001' AND w.name='Gudang Utama';

INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_type, value)
SELECT i.id, w.id, 500,  'Meter',    'CV Jaya Abadi',    'invoice', 'INV-0002', CURRENT_DATE-25, 'invoice', 1600000
FROM items i, warehouses w WHERE i.code='KBL-001' AND w.name='Gudang Utama';

INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_type, value)
SELECT i.id, w.id, 480,  'Meter',    'UD Mitra Setia',   'invoice', 'INV-0003', CURRENT_DATE-21, 'invoice', 3800000
FROM items i, warehouses w WHERE i.code='BSI-001' AND w.name='Gudang Utama';

INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_type, value)
SELECT i.id, w.id, 75,   'Liter',    'PT Sumber Makmur', 'invoice', 'INV-0004', CURRENT_DATE-14, 'invoice', 1305000
FROM items i, warehouses w WHERE i.code='CTK-001' AND w.name='Gudang Utama';

INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_type, value)
SELECT i.id, w.id, 1200, 'Kg',       'PT Sumber Makmur', 'invoice', 'INV-0004', CURRENT_DATE-14, 'invoice', 1890000
FROM items i, warehouses w WHERE i.code='SMN-001' AND w.name='Gudang Utama';

INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_type, value)
SELECT i.id, w.id, 80,   'Meter',    'CV Jaya Abadi',    'invoice', 'INV-0005', CURRENT_DATE-10, 'invoice', 920000
FROM items i, warehouses w WHERE i.code='PVC-001' AND w.name='Gudang Cabang';

INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_type, value)
SELECT i.id, w.id, 300,  'Meter',    'CV Jaya Abadi',    'invoice', 'INV-0005', CURRENT_DATE-10, 'invoice', 975000
FROM items i, warehouses w WHERE i.code='KBL-001' AND w.name='Gudang Cabang';

INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_type, value)
SELECT i.id, w.id, 240,  'Meter',    'UD Mitra Setia',   'invoice', 'INV-0006', CURRENT_DATE-7,  'invoice', 1920000
FROM items i, warehouses w WHERE i.code='BSI-001' AND w.name='Gudang Utama';

-- ============================================================
-- Sync the invoice_seq so new invoices don't collide
-- ============================================================
SELECT setval('invoice_seq', 6);

-- ============================================================
-- VERIFICATION SUMMARY
-- ============================================================
SELECT 'accounts'      AS tbl, COUNT(*) FROM accounts
UNION ALL SELECT 'warehouses',  COUNT(*) FROM warehouses
UNION ALL SELECT 'vendors',     COUNT(*) FROM vendors
UNION ALL SELECT 'branches',    COUNT(*) FROM branches
UNION ALL SELECT 'divisions',   COUNT(*) FROM divisions
UNION ALL SELECT 'items',       COUNT(*) FROM items
UNION ALL SELECT 'inventory',   COUNT(*) FROM inventory
UNION ALL SELECT 'invoices',    COUNT(*) FROM invoices
ORDER BY 1;

SELECT account_number, name, account_type, balance
FROM accounts ORDER BY account_number;
