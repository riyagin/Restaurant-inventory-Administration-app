-- Migration: add_enumerations
-- Pencacahan: break down 1 source item into N output items, with variable yield

CREATE TABLE IF NOT EXISTS enumerations (
  id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  warehouse_id    UUID        NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
  source_item_id  UUID        NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
  source_qty      NUMERIC     NOT NULL,
  source_unit_idx INT         NOT NULL DEFAULT 0,
  output_item_id  UUID        NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
  output_qty      NUMERIC     NOT NULL,
  output_unit_idx INT         NOT NULL DEFAULT 0,
  -- derived: value transferred from source lots (populated by server)
  transferred_value BIGINT    NOT NULL DEFAULT 0,
  date            DATE        NOT NULL DEFAULT CURRENT_DATE,
  notes           TEXT,
  created_by      UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS enumerations_source_item_idx ON enumerations (source_item_id);
CREATE INDEX IF NOT EXISTS enumerations_output_item_idx ON enumerations (output_item_id);
CREATE INDEX IF NOT EXISTS enumerations_date_idx        ON enumerations (date DESC);
