-- Templates for Pembelanjaan Harian.
--
-- The same shopping run repeats: the Tuesday market list, the weekly gas and
-- ice order for one branch. A template is the skeleton — which branch, which
-- division, which warehouse receives, and which items in which unit — with the
-- amounts left out.
--
-- Quantities and prices are deliberately NOT stored, the same decision
-- dispatch_templates made. A template that remembers "12 kg at 18.000" invites
-- someone to accept last month's price without looking, and a wrong price here
-- flows straight into inventory value and the branch's expenses. The template
-- saves the typing that is genuinely repetitive and leaves the two fields that
-- must be checked every time blank.

CREATE TABLE IF NOT EXISTS daily_purchase_templates (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name         TEXT        NOT NULL UNIQUE,
  branch_id    UUID        REFERENCES branches(id) ON DELETE CASCADE,
  division_id  UUID        REFERENCES divisions(id) ON DELETE SET NULL,
  warehouse_id UUID        REFERENCES warehouses(id) ON DELETE SET NULL,
  vendor_id    UUID        REFERENCES vendors(id) ON DELETE SET NULL,
  expense_category_id UUID REFERENCES expense_categories(id) ON DELETE SET NULL,
  notes        TEXT        NOT NULL DEFAULT '',
  created_by   UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS daily_purchase_template_items (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  template_id UUID NOT NULL REFERENCES daily_purchase_templates(id) ON DELETE CASCADE,
  -- NULL for a free-text line; daily shopping is full of things with no
  -- catalogue entry, and a template should be able to remember "es batu" as a
  -- line to fill in without inventing an item for it.
  item_id     UUID REFERENCES items(id) ON DELETE SET NULL,
  description TEXT NOT NULL DEFAULT '',
  unit_index  INT  NOT NULL DEFAULT 0,
  sort_order  INT  NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_daily_purchase_template_items_template
  ON daily_purchase_template_items (template_id);
