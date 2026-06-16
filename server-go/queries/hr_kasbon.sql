-- ── Kasbons ──────────────────────────────────────────────────────────────────

-- name: GetMaxKasbonSeqForYear :one
SELECT COALESCE(MAX(CAST(SUBSTRING(kasbon_number FROM 10) AS INTEGER)), 0)::int AS max_seq
FROM kasbons
WHERE kasbon_number ~ ('^KSB-' || $1::text || '-[0-9]+$');

-- name: CreateKasbon :one
INSERT INTO kasbons (
    id, kasbon_number, employee_id, amount, details, sending_method,
    fund_source_account_id, request_date, resolution_month, status, created_by
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, 'pending', $9)
RETURNING id, kasbon_number, employee_id, amount, details, sending_method,
          fund_source_account_id, request_date, resolution_month, status,
          approved_by, approved_at, approval_note, processed_by, processed_at,
          evidence_photo_path, created_by, created_at;

-- name: GetKasbonByID :one
SELECT id, kasbon_number, employee_id, amount, details, sending_method,
       fund_source_account_id, request_date, resolution_month, status,
       approved_by, approved_at, approval_note, processed_by, processed_at,
       evidence_photo_path, created_by, created_at
FROM kasbons
WHERE id = $1;

-- name: UpdateKasbon :one
UPDATE kasbons
SET amount = $1, details = $2, sending_method = $3, fund_source_account_id = $4,
    resolution_month = $5
WHERE id = $6
RETURNING id, kasbon_number, employee_id, amount, details, sending_method,
          fund_source_account_id, request_date, resolution_month, status,
          approved_by, approved_at, approval_note, processed_by, processed_at,
          evidence_photo_path, created_by, created_at;

-- name: ApproveKasbon :one
UPDATE kasbons
SET status = 'approved', approved_by = $1, approved_at = now(), approval_note = $2
WHERE id = $3
RETURNING id, kasbon_number, employee_id, amount, details, sending_method,
          fund_source_account_id, request_date, resolution_month, status,
          approved_by, approved_at, approval_note, processed_by, processed_at,
          evidence_photo_path, created_by, created_at;

-- name: RejectKasbon :one
UPDATE kasbons
SET status = 'rejected', approved_by = $1, approved_at = now(), approval_note = $2
WHERE id = $3
RETURNING id, kasbon_number, employee_id, amount, details, sending_method,
          fund_source_account_id, request_date, resolution_month, status,
          approved_by, approved_at, approval_note, processed_by, processed_at,
          evidence_photo_path, created_by, created_at;

-- name: SetKasbonCancelled :one
UPDATE kasbons
SET status = 'cancelled'
WHERE id = $1
RETURNING id, kasbon_number, employee_id, amount, details, sending_method,
          fund_source_account_id, request_date, resolution_month, status,
          approved_by, approved_at, approval_note, processed_by, processed_at,
          evidence_photo_path, created_by, created_at;

-- name: SetKasbonProcessed :one
UPDATE kasbons
SET status = 'processed', processed_by = $1, processed_at = now(),
    evidence_photo_path = $2
WHERE id = $3
RETURNING id, kasbon_number, employee_id, amount, details, sending_method,
          fund_source_account_id, request_date, resolution_month, status,
          approved_by, approved_at, approval_note, processed_by, processed_at,
          evidence_photo_path, created_by, created_at;

-- name: SetKasbonResolved :exec
UPDATE kasbons
SET status = 'resolved'
WHERE id = $1;

-- name: ListKasbons :many
SELECT k.id, k.kasbon_number, k.employee_id, k.amount, k.details, k.sending_method,
       k.fund_source_account_id, k.request_date, k.resolution_month, k.status,
       k.approved_by, k.approved_at, k.approval_note, k.processed_by, k.processed_at,
       k.evidence_photo_path, k.created_by, k.created_at,
       e.full_name AS employee_name, e.employee_code,
       a.name AS fund_source_name
FROM kasbons k
JOIN employees e ON e.id = k.employee_id
JOIN accounts a ON a.id = k.fund_source_account_id
WHERE ($1::text = '' OR k.status = $1)
  AND ($2::uuid IS NULL OR k.employee_id = $2)
  AND ($3::text = '' OR lower(e.full_name) LIKE '%' || lower($3) || '%'
       OR lower(e.employee_code) LIKE '%' || lower($3) || '%'
       OR lower(k.kasbon_number) LIKE '%' || lower($3) || '%')
ORDER BY k.created_at DESC;

-- name: GetLastResolvedKasbon :one
SELECT id, kasbon_number, amount, request_date, resolution_month, processed_at
FROM kasbons
WHERE employee_id = $1 AND status = 'resolved'
ORDER BY COALESCE(processed_at, created_at) DESC
LIMIT 1;

-- ── Kasbon Installments ──────────────────────────────────────────────────────

-- name: CreateKasbonInstallment :one
INSERT INTO kasbon_installments (id, kasbon_id, due_month, amount, status)
VALUES (gen_random_uuid(), $1, $2, $3, 'pending')
RETURNING id, kasbon_id, due_month, amount, payroll_line_id, status;

-- name: DeleteKasbonInstallments :exec
DELETE FROM kasbon_installments WHERE kasbon_id = $1;

-- name: ListKasbonInstallments :many
SELECT id, kasbon_id, due_month, amount, payroll_line_id, status
FROM kasbon_installments
WHERE kasbon_id = $1
ORDER BY due_month;

-- name: ListPendingInstallmentsForEmployee :many
SELECT ki.id, ki.kasbon_id, ki.due_month, ki.amount, ki.payroll_line_id, ki.status
FROM kasbon_installments ki
JOIN kasbons k ON k.id = ki.kasbon_id
WHERE k.employee_id = $1
  AND k.status = 'processed'
  AND ki.status = 'pending'
  AND ki.due_month <= $2
ORDER BY ki.due_month;

-- name: MarkKasbonInstallmentDeducted :exec
UPDATE kasbon_installments
SET status = 'deducted', payroll_line_id = $1
WHERE id = $2;

-- name: CountPendingInstallments :one
SELECT COUNT(*) FROM kasbon_installments
WHERE kasbon_id = $1 AND status = 'pending';

-- name: CountInstallments :one
SELECT COUNT(*) FROM kasbon_installments
WHERE kasbon_id = $1;
