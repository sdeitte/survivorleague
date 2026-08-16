-- +goose Up
-- +goose StatementBegin
-- Distinct from the gameplay `status` enum (active/eliminated): a
-- commissioner-removed member needs a separate signal so grading/leaderboard
-- logic (later phases) doesn't have to special-case a 'removed' gameplay
-- status. The row is kept (not hard-deleted) since future phases FK picks
-- to league_membership_id, and removed_at IS NULL is what every
-- membership-scoped query (access checks, member/leaderboard listings)
-- filters on.
ALTER TABLE league_memberships ADD COLUMN removed_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE league_memberships DROP COLUMN removed_at;
-- +goose StatementEnd
