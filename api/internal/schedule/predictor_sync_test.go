package schedule

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// predictorTestIDCounter mirrors every other test file in this package's
// own copy of this pattern (see current_week_test.go's identical comment)
// — each file needs its own unique external_ids, and Go doesn't let two
// _test.go files in the same package declare the same package-level name.
var predictorTestIDCounter = time.Now().UnixNano()

func nextPredictorTestID() int64 {
	predictorTestIDCounter++
	return predictorTestIDCounter
}

func createPredictorTestTeam(t *testing.T, q *gen.Queries, name, conference string) gen.Team {
	t.Helper()
	team, err := q.UpsertTeam(context.Background(), gen.UpsertTeamParams{
		ExternalID: fmt.Sprintf("pred-team-%d", nextPredictorTestID()),
		Name:       name,
		Conference: conference,
	})
	if err != nil {
		t.Fatalf("createPredictorTestTeam: %v", err)
	}
	return team
}

func createPredictorTestGame(t *testing.T, q *gen.Queries, externalID string, week gen.Week, home, away gen.Team) gen.Game {
	t.Helper()
	game, err := q.UpsertGame(context.Background(), gen.UpsertGameParams{
		ExternalID: externalID,
		WeekID:     week.ID,
		HomeTeamID: home.ID,
		AwayTeamID: away.ID,
		KickoffAt:  pgtype.Timestamptz{Time: time.Date(2026, 9, 5, 19, 0, 0, 0, time.UTC), Valid: true},
		Status:     "scheduled",
	})
	if err != nil {
		t.Fatalf("createPredictorTestGame: %v", err)
	}
	return game
}

// --- CFBDClient tests ---

func newPredictorFixtureServer(t *testing.T, wpJSON, spJSON string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics/wp/pregame", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r, "test-key")
		writeJSONFixture(w, wpJSON)
	})
	mux.HandleFunc("/ratings/sp", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r, "test-key")
		writeJSONFixture(w, spJSON)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestCFBDClient_GetPregameWinProbabilities(t *testing.T) {
	wpJSON := `[
		{"gameId": 501, "homeTeam": "Ohio State", "awayTeam": "Michigan", "spread": -3.5, "homeWinProbability": 0.62}
	]`
	server := newPredictorFixtureServer(t, wpJSON, `[]`)
	client := NewCFBDClient(server.Client(), server.URL, "test-key")

	rows, err := client.GetPregameWinProbabilities(context.Background(), 2026)
	if err != nil {
		t.Fatalf("GetPregameWinProbabilities: %v", err)
	}
	if len(rows) != 1 || rows[0].GameID != 501 || rows[0].HomeWinProbability != 0.62 {
		t.Fatalf("rows = %+v, want one row for game 501 with homeWinProbability=0.62", rows)
	}
}

func TestCFBDClient_GetSPRatings(t *testing.T) {
	spJSON := `[
		{"team": "Ohio State", "rating": 32.7, "ranking": 1},
		{"team": "nationalAverages", "rating": -0.5, "ranking": null}
	]`
	server := newPredictorFixtureServer(t, `[]`, spJSON)
	client := NewCFBDClient(server.Client(), server.URL, "test-key")

	rows, err := client.GetSPRatings(context.Background(), 2026)
	if err != nil {
		t.Fatalf("GetSPRatings: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (SyncSPRatings, not this client method, filters nationalAverages)", len(rows))
	}
}

// --- Service.SyncPredictions / Service.SyncSPRatings tests ---

func TestService_SyncPredictions(t *testing.T) {
	q := newTestQueries(t)
	year := uniqueSeasonYear()

	home := createPredictorTestTeam(t, q, "Predictor Home", "Big Ten")
	away := createPredictorTestTeam(t, q, "Predictor Away", "Big Ten")
	week := createCWTestWeek(t, q, int32(year), 1)
	// external_id must be a numeric string here (not e.g. "wp-game-N" like
	// most other test helpers use) since CFBD's real gameId field — and
	// this fixture's — is a bare JSON number, and games.external_id is
	// what SyncPredictions matches it against via fmt.Sprint.
	knownExternalID := fmt.Sprint(nextPredictorTestID())
	game := createPredictorTestGame(t, q, knownExternalID, week, home, away)

	wpJSON := fmt.Sprintf(`[
		{"gameId": %s, "homeTeam": "Predictor Home", "awayTeam": "Predictor Away", "spread": -7.5, "homeWinProbability": 0.71},
		{"gameId": 999999999, "homeTeam": "Unrelated Home", "awayTeam": "Unrelated Away", "spread": 1.0, "homeWinProbability": 0.5}
	]`, knownExternalID)
	server := newPredictorFixtureServer(t, wpJSON, `[]`)
	client := NewCFBDClient(server.Client(), server.URL, "test-key")
	svc := NewService(q, client)

	result, err := svc.SyncPredictions(context.Background(), year)
	if err != nil {
		t.Fatalf("SyncPredictions: %v", err)
	}
	if result.Upserted != 1 {
		t.Errorf("Upserted = %d, want 1 (the matching game)", result.Upserted)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (the unresolvable gameId)", result.Skipped)
	}

	stored, err := q.GetGamePredictionByGameID(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("GetGamePredictionByGameID: %v", err)
	}
	got, err := stored.HomeWinProbability.Float64Value()
	if err != nil || got.Float64 != 0.71 {
		t.Errorf("stored home_win_probability = %v (err=%v), want 0.71", got, err)
	}
}

func TestService_SyncSPRatings(t *testing.T) {
	q := newTestQueries(t)
	year := uniqueSeasonYear()

	team := createPredictorTestTeam(t, q, fmt.Sprintf("SP Team %d", nextPredictorTestID()), "SEC")

	spJSON := fmt.Sprintf(`[
		{"team": %q, "rating": 25.4, "ranking": 7},
		{"team": "nationalAverages", "rating": -0.5, "ranking": null}
	]`, team.Name)
	server := newPredictorFixtureServer(t, `[]`, spJSON)
	client := NewCFBDClient(server.Client(), server.URL, "test-key")
	svc := NewService(q, client)

	result, err := svc.SyncSPRatings(context.Background(), year)
	if err != nil {
		t.Fatalf("SyncSPRatings: %v", err)
	}
	if result.Upserted != 1 {
		t.Errorf("Upserted = %d, want 1 (the real team)", result.Upserted)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (the nationalAverages pseudo-team)", result.Skipped)
	}

	stored, err := q.GetTeamSPRating(context.Background(), gen.GetTeamSPRatingParams{TeamID: team.ID, SeasonYear: int32(year)})
	if err != nil {
		t.Fatalf("GetTeamSPRating: %v", err)
	}
	if stored.Ranking.Int32 != 7 || !stored.Ranking.Valid {
		t.Errorf("stored ranking = %+v, want 7", stored.Ranking)
	}
}
