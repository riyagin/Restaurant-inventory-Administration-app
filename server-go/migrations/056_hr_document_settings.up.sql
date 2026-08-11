-- HR document defaults move out of the generator form and into hr_settings.
--
-- Everything a generated PKWT/PKWTT/SP/Paklaring needs that is a property of the
-- *company* rather than of the employee — letterhead contact details, signing
-- city, who signs, standard working hours and payment wording — is configured
-- once here. The generator page reads them instead of asking every time.
--
-- Document numbering is the same idea taken one step further: a format template
-- plus a running counter, so each generated letter gets the next number without
-- anyone retyping "001/HRD/VII/2026".

ALTER TABLE hr_settings
  ADD COLUMN IF NOT EXISTS company_phone         TEXT    NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS company_email         TEXT    NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS company_city          TEXT    NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS signatory_name        TEXT    NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS signatory_position    TEXT    NOT NULL DEFAULT 'Direktur',
  ADD COLUMN IF NOT EXISTS signatory_national_id TEXT    NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS doc_number_format     TEXT    NOT NULL DEFAULT '{nomor}/HRD/{bulan_romawi}/{tahun}',
  ADD COLUMN IF NOT EXISTS doc_number_counter    INTEGER NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS doc_working_hours     TEXT    NOT NULL DEFAULT '5 (lima) hari kerja per minggu, 8 (delapan) jam per hari',
  ADD COLUMN IF NOT EXISTS doc_payment_info      TEXT    NOT NULL DEFAULT 'transfer ke rekening bank karyawan setiap akhir bulan',
  ADD COLUMN IF NOT EXISTS doc_probation_months  INTEGER NOT NULL DEFAULT 3;

-- The counter is the next number to hand out, so it starts at 1, never 0.
ALTER TABLE hr_settings
  ADD CONSTRAINT hr_settings_doc_number_counter_positive CHECK (doc_number_counter >= 1);
