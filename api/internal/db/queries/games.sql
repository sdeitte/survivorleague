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

-- name: SeedFinalizeGame :one
-- Local-dev-only: fabricates a completed result for an already-synced
-- game (used by cmd/seed-demo, never by the running server). Sets
-- kickoff_at into the past, status='final', a made-up score, and
-- winner_team_id derived from home_wins. Explicitly resets graded_at to
-- NULL so the real grading.Service.GradeGame path (its normal idempotency
-- guard) is what actually grades it, keeping the fabricated data exactly
-- as internally consistent as a real result — this must be an explicit
-- reset, not just "leave it NULL", because a game reused across multiple
-- seed-demo runs (e.g. after cmd/reset-schedule, which deliberately
-- preserves graded_at when it restores everything else from CFBD) would
-- otherwise still be stamped from the previous run, silently short-circuiting
-- GradeGame's guard and leaving every pick on it stuck at 'pending' forever.
UPDATE games SET
    kickoff_at = sqlc.arg(kickoff_at),
    status = 'final',
    home_score = sqlc.arg(home_score),
    away_score = sqlc.arg(away_score),
    winner_team_id = (CASE WHEN sqlc.arg(home_wins)::boolean THEN home_team_id ELSE away_team_id END),
    graded_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: GetGame :one
-- Plain (unjoined) single-game lookup — backs internal/schedule's
-- RefreshGame (Phase 8's admin single-game resync), which only needs
-- week_id/external_id/home_team_id/away_team_id to resolve the game
-- against CFBD, not the joined team names GetGameByIDWithTeams carries for
-- API responses.
SELECT * FROM games WHERE id = sqlc.arg(id);

-- name: GetGameByExternalID :one
-- Backs SyncPredictions: resolves CFBD's win-probability gameId against an
-- already-synced game, mirroring GetTeamByExternalID's role for teams.
SELECT * FROM games WHERE external_id = sqlc.arg(external_id);

-- name: CountUnfinishedConferenceGamesForSeason :one
-- Backs leagues.Service.IsSeasonComplete (the co-champions tiebreaker
-- banner): every conference-relevant game for a season (either team
-- belongs to conference — same relevance rule as
-- ListConferenceRelevantGamesForWeek) that hasn't reached exactly
-- 'final'. Deliberately as strict as TryFinalizeLeagueWeek's own
-- per-week check (postponed/canceled count as unfinished, not
-- terminal) — the season isn't "over" for this purpose if grading
-- itself is still stuck waiting on one of these games. total lets the
-- caller distinguish "0 unfinished because the season is done" from "0
-- unfinished because nothing is synced yet" — only the former means
-- the season is actually complete.
SELECT
    COUNT(*) FILTER (WHERE g.status != 'final') AS unfinished,
    COUNT(*) AS total
FROM games g
JOIN weeks w ON w.id = g.week_id
JOIN teams ht ON ht.id = g.home_team_id
JOIN teams at2 ON at2.id = g.away_team_id
WHERE w.season_year = sqlc.arg(season_year)
  AND (ht.conference = sqlc.arg(conference) OR at2.conference = sqlc.arg(conference));
