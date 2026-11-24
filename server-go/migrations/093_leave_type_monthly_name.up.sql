-- Cuti draws on a monthly allowance (migration 077), never an annual one, so the
-- seeded "Cuti Tahunan" type is renamed. Skipped if a "Cuti Bulanan" type
-- already exists (name is UNIQUE) — rename it by hand in that case.
UPDATE leave_types SET name = 'Cuti Bulanan'
WHERE name = 'Cuti Tahunan'
  AND NOT EXISTS (SELECT 1 FROM leave_types WHERE name = 'Cuti Bulanan');
