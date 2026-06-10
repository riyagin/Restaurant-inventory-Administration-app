CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  username      TEXT        NOT NULL UNIQUE,
  password_hash TEXT        NOT NULL,
  role          TEXT        NOT NULL DEFAULT 'admin',
  created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE vendors (
  id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE accounts (
  id             UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
  name           TEXT   NOT NULL UNIQUE,
  balance        BIGINT NOT NULL DEFAULT 0,
  account_number INT,
  account_type   TEXT   NOT NULL DEFAULT 'asset',
  parent_id      UUID   REFERENCES accounts(id),
  is_system      BOOLEAN NOT NULL DEFAULT false
);


CREATE TABLE warehouses (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name                 TEXT NOT NULL UNIQUE,
  inventory_account_id UUID REFERENCES accounts(id)
);

CREATE TABLE branches (
  id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name               TEXT        NOT NULL UNIQUE,
  revenue_account_id UUID        REFERENCES accounts(id),
  expense_account_id UUID        REFERENCES accounts(id),
  created_at         TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE divisions (
  id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id           UUID        NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  name                TEXT        NOT NULL,
  revenue_account_id  UUID        REFERENCES accounts(id),
  expense_account_id  UUID        REFERENCES accounts(id),
  discount_account_id UUID        REFERENCES accounts(id),
  created_at          TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE (branch_id, name)
);

CREATE TABLE division_categories (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  division_id UUID REFERENCES divisions(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  UNIQUE (division_id, name)
);

CREATE TABLE items (
  id       UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  name     TEXT    NOT NULL,
  code     TEXT    NOT NULL UNIQUE,
  units    JSONB   NOT NULL,
  is_stock BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE inventory (
  id           UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  item_id      UUID    NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  warehouse_id UUID    NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
  quantity     NUMERIC NOT NULL,
  unit_index   INT     NOT NULL,
  value        BIGINT  NOT NULL,
  date         DATE    NOT NULL DEFAULT CURRENT_DATE
);

CREATE TABLE stock_history (
  id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  item_id         UUID        NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  warehouse_id    UUID        NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
  quantity_change NUMERIC     NOT NULL,
  unit_name       TEXT        NOT NULL,
  vendor          TEXT,
  type            TEXT        NOT NULL,
  reference       TEXT,
  date            DATE        NOT NULL DEFAULT CURRENT_DATE,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  source_id       UUID,
  source_type     TEXT,
  value           BIGINT
);

CREATE TABLE stock_transfers (
  id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  item_id           UUID        NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  from_warehouse_id UUID        NOT NULL REFERENCES warehouses(id),
  to_warehouse_id   UUID        NOT NULL REFERENCES warehouses(id),
  quantity          NUMERIC     NOT NULL,
  unit_index        INT         NOT NULL,
  unit_name         TEXT        NOT NULL,
  notes             TEXT,
  transferred_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
  transferred_at    TIMESTAMPTZ DEFAULT NOW(),
  group_id          UUID
);

CREATE TABLE stock_opname (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  warehouse_id  UUID        NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
  notes         TEXT,
  performed_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
  performed_at  TIMESTAMPTZ DEFAULT NOW(),
  operator_name TEXT,
  pic_name      TEXT
);

CREATE TABLE stock_opname_items (
  id                UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  opname_id         UUID    NOT NULL REFERENCES stock_opname(id) ON DELETE CASCADE,
  item_id           UUID    NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
  unit_index        INT     NOT NULL,
  unit_name         TEXT    NOT NULL,
  recorded_quantity NUMERIC NOT NULL,
  actual_quantity   NUMERIC NOT NULL,
  difference        NUMERIC NOT NULL,
  waste_value       BIGINT  NOT NULL DEFAULT 0
);

CREATE TABLE dispatches (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id     UUID        NOT NULL REFERENCES branches(id) ON DELETE RESTRICT,
  division_id   UUID        NOT NULL REFERENCES divisions(id) ON DELETE RESTRICT,
  warehouse_id  UUID        NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
  notes         TEXT,
  dispatched_by UUID        REFERENCES users(id) ON DELETE SET NULL,
  dispatched_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE dispatch_items (
  id          UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  dispatch_id UUID    NOT NULL REFERENCES dispatches(id) ON DELETE CASCADE,
  item_id     UUID    NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
  quantity    NUMERIC NOT NULL,
  unit_index  INT     NOT NULL,
  unit_name   TEXT    NOT NULL
);

CREATE TABLE invoices (
  id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_number   TEXT        NOT NULL UNIQUE,
  date             DATE        NOT NULL DEFAULT CURRENT_DATE,
  due_date         DATE,
  invoice_type     TEXT        NOT NULL DEFAULT 'purchase',
  payment_method   TEXT,
  payment_status   TEXT        NOT NULL DEFAULT 'unpaid',
  amount_paid      BIGINT      NOT NULL DEFAULT 0,
  account_id       UUID        REFERENCES accounts(id),
  warehouse_id     UUID        REFERENCES warehouses(id),
  branch_id        UUID        REFERENCES branches(id),
  division_id      UUID        REFERENCES divisions(id),
  dispatch_id      UUID        REFERENCES dispatches(id),
  vendor_id        UUID        REFERENCES vendors(id),
  reference_number TEXT,
  photo_path       TEXT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE invoice_items (
  id          UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id  UUID    NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  item_id     UUID    REFERENCES items(id),
  vendor_id   UUID    REFERENCES vendors(id),
  quantity    NUMERIC NOT NULL,
  unit_index  INT,
  price       BIGINT  NOT NULL,
  description TEXT
);

CREATE TABLE invoice_templates (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name         TEXT        NOT NULL UNIQUE,
  invoice_type TEXT        NOT NULL DEFAULT 'expense',
  vendor_id    UUID        REFERENCES vendors(id) ON DELETE SET NULL,
  warehouse_id UUID        REFERENCES warehouses(id) ON DELETE SET NULL,
  created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE invoice_template_items (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  template_id UUID NOT NULL REFERENCES invoice_templates(id) ON DELETE CASCADE,
  item_id     UUID REFERENCES items(id) ON DELETE SET NULL,
  description TEXT,
  unit_index  INT  NOT NULL DEFAULT 0,
  sort_order  INT  NOT NULL DEFAULT 0
);

CREATE TABLE recipes (
  id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name             TEXT        NOT NULL,
  output_item_id   UUID        REFERENCES items(id),
  batch_size       NUMERIC     NOT NULL,
  batch_unit_index INT         NOT NULL DEFAULT 0,
  created_at       TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE recipe_ingredients (
  id         UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  recipe_id  UUID    REFERENCES recipes(id) ON DELETE CASCADE,
  item_id    UUID    REFERENCES items(id),
  quantity   NUMERIC NOT NULL,
  unit_index INT     NOT NULL DEFAULT 0
);

CREATE TABLE productions (
  id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  recipe_id       UUID        REFERENCES recipes(id),
  warehouse_id    UUID        REFERENCES warehouses(id),
  batches         NUMERIC     NOT NULL,
  output_quantity NUMERIC     NOT NULL,
  date            DATE        NOT NULL DEFAULT CURRENT_DATE,
  notes           TEXT,
  created_by      UUID        REFERENCES users(id),
  created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE sales (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id  UUID        NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  amount      BIGINT      NOT NULL,
  description TEXT,
  date        DATE        NOT NULL DEFAULT CURRENT_DATE,
  branch_id   UUID        REFERENCES branches(id),
  division_id UUID        REFERENCES divisions(id),
  created_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE pos_imports (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  description  TEXT,
  date         DATE        NOT NULL,
  source_file  TEXT,
  total_amount BIGINT      NOT NULL,
  created_by   UUID        REFERENCES users(id),
  created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE pos_import_lines (
  id         UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
  import_id  UUID   REFERENCES pos_imports(id) ON DELETE CASCADE,
  account_id UUID   REFERENCES accounts(id),
  label      TEXT   NOT NULL,
  amount     BIGINT NOT NULL,
  line_type  TEXT   NOT NULL
);

CREATE TABLE account_adjustments (
  id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id      UUID        NOT NULL REFERENCES accounts(id),
  amount          BIGINT      NOT NULL,
  description     TEXT        NOT NULL,
  created_by      UUID        REFERENCES users(id),
  created_by_name TEXT,
  created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE activity_log (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID        REFERENCES users(id) ON DELETE SET NULL,
  username    TEXT        NOT NULL,
  action      TEXT        NOT NULL,
  entity_type TEXT        NOT NULL,
  entity_id   UUID,
  description TEXT        NOT NULL,
  created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE token_blocklist (
  jti        TEXT        PRIMARY KEY,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE enumerations (
  id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  source_item_id  UUID        REFERENCES items(id) ON DELETE CASCADE,
  output_item_id  UUID        REFERENCES items(id) ON DELETE CASCADE,
  source_qty      NUMERIC     NOT NULL,
  output_qty      NUMERIC     NOT NULL,
  source_unit_idx INT         NOT NULL DEFAULT 0,
  output_unit_idx INT         NOT NULL DEFAULT 0,
  warehouse_id    UUID        REFERENCES warehouses(id),
  notes           TEXT,
  created_by      UUID        REFERENCES users(id),
  created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE UNIQUE INDEX accounts_number_unique ON accounts (account_number) WHERE account_number IS NOT NULL;
CREATE INDEX account_adjustments_account_idx ON account_adjustments (account_id);
CREATE INDEX account_adjustments_created_idx ON account_adjustments (created_at DESC);
CREATE INDEX activity_log_created_idx ON activity_log (created_at DESC);
CREATE INDEX invoice_items_invoice_idx ON invoice_items (invoice_id);
CREATE INDEX invoices_payment_status_idx ON invoices (payment_status);
CREATE INDEX invoices_date_idx ON invoices (date);
CREATE INDEX token_blocklist_expires_idx ON token_blocklist (expires_at);
