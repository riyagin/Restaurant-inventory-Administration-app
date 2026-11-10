-- name: ListUsers :many
SELECT u.id, u.username, u.role, u.created_at, u.branch_id, b.name AS branch_name
FROM users u LEFT JOIN branches b ON b.id = u.branch_id
ORDER BY u.username;

-- name: GetUserByID :one
SELECT id, username, role, created_at, branch_id FROM users WHERE id = $1;

-- name: GetUserBranchScope :one
-- The branch a branch_operator is confined to; no row for any other role.
SELECT branch_id FROM users WHERE id = $1 AND role = 'branch_operator';

-- name: CreateUser :one
INSERT INTO users (id, username, password_hash, role, branch_id)
VALUES (gen_random_uuid(), $1, $2, $3, $4)
RETURNING id, username, role, created_at, branch_id;

-- name: UpdateUser :one
UPDATE users SET username = $1, role = $2, branch_id = $3
WHERE id = $4
RETURNING id, username, role, created_at, branch_id;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $1 WHERE id = $2;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
