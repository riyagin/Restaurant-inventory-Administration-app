-- Restate inventory lots that were left behind by an earlier unit change.
--
-- Nothing in the deduction path converts units: service.FIFODeduct compares the
-- requested quantity directly against inventory.quantity. Every lot of an item
-- therefore has to be denominated in the same unit — the base (last) one in
-- items.units.
--
-- Appending a smaller unit to an existing item used to rewrite items.units
-- without touching the lots, so the lots kept pointing at their old index while
-- the base moved down a level. A lot of 2 'ball' under [ball, pack/40] is 80
-- packs, but every later deduction read it as 2.
--
-- The item handler now rescales lots in the same transaction as the unit edit;
-- this repairs the rows that drifted before that existed.
--
-- Quantity is multiplied by the number of base units per stored unit — the
-- product of every perPrev below the lot's index. Value is deliberately left
-- alone: the same goods bought for the same money, counted in a smaller unit.

WITH drifted AS (
    SELECT
        inv.id,
        -- Postgres has no exact product aggregate, so the product goes through
        -- EXP(SUM(LN(…))). That carries float error (40 came back as
        -- 39.999999999999999), which has no business landing in a quantity
        -- column. Unit ratios are whole or near-whole numbers, so rounding the
        -- factor — not the result — recovers the exact multiplier.
        ROUND(
            (
                SELECT COALESCE(EXP(SUM(LN((u->>'perPrev')::numeric))), 1)
                FROM jsonb_array_elements(i.units) WITH ORDINALITY AS t(u, ord)
                WHERE ord - 1 > inv.unit_index
                  AND (u->>'perPrev') IS NOT NULL
                  AND (u->>'perPrev')::numeric > 0
            ), 6
        ) AS factor,
        (jsonb_array_length(i.units) - 1) AS new_unit_index
    FROM inventory inv
    JOIN items i ON i.id = inv.item_id
    WHERE jsonb_array_length(i.units) > 0
      AND inv.unit_index <> jsonb_array_length(i.units) - 1
)
UPDATE inventory inv
SET quantity   = inv.quantity * drifted.factor,
    unit_index = drifted.new_unit_index
FROM drifted
WHERE inv.id = drifted.id
  AND drifted.factor > 0;
