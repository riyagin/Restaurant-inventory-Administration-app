ALTER TABLE enumerations
    DROP COLUMN IF EXISTS date,
    DROP COLUMN IF EXISTS transferred_value;

ALTER TABLE dispatches ALTER COLUMN division_id SET NOT NULL;
