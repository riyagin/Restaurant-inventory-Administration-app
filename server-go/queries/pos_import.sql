-- name: ListPOSImports :many
SELECT
    pi.id, pi.description, pi.date, pi.source_file, pi.total_amount, pi.created_at,
    u.username AS created_by_name
FROM pos_imports pi
LEFT JOIN users u ON u.id = pi.created_by
WHERE ($1::date IS NULL OR pi.date >= $1)
  AND ($2::date IS NULL OR pi.date <= $2)
ORDER BY pi.date DESC, pi.created_at DESC;

-- name: ListAllPOSImportLines :many
SELECT
    pil.id, pil.import_id, pil.account_id, pil.label, pil.amount, pil.line_type,
    a.name AS account_name
FROM pos_import_lines pil
LEFT JOIN accounts a ON a.id = pil.account_id
ORDER BY pil.import_id, pil.line_type DESC, pil.amount DESC;

-- name: GetPOSImportByID :one
SELECT id, description, date, source_file, total_amount, created_by, created_at
FROM pos_imports WHERE id = $1;

-- name: GetPOSImportLines :many
SELECT
    pil.id, pil.import_id, pil.account_id, pil.label, pil.amount, pil.line_type,
    a.name AS account_name
FROM pos_import_lines pil
LEFT JOIN accounts a ON a.id = pil.account_id
WHERE pil.import_id = $1
ORDER BY pil.line_type, pil.label;

-- name: InsertPOSImport :one
INSERT INTO pos_imports (description, date, source_file, total_amount, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at;

-- name: InsertPOSImportLine :exec
INSERT INTO pos_import_lines (import_id, account_id, label, amount, line_type)
VALUES ($1, $2, $3, $4, $5);

-- name: DeletePOSImportLines :exec
DELETE FROM pos_import_lines WHERE import_id = $1;

-- name: DeletePOSImport :exec
DELETE FROM pos_imports WHERE id = $1;

-- name: GetPOSImportLinesForReversal :many
SELECT account_id, amount, line_type FROM pos_import_lines WHERE import_id = $1;
