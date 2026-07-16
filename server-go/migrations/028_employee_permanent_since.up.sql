-- Track when a contract (PKWT) employee was made permanent (PKWTT). For THR the
-- tenure "day 0" is the permanent-status date, not the original join date: a worker
-- who transitions to permanent starts accruing THR eligibility from permanent_since.
-- NULL means the employee was permanent from the start, so THR falls back to join_date.
ALTER TABLE employees
  ADD COLUMN permanent_since DATE;
