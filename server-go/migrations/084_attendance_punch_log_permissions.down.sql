ALTER TABLE hr_settings
  DROP COLUMN IF EXISTS allow_backdated_permission,
  DROP COLUMN IF EXISTS review_early_minutes,
  DROP COLUMN IF EXISTS review_late_minutes;

ALTER TABLE attendance_records
  DROP COLUMN IF EXISTS review_resolved,
  DROP COLUMN IF EXISTS excused_by;

DROP TABLE IF EXISTS attendance_permissions;
DROP TABLE IF EXISTS attendance_punches;
