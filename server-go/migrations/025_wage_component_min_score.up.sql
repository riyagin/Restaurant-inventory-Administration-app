-- Score-gated wage components. When min_score is set, the component only pays if
-- the employee's monthly performance score (performance_scores.score, 100 − deductions)
-- meets the threshold for the payroll period; otherwise it contributes 0. NULL means
-- the component is never gated. Composes with calc_method (a per_present_day allowance
-- can also be score-gated). Mainly intended for allowance/bonus (e.g. Tunjangan Kinerja).
ALTER TABLE wage_components
  ADD COLUMN min_score INT
  CHECK (min_score IS NULL OR (min_score >= 0 AND min_score <= 100));
