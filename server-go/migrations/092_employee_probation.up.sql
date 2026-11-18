-- 092: Masa percobaan (probation) as a third employment type, next to
-- 'permanent' (tetap / PKWTT) and 'contract' (kontrak / PKWT).
--
-- Probation lasts one month from the join date. Its end date is kept in
-- contract_end_date (the "end of the current term" column), computed by the
-- server as join_date + 1 month; HR then makes the employee tetap through the
-- existing transition, or changes the type to kontrak.
ALTER TABLE employees DROP CONSTRAINT IF EXISTS employees_employment_type_check;
ALTER TABLE employees
  ADD CONSTRAINT employees_employment_type_check
  CHECK (employment_type IN ('permanent', 'contract', 'probation'));
