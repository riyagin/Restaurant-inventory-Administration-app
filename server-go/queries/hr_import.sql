-- name: CreateHRImportBatch :one
INSERT INTO hr_import_batches (id, uploaded_by, filename, payload, row_count, status)
VALUES (gen_random_uuid(), $1, $2, $3, $4, 'parsed')
RETURNING id, uploaded_by, filename, payload, row_count, status, created_at;

-- name: GetHRImportBatch :one
SELECT id, uploaded_by, filename, payload, row_count, status, created_at
FROM hr_import_batches
WHERE id = $1;

-- name: MarkHRImportBatchConfirmed :exec
UPDATE hr_import_batches
SET status = 'confirmed'
WHERE id = $1;
