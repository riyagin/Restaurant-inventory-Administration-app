-- "Terlambat lebih dari 30 menit" means strictly more than 30 minutes, but it was
-- configured with threshold_minutes = 30. Late tiers pick the highest threshold
-- <= minutes late, so exactly 30 minutes late scored 10 points instead of the
-- 8 points of "Terlambat 21 - 30 menit". Only the row still holding the wrong
-- value is touched, so an HR edit made since is left alone.
UPDATE performance_policies
SET threshold_minutes = 31
WHERE rule_type = 'late'
  AND name = 'Terlambat lebih dari 30 menit'
  AND threshold_minutes = 30;
