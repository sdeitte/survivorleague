-- name: UpsertTeamSPRating :one
-- Match/upsert on (team_id, season_year) per game_predictions' same
-- upsert-per-sync-run contract. Callers must never pass CFBD's synthetic
-- "nationalAverages" pseudo-team row — see internal/schedule/sync.go's
-- SyncSPRatings doc comment.
INSERT INTO team_sp_ratings (team_id, season_year, rating, ranking)
VALUES (sqlc.arg(team_id), sqlc.arg(season_year), sqlc.arg(rating), sqlc.arg(ranking))
ON CONFLICT (team_id, season_year) DO UPDATE SET
    rating = EXCLUDED.rating,
    ranking = EXCLUDED.ranking,
    updated_at = now()
RETURNING *;

-- name: GetTeamSPRating :one
SELECT * FROM team_sp_ratings WHERE team_id = sqlc.arg(team_id) AND season_year = sqlc.arg(season_year);

-- name: ListTeamSPRatingsForSeason :many
-- Every synced team's SP+ rating for the season — fetched once per
-- available-teams call and merged in Go (internal/picks/service.go) into
-- a team_id -> rating map, rather than re-joining per row.
SELECT * FROM team_sp_ratings WHERE season_year = sqlc.arg(season_year);
