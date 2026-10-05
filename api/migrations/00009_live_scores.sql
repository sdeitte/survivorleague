-- +goose Up
-- +goose StatementBegin
-- Purely cosmetic, user-facing live score fields — deliberately separate
-- from the authoritative status/home_score/away_score/winner_team_id
-- columns that grading.Service depends on. Those stay sourced only from
-- CFBD's GET /games `completed` flag (via the existing schedule sync/poll
-- path) so grading can never be affected by this feature. These live_*
-- columns are instead refreshed from CFBD's separate GET /scoreboard feed
-- (internal/schedule.Service.RefreshLiveScores, called from the same
-- livepoll tick that already runs during a game's live window) and are
-- read-only input to the UI: "is there a score to show right now, and
-- what is it" — never consulted by grading.
--
-- live_status mirrors CFBD's own scoreboard status string verbatim
-- ("scheduled" | "in_progress" | "completed") rather than reusing this
-- table's own status enum, since the two are deliberately different
-- vocabularies for different purposes.
ALTER TABLE games ADD COLUMN live_status TEXT;
ALTER TABLE games ADD COLUMN live_home_score INTEGER;
ALTER TABLE games ADD COLUMN live_away_score INTEGER;
ALTER TABLE games ADD COLUMN live_period INTEGER;
ALTER TABLE games ADD COLUMN live_clock TEXT;
ALTER TABLE games ADD COLUMN live_updated_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE games DROP COLUMN live_status;
ALTER TABLE games DROP COLUMN live_home_score;
ALTER TABLE games DROP COLUMN live_away_score;
ALTER TABLE games DROP COLUMN live_period;
ALTER TABLE games DROP COLUMN live_clock;
ALTER TABLE games DROP COLUMN live_updated_at;
-- +goose StatementEnd
