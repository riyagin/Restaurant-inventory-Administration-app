-- Pembelanjaan Harian — a branch's daily shopping, paid from its cash box.
--
-- Mechanically this is a purchase: goods arrive, stock goes up, cost is booked.
-- What differs is settlement. A purchase invoice creates a payable that is
-- settled later from a bank or till account; this is handed over in cash at the
-- market stall and is finished the moment it is recorded. There is no vendor
-- credit, no due date, no payment status — so it is its own table rather than an
-- invoice wearing a flag, and nothing here ever touches `Utang Usaha`.
--
-- Every posted row credits the branch's "Kas Kecil - <Cabang>" account, which is
-- what makes the day's count checkable: the box should hold the opening amount,
-- plus top-ups, minus the total of these rows.

CREATE TABLE IF NOT EXISTS daily_purchases (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  number       TEXT        NOT NULL UNIQUE,
  date         DATE        NOT NULL,
  -- The branch whose box paid. Not nullable and not ON DELETE SET NULL: an
  -- orphaned spend cannot be reconciled against any box.
  branch_id    UUID        NOT NULL REFERENCES branches(id) ON DELETE RESTRICT,
  division_id  UUID        REFERENCES divisions(id) ON DELETE SET NULL,
  -- Required once any line is a stock item; a non-stock spend (a repair, a
  -- parking fee) has no warehouse to receive into.
  warehouse_id UUID        REFERENCES warehouses(id) ON DELETE RESTRICT,
  expense_category_id UUID REFERENCES expense_categories(id) ON DELETE SET NULL,
  -- The account actually credited, captured on the row rather than looked up
  -- from the branch at read time. A branch that is later repointed at a new box
  -- must not silently rewrite where last month's money came from.
  petty_cash_account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  vendor_id    UUID        REFERENCES vendors(id) ON DELETE SET NULL,
  total_amount BIGINT      NOT NULL DEFAULT 0,
  notes        TEXT        NOT NULL DEFAULT '',
  photo_path   TEXT,
  -- Cancelling reverses the stock and the journal but keeps the row and its
  -- number, so a day that was reconciled once still reconciles the same way.
  status       TEXT        NOT NULL DEFAULT 'posted' CHECK (status IN ('posted', 'cancelled')),
  created_by   UUID        REFERENCES users(id) ON DELETE SET NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  cancelled_by UUID        REFERENCES users(id) ON DELETE SET NULL,
  cancelled_at TIMESTAMPTZ,
  cancel_reason TEXT       NOT NULL DEFAULT ''
);

-- The reconciliation query is always "this branch, this date", so lead with both.
CREATE INDEX IF NOT EXISTS idx_daily_purchases_branch_date
  ON daily_purchases (branch_id, date);
CREATE INDEX IF NOT EXISTS idx_daily_purchases_date ON daily_purchases (date);
CREATE INDEX IF NOT EXISTS idx_daily_purchases_created_by ON daily_purchases (created_by);

CREATE TABLE IF NOT EXISTS daily_purchase_items (
  id          UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  purchase_id UUID    NOT NULL REFERENCES daily_purchases(id) ON DELETE CASCADE,
  -- NULL for a free-text line: half of daily shopping is things with no entry in
  -- the item catalogue and no reason to gain one.
  item_id     UUID    REFERENCES items(id) ON DELETE SET NULL,
  description TEXT    NOT NULL DEFAULT '',
  quantity    NUMERIC NOT NULL,
  unit_index  INT,
  price       BIGINT  NOT NULL,
  -- The unit-per-base rate used at the time, stored on the line. A reversal must
  -- unwind at the factor that was booked, never at today's catalogue figure —
  -- the same rule invoice_items follows.
  conversion_factor NUMERIC NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_daily_purchase_items_purchase
  ON daily_purchase_items (purchase_id);
CREATE INDEX IF NOT EXISTS idx_daily_purchase_items_item
  ON daily_purchase_items (item_id);

-- Numbering: PH-000001 upward, independent of the invoice sequence so the two
-- never look like each other's neighbours.
CREATE SEQUENCE IF NOT EXISTS daily_purchase_number_seq START 1;
