-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(expires_at))
RETURNING *;

-- name: GetActiveRefreshTokenByHash :one
-- Active = not revoked and not expired. Callers should treat "no rows" as
-- "invalid or already-used token" without distinguishing why, to avoid
-- leaking timing/existence information.
SELECT * FROM refresh_tokens
WHERE token_hash = sqlc.arg(token_hash)
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeRefreshTokenByHash :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE token_hash = sqlc.arg(token_hash) AND revoked_at IS NULL;

-- name: RevokeAllRefreshTokensForUser :exec
-- Backs POST /auth/reset-password's "kill all other active sessions"
-- requirement: a successful password reset revokes every refresh token
-- the user currently holds (reset-password doesn't even take a refresh
-- token as input — this isn't scoped to "the one used to get here").
UPDATE refresh_tokens
SET revoked_at = now()
WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL;
