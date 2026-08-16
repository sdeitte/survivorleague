-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;
-- +goose StatementEnd

-- users
-- +goose StatementBegin
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    is_site_admin   BOOLEAN NOT NULL DEFAULT FALSE,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- teams
-- +goose StatementBegin
CREATE TABLE teams (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id     TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    conference      TEXT NOT NULL,
    logo_url        TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_teams_conference ON teams (conference);
-- +goose StatementEnd

-- weeks
-- +goose StatementBegin
CREATE TABLE weeks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    season_year     INTEGER NOT NULL,
    week_number     INTEGER NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (season_year, week_number)
);
-- +goose StatementEnd

-- games
-- +goose StatementBegin
CREATE TABLE games (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id     TEXT NOT NULL UNIQUE,
    week_id         UUID NOT NULL REFERENCES weeks (id) ON DELETE RESTRICT,
    home_team_id    UUID NOT NULL REFERENCES teams (id) ON DELETE RESTRICT,
    away_team_id    UUID NOT NULL REFERENCES teams (id) ON DELETE RESTRICT,
    kickoff_at      TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL DEFAULT 'scheduled',
    home_score      INTEGER,
    away_score      INTEGER,
    winner_team_id  UUID REFERENCES teams (id) ON DELETE RESTRICT,
    graded_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (home_team_id <> away_team_id)
);
CREATE INDEX idx_games_week_id ON games (week_id);
CREATE INDEX idx_games_kickoff_at ON games (kickoff_at);
CREATE INDEX idx_games_graded_at ON games (graded_at) WHERE graded_at IS NULL;
-- +goose StatementEnd

-- leagues
-- +goose StatementBegin
CREATE TABLE leagues (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                    TEXT NOT NULL,
    season_year             INTEGER NOT NULL,
    conference              TEXT NOT NULL,
    commissioner_user_id    UUID NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    invite_code             TEXT NOT NULL UNIQUE,
    status                  TEXT NOT NULL DEFAULT 'active',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_leagues_commissioner_user_id ON leagues (commissioner_user_id);
-- +goose StatementEnd

-- league_memberships
-- +goose StatementBegin
CREATE TABLE league_memberships (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    league_id           UUID NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    user_id             UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role                TEXT NOT NULL DEFAULT 'player',
    is_contestant       BOOLEAN NOT NULL DEFAULT TRUE,
    status              TEXT NOT NULL DEFAULT 'active',
    eliminated_week_id  UUID REFERENCES weeks (id) ON DELETE RESTRICT,
    eliminated_game_id  UUID REFERENCES games (id) ON DELETE RESTRICT,
    bought_back         BOOLEAN NOT NULL DEFAULT FALSE,
    bought_back_at      TIMESTAMPTZ,
    bought_back_by      UUID REFERENCES users (id) ON DELETE RESTRICT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (league_id, user_id)
);
CREATE INDEX idx_league_memberships_league_id ON league_memberships (league_id);
CREATE INDEX idx_league_memberships_user_id ON league_memberships (user_id);
-- +goose StatementEnd

-- league_invites
-- +goose StatementBegin
CREATE TABLE league_invites (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    league_id       UUID NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    email           TEXT NOT NULL,
    token           TEXT NOT NULL UNIQUE,
    expires_at      TIMESTAMPTZ NOT NULL,
    accepted_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_league_invites_league_id ON league_invites (league_id);
-- +goose StatementEnd

-- picks
-- +goose StatementBegin
CREATE TABLE picks (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    league_membership_id    UUID NOT NULL REFERENCES league_memberships (id) ON DELETE CASCADE,
    week_id                 UUID NOT NULL REFERENCES weeks (id) ON DELETE RESTRICT,
    game_id                 UUID NOT NULL REFERENCES games (id) ON DELETE RESTRICT,
    team_id                 UUID NOT NULL REFERENCES teams (id) ON DELETE RESTRICT,
    result                  TEXT NOT NULL DEFAULT 'pending',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- one pick per membership per week
    UNIQUE (league_membership_id, week_id),
    -- one team per membership ever (never repeat a pick; also makes
    -- buy-back's "used teams stay locked" free)
    UNIQUE (league_membership_id, team_id)
);
CREATE INDEX idx_picks_week_id ON picks (week_id);
CREATE INDEX idx_picks_game_id ON picks (game_id);
-- +goose StatementEnd

-- league_week_results
-- +goose StatementBegin
CREATE TABLE league_week_results (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    league_id       UUID NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    week_id         UUID NOT NULL REFERENCES weeks (id) ON DELETE RESTRICT,
    mass_wipeout    BOOLEAN NOT NULL DEFAULT FALSE,
    processed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (league_id, week_id)
);
-- +goose StatementEnd

-- device_tokens
-- +goose StatementBegin
CREATE TABLE device_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token           TEXT NOT NULL UNIQUE,
    platform        TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at    TIMESTAMPTZ
);
CREATE INDEX idx_device_tokens_user_id ON device_tokens (user_id);
-- +goose StatementEnd

-- notification_preferences
-- +goose StatementBegin
CREATE TABLE notification_preferences (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    pick_reminder       BOOLEAN NOT NULL DEFAULT TRUE,
    eliminated          BOOLEAN NOT NULL DEFAULT TRUE,
    survived            BOOLEAN NOT NULL DEFAULT TRUE,
    mass_wipeout        BOOLEAN NOT NULL DEFAULT TRUE,
    buyback             BOOLEAN NOT NULL DEFAULT TRUE,
    email_enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    push_enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- notifications_log
-- +goose StatementBegin
CREATE TABLE notifications_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    league_id       UUID REFERENCES leagues (id) ON DELETE CASCADE,
    week_id         UUID REFERENCES weeks (id) ON DELETE SET NULL,
    type            TEXT NOT NULL,
    channel         TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    dedupe_key      TEXT NOT NULL UNIQUE,
    sent_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_notifications_log_user_id ON notifications_log (user_id);
CREATE INDEX idx_notifications_log_status ON notifications_log (status) WHERE status = 'pending';
-- +goose StatementEnd

-- refresh_tokens
-- +goose StatementBegin
CREATE TABLE refresh_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash      TEXT NOT NULL UNIQUE,
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens (user_id);
-- +goose StatementEnd

-- audit_log
-- +goose StatementBegin
CREATE TABLE audit_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id   UUID REFERENCES users (id) ON DELETE SET NULL,
    league_id       UUID REFERENCES leagues (id) ON DELETE SET NULL,
    action          TEXT NOT NULL,
    target_type     TEXT,
    target_id       UUID,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_log_actor_user_id ON audit_log (actor_user_id);
CREATE INDEX idx_audit_log_league_id ON audit_log (league_id);
-- +goose StatementEnd

-- sync_runs
-- +goose StatementBegin
CREATE TABLE sync_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'running',
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ,
    error           TEXT,
    details         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sync_runs_kind ON sync_runs (kind);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS sync_runs;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS notifications_log;
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS device_tokens;
DROP TABLE IF EXISTS league_week_results;
DROP TABLE IF EXISTS picks;
DROP TABLE IF EXISTS league_invites;
DROP TABLE IF EXISTS league_memberships;
DROP TABLE IF EXISTS leagues;
DROP TABLE IF EXISTS games;
DROP TABLE IF EXISTS weeks;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
