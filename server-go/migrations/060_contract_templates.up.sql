-- Reusable contract conditions for PKWT / PKWTT.
--
-- Hiring three cashiers means typing the same place of work, wage, working
-- period and job description three times, and every retype is a chance for two
-- contracts for the same role to disagree. A template is a named preset of the
-- *ketentuan* half of a contract, applied to the generator form and then still
-- editable — it fills the form, it does not lock it.
--
-- Deliberately NOT stored here: company letterhead, signatory, standard working
-- hours and payment wording. Those are company-wide and already live in
-- hr_settings (migration 056); duplicating them per template is how the two
-- would drift.
--
-- contract_months replaces a stored end date: a template is reused months apart,
-- so the term has to be relative to whatever start date the contract gets.

CREATE TABLE IF NOT EXISTS hr_contract_templates (
  id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name             TEXT        NOT NULL,
  -- 'pkwt' | 'pkwtt' | '' (blank = offered for both)
  doc_type         TEXT        NOT NULL DEFAULT '' CHECK (doc_type IN ('', 'pkwt', 'pkwtt')),
  position_name    TEXT        NOT NULL DEFAULT '',
  division_name    TEXT        NOT NULL DEFAULT '',
  place_of_work    TEXT        NOT NULL DEFAULT '',
  salary           BIGINT      NOT NULL DEFAULT 0 CHECK (salary >= 0),
  salary_period    TEXT        NOT NULL DEFAULT 'bulan' CHECK (salary_period IN ('bulan', 'hari')),
  job_description  TEXT        NOT NULL DEFAULT '',
  -- PKWT term length in months; 0 = leave the end date to the operator.
  contract_months  INT         NOT NULL DEFAULT 12 CHECK (contract_months >= 0),
  notes            TEXT        NOT NULL DEFAULT '',
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_hr_contract_templates_type ON hr_contract_templates (doc_type);
