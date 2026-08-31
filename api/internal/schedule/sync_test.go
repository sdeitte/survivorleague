package schedule

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// testDatabaseURL mirrors internal/leagues/service_test.go's helper —
// integration tests in this file self-skip (not fail) when no database is
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

func newTestQueries(t *testing.T) *gen.Queries {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDatabaseURL())
	if err != nil {
		t.Skipf("skipping integration test: could not create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: database not reachable (run migrations + docker-compose up): %v", err)
	}
	t.Cleanup(pool.Close)

	return gen.New(pool)
}

// seasonYearCounter hands out a distinct season_year per test so tests in
// this file (and repeated runs against a persistent dev database) never
// collide on the (season_year, week_number)/external_id upsert keys —
// starting well outside any real college football season year. Seeded
// from the wall clock (not a fixed literal) so repeated `go test` runs
// against the same un-truncated dev database don't replay the same
// season_year sequence and collide with a prior run's leftover rows —
// internal/grading/service_test.go hit this exact flake once already
// with the same pattern; fixing it here too rather than waiting to hit
// it again.
var seasonYearCounter = 90000 + int(time.Now().UnixNano()%40000)

func uniqueSeasonYear() int {
	seasonYearCounter++
	return seasonYearCounter
}

// mutableFixtureServer serves whatever JSON is currently held in
// teamsJSON/calendarJSON/gamesJSON, letting a test swap the fixture body
// between two SyncSeason calls to simulate CFBD data changing (renamed
// team, a game going final with a score) or staying the same (idempotency).
type mutableFixtureServer struct {
	server                             *httptest.Server
	teamsJSON, calendarJSON, gamesJSON string
}

func newMutableFixtureServer(t *testing.T, teamsJSON, calendarJSON, gamesJSON string) *mutableFixtureServer {
	t.Helper()
	f := &mutableFixtureServer{teamsJSON: teamsJSON, calendarJSON: calendarJSON, gamesJSON: gamesJSON}
	f.server = newFixtureCFBDServer(t, "test-key", &f.teamsJSON, &f.calendarJSON, &f.gamesJSON)
	return f
}

func (f *mutableFixtureServer) service(t *testing.T, q *gen.Queries) *Service {
	t.Helper()
	client := NewCFBDClient(f.server.Client(), f.server.URL, "test-key")
	return NewService(q, client)
}

func findTeamByExternalID(teams []gen.Team, externalID string) (gen.Team, bool) {
	for _, tm := range teams {
		if tm.ExternalID == externalID {
			return tm, true
		}
	}
	return gen.Team{}, false
}

