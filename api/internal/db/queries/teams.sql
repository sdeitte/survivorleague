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
