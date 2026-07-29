-- Repair stock_history.value on 'opname' rows.
--
-- stock_history.value is signed the same way as quantity_change: positive =
-- stock in, negative = stock out. The flow reports depend on it directly —
-- GetItemMonthlyFlow buckets `value > 0` as value_in and `value < 0` as
-- value_out, and GetItemFlowByType sums it into value_net.
--
-- Every movement type honoured that convention except 'opname', which wrote:
--   * losses   → +waste_value (the magnitude), so a shrinkage was counted as
--                inbound value — doubly wrong, since it also failed to
--                register as an outflow;
--   * surpluses → 0, so the value added to inventory never appeared at all.
--
-- The ledger was always correct (the opname handler posts the real surplus and
-- waste amounts to the journal), so this is a reporting repair only: no account
-- balance, inventory lot, or journal entry is touched.
--
-- The handler now writes the signed value for new opnames; this backfills the
-- rows already stored.

-- 1. Losses: the correct magnitude is already there, only the sign is wrong.
--    Fully deterministic.
UPDATE stock_history
SET value = -value
WHERE type = 'opname' AND quantity_change < 0 AND value > 0;

-- 2. Surpluses: the value was never written, so it has to be recovered from the
--    inventory lot that the same opname created. FIFOAdd stamps that lot with
--    the opname's date, warehouse, item and quantity, so an untouched lot is an
--    exact match. Lots that have since been partially or fully consumed by FIFO
--    no longer match on quantity (or are gone entirely) and are deliberately
--    left at 0 rather than estimated — inventing a price for a financial record
--    is worse than a known gap.
--
--    The `= 1` guard makes the match unambiguous: a surplus row is only filled
--    when exactly one surviving lot can be it.
UPDATE stock_history sh
SET value = inv.value
FROM inventory inv
WHERE sh.type = 'opname'
  AND sh.quantity_change > 0
  AND COALESCE(sh.value, 0) = 0
  AND inv.item_id = sh.item_id
  AND inv.warehouse_id = sh.warehouse_id
  AND inv.date = sh.date
  AND inv.quantity = sh.quantity_change
  AND inv.value > 0
  AND (
        SELECT COUNT(*) FROM inventory i2
        WHERE i2.item_id = sh.item_id
          AND i2.warehouse_id = sh.warehouse_id
          AND i2.date = sh.date
          AND i2.quantity = sh.quantity_change
          AND i2.value > 0
      ) = 1;
