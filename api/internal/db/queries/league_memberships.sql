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
    m.bought_back AS bought_back,
    m.created_at AS joined_at
FROM league_memberships m
JOIN users u ON u.id = m.user_id
WHERE m.league_id = sqlc.arg(league_id) AND m.removed_at IS NULL
ORDER BY m.created_at ASC;

-- name: GetMembershipByIDAndLeague :one
-- Scoped lookup used by buy-back (Phase 6) to validate that membershipId
-- belongs to leagueId before any status/bought_back checks run — mirrors
-- RemoveMembership's same "wrong league, already removed, or nonexistent
-- all collapse to no rows" contract, except this is a plain read (the
-- caller applies its own conditional UPDATE afterward) rather than a
-- mutation.
SELECT * FROM league_memberships
WHERE id = sqlc.arg(id) AND league_id = sqlc.arg(league_id) AND removed_at IS NULL;

-- name: BuyBackMembership :one
-- The buy-back mutation itself (Phase 6): reinstates an eliminated member
-- to status='active' and permanently flags bought_back=true. The WHERE
-- guard (status='eliminated' AND bought_back=false, on top of the id+
-- league_id scope) makes this safe to call concurrently — a race that
-- slips past the service layer's pre-check still can't double-apply a
-- buy-back, since the second racer's UPDATE matches zero rows once the
-- first commits. eliminated_week_id/eliminated_game_id are deliberately
-- left untouched: they remain the historical record of the elimination
-- that was bought back, not cleared.
UPDATE league_memberships
SET status = 'active',
    bought_back = true,
    bought_back_at = now(),
    bought_back_by = sqlc.arg(bought_back_by),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND league_id = sqlc.arg(league_id)
  AND status = 'eliminated'
  AND bought_back = false
RETURNING *;

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
