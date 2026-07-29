-- Restore the pre-049 convention on 'opname' rows: losses carried the waste
-- magnitude unsigned, surpluses carried 0.
--
-- Roll this back together with the handler change, not on its own — the handler
-- writes signed values now, so leaving the new code against the old convention
-- would produce a mix of both.
--
-- This is a faithful inverse of the up migration but not of the surplus values
-- themselves: the up migration only ever filled surplus rows that were 0, so
-- zeroing them again returns them to their stored state.

UPDATE stock_history
SET value = -value
WHERE type = 'opname' AND quantity_change < 0 AND value < 0;

UPDATE stock_history
SET value = 0
WHERE type = 'opname' AND quantity_change > 0 AND COALESCE(value, 0) <> 0;
