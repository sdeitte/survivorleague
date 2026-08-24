-- name: UpsertGamePrediction :one
-- Match/upsert on game_id (UNIQUE) — see 00005_predictor_recap.sql's doc
-- comment on why spread/home_win_probability are nullable and may be
-- absent until CFBD publishes them close to kickoff.
INSERT INTO game_predictions (game_id, spread, home_win_probability)
VALUES (sqlc.arg(game_id), sqlc.arg(spread), sqlc.arg(home_win_probability))
ON CONFLICT (game_id) DO UPDATE SET
    spread = EXCLUDED.spread,
    home_win_probability = EXCLUDED.home_win_probability,
    updated_at = now()
RETURNING *;

-- name: GetGamePredictionByGameID :one
SELECT * FROM game_predictions WHERE game_id = sqlc.arg(game_id);

-- name: ListGamePredictionsForWeek :many
-- Every game_predictions row for games in the given week — merged onto
-- ListAvailableTeamsForWeek's result in Go (internal/picks/service.go),
-- not joined in SQL, so this stays a plain add-on next to the existing
-- available-teams query rather than modifying it.
SELECT gp.* FROM game_predictions gp
JOIN games g ON g.id = gp.game_id
WHERE g.week_id = sqlc.arg(week_id);
