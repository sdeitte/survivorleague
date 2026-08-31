-- +goose Up
-- +goose StatementBegin
-- Distinguishes a real, poolable FBS team (the only kind synced before this
-- migration) from a minimal stub row created for a non-FBS opponent (e.g.
-- an FCS team an FBS team plays in a cupcake week) — see
-- internal/schedule/sync.go's resolveNonFBSOpponent. Existing rows are all
-- real FBS teams, so the default backfills them correctly with no data
-- migration needed.
ALTER TABLE teams ADD COLUMN is_fbs BOOLEAN NOT NULL DEFAULT true;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE teams DROP COLUMN is_fbs;
-- +goose StatementEnd
