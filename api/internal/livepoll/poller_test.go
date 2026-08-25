package livepoll

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/grading"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
	"github.com/sdeitte/survivor-league-api/internal/picks"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

// testDatabaseURL mirrors every other package's own copy of this helper —
// this integration test self-skips (not fails) when no database is
// reachable, so `go test ./...` still passes without the local
// docker-compose Postgres running.
func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
}

var idCounter = time.Now().UnixNano()

// nextIntID hands out ever-increasing small-ish ints for CFBD fixture
// ids (team/game external ids) — needs to be an int (not a UUID) since
// that's the shape CFBD's own JSON schema uses.
func nextIntID() int {
	idCounter++
	return int(idCounter % 1_000_000_000)
}

// uniqueSeasonYear returns a season_year that's extremely unlikely to
// collide with a previous `go test` run against the same persistent dev
// database — unlike a fixed per-process starting counter (the convention
// other packages' tests use, safe there since those tests don't assert an
// exact game count within a week), this test's assertions depend on a
// truly fresh week with no leftover games from an earlier run, so the
// value is derived from the wall clock rather than a fixed base.
func uniqueSeasonYear() int {
	return 80000 + nextIntID()%100000
}

func createTestUser(t *testing.T, q *gen.Queries, label string) gen.User {
	t.Helper()
	u, err := q.CreateUser(context.Background(), gen.CreateUserParams{
		Email:        fmt.Sprintf("%s-%d@example.test", label, nextIntID()),
		PasswordHash: "test-hash-not-a-real-argon2id-value",
		DisplayName:  label,
		IsSiteAdmin:  false,
	})
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	return u
}

// mockCFBDServer is a minimal, mutation-friendly stand-in for CFBD's real
// API — serves whatever JSON is currently held for /teams/fbs, /calendar,
// and /games, and lets a test flip the /games response between "still in
// progress" and "final with a score" mid-test to simulate a live game
// concluding, without ever making a real network call.
type mockCFBDServer struct {
	server *httptest.Server

	mu                                 sync.Mutex
	teamsJSON, calendarJSON, gamesJSON string
}

func newMockCFBDServer(t *testing.T) *mockCFBDServer {
	t.Helper()
	m := &mockCFBDServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/teams/fbs", func(w http.ResponseWriter, r *http.Request) { m.serve(w, &m.teamsJSON) })
	mux.HandleFunc("/calendar", func(w http.ResponseWriter, r *http.Request) { m.serve(w, &m.calendarJSON) })
	mux.HandleFunc("/games", func(w http.ResponseWriter, r *http.Request) { m.serve(w, &m.gamesJSON) })
	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockCFBDServer) serve(w http.ResponseWriter, body *string) {
	m.mu.Lock()
	b := *body
	m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b))
}

func (m *mockCFBDServer) setTeams(json string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teamsJSON = json
}

func (m *mockCFBDServer) setCalendar(json string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calendarJSON = json
}

func (m *mockCFBDServer) setGames(json string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gamesJSON = json
}

func buildTeamsJSON(homeID, awayID int) string {
	return fmt.Sprintf(`[
  {"id": %d, "school": "Poll Home", "mascot": "H", "abbreviation": "H", "alternateNames": [], "conference": "Big Ten", "division": null, "classification": "fbs", "color": null, "alternateColor": null, "logos": [], "twitter": null, "location": null},
  {"id": %d, "school": "Poll Away", "mascot": "A", "abbreviation": "A", "alternateNames": [], "conference": "Big Ten", "division": null, "classification": "fbs", "color": null, "alternateColor": null, "logos": [], "twitter": null, "location": null}
]`, homeID, awayID)
}

func buildCalendarJSON(seasonYear, weekNumber int) string {
	return fmt.Sprintf(`[{"season": %d, "week": %d, "seasonType": "regular", "startDate": "2025-08-23T00:00:00.000Z", "endDate": "2025-08-30T00:00:00.000Z"}]`, seasonYear, weekNumber)
}

// buildGamesJSON renders a single game's CFBD JSON. homePoints/awayPoints
// nil means "not reported yet" (rendered as JSON null); completed=false
// with nil points is the "still in progress / not started" shape this
// package deliberately grades nothing from (see the package doc comment's
// note on CFBD tiers).
func buildGamesJSON(gameID, seasonYear, weekNumber, homeID, awayID int, startDate string, completed bool, homePoints, awayPoints *int) string {
	homePointsJSON, awayPointsJSON := "null", "null"
	if homePoints != nil {
		homePointsJSON = fmt.Sprint(*homePoints)
	}
	if awayPoints != nil {
		awayPointsJSON = fmt.Sprint(*awayPoints)
	}
	return fmt.Sprintf(`[{
  "id": %d, "season": %d, "week": %d, "seasonType": "regular",
  "startDate": %q, "startTimeTBD": false, "completed": %t,
  "neutralSite": false, "conferenceGame": true, "attendance": null, "venueId": null, "venue": null,
  "homeId": %d, "homeTeam": "Poll Home", "homeConference": "Big Ten", "homePoints": %s,
  "awayId": %d, "awayTeam": "Poll Away", "awayConference": "Big Ten", "awayPoints": %s
}]`, gameID, seasonYear, weekNumber, startDate, completed, homeID, homePointsJSON, awayID, awayPointsJSON)
}

