UPDATE leave_types SET name = 'Cuti Tahunan'
WHERE name = 'Cuti Bulanan'
  AND NOT EXISTS (SELECT 1 FROM leave_types WHERE name = 'Cuti Tahunan');
