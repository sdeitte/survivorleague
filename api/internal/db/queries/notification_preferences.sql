-- Phase 7: per-user notification preferences backing
-- GET/PUT /me/notification-preferences, and read by the dispatcher before
-- sending each outbox row.

-- name: GetOrCreateNotificationPreferences :one
-- Upsert-or-get in one statement: a brand-new user has no
-- notification_preferences row yet (nothing creates one at registration
-- time), so the first ever access — either the GET endpoint or the
-- dispatcher checking a preference before sending — lazily creates it with
-- the table's column defaults (see 00001_init.sql: every type is on by
-- default, matching the plan's "opt-out" framing for `survived` — on by
-- default, but the user can disable it). The `DO UPDATE SET user_id =
-- EXCLUDED.user_id` is a deliberate no-op update purely so RETURNING
-- always yields a row on conflict too, not just on insert.
INSERT INTO notification_preferences (user_id)
VALUES (sqlc.arg(user_id))
ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING *;

-- name: UpsertNotificationPreferences :one
-- PUT /me/notification-preferences is a full-replace upsert (not a
-- partial patch): a caller who has never had a preferences row yet (see
-- GetOrCreateNotificationPreferences above) can still PUT straight away
-- without a prior GET.
INSERT INTO notification_preferences
    (user_id, pick_reminder, eliminated, survived, mass_wipeout, buyback, email_enabled, push_enabled)
VALUES
    (sqlc.arg(user_id), sqlc.arg(pick_reminder), sqlc.arg(eliminated), sqlc.arg(survived),
     sqlc.arg(mass_wipeout), sqlc.arg(buyback), sqlc.arg(email_enabled), sqlc.arg(push_enabled))
ON CONFLICT (user_id) DO UPDATE SET
    pick_reminder = EXCLUDED.pick_reminder,
    eliminated    = EXCLUDED.eliminated,
    survived      = EXCLUDED.survived,
    mass_wipeout  = EXCLUDED.mass_wipeout,
    buyback       = EXCLUDED.buyback,
    email_enabled = EXCLUDED.email_enabled,
    push_enabled  = EXCLUDED.push_enabled,
    updated_at    = now()
RETURNING *;
