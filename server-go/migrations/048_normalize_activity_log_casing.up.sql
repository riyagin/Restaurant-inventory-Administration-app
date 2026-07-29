-- Normalize activity_log.action and entity_type to the lower_snake_case form the
-- UI filters on.
--
-- The legacy Node backend wrote lowercase ('create', 'item'); the Go handlers
-- write uppercase actions ('CREATE') and a mix of PascalCase and snake_case
-- entity types ('Invoice', 'hr_employee'). The list/count queries match both
-- columns with a plain `=`, so every unnormalized row is unreachable from the
-- entity-type and action dropdowns. service.LogActivity normalizes new writes;
-- this backfills the rows already stored.

UPDATE activity_log
SET action = lower(action)
WHERE action <> lower(action);

-- PascalCase → snake_case. Handle the acronym boundary first ('POSImport' →
-- 'POS_Import') so the general lower→Upper rule below does not split the run.
UPDATE activity_log
SET entity_type = lower(
        regexp_replace(
            regexp_replace(entity_type, '([A-Z]+)([A-Z][a-z])', '\1_\2', 'g'),
            '([a-z0-9])([A-Z])', '\1_\2', 'g'
        )
    )
WHERE entity_type ~ '[A-Z]';

-- Two entity types the legacy Node backend used under different names than the
-- Go handlers. Casing alone does not merge them, so map the aliases explicitly.
-- (Only entity_type is remapped; 'transfer' remains a valid *action* value.)
UPDATE activity_log SET entity_type = 'stock_opname'   WHERE entity_type = 'opname';
UPDATE activity_log SET entity_type = 'stock_transfer' WHERE entity_type = 'transfer';