// TestService_SyncSeason_Success runs a full sync against the standard
// fixture data (see cfbd_client_test.go's fixtureTeamsJSON/
// fixtureCalendarJSON/fixtureGamesJSON) and checks every part of the
// contract in one pass: team upsert with normalized conference names, week
// upsert filtered to regular season only, game upsert with resolved
// team/week ids, and the skip/defer accounting for the fixture's edge
// cases (a TBD-kickoff game, a game against an unsynced non-FBS opponent).
func TestService_SyncSeason_Success(t *testing.T) {
	q := newTestQueries(t)
	year := uniqueSeasonYear()
	fixture := newMutableFixtureServer(t, fixtureTeamsJSON, fixtureCalendarJSON, fixtureGamesJSON)
	svc := fixture.service(t, q)

	result, err := svc.SyncSeason(context.Background(), year)
	if err != nil {
		t.Fatalf("SyncSeason: %v", err)
	}

	if result.TeamsUpserted != 4 {
		t.Errorf("TeamsUpserted = %d, want 4", result.TeamsUpserted)
	}
	// The calendar declares two "regular" weeks (the postseason entry must
	// be filtered out — see fixtureCalendarJSON's comment), but every
	// fixture game is week 1 — week 2 has none, so it's pruned as an empty
	// week (see DeleteWeekIfNoGames's doc comment). Net: 1 week kept, 1
	// pruned.
	if result.WeeksUpserted != 1 {
		t.Errorf("WeeksUpserted = %d, want 1", result.WeeksUpserted)
	}
	if result.WeeksPruned != 1 {
		t.Errorf("WeeksPruned = %d, want 1", result.WeeksPruned)
	}
	// Games 101 and 102 resolve; game 104 (home team id=999, never synced
	// as a team) must be skipped, not crash the sync.
	if result.GamesUpserted != 2 {
		t.Errorf("GamesUpserted = %d, want 2", result.GamesUpserted)
	}
	if result.GamesSkipped != 1 {
		t.Errorf("GamesSkipped = %d, want 1", result.GamesSkipped)
	}
	if len(result.SkippedGames) != 1 || result.SkippedGames[0].ExternalID != "104" {
		t.Errorf("SkippedGames = %+v, want a single entry for external_id 104", result.SkippedGames)
	}
	// Game 102 is startTimeTBD=true.
	if result.DeferredKickoffGames != 1 {
		t.Errorf("DeferredKickoffGames = %d, want 1", result.DeferredKickoffGames)
	}
	// Every conference in the fixture ("Big Ten", "SEC", "American
	// Athletic") has a normalization table entry, so nothing should be
	// reported as unmapped.
	if len(result.UnmappedConferences) != 0 {
		t.Errorf("UnmappedConferences = %v, want none", result.UnmappedConferences)
	}

	// --- Verify the normalization actually landed in the DB, not just in
	// the in-memory result. ---
	teams, err := q.ListTeams(context.Background(), pgtype.Text{})
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	osu, ok := findTeamByExternalID(teams, "1")
	if !ok {
		t.Fatal("Ohio State (external_id=1) not found after sync")
	}
	if osu.Conference != "Big Ten" {
		t.Errorf("Ohio State conference = %q, want %q", osu.Conference, "Big Ten")
	}
	if osu.Name != "Ohio State" {
		t.Errorf("Ohio State name = %q, want %q", osu.Name, "Ohio State")
	}
	army, ok := findTeamByExternalID(teams, "4")
	if !ok {
		t.Fatal("Army (external_id=4) not found after sync")
	}
	if army.Conference != "American Athletic Conference" {
		t.Errorf("Army conference = %q, want the normalized %q (CFBD raw value is %q)", army.Conference, "American Athletic Conference", "American Athletic")
	}

	weeks, err := q.ListWeeksBySeasonYear(context.Background(), int32(year))
	if err != nil {
		t.Fatalf("ListWeeksBySeasonYear: %v", err)
	}
	if len(weeks) != 1 {
		t.Fatalf("ListWeeksBySeasonYear returned %d rows, want 1 (week 2 has no games and is pruned)", len(weeks))
	}

	game101, err := findGameByExternalID(context.Background(), q, weeks, "101")
	if err != nil {
		t.Fatal(err)
	}
	if game101.Status != "scheduled" {
		t.Errorf("game 101 status = %q, want %q", game101.Status, "scheduled")
	}
	if !game101.KickoffAt.Valid {
		t.Error("game 101 kickoff_at is not set")
	}
}

// findGameByExternalID scans every synced week's games for one matching
// externalID — a small helper so tests can assert on a specific game's
// upserted row without a dedicated by-external-id query existing in the
// production query set (external_id lookups aren't part of the read API).
func findGameByExternalID(ctx context.Context, q *gen.Queries, weeks []gen.Week, externalID string) (gen.ListGamesByWeekWithTeamsRow, error) {
	for _, week := range weeks {
		games, err := q.ListGamesByWeekWithTeams(ctx, week.ID)
		if err != nil {
			return gen.ListGamesByWeekWithTeamsRow{}, err
		}
		for _, g := range games {
			if g.ExternalID == externalID {
				return g, nil
			}
		}
	}
	return gen.ListGamesByWeekWithTeamsRow{}, errGameNotFoundInTest(externalID)
}

type errGameNotFoundInTest string

func (e errGameNotFoundInTest) Error() string { return "game not found in synced weeks: " + string(e) }

