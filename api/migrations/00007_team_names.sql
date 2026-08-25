-- +goose Up
-- +goose StatementBegin
-- team_name is per-membership (a user can have a different team name in
-- each league they're in), not on users — see internal/leagues.Service's
-- CreateLeague/JoinByCode doc comments for why it's required going
-- forward at join/create time. Nullable here (no backfill): existing
-- memberships from before this shipped stay NULL until their owner sets
-- one via the one-time prompt on the league overview page — that prompt
-- logic is nothing more than "is my own membership's team_name still
-- null", so it needs no per-league special-casing to only affect
-- pre-existing memberships.
ALTER TABLE league_memberships ADD COLUMN team_name TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE league_memberships DROP COLUMN team_name;
-- +goose StatementEnd
