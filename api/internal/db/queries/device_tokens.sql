-- Phase 7: device token registration backing POST/DELETE /me/device-tokens.

-- name: UpsertDeviceToken :one
-- token itself is globally UNIQUE (not a composite (user_id, token) key —
-- see device_tokens' definition in 00001_init.sql), so re-registering the
-- same Expo push token reassigns it to whichever user_id/platform is
-- registering now (covers a token surviving a logout/login as a different
-- account on a shared device) rather than erroring.
INSERT INTO device_tokens (user_id, token, platform, last_used_at)
VALUES (sqlc.arg(user_id), sqlc.arg(token), sqlc.arg(platform), now())
ON CONFLICT (token) DO UPDATE SET
    user_id      = EXCLUDED.user_id,
    platform     = EXCLUDED.platform,
    updated_at   = now(),
    last_used_at = now()
RETURNING *;

-- name: DeleteDeviceToken :exec
DELETE FROM device_tokens WHERE user_id = sqlc.arg(user_id) AND token = sqlc.arg(token);

-- name: ListDeviceTokensForUser :many
SELECT * FROM device_tokens WHERE user_id = sqlc.arg(user_id);
