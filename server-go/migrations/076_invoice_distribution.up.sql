-- Faktur distribusi — one purchase, many destinations.
--
-- A bulk purchase is often split on arrival: part of the delivery goes into a
-- warehouse, the rest straight to one or more branches. Booking it as one
-- invoice per destination splits a single supplier bill (one payable, one
-- payment) into several that never add up to the paper invoice, so instead the
-- invoice itself carries the split.
--
-- A distributed invoice has no header warehouse/branch/division. Every line is
-- allocated, in the line's own unit, across any number of destinations, not
-- necessarily evenly:
--
--   warehouse  → a stock lot is added there, Dr Persediaan - <gudang>
--   branch     → consumed on arrival, Dr Beban - <cabang> / <divisi>
--                (the same account a dispatch to that division debits)
--
-- The allocation's value, base-unit quantity and the account it debited are
-- frozen on the row: an edit or deletion must unwind exactly what was booked,
-- not what today's catalogue or chart of accounts would say.

ALTER TABLE invoices
  ADD COLUMN IF NOT EXISTS is_distributed BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS invoice_item_allocations (
  id               UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id       UUID    NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  invoice_item_id  UUID    NOT NULL REFERENCES invoice_items(id) ON DELETE CASCADE,
  warehouse_id     UUID    REFERENCES warehouses(id),
  branch_id        UUID    REFERENCES branches(id),
  division_id      UUID    REFERENCES divisions(id),
  -- In the line's entered unit; the allocations of a line sum to its quantity.
  quantity         NUMERIC NOT NULL CHECK (quantity > 0),
  -- quantity × the line's conversion factor — what went into the lot.
  base_quantity    NUMERIC NOT NULL,
  -- This allocation's share of the line value; a line's shares sum to it exactly.
  value            BIGINT  NOT NULL,
  debit_account_id UUID    REFERENCES accounts(id),
  sort_order       INT     NOT NULL DEFAULT 0,
  CONSTRAINT invoice_allocation_destination_check
    CHECK ((warehouse_id IS NOT NULL) <> (branch_id IS NOT NULL)),
  CONSTRAINT invoice_allocation_division_check
    CHECK (division_id IS NULL OR branch_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_invoice_item_allocations_invoice ON invoice_item_allocations(invoice_id);
CREATE INDEX IF NOT EXISTS idx_invoice_item_allocations_item    ON invoice_item_allocations(invoice_item_id);
