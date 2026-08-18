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

-- name: CloseLeague :one
-- Closes a league (Commissioner-only). Deliberately an UPDATE, not a
-- DELETE — the league row, its memberships, picks, and full history all
-- stay in place; only status flips. See
-- internal/leagues.Service.CloseLeague's doc comment for what "closed"
-- means to the rest of the API. WHERE status != 'closed' guards against a
-- double-close (concurrent or repeated) returning no rows, which the
-- service maps to ErrLeagueAlreadyClosed.
UPDATE leagues
SET status = 'closed', updated_at = now()
WHERE id = sqlc.arg(id) AND status != 'closed'
RETURNING *;

-- name: ListLeaguesAdmin :many
-- Backs GET /admin/leagues (Phase 8, requireSiteAdmin) — every league in
-- the system, not scoped to the requester (unlike GET /leagues). Joins the
-- commissioner's user row for display_name/email, and computes
-- member_count inline (non-removed league_memberships) the same way
-- ListUsersAdmin computes league_count.
SELECT
    l.id, l.name, l.conference, l.season_year, l.status, l.created_at,
    l.commissioner_user_id,
    u.display_name AS commissioner_display_name,
    u.email AS commissioner_email,
    (SELECT count(*) FROM league_memberships m WHERE m.league_id = l.id AND m.removed_at IS NULL)::bigint AS member_count
FROM leagues l
JOIN users u ON u.id = l.commissioner_user_id
ORDER BY l.created_at DESC, l.id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountLeaguesAdmin :one
SELECT count(*) FROM leagues;
