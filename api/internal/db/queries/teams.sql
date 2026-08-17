-- name: UpsertTeam :one
-- Match/upsert on external_id (CFBD's team id) per the Phase 3 sync
-- contract. conference is always the *normalized* name (mapped from
-- CFBD's raw string by internal/schedule's normalization table before this
-- is ever called) — never CFBD's raw string.
INSERT INTO teams (external_id, name, conference, logo_url)
VALUES (sqlc.arg(external_id), sqlc.arg(name), sqlc.arg(conference), sqlc.arg(logo_url))
ON CONFLICT (external_id) DO UPDATE SET
    name = EXCLUDED.name,
    conference = EXCLUDED.conference,
    logo_url = EXCLUDED.logo_url,
    updated_at = now()
RETURNING *;

-- name: ListTeams :many
-- conference is an optional exact-match filter: pass a NULL narg to list
-- every team, or a canonical conference name to filter to it.
SELECT * FROM teams
WHERE sqlc.narg(conference)::text IS NULL OR conference = sqlc.narg(conference)
ORDER BY name ASC;

-- name: GetTeamByID :one
SELECT * FROM teams WHERE id = sqlc.arg(id);

-- name: ListEligibleConferences :many
-- Conferences a league can be created for: real FBS conferences with
-- enough member teams to sustain a ~13-week survivor season, excluding
-- FBS Independents (not a real conference — its members don't play each
-- other on a fixed schedule, so it can't anchor a conference-scoped
-- pool). The 13-team minimum and the Independents exclusion were both
-- explicit product decisions after post-realignment data showed Conference
-- USA/Mountain West/Pac-12 had shrunk to single digits. Computed live from
-- teams.conference (not a hardcoded list) so this stays correct through
-- future realignment without a code change — see
-- internal/schedule/conferences.go's FBSConferences for the separate,
-- still-hardcoded canonical name list this filters against (used for CFBD
-- normalization, not eligibility).
SELECT conference
FROM teams
WHERE conference != 'FBS Independents'
GROUP BY conference
HAVING count(*) >= sqlc.arg(min_teams)::int
ORDER BY conference ASC;

-- name: GetTeamByExternalID :one
-- Backs RefreshWeek's team-id resolution: unlike SyncSeason (which
-- upserts every team fresh from a /teams/fbs response), a narrow week
-- refresh only pulls /games and needs to resolve CFBD's homeId/awayId
-- against teams already synced by the daily full sync.
SELECT * FROM teams WHERE external_id = sqlc.arg(external_id);
