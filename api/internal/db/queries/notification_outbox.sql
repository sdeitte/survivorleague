-- Phase 7: the notification_outbox queue and its dispatcher. See
-- internal/notify's package doc comment for the full design.

-- name: EnqueueNotification :one
-- ON CONFLICT (dedupe_key) DO NOTHING is the single-shot-per-event
-- guarantee: every trigger site in internal/notify calls this
-- unconditionally (self-heal re-grading, a re-fired cron tick, ...) and
-- relies on this to make repeat calls no-ops. Mapped by the caller to "no
-- rows returned = already enqueued", not an error.
INSERT INTO notification_outbox (user_id, league_id, week_id, type, channel, payload, dedupe_key)
VALUES (sqlc.arg(user_id), sqlc.narg(league_id), sqlc.narg(week_id), sqlc.arg(type), sqlc.arg(channel), sqlc.arg(payload), sqlc.arg(dedupe_key))
ON CONFLICT (dedupe_key) DO NOTHING
RETURNING *;

-- name: ClaimPendingNotifications :many
-- The dispatcher's claim step: FOR UPDATE SKIP LOCKED is what guarantees a
-- row is only ever claimed by one concurrent dispatcher process/tick at a
-- time — a second caller racing this same query simply skips any row the
-- first has already locked rather than blocking or double-claiming it.
-- Held for the lifetime of the caller's transaction (see
-- notify.Service.DispatchBatch), which also covers the actual
-- push/email send attempts — acceptable at this scale (a handful of
-- notifications per tick), and specifically what makes a mid-dispatch
-- crash safe: an aborted transaction leaves every claimed row exactly as
-- it was (still 'pending'), so the next tick just retries it, rather than
-- leaving rows stranded in some intermediate "claimed but never finished"
-- state.
SELECT * FROM notification_outbox
WHERE status = 'pending'
ORDER BY created_at ASC
LIMIT sqlc.arg(limit_count)
FOR UPDATE SKIP LOCKED;

-- name: MarkNotificationSent :exec
UPDATE notification_outbox SET status = 'sent', sent_at = now() WHERE id = sqlc.arg(id);

-- name: MarkNotificationSkipped :exec
-- Terminal, non-retried outcome for an opted-out preference or "nothing to
-- deliver to" (e.g. a push row for a user with zero registered device
-- tokens) — see internal/notify's dispatch doc comment for why this is
-- deliberately distinct from 'failed'.
UPDATE notification_outbox SET status = 'skipped' WHERE id = sqlc.arg(id);

-- name: MarkNotificationFailedOrRetry :one
-- Increments attempts and, once sqlc.arg(max_attempts) is reached, moves
-- the row to the terminal 'failed' status; otherwise leaves it 'pending'
-- so the next dispatcher tick retries it. Bounded-retry counterpart to
-- MarkNotificationSent/MarkNotificationSkipped's single-shot outcomes.
UPDATE notification_outbox
SET attempts = attempts + 1,
    status = CASE WHEN attempts + 1 >= sqlc.arg(max_attempts) THEN 'failed' ELSE 'pending' END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpsertNotificationLog :exec
-- Writes the audit/dedupe record once a row's outcome is known (mirrors
-- the outbox row's own dedupe_key 1:1, so this is always exactly one log
-- row per outbox row). ON CONFLICT DO UPDATE (rather than DO NOTHING)
-- because a retried row calls this again with a new status each attempt.
INSERT INTO notifications_log (user_id, league_id, week_id, type, channel, status, dedupe_key, sent_at)
VALUES (sqlc.arg(user_id), sqlc.narg(league_id), sqlc.narg(week_id), sqlc.arg(type), sqlc.arg(channel), sqlc.arg(status), sqlc.arg(dedupe_key), sqlc.narg(sent_at))
ON CONFLICT (dedupe_key) DO UPDATE SET
    status  = EXCLUDED.status,
    sent_at = EXCLUDED.sent_at;

-- name: CountPendingNotifications :one
-- Test/verification helper — lets integration tests assert "N rows
-- enqueued" without reaching for raw SQL.
SELECT count(*) FROM notification_outbox WHERE status = 'pending';

-- name: ListNotificationOutboxForUser :many
-- Test/verification + potential future admin-debugging helper: every
-- outbox row for a user, newest first.
SELECT * FROM notification_outbox WHERE user_id = sqlc.arg(user_id) ORDER BY created_at DESC;
