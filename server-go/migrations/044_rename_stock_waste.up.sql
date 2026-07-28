-- 044: "Stock Waste" -> "Selisih Persediaan".
--
-- The name stopped describing the contents. The account now carries inventory
-- differences in both directions and from three sources:
--
--   * stock opname shortfalls  Dr, at the FIFO cost of the lots consumed
--   * stock opname surpluses   Cr, at the last purchase price converted to the
--                              base unit
--   * manual inventory lot create / revalue / delete, which previously moved the
--     warehouse inventory account with no counter-leg at all
--
-- Only the first is waste. Calling the whole thing "Stock Waste" invites reading
-- a net figure — shrinkage minus over-counts minus manual corrections — as a
-- pure loss.
--
-- Balance and account_type are deliberately untouched. The account is still
-- typed `asset` with a negative balance, inherited from the legacy Node code
-- (`INSERT INTO accounts (name, balance)` with no account_type, then
-- `balance = balance - waste`). Reclassifying it to `expense` and flipping the
-- sign moves ~245M through the P&L and is a separate, deliberate decision.
--
-- Idempotent, and safe if the account was never created.

DO $$
BEGIN
  -- Already renamed, or a fresh database where the Go code created it with the
  -- new name from the start.
  IF EXISTS (SELECT 1 FROM accounts WHERE name = 'Selisih Persediaan') THEN
    RAISE NOTICE '044: "Selisih Persediaan" already exists, nothing to rename';
    RETURN;
  END IF;

  UPDATE accounts SET name = 'Selisih Persediaan' WHERE name = 'Stock Waste';
END $$;
