CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS users (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  username      TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL DEFAULT 'admin',
  created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS warehouses (
  id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS vendors (
  id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS accounts (
  id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name    TEXT NOT NULL,
  balance BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS branches (
  id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS divisions (
  id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id UUID REFERENCES branches(id),
  name      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS items (
  id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name     TEXT NOT NULL,
  code     TEXT UNIQUE NOT NULL,
  units    JSONB NOT NULL DEFAULT '[]',
  is_stock BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS inventory (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  item_id      UUID REFERENCES items(id),
  warehouse_id UUID REFERENCES warehouses(id),
  quantity     NUMERIC NOT NULL DEFAULT 0,
  unit_index   INT NOT NULL DEFAULT 0,
  value        BIGINT NOT NULL DEFAULT 0,
  date         DATE NOT NULL DEFAULT CURRENT_DATE
);

CREATE TABLE IF NOT EXISTS invoices (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_number   TEXT,
  invoice_type     TEXT NOT NULL DEFAULT 'purchase',
  date             DATE NOT NULL DEFAULT CURRENT_DATE,
  warehouse_id     UUID REFERENCES warehouses(id),
  vendor_id        UUID REFERENCES vendors(id),
  account_id       UUID REFERENCES accounts(id),
  branch_id        UUID REFERENCES branches(id),
  division_id      UUID REFERENCES divisions(id),
  dispatch_id      UUID,
  payment_status   TEXT NOT NULL DEFAULT 'unpaid',
  payment_method   TEXT,
  photo_path       TEXT,
  reference_number TEXT,
  created_at       TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS invoice_items (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id   UUID REFERENCES invoices(id) ON DELETE CASCADE,
  item_id      UUID REFERENCES items(id),
  vendor_id    UUID REFERENCES vendors(id),
  unit_index   INT,
  quantity     NUMERIC,
  price        BIGINT,
  description  TEXT
);

CREATE TABLE IF NOT EXISTS stock_history (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  item_id         UUID REFERENCES items(id),
  warehouse_id    UUID REFERENCES warehouses(id),
  quantity_change NUMERIC NOT NULL,
  unit_name       TEXT,
  vendor          TEXT,
  type            TEXT NOT NULL,
  reference       TEXT,
  source_id       UUID,
  source_type     TEXT,
  value           BIGINT,
  date            DATE NOT NULL DEFAULT CURRENT_DATE,
  created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS stock_transfers (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id     UUID,
  item_id      UUID REFERENCES items(id),
  from_wh_id   UUID REFERENCES warehouses(id),
  to_wh_id     UUID REFERENCES warehouses(id),
  quantity     NUMERIC NOT NULL,
  unit_index   INT NOT NULL DEFAULT 0,
  unit_name    TEXT,
  value        BIGINT,
  date         DATE NOT NULL DEFAULT CURRENT_DATE,
  created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS dispatches (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id    UUID REFERENCES branches(id),
  division_id  UUID REFERENCES divisions(id),
  warehouse_id UUID REFERENCES warehouses(id),
  date         DATE NOT NULL DEFAULT CURRENT_DATE,
  notes        TEXT,
  created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS dispatch_items (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  dispatch_id UUID REFERENCES dispatches(id) ON DELETE CASCADE,
  item_id     UUID REFERENCES items(id),
  quantity    NUMERIC NOT NULL,
  unit_index  INT NOT NULL DEFAULT 0,
  unit_name   TEXT,
  value       BIGINT
);

CREATE TABLE IF NOT EXISTS stock_opname (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  warehouse_id  UUID REFERENCES warehouses(id),
  notes         TEXT,
  operator_name TEXT,
  pic_name      TEXT,
  recorded_by   TEXT,
  created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS stock_opname_items (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  opname_id       UUID REFERENCES stock_opname(id) ON DELETE CASCADE,
  item_id         UUID REFERENCES items(id),
  system_qty      NUMERIC,
  actual_qty      NUMERIC,
  unit_index      INT NOT NULL DEFAULT 0,
  unit_name       TEXT,
  difference      NUMERIC
);

CREATE TABLE IF NOT EXISTS sales (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  item_id      UUID REFERENCES items(id),
  warehouse_id UUID REFERENCES warehouses(id),
  quantity     NUMERIC NOT NULL,
  unit_index   INT NOT NULL DEFAULT 0,
  unit_name    TEXT,
  price        BIGINT,
  date         DATE NOT NULL DEFAULT CURRENT_DATE,
  created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS activity_log (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID,
  username    TEXT,
  action      TEXT,
  entity_type TEXT,
  entity_id   UUID,
  description TEXT,
  created_at  TIMESTAMPTZ DEFAULT NOW()
);

INSERT INTO users (username, password_hash, role)
VALUES ('admin', crypt('admin', gen_salt('bf')), 'admin');
