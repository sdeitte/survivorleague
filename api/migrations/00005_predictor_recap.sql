-- +goose Up
-- +goose StatementBegin
-- game_predictions holds CFBD's pregame win-probability/spread for a game
-- (GET /metrics/wp/pregame), synced alongside the schedule (see
-- internal/schedule/sync.go). CFBD only publishes this in the ~1 week
-- before kickoff, not for the whole season in advance, so both columns
-- are nullable and simply absent until CFBD has it. spread/
-- home_win_probability are from the HOME team's perspective as CFBD
-- reports them; callers normalize per-team (negate spread for the away
-- team) at read time rather than storing it twice.
CREATE TABLE game_predictions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id                 UUID NOT NULL UNIQUE REFERENCES games (id) ON DELETE CASCADE,
    spread                  NUMERIC,
    home_win_probability    NUMERIC,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- team_sp_ratings holds CFBD's SP+ power rating (GET /ratings/sp), synced
-- alongside the schedule. Unlike game_predictions this is available for
-- the whole season up front, so it's the fallback matchup-strength signal
-- shown on the pick screen for weeks too far out for a real win
-- probability yet. CFBD's response always includes a synthetic
-- "nationalAverages" pseudo-team row that sync must skip — see
-- internal/schedule/sync.go's SyncSPRatings doc comment.
CREATE TABLE team_sp_ratings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id         UUID NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    season_year     INTEGER NOT NULL,
    rating          NUMERIC NOT NULL,
    ranking         INTEGER,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, season_year)
);

-- week_recaps holds the AI-generated (Anthropic API) weekly recap text for
-- a league's week, written once TryFinalizeLeagueWeek finalizes it (see
-- internal/grading/service.go). UNIQUE(league_id, week_id) makes
-- regeneration/retry a safe upsert, same pattern as league_week_results.
CREATE TABLE week_recaps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    league_id       UUID NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    week_id         UUID NOT NULL REFERENCES weeks (id) ON DELETE CASCADE,
    body            TEXT NOT NULL,
    generated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (league_id, week_id)
);

-- weekly_recap mirrors the existing per-type notification_preferences
-- columns (pick_reminder/eliminated/survived/mass_wipeout/buyback) —
-- email-only delivery (see NotifyService.EnqueueWeeklyRecap), gated the
-- same way as every other type.
ALTER TABLE notification_preferences ADD COLUMN weekly_recap BOOLEAN NOT NULL DEFAULT TRUE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE notification_preferences DROP COLUMN weekly_recap;
DROP TABLE IF EXISTS week_recaps;
DROP TABLE IF EXISTS team_sp_ratings;
DROP TABLE IF EXISTS game_predictions;
-- +goose StatementEnd
