-- ── THR Runs ─────────────────────────────────────────────────────────────────

-- name: CreateThrRun :one
INSERT INTO thr_runs (id, name, payment_date, status, created_by)
VALUES (gen_random_uuid(), $1, $2, 'open', $3)
RETURNING id, name, payment_date, status, created_by, closed_at, paid_at, created_at;

-- name: GetThrRunByID :one
SELECT id, name, payment_date, status, created_by, closed_at, paid_at, created_at
FROM thr_runs
WHERE id = $1;

-- name: ListThrRuns :many
SELECT
    r.id, r.name, r.payment_date, r.status, r.created_by,
    r.closed_at, r.paid_at, r.created_at,
    COALESCE(SUM(l.thr_amount), 0)::bigint       AS total_thr,
    COUNT(l.id)::int                             AS line_count,
    COUNT(l.id) FILTER (WHERE l.reviewed)::int   AS reviewed_count
FROM thr_runs r
LEFT JOIN thr_lines l ON l.thr_run_id = r.id
GROUP BY r.id
ORDER BY r.payment_date DESC;

-- name: GetThrRunSummary :one
SELECT
    COALESCE(SUM(l.thr_amount), 0)::bigint AS total_thr,
    COALESCE(SUM(l.computed_amount), 0)::bigint AS total_computed,
    COUNT(l.id)::int AS line_count,
    COUNT(l.id) FILTER (WHERE l.reviewed)::int AS reviewed_count
FROM thr_lines l
WHERE l.thr_run_id = $1;

-- name: DeleteThrRun :exec
DELETE FROM thr_runs WHERE id = $1;

-- name: CloseThrRun :one
UPDATE thr_runs
SET status = 'closed', closed_at = now()
WHERE id = $1
RETURNING id, name, payment_date, status, created_by, closed_at, paid_at, created_at;

-- name: MarkThrRunPaid :one
UPDATE thr_runs
SET status = 'paid', paid_at = now()
WHERE id = $1
RETURNING id, name, payment_date, status, created_by, closed_at, paid_at, created_at;

-- ── THR Lines ────────────────────────────────────────────────────────────────

-- name: CreateThrLine :one
INSERT INTO thr_lines (
    id, thr_run_id, employee_id, wage_structure_id, base_salary, join_date,
    months_worked, thr_ratio, computed_amount, thr_amount
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, thr_run_id, employee_id, wage_structure_id, base_salary, join_date,
          months_worked, thr_ratio, computed_amount, thr_amount,
          reviewed, reviewed_by, reviewed_at, review_note;

-- name: GetThrLineByID :one
SELECT id, thr_run_id, employee_id, wage_structure_id, base_salary, join_date,
       months_worked, thr_ratio, computed_amount, thr_amount,
       reviewed, reviewed_by, reviewed_at, review_note
FROM thr_lines
WHERE id = $1;

-- name: GetThrLineByRunEmployee :one
SELECT id, thr_run_id, employee_id, wage_structure_id, base_salary, join_date,
       months_worked, thr_ratio, computed_amount, thr_amount,
       reviewed, reviewed_by, reviewed_at, review_note
FROM thr_lines
WHERE thr_run_id = $1 AND employee_id = $2;

-- name: ListThrLinesForRun :many
SELECT
    l.id, l.thr_run_id, l.employee_id, l.wage_structure_id, l.base_salary, l.join_date,
    l.months_worked, l.thr_ratio, l.computed_amount, l.thr_amount,
    l.reviewed, l.reviewed_by, l.reviewed_at, l.review_note,
    e.full_name AS employee_name, e.employee_code,
    e.position_id, pos.name AS position_name,
    e.branch_id, b.name AS branch_name
FROM thr_lines l
JOIN employees e ON e.id = l.employee_id
LEFT JOIN positions pos ON pos.id = e.position_id
LEFT JOIN branches b ON b.id = e.branch_id
WHERE l.thr_run_id = $1
  AND (sqlc.arg('q')::text = '' OR lower(e.full_name) LIKE '%' || lower(sqlc.arg('q')) || '%'
       OR lower(e.employee_code) LIKE '%' || lower(sqlc.arg('q')) || '%')
  AND (sqlc.narg('position_id')::uuid IS NULL OR e.position_id = sqlc.narg('position_id'))
  AND (sqlc.narg('branch_id')::uuid IS NULL OR e.branch_id = sqlc.narg('branch_id'))
ORDER BY
    CASE WHEN sqlc.arg('sort')::text = 'thr_amount' AND sqlc.arg('order')::text = 'asc'  THEN l.thr_amount END ASC,
    CASE WHEN sqlc.arg('sort')::text = 'thr_amount' AND sqlc.arg('order')::text = 'desc' THEN l.thr_amount END DESC,
    CASE WHEN sqlc.arg('sort')::text = 'name'       AND sqlc.arg('order')::text = 'desc' THEN e.full_name END DESC,
    e.full_name ASC;

-- name: CountUnreviewedThrLines :one
SELECT COUNT(*) FROM thr_lines
WHERE thr_run_id = $1 AND reviewed = false;

-- name: ListThrLineBranchTotals :many
SELECT e.branch_id, COALESCE(SUM(l.thr_amount), 0)::bigint AS total_thr
FROM thr_lines l
JOIN employees e ON e.id = l.employee_id
WHERE l.thr_run_id = $1
GROUP BY e.branch_id;

-- name: UpdateThrLineReview :one
UPDATE thr_lines
SET thr_amount = $1,
    reviewed = true, reviewed_by = $2, reviewed_at = now(), review_note = $3
WHERE id = $4
RETURNING id, thr_run_id, employee_id, wage_structure_id, base_salary, join_date,
          months_worked, thr_ratio, computed_amount, thr_amount,
          reviewed, reviewed_by, reviewed_at, review_note;

-- name: UnreviewThrLine :one
UPDATE thr_lines
SET reviewed = false, reviewed_by = NULL, reviewed_at = NULL
WHERE id = $1
RETURNING id, thr_run_id, employee_id, wage_structure_id, base_salary, join_date,
          months_worked, thr_ratio, computed_amount, thr_amount,
          reviewed, reviewed_by, reviewed_at, review_note;

-- name: DeleteThrLine :exec
DELETE FROM thr_lines WHERE id = $1;

-- name: GetThrLineForPayslip :one
SELECT
    l.id, l.thr_run_id, l.employee_id,
    l.base_salary, l.months_worked, l.thr_ratio, l.computed_amount, l.thr_amount,
    l.review_note,
    e.full_name AS employee_name, e.employee_code, e.join_date,
    pos.name AS position_name,
    b.name   AS branch_name,
    r.name AS run_name, r.payment_date, r.status AS run_status
FROM thr_lines l
JOIN thr_runs r        ON r.id = l.thr_run_id
JOIN employees e       ON e.id = l.employee_id
LEFT JOIN positions pos ON pos.id = e.position_id
LEFT JOIN branches  b   ON b.id  = e.branch_id
WHERE l.id = $1;
