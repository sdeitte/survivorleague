-- name: GetPickByMembershipAndWeek :one
-- Backs GET .../picks/me. A no-rows result (pgx.ErrNoRows) means the
-- membership has not picked for this week yet — mapped by the service to
-- ErrPickNotFound.
SELECT * FROM picks
WHERE league_membership_id = sqlc.arg(league_membership_id) AND week_id = sqlc.arg(week_id);

-- name: GetPickByMembershipAndWeekForUpdate :one
-- Same as GetPickByMembershipAndWeek but row-locked, used inside the
-- upsert transaction so a concurrent upsert for the same
-- (league_membership_id, week_id) can't race past the lock check between
-- "read the current pick's game" and "write the new one".
SELECT * FROM picks
WHERE league_membership_id = sqlc.arg(league_membership_id) AND week_id = sqlc.arg(week_id)
FOR UPDATE;

-- name: UpsertPick :one
-- Create-or-update a membership's pick for a week. ON CONFLICT targets the
-- UNIQUE(league_membership_id, week_id) constraint — this is what makes
-- "changing your mind" an UPDATE of the same row (not a second row), which
-- is exactly what frees a since-abandoned team for a different week (see
-- the plan's "used" rule). The service layer must have already verified
-- the existing pick (if any) isn't locked before calling this.
--
-- This can still fail on the OTHER unique constraint,
-- UNIQUE(league_membership_id, team_id), if team_id is already committed
-- to a different week's row — the service layer catches that
-- (23505 on the team_id constraint) and maps it to ErrTeamAlreadyUsed
-- rather than surfacing a raw DB error.
INSERT INTO picks (league_membership_id, week_id, game_id, team_id)
VALUES (sqlc.arg(league_membership_id), sqlc.arg(week_id), sqlc.arg(game_id), sqlc.arg(team_id))
ON CONFLICT (league_membership_id, week_id) DO UPDATE SET
    game_id = EXCLUDED.game_id,
    team_id = EXCLUDED.team_id,
    updated_at = now()
RETURNING *;

-- name: ListUsedTeamIDsForMembershipExcludingWeek :many
-- Every team_id currently sitting in one of this membership's picks for a
-- week OTHER than the given one — regardless of whether that other pick's
-- game has locked yet, per the "used" rule (a team is only free again once
-- no row anywhere holds it). Backs available-teams' is_used_elsewhere.
SELECT team_id FROM picks
WHERE league_membership_id = sqlc.arg(league_membership_id) AND week_id != sqlc.arg(week_id);

-- name: ListAvailableTeamsForWeek :many
-- Every team in the league's conference that has a game in the given week,
-- with its opponent, game id, and kickoff time inline so the picks screen
-- doesn't need N+1 lookups. is_locked/is_used_elsewhere are computed by
-- the service layer (the former against time.Now(), the latter against
-- ListUsedTeamIDsForMembershipExcludingWeek's result), not this query.
SELECT
    t.id AS team_id,
    t.name AS team_name,
    t.logo_url AS team_logo_url,
    ot.id AS opponent_team_id,
    ot.name AS opponent_name,
    ot.logo_url AS opponent_logo_url,
    g.id AS game_id,
    g.kickoff_at AS kickoff_at,
    (g.home_team_id = t.id) AS is_home
FROM teams t
JOIN games g ON g.week_id = sqlc.arg(week_id) AND (g.home_team_id = t.id OR g.away_team_id = t.id)
JOIN teams ot ON ot.id = (CASE WHEN g.home_team_id = t.id THEN g.away_team_id ELSE g.home_team_id END)
WHERE t.conference = sqlc.arg(conference)
ORDER BY g.kickoff_at ASC, t.name ASC;

-- name: ListPicksByMembershipForSeason :many
-- Every week of the season for one membership (LEFT JOINed against that
-- membership's pick, if any — a week with no pick still appears, with
-- null pick/game/team/result/kickoff columns), with team/opponent names
-- and home/away inline so the leaderboard's expandable per-contestant
-- history doesn't need N+1 lookups. Backs GET
-- .../members/{membershipId}/picks; the service/handler layer applies the
-- same pre-lock privacy rule ListPicksByWeekForLeague's callers do (hiding
-- every pick-identifying field, not just team_id, for another member's
-- not-yet-started pick) — this query itself doesn't know who's asking.
SELECT
    w.id AS week_id,
    w.week_number AS week_number,
    p.id AS pick_id,
    p.game_id AS game_id,
    p.team_id AS team_id,
    p.result AS result,
    g.kickoff_at AS kickoff_at,
    t.name AS team_name,
    t.logo_url AS team_logo_url,
    ot.name AS opponent_name,
    ot.logo_url AS opponent_logo_url,
    -- Selected as the raw nullable column (not a computed
    -- home_team_id = team_id boolean expression) because sqlc's
    -- nullability inference doesn't reliably mark a CASE-derived boolean
    -- as nullable through this join chain, and generating a non-nullable
    -- Go bool for a column that's genuinely NULL on a no-pick week would
    -- panic on scan. is_home is computed in Go instead (see
    -- ListMembershipPicksForSeason) by comparing this against team_id,
    -- both of which sqlc correctly infers as nullable since they're real
    -- columns, not derived expressions.
    g.home_team_id AS home_team_id
FROM weeks w
LEFT JOIN picks p ON p.week_id = w.id AND p.league_membership_id = sqlc.arg(league_membership_id)
LEFT JOIN games g ON g.id = p.game_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN teams ot ON ot.id = (CASE WHEN g.home_team_id = p.team_id THEN g.away_team_id ELSE g.home_team_id END)
WHERE w.season_year = sqlc.arg(season_year)
ORDER BY w.week_number ASC;

-- name: ListPicksByWeekForLeague :many
-- Every non-removed member of the league with their pick status for the
-- given week (LEFT JOINed — members with no pick yet still appear, with
-- null pick/game/team/kickoff columns). Backs GET .../picks; the service
-- layer applies the pre-lock privacy rule (hiding game_id/team_id for
-- other members' not-yet-started picks), not this query.
SELECT
    m.id AS membership_id,
    u.display_name AS display_name,
    p.id AS pick_id,
    p.game_id AS game_id,
    p.team_id AS team_id,
    g.kickoff_at AS kickoff_at
FROM league_memberships m
JOIN users u ON u.id = m.user_id
LEFT JOIN picks p ON p.league_membership_id = m.id AND p.week_id = sqlc.arg(week_id)
LEFT JOIN games g ON g.id = p.game_id
WHERE m.league_id = sqlc.arg(league_id) AND m.removed_at IS NULL
ORDER BY u.display_name ASC;

-- name: ListPickCountsForWeek :many
-- Live per-team pick counts within one league's week — how many of this
-- league's members currently have a pick committed to each team, right
-- now, with no lock-status gating (shown as decision-support on the pick
-- screen itself, not a post-lock reveal — see the matchup-stats/live
-- pick-% feature). Scoped by league_id (not just week_id) since the same
-- conference's week is shared across every league in it, and a percentage
-- must only reflect the asking league's own members.
SELECT p.team_id, count(*) AS pick_count
FROM picks p
JOIN league_memberships lm ON lm.id = p.league_membership_id
WHERE lm.league_id = sqlc.arg(league_id) AND p.week_id = sqlc.arg(week_id)
GROUP BY p.team_id;
