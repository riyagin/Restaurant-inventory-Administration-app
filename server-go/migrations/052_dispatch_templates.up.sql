-- Reusable dispatch skeletons, mirroring invoice_templates.
--
-- A dispatch to a branch/division repeats the same basket of items most days,
-- so a template pre-fills the header (source warehouse, destination) and the
-- item rows. Quantities are deliberately NOT stored: they are the one thing
-- that genuinely changes per dispatch, and a stale default is worse than an
-- empty field.

CREATE TABLE dispatch_templates (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name         TEXT        NOT NULL UNIQUE,
  warehouse_id UUID        REFERENCES warehouses(id) ON DELETE SET NULL,
  branch_id    UUID        REFERENCES branches(id)   ON DELETE SET NULL,
  division_id  UUID        REFERENCES divisions(id)  ON DELETE SET NULL,
  notes        TEXT,
  created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE dispatch_template_items (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  template_id UUID NOT NULL REFERENCES dispatch_templates(id) ON DELETE CASCADE,
  item_id     UUID REFERENCES items(id) ON DELETE CASCADE,
  unit_index  INT  NOT NULL DEFAULT 0,
  sort_order  INT  NOT NULL DEFAULT 0
);

CREATE INDEX idx_dispatch_template_items_template ON dispatch_template_items(template_id);
