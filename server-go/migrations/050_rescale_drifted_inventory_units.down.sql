-- No down migration.
--
-- The up migration restated lot quantities into the item's base unit; it did not
-- record which lots it touched, and after the fact a correctly-based lot is
-- indistinguishable from one that was always correct. Reversing it would mean
-- guessing which lots to divide back down, and a wrong guess silently changes
-- stock levels.
--
-- Restore from a backup instead if this has to be undone.
SELECT 1;
