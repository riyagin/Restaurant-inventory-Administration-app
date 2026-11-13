-- 1. Graded benefits: replace the per-component, all-or-nothing min_score gate
--    with ONE company-wide grade scale. Each tier says "a monthly performance
--    score of at least min_score earns pay_percent of every grade-linked
--    component". The tier used is the highest min_score the score reaches.
--
-- 2. Position (role) wage templates: a default base salary + component amounts
--    per position. A template only PREFILLS an employee's wage structure, or is
--    bulk-applied as a new dated wage version; editing it never changes anyone's
--    pay on its own.

CREATE TABLE performance_grades (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  label       TEXT        NOT NULL UNIQUE CHECK (btrim(label) <> ''),
  min_score   INT         NOT NULL UNIQUE CHECK (min_score BETWEEN 0 AND 100),
  pay_percent INT         NOT NULL CHECK (pay_percent BETWEEN 0 AND 100),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE wage_components
  ADD COLUMN grade_linked BOOLEAN NOT NULL DEFAULT false;

UPDATE wage_components SET grade_linked = true WHERE min_score IS NOT NULL;

-- Seed the scale. When every gated component shared one threshold X, seed
-- "≥ X → 100%, below → 0%" so pay is exactly what min_score produced. Otherwise
-- seed an illustrative four-tier scale for HR to adjust.
DO $$
DECLARE
  n INT;
  x INT;
BEGIN
  SELECT count(DISTINCT min_score), min(min_score) INTO n, x
  FROM wage_components WHERE min_score IS NOT NULL;

  IF n = 1 AND x > 0 THEN
    INSERT INTO performance_grades (label, min_score, pay_percent)
    VALUES ('A', x, 100), ('B', 0, 0);
  ELSIF n = 1 THEN
    INSERT INTO performance_grades (label, min_score, pay_percent)
    VALUES ('A', 0, 100);
  ELSE
    INSERT INTO performance_grades (label, min_score, pay_percent)
    VALUES ('A', 90, 100), ('B', 80, 75), ('C', 70, 50), ('D', 0, 0);
  END IF;
END $$;

ALTER TABLE wage_components DROP COLUMN min_score;

-- The grade a payroll line was paid at, frozen at generation like its
-- component amounts: a later edit of the scale must not rewrite history.
ALTER TABLE payroll_lines
  ADD COLUMN performance_grade TEXT,
  ADD COLUMN grade_pay_percent INT CHECK (grade_pay_percent BETWEEN 0 AND 100);

CREATE TABLE position_wage_templates (
  position_id            UUID        PRIMARY KEY REFERENCES positions(id) ON DELETE CASCADE,
  base_salary            BIGINT      NOT NULL CHECK (base_salary >= 0),
  working_days_per_month INT         NOT NULL CHECK (working_days_per_month BETWEEN 1 AND 31),
  updated_by             UUID        REFERENCES users(id),
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE position_wage_template_components (
  position_id       UUID   NOT NULL REFERENCES position_wage_templates(position_id) ON DELETE CASCADE,
  wage_component_id UUID   NOT NULL REFERENCES wage_components(id),
  amount            BIGINT NOT NULL CHECK (amount >= 0),
  PRIMARY KEY (position_id, wage_component_id)
);

CREATE INDEX idx_position_wage_template_components_component
  ON position_wage_template_components (wage_component_id);
