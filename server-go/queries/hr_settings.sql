-- ── HR Settings (singleton, id = 1) ──────────────────────────────────────────

-- name: GetHRSettings :one
SELECT id, company_name, address, logo_path, payslip_footer, absence_grace_days, updated_at
FROM hr_settings
WHERE id = 1;

-- name: UpdateHRSettings :one
INSERT INTO hr_settings (id, company_name, address, payslip_footer, absence_grace_days, updated_at)
VALUES (1, $1, $2, $3, $4, now())
ON CONFLICT (id) DO UPDATE
SET company_name = $1, address = $2, payslip_footer = $3, absence_grace_days = $4, updated_at = now()
RETURNING id, company_name, address, logo_path, payslip_footer, absence_grace_days, updated_at;

-- name: UpdateHRSettingsLogo :one
UPDATE hr_settings
SET logo_path = $1, updated_at = now()
WHERE id = 1
RETURNING id, company_name, address, logo_path, payslip_footer, absence_grace_days, updated_at;
