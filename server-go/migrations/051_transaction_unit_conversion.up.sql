-- Record the unit→base conversion each purchase and dispatch line was booked at.
--
-- Inventory lots are denominated in the item's base (last) unit — see migration
-- 050 — but a line is entered in whatever unit the goods arrive in: 2 dus, not
-- 48 kaleng. The handler now multiplies the entered quantity by the number of
-- base units per entered unit before touching inventory.
--
-- That factor normally comes from items.units, but the operator can override it
-- for a single transaction (a supplier's dus holding 20 instead of the usual
-- 24). The override must not be written back to items.units — every other
-- transaction still uses the catalogue figure — so it is stored on the line.
--
-- Storing it is also what makes an edit or a cancellation reversible: the
-- reversal has to deduct exactly the base quantity that was added, which the
-- current items.units may no longer reproduce.
--
-- Existing rows get 1: they were booked before any conversion existed, so their
-- stored quantity IS the base quantity that reached inventory.

ALTER TABLE invoice_items
  ADD COLUMN conversion_factor NUMERIC NOT NULL DEFAULT 1
  CHECK (conversion_factor > 0);

ALTER TABLE dispatch_items
  ADD COLUMN conversion_factor NUMERIC NOT NULL DEFAULT 1
  CHECK (conversion_factor > 0);
