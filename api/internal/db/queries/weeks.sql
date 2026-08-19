-- name: UpsertWeek :one
-- Match/upsert on (season_year, week_number) per the Phase 3 sync
-- contract. Weeks carry no other CFBD-sourced fields (the calendar
-- endpoint's start/end dates aren't part of this schema — see the plan's
-- Data Model), so the update arm only bumps updated_at.
INSERT INTO weeks (season_year, week_number)
VALUES (sqlc.arg(season_year), sqlc.arg(week_number))
ON CONFLICT (season_year, week_number) DO UPDATE SET
    updated_at = now()
RETURNING *;

-- name: ListWeeksBySeasonYear :many
SELECT * FROM weeks WHERE season_year = sqlc.arg(season_year) ORDER BY week_number ASC;

-- name: ListWeeksBySeasonYearAndConference :many
-- Same as ListWeeksBySeasonYear but restricted to weeks that have at least
-- one game involving conference — weeks are global/shared across every
-- conference (see ListWeekKickoffRangesForConference), so a week with only
-- e.g. an Army-Navy game (American Athletic) would otherwise show up as a
-- selectable-but-empty week for every other conference's leagues too.
SELECT DISTINCT w.* FROM weeks w
JOIN games g ON g.week_id = w.id
JOIN teams t ON (t.id = g.home_team_id OR t.id = g.away_team_id) AND t.conference = sqlc.arg(conference)
WHERE w.season_year = sqlc.arg(season_year)
ORDER BY w.week_number ASC;

-- name: GetWeekByID :one
SELECT * FROM weeks WHERE id = sqlc.arg(id);

-- name: GetWeekBySeasonAndNumber :one
-- Backs RefreshWeek: the week must already exist (created by the daily
-- full SyncSeason) — a narrow week refresh never creates a week itself.
SELECT * FROM weeks WHERE season_year = sqlc.arg(season_year) AND week_number = sqlc.arg(week_number);

-- name: DeleteWeekIfNoGames :execrows
-- Best-effort cleanup for a week that CFBD's calendar lists as
-- seasonType=regular (so SyncSeason inserts it) but that turned out to
-- have zero actual games attached — e.g. a scheduling-gap week CFBD
-- reports between the end of regular-season play and conference
-- championship week. Deleting is safe: every FK into weeks (games,
-- picks, league_memberships.eliminated_week_id, league_week_results) is
-- ON DELETE RESTRICT, so this is a genuine no-op (0 rows affected, no
-- error) for any week that has real games or history attached, not just
-- for ones with zero games right now.
DELETE FROM weeks w
WHERE w.id = sqlc.arg(id)
  AND NOT EXISTS (SELECT 1 FROM games g WHERE g.week_id = w.id);

-- name: ListWeekKickoffRangesForConference :many
-- Every week of the season that has at least one game involving a team
-- in conference, with that week's earliest and latest kickoff among
-- those games. Backs the "current week" calculation (the week whose
-- kickoff window brackets now, or — in the gap between one week ending
-- and the next starting — the nearest upcoming week): the service layer
-- picks the right row from this list, this query just supplies the
-- per-week ranges in week order.
SELECT
    w.id AS week_id,
    w.week_number,
    min(g.kickoff_at)::timestamptz AS min_kickoff,
    max(g.kickoff_at)::timestamptz AS max_kickoff
FROM weeks w
JOIN games g ON g.week_id = w.id
JOIN teams t ON (t.id = g.home_team_id OR t.id = g.away_team_id) AND t.conference = sqlc.arg(conference)
WHERE w.season_year = sqlc.arg(season_year)
GROUP BY w.id, w.week_number
ORDER BY w.week_number ASC;

-- name: ListLiveWindowWeeks :many
-- Distinct (season_year, week_number) among games currently inside the
-- live poll window: kicked off but not yet final, and not so long ago
-- that they've fallen out the far end of the window. This is the live
-- poll loop's cheap "is there anything to even check" gate — a
-- zero-row result means no CFBD call is made this tick at all.
SELECT DISTINCT w.season_year, w.week_number
FROM games g
JOIN weeks w ON w.id = g.week_id
WHERE g.kickoff_at <= sqlc.arg(now)::timestamptz
  AND g.kickoff_at >= sqlc.arg(window_start)::timestamptz
  AND g.status <> 'final';
