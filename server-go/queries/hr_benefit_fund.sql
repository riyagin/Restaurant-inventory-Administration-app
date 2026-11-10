-- Dana Kesejahteraan Karyawan (health benefit + pension fund). See migration 085.

-- name: GetBenefitMonthlyAmount :one
-- The employee's monthly benefit allowance: the sum of 'benefit_fund' components on
-- the wage structure in force on the given date. 0 when none.
SELECT COALESCE(SUM(ewc.amount), 0)::bigint AS monthly_amount
FROM wage_structures ws
JOIN employee_wage_components ewc ON ewc.wage_structure_id = ws.id
JOIN wage_components wc ON wc.id = ewc.wage_component_id AND wc.type = 'benefit_fund'
WHERE ws.id = (
  SELECT w2.id FROM wage_structures w2
  WHERE w2.employee_id = sqlc.arg(employee_id)
    AND w2.effective_date <= sqlc.arg(as_of)::date
    AND (w2.end_date IS NULL OR w2.end_date >= sqlc.arg(as_of)::date)
  ORDER BY w2.effective_date DESC
  LIMIT 1
);

-- name: SumCoveredClaimsForMonth :one
SELECT COALESCE(SUM(covered_amount), 0)::bigint AS covered
FROM benefit_claims
WHERE employee_id = $1 AND period_month = $2 AND status = 'paid';

-- name: GetBenefitBalances :one
SELECT
  COALESCE(SUM(amount) FILTER (WHERE fund = 'health'), 0)::bigint  AS health_balance,
  COALESCE(SUM(amount) FILTER (WHERE fund = 'pension'), 0)::bigint AS pension_balance
FROM benefit_fund_movements
WHERE employee_id = $1;

-- name: ListBenefitFundSummary :many
-- One row per employee who has an allowance, a balance, or a claim this month.
SELECT
  e.id, e.employee_code, e.full_name, e.status, e.join_date, e.resign_date,
  b.name AS branch_name,
  COALESCE(ma.monthly_amount, 0)::bigint AS monthly_amount,
  COALESCE(cl.covered, 0)::bigint        AS covered_this_month,
  COALESCE(cl.claim_count, 0)::int       AS claims_this_month,
  COALESCE(bal.health, 0)::bigint        AS health_balance,
  COALESCE(bal.pension, 0)::bigint       AS pension_balance,
  COALESCE(tot.claim_count, 0)::int      AS claims_total,
  COALESCE(tot.claimed, 0)::bigint       AS claimed_total
FROM employees e
LEFT JOIN branches b ON b.id = e.branch_id
LEFT JOIN LATERAL (
  SELECT SUM(ewc.amount) AS monthly_amount
  FROM employee_wage_components ewc
  JOIN wage_components wc ON wc.id = ewc.wage_component_id AND wc.type = 'benefit_fund'
  WHERE ewc.wage_structure_id = (
    SELECT w2.id FROM wage_structures w2
    WHERE w2.employee_id = e.id
      AND w2.effective_date <= sqlc.arg(as_of)::date
      AND (w2.end_date IS NULL OR w2.end_date >= sqlc.arg(as_of)::date)
    ORDER BY w2.effective_date DESC LIMIT 1)
) ma ON true
LEFT JOIN LATERAL (
  SELECT SUM(c.covered_amount) AS covered, COUNT(*) AS claim_count
  FROM benefit_claims c
  WHERE c.employee_id = e.id AND c.period_month = sqlc.arg(period_month)::date AND c.status = 'paid'
) cl ON true
LEFT JOIN LATERAL (
  SELECT SUM(m.amount) FILTER (WHERE m.fund = 'health')  AS health,
         SUM(m.amount) FILTER (WHERE m.fund = 'pension') AS pension
  FROM benefit_fund_movements m WHERE m.employee_id = e.id
) bal ON true
LEFT JOIN LATERAL (
  SELECT COUNT(*) AS claim_count, SUM(c.total_amount) AS claimed
  FROM benefit_claims c WHERE c.employee_id = e.id AND c.status = 'paid'
) tot ON true
WHERE COALESCE(ma.monthly_amount, 0) > 0
   OR COALESCE(bal.health, 0) <> 0
   OR COALESCE(bal.pension, 0) <> 0
   OR COALESCE(cl.claim_count, 0) > 0
ORDER BY e.full_name;

-- name: GetMaxBenefitClaimSeqForYear :one
SELECT COALESCE(MAX(CAST(SUBSTRING(claim_number FROM 10) AS INTEGER)), 0)::int AS max_seq
FROM benefit_claims
WHERE claim_number ~ ('^KLM-' || $1::text || '-[0-9]+$');

