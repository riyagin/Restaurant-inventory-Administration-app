UPDATE performance_policies
SET threshold_minutes = 30
WHERE rule_type = 'late'
  AND name = 'Terlambat lebih dari 30 menit'
  AND threshold_minutes = 31;
