-- Phase 7: the hourly pick_reminder scan. See internal/notify/reminder.go
-- for the full design.

-- name: ListActiveContestantMembershipsForReminderScan :many
-- Every membership the reminder scan needs to consider: an active,
-- playing (is_contestant=true), non-removed member of an active league.
-- Carries the league's conference/season_year alongside so the caller can
-- feed them straight into GetNearestUnpickedGameForMembership without a
-- second lookup per row.
SELECT
    lm.id AS membership_id,
    lm.user_id AS user_id,
    lm.league_id AS league_id,
    l.conference AS conference,
    l.season_year AS season_year
FROM league_memberships lm
JOIN leagues l ON l.id = lm.league_id
WHERE lm.status = 'active'
  AND lm.is_contestant = true
  AND lm.removed_at IS NULL
  AND l.status = 'active';

-- name: GetNearestUnpickedGameForMembership :one
-- The membership's very next deadline: the soonest-kicking-off,
-- conference-relevant game whose week the membership has not yet
-- submitted any pick for. This is the "current week"/"nearest not-yet-
-- locked game" the plan's pick_reminder trigger reminds against — a
-- membership that has already picked for every upcoming week's earliest
-- game gets pgx.ErrNoRows here (mapped by the caller to "nothing to
-- remind, skip").
SELECT g.id AS game_id, g.week_id AS week_id, g.kickoff_at AS kickoff_at
FROM games g
JOIN teams ht ON ht.id = g.home_team_id
JOIN teams at ON at.id = g.away_team_id
JOIN weeks w ON w.id = g.week_id
WHERE (ht.conference = sqlc.arg(conference) OR at.conference = sqlc.arg(conference))
  AND w.season_year = sqlc.arg(season_year)
  AND g.kickoff_at > now()
  AND NOT EXISTS (
      SELECT 1 FROM picks p
      WHERE p.league_membership_id = sqlc.arg(membership_id) AND p.week_id = w.id
  )
ORDER BY g.kickoff_at ASC
LIMIT 1;
