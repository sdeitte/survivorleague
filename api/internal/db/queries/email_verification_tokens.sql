-- name: CreateEmailVerificationToken :one
INSERT INTO email_verification_tokens (user_id, token_hash, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(expires_at))
RETURNING *;

-- name: GetActiveEmailVerificationTokenByHash :one
-- Active = not used and not expired — see
-- GetActivePasswordResetTokenByHash's comment in password_reset_tokens.sql
-- for why "no rows" stays undifferentiated all the way out to the API
-- response.
SELECT * FROM email_verification_tokens
WHERE token_hash = sqlc.arg(token_hash)
  AND used_at IS NULL
  AND expires_at > now();

-- name: MarkEmailVerificationTokenUsed :one
-- Conditioned on used_at IS NULL so a concurrent replay of the same token
-- loses the race cleanly (RETURNING zero rows -> pgx.ErrNoRows), same
-- pattern as MarkPasswordResetTokenUsed.
UPDATE email_verification_tokens
SET used_at = now()
WHERE id = sqlc.arg(id) AND used_at IS NULL
RETURNING *;

-- name: InvalidatePendingEmailVerificationTokens :exec
-- Called before minting a fresh verification token (registration's
-- automatic first send, or an explicit POST /auth/resend-verification) so
-- at most one unused verification token ever exists per user — an old,
-- un-clicked link stops working the moment a newer one is issued instead
-- of leaving multiple simultaneously-valid tokens outstanding.
UPDATE email_verification_tokens
SET used_at = now()
WHERE user_id = sqlc.arg(user_id) AND used_at IS NULL;
