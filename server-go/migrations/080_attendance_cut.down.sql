ALTER TABLE payroll_lines
  DROP COLUMN IF EXISTS attendance_cut_deduction,
  DROP COLUMN IF EXISTS attendance_cut_minutes;