// TestService_SyncSeason_IdempotentOnRepeatedRuns runs SyncSeason twice
// against byte-identical fixture data and asserts the DB row counts for
// this sync's teams/weeks/games don't double — the core idempotency
// contract ("match/upsert on external_id / (season_year, week_number)").
func TestService_SyncSeason_IdempotentOnRepeatedRuns(t *testing.T) {
	q := newTestQueries(t)
	year := uniqueSeasonYear()
	fixture := newMutableFixtureServer(t, fixtureTeamsJSON, fixtureCalendarJSON, fixtureGamesJSON)
	svc := fixture.service(t, q)

	first, err := svc.SyncSeason(context.Background(), year)
	if err != nil {
		t.Fatalf("first SyncSeason: %v", err)
	}
	second, err := svc.SyncSeason(context.Background(), year)
	if err != nil {
		t.Fatalf("second SyncSeason: %v", err)
	}

	// Both runs report the same "upserted" counts (every row is touched
	// both times), but what actually matters is the DB doesn't accumulate
	// duplicate rows — checked below via direct row counts.
	if first.TeamsUpserted != second.TeamsUpserted {
		t.Errorf("TeamsUpserted changed between runs: %d then %d", first.TeamsUpserted, second.TeamsUpserted)
	}
	if first.GamesUpserted != second.GamesUpserted {
		t.Errorf("GamesUpserted changed between runs: %d then %d", first.GamesUpserted, second.GamesUpserted)
	}

	teams, err := q.ListTeams(context.Background(), pgtype.Text{})
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	teamCount := 0
	for _, tm := range teams {
		switch tm.ExternalID {
		case "1", "2", "3", "4":
			teamCount++
		}
	}
	if teamCount != 4 {
		t.Errorf("teams with external_id 1-4 after two syncs: got %d rows total, want exactly 4 (no duplicates)", teamCount)
	}

	weeks, err := q.ListWeeksBySeasonYear(context.Background(), int32(year))
	if err != nil {
		t.Fatalf("ListWeeksBySeasonYear: %v", err)
	}
	if len(weeks) != 1 {
		t.Errorf("weeks for season_year=%d after two syncs: got %d rows, want exactly 1 (week 2 has no games and is pruned; no duplicates)", year, len(weeks))
	}

	totalGames := 0
	for _, week := range weeks {
		games, err := q.ListGamesByWeekWithTeams(context.Background(), week.ID)
		if err != nil {
			t.Fatalf("ListGamesByWeekWithTeams: %v", err)
		}
		totalGames += len(games)
	}
	if totalGames != 2 {
		t.Errorf("games for season_year=%d after two syncs: got %d rows, want exactly 2 (no duplicates)", year, totalGames)
	}
}

// fixtureGamesJSONUpdated re-serves game 101 as now completed with a final
// score (Ohio State framed as the winner) and renames Ohio State slightly —
// used by TestService_SyncSeason_UpdatesExistingRowsOnChange to prove a
// second sync with *changed* upstream data updates the existing row rather
// than inserting a duplicate.
const fixtureTeamsJSONUpdated = `[
  {"id": 1, "school": "Ohio State Buckeyes", "conference": "Big Ten", "logos": ["https://example.com/logos/ohio-state-v2.png"]},
  {"id": 2, "school": "Michigan", "conference": "Big Ten", "logos": []},
  {"id": 3, "school": "Alabama", "conference": "SEC", "logos": []},
  {"id": 4, "school": "Army", "conference": "American Athletic", "logos": []}
]`

const fixtureGamesJSONUpdated = `[
  {
    "id": 101, "season": 2025, "week": 1, "seasonType": "regular",
    "startDate": "2025-08-30T17:00:00.000Z", "startTimeTBD": false, "completed": true,
    "homeId": 1, "homeTeam": "Ohio State Buckeyes", "homePoints": 45,
    "awayId": 2, "awayTeam": "Michigan", "awayPoints": 17
  },
  {
    "id": 102, "season": 2025, "week": 1, "seasonType": "regular",
    "startDate": "2025-08-28T00:00:00.000Z", "startTimeTBD": true, "completed": false,
    "homeId": 3, "homeTeam": "Alabama", "homePoints": null,
    "awayId": 4, "awayTeam": "Army", "awayPoints": null
  },
  {
    "id": 104, "season": 2025, "week": 1, "seasonType": "regular",
    "startDate": "2025-08-30T20:00:00.000Z", "startTimeTBD": false, "completed": false,
    "homeId": 999, "homeTeam": "Chattanooga", "homePoints": null,
    "awayId": 1, "awayTeam": "Ohio State Buckeyes", "awayPoints": null
  }
]`

