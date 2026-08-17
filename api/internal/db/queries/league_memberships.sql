-- name: CreateLeagueMembership :one
INSERT INTO league_memberships (league_id, user_id, role, is_contestant, status)
VALUES (sqlc.arg(league_id), sqlc.arg(user_id), sqlc.arg(role), sqlc.arg(is_contestant), 'active')
RETURNING *;

-- name: GetLeagueMembershipByID :one
SELECT * FROM league_memberships WHERE id = sqlc.arg(id);

-- name: GetMembershipByLeagueAndUser :one
-- Excludes removed members by design: this backs requireLeagueMember, where
-- a removed_at row must behave exactly like "never joined" (403).
SELECT * FROM league_memberships
WHERE league_id = sqlc.arg(league_id) AND user_id = sqlc.arg(user_id) AND removed_at IS NULL;

-- name: ListActiveMembersWithUser :many
SELECT
    m.id AS membership_id,
    m.user_id AS user_id,
    u.display_name AS display_name,
    m.role AS role,
    m.is_contestant AS is_contestant,
    m.status AS status,
    m.created_at AS joined_at
FROM league_memberships m
JOIN users u ON u.id = m.user_id
WHERE m.league_id = sqlc.arg(league_id) AND m.removed_at IS NULL
ORDER BY m.created_at ASC;

-- name: RemoveMembership :one
-- Soft-delete: only affects a currently-active (non-removed) row scoped to
-- the given league, so this doubles as the "membership belongs to this
-- league and isn't already removed" check — no rows back means 404/400 to
-- the caller, not an error.
UPDATE league_memberships
SET removed_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND league_id = sqlc.arg(league_id) AND removed_at IS NULL
RETURNING *;

-- name: UpdateCommissionerIsContestant :one
UPDATE league_memberships
SET is_contestant = sqlc.arg(is_contestant), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpsertLeagueMembershipOnJoin :one
-- Handles both fresh joins and rejoin-after-removal against the
-- UNIQUE(league_id, user_id) constraint in one statement:
--   - no existing row                     -> plain INSERT.
--   - existing row with removed_at set    -> DO UPDATE resets it to a fresh
--                                             active player membership (a
--                                             "new row" in spirit, even
--                                             though it reuses the id).
--   - existing row with removed_at IS NULL (still an active/eliminated
--     member) -> the DO UPDATE...WHERE guard doesn't match, so neither the
--     INSERT nor the UPDATE applies and RETURNING yields zero rows. Callers
--     treat "no rows" as "already a member" (409) — this also protects a
--     commissioner's own membership from ever being reset by this query.
INSERT INTO league_memberships (league_id, user_id, role, is_contestant, status)
VALUES (sqlc.arg(league_id), sqlc.arg(user_id), 'player', true, 'active')
ON CONFLICT (league_id, user_id) DO UPDATE
SET role = 'player',
    is_contestant = true,
    status = 'active',
    removed_at = NULL,
    eliminated_week_id = NULL,
    eliminated_game_id = NULL,
    bought_back = false,
    bought_back_at = NULL,
    bought_back_by = NULL,
    updated_at = now()
WHERE league_memberships.removed_at IS NOT NULL
RETURNING *;