// TestPoller_RealTickerGradesGameOnceItGoesFinal is the live-poll-loop
// integration test the plan's Phase 5 verification section calls for: it
// drives the actual Poller.Start ticker (never calls GradeGame or tick
// directly), against a mock CFBD server whose /games response is mutated
// mid-test, and confirms the real background loop — not a direct service
// call — is what performs the grading and elimination.
func TestPoller_RealTickerGradesGameOnceItGoesFinal(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDatabaseURL())
	if err != nil {
		t.Skipf("skipping integration test: could not create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: database not reachable (run migrations + docker-compose up): %v", err)
	}
	t.Cleanup(pool.Close)
	q := gen.New(pool)

	leaguesSvc := leagues.NewService(q, pool)
	picksSvc := picks.NewService(q, pool)
	gradingSvc := grading.NewService(q, pool)

	homeID, awayID, gameExternalID := nextIntID(), nextIntID(), nextIntID()
	seasonYear := uniqueSeasonYear()
	const weekNumber = 1

	mock := newMockCFBDServer(t)
	mock.setTeams(buildTeamsJSON(homeID, awayID))
	mock.setCalendar(buildCalendarJSON(seasonYear, weekNumber))
	// Kickoff is initially far in the future so a pick can be legally
	// submitted against it below (picks reject a submission whose game has
	// already kicked off).
	futureKickoff := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	mock.setGames(buildGamesJSON(gameExternalID, seasonYear, weekNumber, homeID, awayID, futureKickoff, false, nil, nil))

	cfbdClient := schedule.NewCFBDClient(mock.server.Client(), mock.server.URL, "test-key")
	scheduleSvc := schedule.NewService(q, cfbdClient)

	if _, err := scheduleSvc.SyncSeason(ctx, seasonYear); err != nil {
		t.Fatalf("initial SyncSeason: %v", err)
	}

	week, err := q.GetWeekBySeasonAndNumber(ctx, gen.GetWeekBySeasonAndNumberParams{SeasonYear: int32(seasonYear), WeekNumber: weekNumber})
	if err != nil {
		t.Fatalf("GetWeekBySeasonAndNumber: %v", err)
	}
	homeTeam, err := q.GetTeamByExternalID(ctx, fmt.Sprint(homeID))
	if err != nil {
		t.Fatalf("GetTeamByExternalID(home): %v", err)
	}
	awayTeam, err := q.GetTeamByExternalID(ctx, fmt.Sprint(awayID))
	if err != nil {
		t.Fatalf("GetTeamByExternalID(away): %v", err)
	}
	// Look up by external_id rather than assuming this is the only game in
	// the week: season_year counters are per-process (like every other
	// package's own test helpers), so a persistent dev database can carry
	// leftover games in the same (season_year, week_number) from an
	// earlier `go test` run.
	games, err := scheduleSvc.ListGamesByWeek(ctx, week.ID)
	if err != nil {
		t.Fatalf("ListGamesByWeek: %v", err)
	}
	var game gen.ListGamesByWeekWithTeamsRow
	found := false
	for _, g := range games {
		if g.ExternalID == fmt.Sprint(gameExternalID) {
			game = g
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("game with external_id=%d not found in week's games: %+v", gameExternalID, games)
	}

	winnerUser := createTestUser(t, q, "poll-winner")
	loserUser := createTestUser(t, q, "poll-loser")
	league, winnerMember, err := leaguesSvc.CreateLeague(ctx, winnerUser.ID, "Poll Loop League", int32(seasonYear), "Big Ten", "Test Team")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}
	loserMember, err := leaguesSvc.JoinByCode(ctx, league.ID, loserUser.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}

	if _, err := picksSvc.UpsertPick(ctx, winnerMember.ID, week.ID, "Big Ten", game.ID, homeTeam.ID); err != nil {
		t.Fatalf("UpsertPick (winner, home team): %v", err)
	}
	if _, err := picksSvc.UpsertPick(ctx, loserMember.ID, week.ID, "Big Ten", game.ID, awayTeam.ID); err != nil {
		t.Fatalf("UpsertPick (loser, away team): %v", err)
	}

	// Simulate kickoff having just passed — there's no live game clock in
	// tests, so directly rewrite kickoff_at into the past (same technique
	// internal/picks' own tests use), then keep the mock CFBD server's
	// reported startDate in sync with it so the poll loop's own refresh
	// doesn't fight this write.
	pastKickoff := time.Now().Add(-10 * time.Minute)
	if _, err := pool.Exec(ctx, `UPDATE games SET kickoff_at = $1 WHERE id = $2`, pastKickoff, game.ID); err != nil {
		t.Fatalf("simulate kickoff passing: %v", err)
	}
	pastKickoffRFC3339 := pastKickoff.UTC().Format(time.RFC3339)
	mock.setGames(buildGamesJSON(gameExternalID, seasonYear, weekNumber, homeID, awayID, pastKickoffRFC3339, false, nil, nil))

	poller := NewPoller(scheduleSvc, gradingSvc, WithInterval(150*time.Millisecond), WithLiveWindow(1*time.Hour))
	poller.Start(ctx)
	t.Cleanup(poller.Stop)

	// Let a couple of ticks pass while CFBD still reports the game as not
	// completed — confirm the loop does NOT grade anything prematurely.
	time.Sleep(500 * time.Millisecond)
	stillPending, err := picksSvc.GetPick(ctx, winnerMember.ID, week.ID)
	if err != nil {
		t.Fatalf("GetPick (mid-game check): %v", err)
	}
	if stillPending.Result != "pending" {
		t.Fatalf("pick.Result while CFBD still reports the game in progress = %q, want %q (graded too early)", stillPending.Result, "pending")
	}

	// Now the mock CFBD server reports the game final — home team wins.
	homePts, awayPts := 24, 10
	mock.setGames(buildGamesJSON(gameExternalID, seasonYear, weekNumber, homeID, awayID, pastKickoffRFC3339, true, &homePts, &awayPts))

	// Wait for the poll loop's own next tick to notice and grade it.
	deadline := time.Now().Add(5 * time.Second)
	var winnerPick gen.Pick
	for {
		winnerPick, err = picksSvc.GetPick(ctx, winnerMember.ID, week.ID)
		if err != nil {
			t.Fatalf("GetPick (waiting for grading): %v", err)
		}
		if winnerPick.Result != "pending" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the live poll loop to grade the game")
		}
		time.Sleep(75 * time.Millisecond)
	}

	if winnerPick.Result != "win" {
		t.Errorf("winner pick.Result = %q, want %q", winnerPick.Result, "win")
	}
	loserPick, err := picksSvc.GetPick(ctx, loserMember.ID, week.ID)
	if err != nil {
		t.Fatalf("GetPick (loser): %v", err)
	}
	if loserPick.Result != "loss" {
		t.Errorf("loser pick.Result = %q, want %q", loserPick.Result, "loss")
	}

	// The poll loop must also have driven TryFinalizeLeagueWeek for this
	// league — give elimination a brief window to land (it happens in the
	// same tick right after grading, but poll asynchronously to avoid a
	// flaky race on the exact same tick boundary).
	deadline = time.Now().Add(5 * time.Second)
	var loserAfter gen.LeagueMembership
	for {
		loserAfter, err = leaguesSvc.GetMembershipByID(ctx, loserMember.ID)
		if err != nil {
			t.Fatalf("GetMembershipByID: %v", err)
		}
		if loserAfter.Status == "eliminated" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for the live poll loop to finalize the league week; loser status = %q", loserAfter.Status)
		}
		time.Sleep(75 * time.Millisecond)
	}
	if loserAfter.EliminatedWeekID != week.ID {
		t.Errorf("loser eliminated_week_id = %v, want %v", loserAfter.EliminatedWeekID, week.ID)
	}
	if loserAfter.EliminatedGameID != game.ID {
		t.Errorf("loser eliminated_game_id = %v, want %v", loserAfter.EliminatedGameID, game.ID)
	}

	winnerAfter, err := leaguesSvc.GetMembershipByID(ctx, winnerMember.ID)
	if err != nil {
		t.Fatalf("GetMembershipByID (winner): %v", err)
	}
	if winnerAfter.Status != "active" {
		t.Errorf("winner status = %q, want %q", winnerAfter.Status, "active")
	}

	result, err := q.GetLeagueWeekResultByLeagueAndWeek(ctx, gen.GetLeagueWeekResultByLeagueAndWeekParams{LeagueID: league.ID, WeekID: week.ID})
	if err != nil {
		t.Fatalf("GetLeagueWeekResultByLeagueAndWeek: %v", err)
	}
	if result.MassWipeout {
		t.Error("MassWipeout = true, want false (the winner won)")
	}
}
