-- name: GetUserByUsername :one
SELECT id, username, password_hash, role
FROM users
WHERE username = $1;

-- name: InsertTokenBlocklist :exec
INSERT INTO token_blocklist (jti, expires_at)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: IsTokenBlocked :one
SELECT EXISTS(
  SELECT 1 FROM token_blocklist WHERE jti = $1
) AS blocked;

-- name: DeleteExpiredTokens :exec
DELETE FROM token_blocklist WHERE expires_at < NOW();
