-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name, is_site_admin)
VALUES (lower(sqlc.arg(email)), sqlc.arg(password_hash), sqlc.arg(display_name), sqlc.arg(is_site_admin))
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = lower(sqlc.arg(email));

-- name: GetUserByID :one
SELECT * FROM users WHERE id = sqlc.arg(id);

-- name: UpdateUserDisplayName :one
UPDATE users
SET display_name = sqlc.arg(display_name), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateUserStatus :one
-- Backs POST /admin/users/:id/disable and .../enable (Phase 8). status is
-- validated against the app-level enum (active/disabled — see
-- internal/admin's UserStatus constants) by the caller, not a DB CHECK
-- constraint, matching every other status column in this schema (see
-- notification_outbox.sql's comment on the same convention). Login already
-- rejects any user.status != 'active' (internal/auth.Service.Login), so
-- setting status='disabled' here is what actually blocks a disabled user's
-- next login attempt.
UPDATE users
SET status = sqlc.arg(status), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateUserPasswordHash :one
-- Backs POST /auth/reset-password. The caller (internal/auth.Service.
-- ResetPassword) computes password_hash via the same argon2id
-- HashPassword helper Register uses — this query just persists it.
UPDATE users
SET password_hash = sqlc.arg(password_hash), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkUserEmailVerified :one
-- Backs POST /auth/verify-email.
UPDATE users
SET email_verified_at = now(), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListUsersAdmin :many
-- Backs GET /admin/users (Phase 8, requireSiteAdmin) — every user in the
-- system, not scoped to the requester (unlike every other user-facing
-- endpoint so far). league_count is how many non-removed league_memberships
-- rows this user has, computed inline rather than via a join+GROUP BY so a
-- user in zero leagues still gets one output row.
SELECT
    u.id, u.email, u.display_name, u.is_site_admin, u.status, u.created_at,
    (SELECT count(*) FROM league_memberships m WHERE m.user_id = u.id AND m.removed_at IS NULL)::bigint AS league_count
FROM users u
ORDER BY u.created_at DESC, u.id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountUsersAdmin :one
SELECT count(*) FROM users;