-- name: CreateBenefitClaim :one
INSERT INTO benefit_claims (
  claim_number, employee_id, claim_date, period_month, total_amount, covered_amount,
  excess_amount, monthly_amount, available_before, fund_source_account_id, kasbon_id,
  leave_request_id, description, journal_entry_id, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: SetBenefitClaimJournal :exec
UPDATE benefit_claims SET journal_entry_id = $2 WHERE id = $1;

-- name: GetBenefitClaimByID :one
SELECT * FROM benefit_claims WHERE id = $1;

-- name: SetBenefitClaimCancelled :one
UPDATE benefit_claims
SET status = 'cancelled', cancelled_by = $2, cancelled_at = now(), cancel_note = $3
WHERE id = $1 AND status = 'paid'
RETURNING *;

-- name: ListBenefitClaims :many
-- Filter by employee and/or month; pass NULL to skip a filter.
SELECT c.*, e.employee_code, e.full_name,
       k.kasbon_number, k.status AS kasbon_status,
       a.name AS fund_source_name
FROM benefit_claims c
JOIN employees e ON e.id = c.employee_id
LEFT JOIN kasbons k ON k.id = c.kasbon_id
LEFT JOIN accounts a ON a.id = c.fund_source_account_id
WHERE (sqlc.narg(employee_id)::uuid IS NULL OR c.employee_id = sqlc.narg(employee_id)::uuid)
  AND (sqlc.narg(period_month)::date IS NULL OR c.period_month = sqlc.narg(period_month)::date)
ORDER BY c.claim_date DESC, c.created_at DESC;

-- name: CreateBenefitMovement :one
INSERT INTO benefit_fund_movements (
  employee_id, fund, movement_type, amount, period_month, movement_date,
  payroll_period_id, claim_id, journal_entry_id, note, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ListBenefitMovementsByEmployee :many
SELECT m.*, c.claim_number
FROM benefit_fund_movements m
LEFT JOIN benefit_claims c ON c.id = m.claim_id
WHERE m.employee_id = $1
ORDER BY m.movement_date DESC, m.created_at DESC;

-- name: CountBenefitMovementsForPeriod :one
SELECT COUNT(*)::int FROM benefit_fund_movements
WHERE payroll_period_id = $1 AND movement_type = 'contribution';

-- name: ListBenefitContributionsForPeriod :many
-- Per payroll line: the employee's benefit_fund contribution snapshotted at
-- generation (and possibly adjusted at review), with the branch the expense
-- belongs to.
SELECT pl.employee_id, e.full_name, e.branch_id, b.name AS branch_name,
       COALESCE(SUM(plc.amount), 0)::bigint AS contribution
FROM payroll_lines pl
JOIN employees e ON e.id = pl.employee_id
LEFT JOIN branches b ON b.id = e.branch_id
JOIN payroll_line_components plc ON plc.payroll_line_id = pl.id AND plc.type = 'benefit_fund'
WHERE pl.payroll_period_id = $1
GROUP BY pl.employee_id, e.full_name, e.branch_id, b.name;

-- name: ListCoveredClaimsByEmployeeForMonth :many
SELECT employee_id, COALESCE(SUM(covered_amount), 0)::bigint AS covered
FROM benefit_claims
WHERE period_month = $1 AND status = 'paid'
GROUP BY employee_id;

-- name: CountUnsettledPayrollForEmployee :one
-- Payroll periods holding this employee whose benefit contribution has not reached
-- the fund yet: still open, or closed but the ledger posting is not done.
SELECT COUNT(*)::int
FROM payroll_lines pl
JOIN payroll_periods pp ON pp.id = pl.payroll_period_id
LEFT JOIN payroll_postings ps ON ps.period_id = pp.id
WHERE pl.employee_id = $1
  AND (pp.status = 'open' OR ps.status IS DISTINCT FROM 'posted');

-- name: ListEmployeesContributedForMonth :many
-- Employees whose contribution for this month is already booked (a final-month
-- settlement at payout), so the period posting must not book it again.
SELECT DISTINCT employee_id
FROM benefit_fund_movements
WHERE period_month = $1 AND movement_type = 'contribution';

-- name: CountContributionForEmployeeMonth :one
SELECT COUNT(*)::int FROM benefit_fund_movements
WHERE employee_id = $1 AND period_month = $2 AND movement_type = 'contribution';
