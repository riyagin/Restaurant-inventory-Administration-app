-- name: ListUsers :many
SELECT id, username, role, created_at FROM users ORDER BY username;

-- name: GetUserByID :one
SELECT id, username, role, created_at FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (id, username, password_hash, role)
VALUES (gen_random_uuid(), $1, $2, $3)
RETURNING id, username, role, created_at;

-- name: UpdateUser :one
UPDATE users SET username = $1, role = $2
WHERE id = $3
RETURNING id, username, role, created_at;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $1 WHERE id = $2;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
