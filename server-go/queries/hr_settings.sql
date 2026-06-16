-- ── HR Settings (singleton, id = 1) ──────────────────────────────────────────

-- name: GetHRSettings :one
SELECT id, company_name, address, logo_path, payslip_footer, updated_at
FROM hr_settings
WHERE id = 1;

-- name: UpdateHRSettings :one
UPDATE hr_settings
SET company_name = $1, address = $2, payslip_footer = $3, updated_at = now()
WHERE id = 1
RETURNING id, company_name, address, logo_path, payslip_footer, updated_at;

-- name: UpdateHRSettingsLogo :one
UPDATE hr_settings
SET logo_path = $1, updated_at = now()
WHERE id = 1
RETURNING id, company_name, address, logo_path, payslip_footer, updated_at;