func TestService_SyncSeason_UpdatesExistingRowsOnChange(t *testing.T) {
	q := newTestQueries(t)
	year := uniqueSeasonYear()
	fixture := newMutableFixtureServer(t, fixtureTeamsJSON, fixtureCalendarJSON, fixtureGamesJSON)
	svc := fixture.service(t, q)

	if _, err := svc.SyncSeason(context.Background(), year); err != nil {
		t.Fatalf("first SyncSeason: %v", err)
	}

	teamsBefore, err := q.ListTeams(context.Background(), pgtype.Text{})
	if err != nil {
		t.Fatalf("ListTeams (before): %v", err)
	}
	osuBefore, ok := findTeamByExternalID(teamsBefore, "1")
	if !ok {
		t.Fatal("Ohio State not found after first sync")
	}

	// Swap in the "updated" fixture — same external ids, changed values —
	// and sync again.
	fixture.teamsJSON = fixtureTeamsJSONUpdated
	fixture.gamesJSON = fixtureGamesJSONUpdated

	result, err := svc.SyncSeason(context.Background(), year)
	if err != nil {
		t.Fatalf("second SyncSeason: %v", err)
	}
	if result.TeamsUpserted != 4 || result.GamesUpserted != 2 {
		t.Fatalf("second SyncSeason result = %+v, want 4 teams / 2 games upserted", result)
	}

	teamsAfter, err := q.ListTeams(context.Background(), pgtype.Text{})
	if err != nil {
		t.Fatalf("ListTeams (after): %v", err)
	}
	osuAfter, ok := findTeamByExternalID(teamsAfter, "1")
	if !ok {
		t.Fatal("Ohio State not found after second sync")
	}
	if osuAfter.ID != osuBefore.ID {
		t.Error("Ohio State's row id changed between syncs — expected the same row to be updated, not replaced")
	}
	if osuAfter.Name != "Ohio State Buckeyes" {
		t.Errorf("Ohio State name after update = %q, want %q", osuAfter.Name, "Ohio State Buckeyes")
	}

	weeks, err := q.ListWeeksBySeasonYear(context.Background(), int32(year))
	if err != nil {
		t.Fatalf("ListWeeksBySeasonYear: %v", err)
	}
	game101, err := findGameByExternalID(context.Background(), q, weeks, "101")
	if err != nil {
		t.Fatal(err)
	}
	if game101.Status != "final" {
		t.Errorf("game 101 status after update = %q, want %q", game101.Status, "final")
	}
	if !game101.HomeScore.Valid || game101.HomeScore.Int32 != 45 {
		t.Errorf("game 101 home_score = %+v, want 45", game101.HomeScore)
	}
	if !game101.AwayScore.Valid || game101.AwayScore.Int32 != 17 {
		t.Errorf("game 101 away_score = %+v, want 17", game101.AwayScore)
	}
	if !game101.WinnerTeamID.Valid {
		t.Fatal("game 101 winner_team_id not set after going final")
	}
	if game101.WinnerTeamID != osuAfter.ID {
		t.Error("game 101 winner_team_id does not point at Ohio State (the higher-scoring team)")
	}
}

// TestService_SyncSeason_GameWithUnparseableStartDate is a defensive test:
// CFBD's documented Game.startDate is a required field, so a genuinely
// empty/unparseable date is not expected from the real API, but the sync
// must not crash the whole run over one malformed record if CFBD's API
// ever drifts from its documented schema. Distinct from the
// startTimeTBD=true case (game 102 in the standard fixture, which DOES
// still carry a real startDate and is upserted, just flagged as deferred —
// see TestService_SyncSeason_Success).
func TestService_SyncSeason_GameWithUnparseableStartDate(t *testing.T) {
	q := newTestQueries(t)
	year := uniqueSeasonYear()

	badGamesJSON := `[
    {
      "id": 201, "season": 2025, "week": 1, "seasonType": "regular",
      "startDate": "", "startTimeTBD": true, "completed": false,
      "homeId": 1, "homeTeam": "Ohio State", "homePoints": null,
      "awayId": 2, "awayTeam": "Michigan", "awayPoints": null
    }
  ]`

	fixture := newMutableFixtureServer(t, fixtureTeamsJSON, fixtureCalendarJSON, badGamesJSON)
	svc := fixture.service(t, q)

	result, err := svc.SyncSeason(context.Background(), year)
	if err != nil {
		t.Fatalf("SyncSeason must not fail the whole run over one bad game: %v", err)
	}
	if result.GamesUpserted != 0 {
		t.Errorf("GamesUpserted = %d, want 0 (the only game had an unparseable start date)", result.GamesUpserted)
	}
	if result.GamesSkipped != 1 {
		t.Errorf("GamesSkipped = %d, want 1", result.GamesSkipped)
	}
	if len(result.SkippedGames) != 1 || result.SkippedGames[0].ExternalID != "201" {
		t.Errorf("SkippedGames = %+v, want a single entry for external_id 201", result.SkippedGames)
	}
}

