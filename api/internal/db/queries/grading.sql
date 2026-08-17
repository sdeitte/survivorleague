-- Phase 5: grading/elimination pipeline queries. See internal/grading's
-- package doc comment for the full GradeGame/TryFinalizeLeagueWeek
-- contract these back.

-- name: GetGameForGradingForUpdate :one
-- Row-locked read of a game for GradeGame. Locking here (rather than a
-- plain SELECT) is what makes the graded_at IS NULL check-then-set
-- atomic across concurrent callers: a second GradeGame(gameID) call for
-- the same game blocks on this lock until the first transaction commits
-- (graded_at now set), then sees GradedAt already valid and no-ops —
-- exactly the "a re-fired poll can never double-grade" guarantee.
SELECT * FROM games WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: GradePicksForGame :exec
-- Grades every still-pending pick on this game in one statement: win if
-- the pick's team is the game's winner, loss otherwise. Only ever called
-- after the caller has confirmed winner_team_id IS NOT NULL (a tie/
-- undetermined winner is left ungraded entirely, never reaches this
-- query) — otherwise every pick would wrongly grade as a loss, since
-- `team_id = NULL` is never true in SQL.
UPDATE picks
SET result = CASE WHEN team_id = sqlc.arg(winner_team_id) THEN 'win' ELSE 'loss' END,
    updated_at = now()
WHERE game_id = sqlc.arg(game_id) AND result = 'pending';

-- name: ListLeagueIDsWithPicksForGame :many
-- Every distinct league with at least one pick on this game — the
-- "(league_id, week_id) pairs touched by picks on this game" GradeGame's
-- contract calls for (week_id is simply the game's own week_id, constant
-- across all of them, so only league_id varies here).
SELECT DISTINCT lm.league_id
FROM picks p
JOIN league_memberships lm ON lm.id = p.league_membership_id
WHERE p.game_id = sqlc.arg(game_id);

-- name: MarkGameGraded :exec
-- Sets the graded_at idempotency guard. WHERE graded_at IS NULL is
-- defensive redundancy on top of the FOR UPDATE row lock above — belt and
-- suspenders against ever re-grading a game.
UPDATE games SET graded_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND graded_at IS NULL;

-- name: ListConferenceRelevantGamesForWeek :many
-- Every game in the week where either team belongs to the given
-- conference — the set TryFinalizeLeagueWeek must fully resolve (no
-- postponed/canceled, all final) before a league's week can finalize.
SELECT g.* FROM games g
JOIN teams ht ON ht.id = g.home_team_id
JOIN teams at ON at.id = g.away_team_id
WHERE g.week_id = sqlc.arg(week_id)
  AND (ht.conference = sqlc.arg(conference) OR at.conference = sqlc.arg(conference));

-- name: ListActiveContestantMembershipsForLeague :many
-- Every membership that actually participates in grading/elimination:
-- status='active' (not already eliminated), is_contestant=true (a
-- manage-only commissioner never plays), removed_at IS NULL.
SELECT * FROM league_memberships
WHERE league_id = sqlc.arg(league_id)
  AND status = 'active'
  AND is_contestant = true
  AND removed_at IS NULL;

-- name: InsertLeagueWeekResultIfAbsent :one
-- The idempotency guard for league-week finalization: ON CONFLICT DO
-- NOTHING against UNIQUE(league_id, week_id) means a second concurrent
-- (or re-fired) TryFinalizeLeagueWeek call for the same league/week gets
-- zero rows back (mapped by the service to pgx.ErrNoRows, same pattern as
-- UpsertLeagueMembershipOnJoin) — "another concurrent call already
-- finalized this, skip". Elimination side effects are only ever applied
-- by the caller that actually wins this insert.
INSERT INTO league_week_results (league_id, week_id, mass_wipeout, processed_at)
VALUES (sqlc.arg(league_id), sqlc.arg(week_id), sqlc.arg(mass_wipeout), now())
ON CONFLICT (league_id, week_id) DO NOTHING
RETURNING *;

-- name: GetLeagueWeekResultByLeagueAndWeek :one
SELECT * FROM league_week_results
WHERE league_id = sqlc.arg(league_id) AND week_id = sqlc.arg(week_id);

-- name: EliminateMembership :one
-- game_id may be a NULL narg (the missed-pick case — nothing to point
-- eliminated_game_id at).
UPDATE league_memberships
SET status = 'eliminated',
    eliminated_week_id = sqlc.arg(week_id),
    eliminated_game_id = sqlc.narg(game_id),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListLeagueIDsWithPicksForWeek :many
-- Every distinct league with at least one pick for this week — used by the
-- live poll loop to know which leagues to attempt TryFinalizeLeagueWeek
-- for after refreshing a week's games. NOTE: a league whose contestants
-- ALL missed their pick that week (zero picks rows at all) won't appear
-- here — a known, documented gap (no game ever "finishes" to trigger this
-- league's finalization in that scenario); resolving it is out of this
-- phase's scope (would need a periodic sweep, not just a
-- game-finalization trigger).
SELECT DISTINCT lm.league_id
FROM picks p
JOIN league_memberships lm ON lm.id = p.league_membership_id
WHERE p.week_id = sqlc.arg(week_id);
