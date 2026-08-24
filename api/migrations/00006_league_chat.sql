-- +goose Up
-- +goose StatementBegin
-- league_messages is the smack-talk chat feed — one flat, append-only
-- table per the plan's design (no threading/reactions). No expires_at or
-- cleanup job: the 7-day TTL is enforced entirely at read time
-- (internal/chat.Service.ListRecentMessages filters on created_at), so an
-- old message just stops being returned rather than needing anything
-- scheduled to delete it.
CREATE TABLE league_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    league_id       UUID NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_league_messages_league_id_created_at ON league_messages (league_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS league_messages;
-- +goose StatementEnd
