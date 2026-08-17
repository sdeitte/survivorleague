-- name: UpsertGame :one
-- Match/upsert on external_id (CFBD's game id) per the Phase 3 sync
-- contract. graded_at is deliberately excluded from the UPDATE SET list —
-- it's a Phase 5 grading-pipeline idempotency guard this sync must never
-- touch, so on conflict its existing value (NULL until Phase 5 grades the
-- game) is left exactly as-is.
INSERT INTO games (
    external_id, week_id, home_team_id, away_team_id, kickoff_at,
    status, home_score, away_score, winner_team_id
)
VALUES (
    sqlc.arg(external_id), sqlc.arg(week_id), sqlc.arg(home_team_id), sqlc.arg(away_team_id), sqlc.arg(kickoff_at),
    sqlc.arg(status), sqlc.arg(home_score), sqlc.arg(away_score), sqlc.arg(winner_team_id)
)
ON CONFLICT (external_id) DO UPDATE SET
    week_id = EXCLUDED.week_id,
    home_team_id = EXCLUDED.home_team_id,
    away_team_id = EXCLUDED.away_team_id,
    kickoff_at = EXCLUDED.kickoff_at,
    status = EXCLUDED.status,
    home_score = EXCLUDED.home_score,
    away_score = EXCLUDED.away_score,
    winner_team_id = EXCLUDED.winner_team_id,
    updated_at = now()
RETURNING *;

-- name: ListGamesByWeekWithTeams :many
-- Joined with both teams' name/conference/logo so clients don't need N+1
-- lookups per the GET /weeks/:id/games contract.
SELECT
    g.id, g.external_id, g.week_id, g.home_team_id, g.away_team_id, g.kickoff_at,
    g.status, g.home_score, g.away_score, g.winner_team_id, g.graded_at,
    g.created_at, g.updated_at,
    ht.name AS home_team_name, ht.conference AS home_team_conference, ht.logo_url AS home_team_logo_url,
    at.name AS away_team_name, at.conference AS away_team_conference, at.logo_url AS away_team_logo_url
FROM games g
JOIN teams ht ON ht.id = g.home_team_id
JOIN teams at ON at.id = g.away_team_id
WHERE g.week_id = sqlc.arg(week_id)
ORDER BY g.kickoff_at ASC;

-- name: GetGameByIDWithTeams :one
SELECT
    g.id, g.external_id, g.week_id, g.home_team_id, g.away_team_id, g.kickoff_at,
    g.status, g.home_score, g.away_score, g.winner_team_id, g.graded_at,
    g.created_at, g.updated_at,
    ht.name AS home_team_name, ht.conference AS home_team_conference, ht.logo_url AS home_team_logo_url,
    at.name AS away_team_name, at.conference AS away_team_conference, at.logo_url AS away_team_logo_url
FROM games g
JOIN teams ht ON ht.id = g.home_team_id
JOIN teams at ON at.id = g.away_team_id
WHERE g.id = sqlc.arg(id);
