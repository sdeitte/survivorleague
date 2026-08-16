-- name: CreateLeague :one
INSERT INTO leagues (name, season_year, conference, commissioner_user_id, invite_code)
VALUES (sqlc.arg(name), sqlc.arg(season_year), sqlc.arg(conference), sqlc.arg(commissioner_user_id), sqlc.arg(invite_code))
RETURNING *;

-- name: GetLeagueByID :one
SELECT * FROM leagues WHERE id = sqlc.arg(id);

-- name: GetLeagueByInviteCode :one
SELECT * FROM leagues WHERE invite_code = sqlc.arg(invite_code);

-- name: LeagueInviteCodeExists :one
SELECT EXISTS (SELECT 1 FROM leagues WHERE invite_code = sqlc.arg(invite_code)) AS exists;

-- name: ListLeaguesForUser :many
-- Leagues the given user has a non-removed membership in, along with their
-- role/is_contestant/status in each (GET /leagues needs both in one shot).
SELECT
    l.*,
    m.id AS membership_id,
    m.role AS member_role,
    m.is_contestant AS member_is_contestant,
    m.status AS member_status
FROM leagues l
JOIN league_memberships m ON m.league_id = l.id
WHERE m.user_id = sqlc.arg(user_id) AND m.removed_at IS NULL
ORDER BY l.created_at DESC;

-- name: UpdateLeagueName :one
UPDATE leagues
SET name = sqlc.arg(name), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateLeagueInviteCode :one
UPDATE leagues
SET invite_code = sqlc.arg(invite_code), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;
