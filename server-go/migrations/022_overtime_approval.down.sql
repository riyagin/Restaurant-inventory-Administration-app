DROP INDEX IF EXISTS idx_overtime_requests_status;

ALTER TABLE overtime_requests
  DROP COLUMN status,
  DROP COLUMN decided_by,
  DROP COLUMN decided_at,
  DROP COLUMN decision_note;