// TestService_SyncSeason_ExcludesKnownBadGames guards the
// excludedGameExternalIDs denylist: a game on that list is skipped (not
// upserted) even though it's otherwise perfectly valid CFBD data, while an
// unrelated game in the same response still syncs normally.
func TestService_SyncSeason_ExcludesKnownBadGames(t *testing.T) {
	q := newTestQueries(t)
	year := uniqueSeasonYear()

	gamesJSON := `[
    {
      "id": 401864494, "season": 2025, "week": 1, "seasonType": "regular",
      "startDate": "2026-08-29T19:00:00.000Z", "startTimeTBD": false, "completed": false,
      "homeId": 1, "homeTeam": "Ohio State", "homePoints": null,
      "awayId": 2, "awayTeam": "Michigan", "awayPoints": null
    },
    {
      "id": 999999, "season": 2025, "week": 1, "seasonType": "regular",
      "startDate": "2026-09-05T00:00:00.000Z", "startTimeTBD": false, "completed": false,
      "homeId": 1, "homeTeam": "Ohio State", "homePoints": null,
      "awayId": 2, "awayTeam": "Michigan", "awayPoints": null
    }
  ]`

	fixture := newMutableFixtureServer(t, fixtureTeamsJSON, fixtureCalendarJSON, gamesJSON)
	svc := fixture.service(t, q)

	result, err := svc.SyncSeason(context.Background(), year)
	if err != nil {
		t.Fatalf("SyncSeason: %v", err)
	}
	if result.GamesUpserted != 1 {
		t.Errorf("GamesUpserted = %d, want 1 (only the non-excluded game)", result.GamesUpserted)
	}
	if result.GamesSkipped != 1 {
		t.Errorf("GamesSkipped = %d, want 1 (the excluded game)", result.GamesSkipped)
	}
	if len(result.SkippedGames) != 1 || result.SkippedGames[0].ExternalID != "401864494" {
		t.Errorf("SkippedGames = %+v, want a single entry for external_id 401864494", result.SkippedGames)
	}
}

