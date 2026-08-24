-- name: UpsertWeekRecap :one
-- Match/upsert on (league_id, week_id) — safe to regenerate/retry a recap
-- for the same league/week (e.g. a redeploy re-running a failed
-- generation), same pattern as every other sync-style upsert in this
-- codebase.
INSERT INTO week_recaps (league_id, week_id, body)
VALUES (sqlc.arg(league_id), sqlc.arg(week_id), sqlc.arg(body))
ON CONFLICT (league_id, week_id) DO UPDATE SET
    body = EXCLUDED.body,
    generated_at = now()
RETURNING *;

-- name: GetLatestWeekRecapForLeague :one
-- Backs GET .../leagues/:id/recap: the most recently generated recap for
-- the league, regardless of which week it was for — the Leaderboard page
-- always wants "the latest thing that happened", not a specific week.
SELECT * FROM week_recaps
WHERE league_id = sqlc.arg(league_id)
ORDER BY generated_at DESC
LIMIT 1;

-- name: ListWeekRecapFactsForLeague :many
-- Everything internal/recap needs to build one week's prompt facts for a
-- league, in one query: every non-removed member (picked or not — a NULL
-- pick_result/team_name means they missed their pick, itself a fact worth
-- the recap knowing), their pick's team/opponent/final score, and whether
-- THIS week is the one that eliminated them (compare eliminated_week_id
-- to the week being recapped — a membership eliminated in an EARLIER week
-- is simply not this week's news).
--
-- home_team_id is selected as the raw nullable column, NOT a
-- (g.home_team_id = p.team_id) derived boolean — sqlc's nullability
-- inference doesn't reliably mark a CASE/comparison-derived boolean as
-- nullable through this LEFT JOIN chain, and a member with no pick this
-- week makes that comparison genuinely NULL (p.team_id is NULL), which
-- would panic scanning into a non-nullable Go bool. internal/recap
-- computes "picked home" itself by comparing this against team_id, both
-- of which sqlc correctly infers as nullable since they're real columns —
-- identical to ListPicksByMembershipForSeason's (internal/db/queries/
-- picks.sql) same workaround for the same reason.
SELECT
    m.id AS membership_id,
    u.display_name AS display_name,
    m.eliminated_week_id AS eliminated_week_id,
    m.bought_back AS bought_back,
    p.result AS pick_result,
    p.team_id AS team_id,
    t.name AS team_name,
    ot.name AS opponent_name,
    g.home_score AS home_score,
    g.away_score AS away_score,
    g.home_team_id AS home_team_id
FROM league_memberships m
JOIN users u ON u.id = m.user_id
LEFT JOIN picks p ON p.league_membership_id = m.id AND p.week_id = sqlc.arg(week_id)
LEFT JOIN games g ON g.id = p.game_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN teams ot ON ot.id = (CASE WHEN g.home_team_id = p.team_id THEN g.away_team_id ELSE g.home_team_id END)
WHERE m.league_id = sqlc.arg(league_id) AND m.removed_at IS NULL
ORDER BY u.display_name ASC;
