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

-- name: GetWeekByID :one
SELECT * FROM weeks WHERE id = sqlc.arg(id);

-- name: GetWeekBySeasonAndNumber :one
-- Backs RefreshWeek: the week must already exist (created by the daily
-- full SyncSeason) — a narrow week refresh never creates a week itself.
SELECT * FROM weeks WHERE season_year = sqlc.arg(season_year) AND week_number = sqlc.arg(week_number);

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
