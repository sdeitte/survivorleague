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
