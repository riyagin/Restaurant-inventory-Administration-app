-- ── HR Settings (singleton, id = 1) ──────────────────────────────────────────

-- name: GetHRSettings :one
SELECT id, company_name, address, logo_path, payslip_footer, absence_grace_days, updated_at,
       company_phone, company_email, company_city,
       signatory_name, signatory_position, signatory_national_id,
       doc_number_format, doc_number_counter,
       doc_working_hours, doc_payment_info, doc_probation_months
FROM hr_settings
WHERE id = 1;

-- name: UpdateHRSettings :one
INSERT INTO hr_settings (
  id, company_name, address, payslip_footer, absence_grace_days,
  company_phone, company_email, company_city,
  signatory_name, signatory_position, signatory_national_id,
  doc_number_format, doc_number_counter,
  doc_working_hours, doc_payment_info, doc_probation_months, updated_at)
VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, now())
ON CONFLICT (id) DO UPDATE
SET company_name = $1, address = $2, payslip_footer = $3, absence_grace_days = $4,
    company_phone = $5, company_email = $6, company_city = $7,
    signatory_name = $8, signatory_position = $9, signatory_national_id = $10,
    doc_number_format = $11, doc_number_counter = $12,
    doc_working_hours = $13, doc_payment_info = $14, doc_probation_months = $15,
    updated_at = now()
RETURNING id, company_name, address, logo_path, payslip_footer, absence_grace_days, updated_at,
          company_phone, company_email, company_city,
          signatory_name, signatory_position, signatory_national_id,
          doc_number_format, doc_number_counter,
          doc_working_hours, doc_payment_info, doc_probation_months;

-- name: UpdateHRSettingsLogo :one
UPDATE hr_settings
SET logo_path = $1, updated_at = now()
WHERE id = 1
RETURNING id, company_name, address, logo_path, payslip_footer, absence_grace_days, updated_at,
          company_phone, company_email, company_city,
          signatory_name, signatory_position, signatory_national_id,
          doc_number_format, doc_number_counter,
          doc_working_hours, doc_payment_info, doc_probation_months;

-- ConsumeHRDocumentNumber hands out the current counter and advances it in one
-- statement, so two people generating a letter at the same time cannot be given
-- the same number.
-- name: ConsumeHRDocumentNumber :one
UPDATE hr_settings
SET doc_number_counter = doc_number_counter + 1, updated_at = now()
WHERE id = 1
RETURNING doc_number_counter - 1 AS reserved, doc_number_format;
