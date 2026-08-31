-- name: UpsertTeam :one
-- Match/upsert on external_id (CFBD's team id) per the Phase 3 sync
-- contract. conference is always the *normalized* name (mapped from
-- CFBD's raw string by internal/schedule's normalization table before this
-- is ever called) — never CFBD's raw string. Always sets is_fbs=true: this
-- is the authoritative FBS sync path (driven by GET /teams/fbs), so it also
-- promotes a row that resolveNonFBSOpponent previously created as a stub
-- (see UpsertNonFBSOpponentTeam below) if CFBD ever reclassifies that team.
INSERT INTO teams (external_id, name, conference, logo_url, is_fbs)
VALUES (sqlc.arg(external_id), sqlc.arg(name), sqlc.arg(conference), sqlc.arg(logo_url), true)
ON CONFLICT (external_id) DO UPDATE SET
    name = EXCLUDED.name,
    conference = EXCLUDED.conference,
    logo_url = EXCLUDED.logo_url,
    is_fbs = true,
    updated_at = now()
RETURNING *;

-- name: UpsertNonFBSOpponentTeam :one
-- Minimal team row for a non-FBS opponent encountered while resolving an
-- FBS team's game (see internal/schedule/sync.go's resolveNonFBSOpponent)
-- — inserted only so the game itself can be stored (games.home_team_id/
-- away_team_id are NOT NULL FKs into teams). On conflict, the existing row
-- is returned untouched: this must never rename or downgrade a team that's
-- also tracked as real via UpsertTeam (is_fbs=true stays true).
INSERT INTO teams (external_id, name, conference, is_fbs, logo_url)
VALUES (sqlc.arg(external_id), sqlc.arg(name), sqlc.arg(conference), false, NULL)
ON CONFLICT (external_id) DO UPDATE SET external_id = teams.external_id
RETURNING *;

-- name: ListTeams :many
-- conference is an optional exact-match filter: pass a NULL narg to list
-- every team, or a canonical conference name to filter to it. Always
-- scoped to is_fbs=true — a stub non-FBS opponent row (see
-- UpsertNonFBSOpponentTeam) is an implementation detail of game storage,
-- never a real, poolable team.
SELECT * FROM teams
WHERE is_fbs = true
  AND (sqlc.narg(conference)::text IS NULL OR conference = sqlc.narg(conference))
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
WHERE conference != 'FBS Independents' AND is_fbs = true
GROUP BY conference
HAVING count(*) >= sqlc.arg(min_teams)::int
ORDER BY conference ASC;

-- name: GetTeamByExternalID :one
-- Backs RefreshWeek's team-id resolution: unlike SyncSeason (which
-- upserts every team fresh from a /teams/fbs response), a narrow week
-- refresh only pulls /games and needs to resolve CFBD's homeId/awayId
-- against teams already synced by the daily full sync.
SELECT * FROM teams WHERE external_id = sqlc.arg(external_id);

-- name: GetTeamByName :one
-- Backs SyncSPRatings: CFBD's /ratings/sp response identifies teams by
-- name (not external_id, unlike every other CFBD endpoint this package
-- consumes), so this is the one lookup keyed on name rather than
-- external_id. A no-rows result is expected for CFBD's synthetic
-- "nationalAverages" pseudo-team row — see cfbdTeamSP's doc comment.
SELECT * FROM teams WHERE name = sqlc.arg(name);
