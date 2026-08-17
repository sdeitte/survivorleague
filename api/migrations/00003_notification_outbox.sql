-- +goose Up
-- +goose StatementBegin
-- notification_outbox is Phase 7's pending-work queue — distinct from
-- notifications_log (which is the SENT/audit record described in the
-- Phase 0 schema, keyed by its own UNIQUE dedupe_key). This table is what
-- the dispatcher's `SELECT ... FOR UPDATE SKIP LOCKED` loop claims batches
-- from; notifications_log is written once a row's outcome (sent/failed/
-- skipped) is known. dedupe_key carries the exact same one-shot-per-event
-- semantics as notifications_log.dedupe_key (a plain UNIQUE column, not a
-- composite constraint — see notifications_log's own definition in
-- 00001_init.sql) so `INSERT ... ON CONFLICT (dedupe_key) DO NOTHING` at
-- enqueue time is what makes every trigger site in this phase safe to call
-- more than once for the same logical event (self-heal re-grading, a
-- re-fired cron tick, etc.) without ever double-queuing.
CREATE TABLE notification_outbox (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    league_id       UUID REFERENCES leagues (id) ON DELETE CASCADE,
    week_id         UUID REFERENCES weeks (id) ON DELETE SET NULL,
    type            TEXT NOT NULL,
    channel         TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    dedupe_key      TEXT NOT NULL UNIQUE,
    -- 'pending' -> ('sent' | 'failed' | 'skipped'). No DB-level enum check
    -- (mirrors every other status column in this schema, e.g.
    -- notifications_log.status, league_memberships.status) — validated in
    -- Go.  'skipped' covers both an opted-out preference and "nothing to
    -- deliver to" (no device tokens registered for a push row); neither is
    -- retried.
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ
);
CREATE INDEX idx_notification_outbox_status ON notification_outbox (status) WHERE status = 'pending';
CREATE INDEX idx_notification_outbox_user_id ON notification_outbox (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS notification_outbox;
-- +goose StatementEnd
