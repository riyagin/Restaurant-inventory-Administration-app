-- HR Settings (prompt 09). Singleton company-header settings used to render PDF
-- payslips (Slip Gaji). One row only, enforced by the id = 1 CHECK, mirroring the
-- payroll_settings pattern from migration 012.
--
-- logo_path stores just the filename of an uploaded logo living in server/uploads/
-- (same convention as employee photos / invoice photos); it is nullable.

CREATE TABLE hr_settings (
  id             INT         PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  company_name   TEXT        NOT NULL DEFAULT '',
  address        TEXT        NOT NULL DEFAULT '',
  logo_path      TEXT,
  payslip_footer TEXT        NOT NULL DEFAULT '',
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO hr_settings (id, company_name, address, payslip_footer)
VALUES (1, '', '', '')
ON CONFLICT (id) DO NOTHING;
