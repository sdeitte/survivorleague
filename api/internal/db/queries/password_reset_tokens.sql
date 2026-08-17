-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(expires_at))
RETURNING *;

-- name: GetActivePasswordResetTokenByHash :one
-- Active = not used and not expired. Callers should treat "no rows" as
-- "invalid or expired token" without distinguishing why, to avoid leaking
-- timing/existence information — mirrors GetActiveRefreshTokenByHash's own
-- comment in refresh_tokens.sql. This is what backs POST
-- /auth/reset-password and /auth/verify-email's single generic error
-- message for invalid/expired/already-used tokens.
SELECT * FROM password_reset_tokens
WHERE token_hash = sqlc.arg(token_hash)
  AND used_at IS NULL
  AND expires_at > now();

-- name: MarkPasswordResetTokenUsed :one
-- Conditioned on used_at IS NULL (not just id match) so a concurrent
-- replay of the same token loses the race cleanly: RETURNING zero rows
-- (mapped to pgx.ErrNoRows by the caller, same pattern as
-- BuyBackMembership's race handling) rather than silently double-applying.
UPDATE password_reset_tokens
SET used_at = now()
WHERE id = sqlc.arg(id) AND used_at IS NULL
RETURNING *;
