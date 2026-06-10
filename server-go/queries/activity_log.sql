-- name: InsertActivityLog :exec
INSERT INTO activity_log (id, user_id, username, action, entity_type, entity_id, description)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6);

-- name: ListActivityLog :many
SELECT
    al.id, al.user_id, al.username, al.action, al.entity_type,
    al.entity_id, al.description, al.created_at
FROM activity_log al
WHERE ($1 = '' OR al.entity_type = $1)
  AND ($2 = '' OR al.action = $2)
  AND ($3 = '' OR al.username ILIKE '%' || $3 || '%')
  AND ($4::date IS NULL OR al.created_at::date >= $4)
  AND ($5::date IS NULL OR al.created_at::date <= $5)
ORDER BY al.created_at DESC
LIMIT $6 OFFSET $7;

-- name: CountActivityLog :one
SELECT COUNT(*) FROM activity_log
WHERE ($1 = '' OR entity_type = $1)
  AND ($2 = '' OR action = $2)
  AND ($3 = '' OR username ILIKE '%' || $3 || '%')
  AND ($4::date IS NULL OR created_at::date >= $4)
  AND ($5::date IS NULL OR created_at::date <= $5);

-- name: ListActivityLogForExport :many
SELECT id, username, action, entity_type, entity_id, description, created_at
FROM activity_log
WHERE ($1::date IS NULL OR created_at::date >= $1)
  AND ($2::date IS NULL OR created_at::date <= $2)
ORDER BY created_at DESC;

-- name: DeleteOldActivityLog :execrows
DELETE FROM activity_log WHERE created_at < $1;
