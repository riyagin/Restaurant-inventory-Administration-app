DROP TABLE IF EXISTS position_wage_template_components;
DROP TABLE IF EXISTS position_wage_templates;

ALTER TABLE payroll_lines
  DROP COLUMN IF EXISTS grade_pay_percent,
  DROP COLUMN IF EXISTS performance_grade;

-- Best-effort restore of the all-or-nothing gate: a grade-linked component pays
-- only from the lowest tier that pays in full.
ALTER TABLE wage_components
  ADD COLUMN min_score INT CHECK (min_score IS NULL OR (min_score >= 0 AND min_score <= 100));

UPDATE wage_components
SET min_score = (SELECT min(min_score) FROM performance_grades WHERE pay_percent = 100)
WHERE grade_linked;

ALTER TABLE wage_components DROP COLUMN grade_linked;

DROP TABLE IF EXISTS performance_grades;