// TestService_SyncSeason_NonFBSOpponentStoredAsStub is the regression test
// for the real production bug this covers: an FBS team's game against an
// FCS opponent (e.g. a week-1 cupcake) was previously dropped entirely —
// see resolveNonFBSOpponent's doc comment — leaving that FBS team with no
// pickable game that week. Two games: one real FBS-vs-FCS game (must now
// sync, with a minimal is_fbs=false stub row for the FCS side) and one
// between two teams neither of which is FBS (must still be skipped, same
// as before — CFBD's unfiltered GET /games includes plenty of games with
// nothing to do with this app).
func TestService_SyncSeason_NonFBSOpponentStoredAsStub(t *testing.T) {
	q := newTestQueries(t)
	year := uniqueSeasonYear()

	gamesJSON := `[
    {
      "id": 301, "season": 2025, "week": 1, "seasonType": "regular",
      "startDate": "2025-08-30T17:00:00.000Z", "startTimeTBD": false, "completed": false,
      "homeId": 1, "homeTeam": "Ohio State", "homeConference": "Big Ten", "homeClassification": "fbs", "homePoints": null,
      "awayId": 501, "awayTeam": "Indiana State", "awayConference": "MVFC", "awayClassification": "fcs", "awayPoints": null
    },
    {
      "id": 302, "season": 2025, "week": 1, "seasonType": "regular",
      "startDate": "2025-08-30T17:00:00.000Z", "startTimeTBD": false, "completed": false,
      "homeId": 502, "homeTeam": "Random FCS Team A", "homeConference": "CAA", "homeClassification": "fcs", "homePoints": null,
      "awayId": 503, "awayTeam": "Random FCS Team B", "awayConference": "CAA", "awayClassification": "fcs", "awayPoints": null
    }
  ]`

	fixture := newMutableFixtureServer(t, fixtureTeamsJSON, fixtureCalendarJSON, gamesJSON)
	svc := fixture.service(t, q)

	result, err := svc.SyncSeason(context.Background(), year)
	if err != nil {
		t.Fatalf("SyncSeason: %v", err)
	}
	if result.GamesUpserted != 1 {
		t.Errorf("GamesUpserted = %d, want 1 (Ohio State vs Indiana State)", result.GamesUpserted)
	}
	if result.GamesSkipped != 1 {
		t.Errorf("GamesSkipped = %d, want 1 (the FCS-vs-FCS game involving no FBS team we track)", result.GamesSkipped)
	}
	if len(result.SkippedGames) != 1 || result.SkippedGames[0].ExternalID != "302" {
		t.Errorf("SkippedGames = %+v, want a single entry for external_id 302", result.SkippedGames)
	}

	stub, err := q.GetTeamByExternalID(context.Background(), "501")
	if err != nil {
		t.Fatalf("GetTeamByExternalID(501) (the FCS opponent stub): %v", err)
	}
	if stub.IsFbs {
		t.Error("stub opponent team is_fbs = true, want false")
	}
	if stub.Name != "Indiana State" {
		t.Errorf("stub opponent team name = %q, want %q", stub.Name, "Indiana State")
	}
	if stub.Conference != "MVFC" {
		t.Errorf("stub opponent team conference = %q, want %q", stub.Conference, "MVFC")
	}

	// The stub must never leak into ListTeams (the picker/eligibility
	// surface) — only real FBS teams belong there.
	allTeams, err := q.ListTeams(context.Background(), pgtype.Text{})
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if _, found := findTeamByExternalID(allTeams, "501"); found {
		t.Error("ListTeams includes the non-FBS stub team, want it excluded")
	}
}

// TestNormalizeConference_UnmappedNameIsSurfacedNotDropped exercises the
// normalization table's fallback path directly (no DB/HTTP needed): a raw
// CFBD conference string with no table entry is still stored (never
// silently dropped), but reported as unmapped so an operator can fix the
// table before it causes a pick-eligibility mismatch.
func TestNormalizeConference_UnmappedNameIsSurfacedNotDropped(t *testing.T) {
	normalized, ok := NormalizeConference("Totally New Conference")
	if ok {
		t.Fatal("NormalizeConference on an unmapped name: ok = true, want false")
	}
	if normalized != "Totally New Conference" {
		t.Errorf("NormalizeConference on an unmapped name = %q, want the raw value passed through unchanged", normalized)
	}
}

func TestNormalizeConference_MapsAllDocumentedCFBDNames(t *testing.T) {
	cases := map[string]string{
		"ACC":               "ACC",
		"American Athletic": "American Athletic Conference",
		"Big 12":            "Big 12",
		"Big Ten":           "Big Ten",
		"Conference USA":    "Conference USA",
		"FBS Independents":  "FBS Independents",
		"Mid-American":      "Mid-American Conference",
		"Mountain West":     "Mountain West Conference",
		"Pac-12":            "Pac-12",
		"SEC":               "SEC",
		"Sun Belt":          "Sun Belt Conference",
	}
	for raw, wantCanonical := range cases {
		got, ok := NormalizeConference(raw)
		if !ok {
			t.Errorf("NormalizeConference(%q): ok = false, want true", raw)
		}
		if got != wantCanonical {
			t.Errorf("NormalizeConference(%q) = %q, want %q", raw, got, wantCanonical)
		}
		if !IsValidConference(got) {
			t.Errorf("NormalizeConference(%q) produced %q, which is not in the canonical FBSConferences list", raw, got)
		}
	}
}
