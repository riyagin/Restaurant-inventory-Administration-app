ALTER TABLE hr_settings DROP CONSTRAINT IF EXISTS hr_settings_doc_number_counter_positive;

ALTER TABLE hr_settings
  DROP COLUMN IF EXISTS company_phone,
  DROP COLUMN IF EXISTS company_email,
  DROP COLUMN IF EXISTS company_city,
  DROP COLUMN IF EXISTS signatory_name,
  DROP COLUMN IF EXISTS signatory_position,
  DROP COLUMN IF EXISTS signatory_national_id,
  DROP COLUMN IF EXISTS doc_number_format,
  DROP COLUMN IF EXISTS doc_number_counter,
  DROP COLUMN IF EXISTS doc_working_hours,
  DROP COLUMN IF EXISTS doc_payment_info,
  DROP COLUMN IF EXISTS doc_probation_months;
