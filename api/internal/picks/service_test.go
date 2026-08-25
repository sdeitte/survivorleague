package picks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
)

// testDatabaseURL mirrors internal/leagues/service_test.go's and
// internal/schedule/sync_test.go's helper — these integration tests
// self-skip (not fail) when no database is reachable, so `go test ./...`
// still passes without the local docker-compose Postgres running.
func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
}

type testEnv struct {
	picks   *Service
	leagues *leagues.Service
	q       *gen.Queries
	pool    *pgxpool.Pool
}

func newTestEnv(t *testing.T) testEnv {
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

	q := gen.New(pool)
	return testEnv{
		picks:   NewService(q, pool),
		leagues: leagues.NewService(q, pool),
		q:       q,
		pool:    pool,
	}
}

// idCounter hands out ever-increasing suffixes so fixtures created by
// different tests never collide on external_id/season_year/email
// uniqueness within a single run, seeded from wall-clock nanoseconds (like
// internal/leagues/service_test.go's createTestUser) so repeated `go test`
// invocations against the same persistent dev database don't collide with
// each other either — unlike UpsertTeam/UpsertWeek/UpsertGame (idempotent
// upserts on conflict), users.email is a plain INSERT with no ON CONFLICT,
// so a fixed-starting-point counter that resets every process run would
// collide on a second consecutive run.
var idCounter = time.Now().UnixNano()

func nextID() int64 {
	idCounter++
	return idCounter
}

func createTestUser(t *testing.T, q *gen.Queries, label string) gen.User {
	t.Helper()
	u, err := q.CreateUser(context.Background(), gen.CreateUserParams{
		Email:        fmt.Sprintf("%s-%d@example.test", label, nextID()),
		PasswordHash: "test-hash-not-a-real-argon2id-value",
		DisplayName:  label,
		IsSiteAdmin:  false,
	})
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	return u
}

func createTestTeam(t *testing.T, q *gen.Queries, name, conference string) gen.Team {
	t.Helper()
	team, err := q.UpsertTeam(context.Background(), gen.UpsertTeamParams{
		ExternalID: fmt.Sprintf("team-%d", nextID()),
		Name:       name,
		Conference: conference,
	})
	if err != nil {
		t.Fatalf("createTestTeam: %v", err)
	}
	return team
}

func createTestWeek(t *testing.T, q *gen.Queries, seasonYear, weekNumber int32) gen.Week {
	t.Helper()
	week, err := q.UpsertWeek(context.Background(), gen.UpsertWeekParams{
		SeasonYear: seasonYear,
		WeekNumber: weekNumber,
	})
	if err != nil {
		t.Fatalf("createTestWeek: %v", err)
	}
	return week
}

func createTestGame(t *testing.T, q *gen.Queries, week gen.Week, home, away gen.Team, kickoffAt time.Time) gen.Game {
	t.Helper()
	game, err := q.UpsertGame(context.Background(), gen.UpsertGameParams{
		ExternalID: fmt.Sprintf("game-%d", nextID()),
		WeekID:     week.ID,
		HomeTeamID: home.ID,
		AwayTeamID: away.ID,
		KickoffAt:  pgtype.Timestamptz{Time: kickoffAt, Valid: true},
		Status:     "scheduled",
	})
	if err != nil {
		t.Fatalf("createTestGame: %v", err)
	}
	return game
}

// setKickoffInPast directly rewrites a game's kickoff_at to a time in the
// past — this is exactly how the plan's E2E verification says to simulate
// "kickoff passing" (there's no live game clock to fast-forward), and is
// used here the same way to unit-test the lock transition.
func setKickoffInPast(t *testing.T, pool *pgxpool.Pool, gameID pgtype.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE games SET kickoff_at = $1 WHERE id = $2`, time.Now().Add(-1*time.Hour), gameID)
	if err != nil {
		t.Fatalf("setKickoffInPast: %v", err)
	}
}

// seasonYearCounter mirrors internal/schedule/sync_test.go's own counter —
// deliberately NOT derived from nextID()'s nanosecond-scale values (which
// would overflow int32); weeks are upserted on (season_year, week_number),
// so a counter that repeats across separate `go test` runs just reuses (not
// collides on) the same week rows, which is harmless here.
var seasonYearCounter int32 = 91000

func uniqueSeasonYear() int32 {
	seasonYearCounter++
	return seasonYearCounter
}

// fixture bundles a Big Ten league with a commissioner (auto-added as an
// active contestant) and a two-week schedule with teams from both the
// league's own conference (Big Ten) and an unrelated one (SEC), so tests
// can exercise both the happy path and the conference-eligibility
// rejection without building their own schedule each time.
type fixture struct {
	env      testEnv
	league   gen.League
	member   gen.LeagueMembership // the commissioner's own membership
	week1    gen.Week
	week2    gen.Week
	teamA    gen.Team // Big Ten, has games in both week1 and week2
	teamB    gen.Team // Big Ten, week1 only
	oppX     gen.Team // Big Ten, teamA's week1 opponent
	oppY     gen.Team // Big Ten, teamA's week2 opponent
	oppZ     gen.Team // Big Ten, teamB's week1 opponent
	secTeam  gen.Team // SEC — wrong conference for this league
	gameA1   gen.Game // teamA vs oppX, week1
	gameA2   gen.Game // teamA vs oppY, week2
	gameB1   gen.Game // teamB vs oppZ, week1
	gameSec1 gen.Game // secTeam vs oppX, week1 (a non-conference game so oppX also has an SEC opponent available — unused by most tests but keeps the schedule realistic)
}

func newFixture(t *testing.T, kickoffOffset time.Duration) fixture {
	t.Helper()
	env := newTestEnv(t)
	commissioner := createTestUser(t, env.q, "commish")
	league, member, err := env.leagues.CreateLeague(context.Background(), commissioner.ID, "Test Survivor League", 2026, "Big Ten", "Test Team")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}

	seasonYear := uniqueSeasonYear()
	week1 := createTestWeek(t, env.q, seasonYear, 1)
	week2 := createTestWeek(t, env.q, seasonYear, 2)

	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	oppX := createTestTeam(t, env.q, "Opponent X", "Big Ten")
	oppY := createTestTeam(t, env.q, "Opponent Y", "Big Ten")
	oppZ := createTestTeam(t, env.q, "Opponent Z", "Big Ten")
	secTeam := createTestTeam(t, env.q, "SEC Team", "SEC")

	kickoff := time.Now().Add(kickoffOffset)
	gameA1 := createTestGame(t, env.q, week1, teamA, oppX, kickoff)
	gameA2 := createTestGame(t, env.q, week2, teamA, oppY, kickoff.Add(7*24*time.Hour))
	gameB1 := createTestGame(t, env.q, week1, teamB, oppZ, kickoff)
	gameSec1 := createTestGame(t, env.q, week1, secTeam, oppX, kickoff) // note: reuses oppX id twice in one week only for realism of "has multiple games"; not used to double-book oppX in tests

	return fixture{
		env: env, league: league, member: member,
		week1: week1, week2: week2,
		teamA: teamA, teamB: teamB, oppX: oppX, oppY: oppY, oppZ: oppZ, secTeam: secTeam,
		gameA1: gameA1, gameA2: gameA2, gameB1: gameB1, gameSec1: gameSec1,
	}
}

// TestService_UpsertPick_HappyPath confirms a straightforward first-time
// pick succeeds, is not locked, and round-trips through GetPick.
func TestService_UpsertPick_HappyPath(t *testing.T) {
	f := newFixture(t, 48*time.Hour)

	pick, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameA1.ID, f.teamA.ID)
	if err != nil {
		t.Fatalf("UpsertPick: %v", err)
	}
	if pick.TeamID != f.teamA.ID || pick.GameID != f.gameA1.ID {
		t.Errorf("pick = %+v, want team=%v game=%v", pick, f.teamA.ID, f.gameA1.ID)
	}

	got, err := f.env.picks.GetPick(context.Background(), f.member.ID, f.week1.ID)
	if err != nil {
		t.Fatalf("GetPick: %v", err)
	}
	if got.ID != pick.ID {
		t.Errorf("GetPick returned a different row than UpsertPick")
	}
}

// TestService_UpsertPick_GameNotInWeek covers both a game that exists but
// belongs to a different week, and a game id that doesn't exist at all —
// both fold into the same ErrGameNotInWeek per the service's doc comment.
func TestService_UpsertPick_GameNotInWeek(t *testing.T) {
	f := newFixture(t, 48*time.Hour)

	// gameA2 belongs to week2, not week1.
	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameA2.ID, f.teamA.ID); !errors.Is(err, ErrGameNotInWeek) {
		t.Errorf("wrong-week game: got err %v, want %v", err, ErrGameNotInWeek)
	}

	randomID := pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true}
	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, randomID, f.teamA.ID); !errors.Is(err, ErrGameNotInWeek) {
		t.Errorf("nonexistent game: got err %v, want %v", err, ErrGameNotInWeek)
	}
}

// TestService_UpsertPick_TeamNotInGame covers a team_id that's neither of
// the game's two teams (here, a team from a wholly different game).
func TestService_UpsertPick_TeamNotInGame(t *testing.T) {
	f := newFixture(t, 48*time.Hour)

	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameA1.ID, f.teamB.ID); !errors.Is(err, ErrTeamNotInGame) {
		t.Errorf("got err %v, want %v", err, ErrTeamNotInGame)
	}
}

// TestService_UpsertPick_WrongConferenceRejected picks the away team of a
// non-conference game — this league is locked to Big Ten, and the SEC team
// is one of the two teams in a real game, but doesn't belong to the
// league's conference. The opponent (oppX, Big Ten) picking is fine; the
// SEC team is not.
func TestService_UpsertPick_WrongConferenceRejected(t *testing.T) {
	f := newFixture(t, 48*time.Hour)

	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameSec1.ID, f.secTeam.ID); !errors.Is(err, ErrTeamWrongConference) {
		t.Errorf("got err %v, want %v", err, ErrTeamWrongConference)
	}

	// The other team in that same game (oppX, Big Ten) is fine — proves
	// only the *picked* team needs to match the conference, not both teams
	// in the game (this is how non-conference opponents work).
	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameSec1.ID, f.oppX.ID); err != nil {
		t.Errorf("picking the in-conference team of a non-conference game: got err %v, want nil", err)
	}
}

// TestService_UpsertPick_CannotPickAlreadyStartedGame confirms a brand-new
// pick (no prior pick for the week at all) into a game that has already
// kicked off is rejected — see UpsertPick's doc comment for why this is
// enforced even though it isn't one of the API contract's four explicitly
// numbered steps.
func TestService_UpsertPick_CannotPickAlreadyStartedGame(t *testing.T) {
	f := newFixture(t, -1*time.Hour) // kickoff already in the past

	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameA1.ID, f.teamA.ID); !errors.Is(err, ErrPickLocked) {
		t.Errorf("got err %v, want %v", err, ErrPickLocked)
	}
}

// TestService_UpsertPick_ChangingMindBeforeLockFreesTeamForOtherWeek is the
// core scenario from the plan: pick teamA for week1, then change your mind
// to teamB for week1 before teamA's game locks — teamA must then be free
// to pick for week2 (a different week's row), because the week1 row no
// longer holds it. If this were an INSERT-a-second-row bug instead of an
// UPDATE-in-place upsert, the later week2 pick of teamA would wrongly
// collide with UNIQUE(league_membership_id, team_id) against the abandoned
// week1 row.
func TestService_UpsertPick_ChangingMindBeforeLockFreesTeamForOtherWeek(t *testing.T) {
	f := newFixture(t, 48*time.Hour) // far enough out that nothing locks mid-test

	first, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameA1.ID, f.teamA.ID)
	if err != nil {
		t.Fatalf("initial pick of teamA for week1: %v", err)
	}

	second, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameB1.ID, f.teamB.ID)
	if err != nil {
		t.Fatalf("changing week1 pick to teamB: %v", err)
	}
	if second.ID != first.ID {
		t.Error("changing a pick before lock inserted a new row instead of updating the existing week1 row")
	}
	if second.TeamID != f.teamB.ID {
		t.Errorf("week1 pick team = %v, want teamB (%v)", second.TeamID, f.teamB.ID)
	}

	// teamA must now be free to pick for week2 — this is the assertion
	// that actually proves the "used" rule works: it must NOT collide with
	// UNIQUE(league_membership_id, team_id) against the abandoned week1 row.
	week2Pick, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week2.ID, f.league.Conference, f.gameA2.ID, f.teamA.ID)
	if err != nil {
		t.Fatalf("picking teamA (abandoned in week1) for week2: got err %v, want nil", err)
	}
	if week2Pick.TeamID != f.teamA.ID {
		t.Errorf("week2 pick team = %v, want teamA (%v)", week2Pick.TeamID, f.teamA.ID)
	}

	// And week1's pick is still teamB, untouched by the week2 write.
	week1Pick, err := f.env.picks.GetPick(context.Background(), f.member.ID, f.week1.ID)
	if err != nil {
		t.Fatalf("GetPick week1: %v", err)
	}
	if week1Pick.TeamID != f.teamB.ID {
		t.Errorf("week1 pick team after the week2 write = %v, want teamB (%v) — unchanged", week1Pick.TeamID, f.teamB.ID)
	}
}

// TestService_UpsertPick_LockedPickCannotChange confirms that once the
// game behind a membership's CURRENT pick for a week has kicked off, that
// pick is frozen — attempting to swap to a different, still-open game that
// week is rejected with ErrPickLocked. Kickoff is simulated by directly
// rewriting kickoff_at into the past, per the plan's E2E verification
// instructions (there's no live game clock in tests).
func TestService_UpsertPick_LockedPickCannotChange(t *testing.T) {
	f := newFixture(t, 48*time.Hour)

	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameA1.ID, f.teamA.ID); err != nil {
		t.Fatalf("initial pick: %v", err)
	}

	setKickoffInPast(t, f.env.pool, f.gameA1.ID)

	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameB1.ID, f.teamB.ID); !errors.Is(err, ErrPickLocked) {
		t.Errorf("swapping a locked pick: got err %v, want %v", err, ErrPickLocked)
	}

	// And the pick on file is still teamA/gameA1 — the rejected attempt
	// must not have partially applied.
	still, err := f.env.picks.GetPick(context.Background(), f.member.ID, f.week1.ID)
	if err != nil {
		t.Fatalf("GetPick: %v", err)
	}
	if still.TeamID != f.teamA.ID || still.GameID != f.gameA1.ID {
		t.Errorf("pick after rejected swap = %+v, want unchanged team=%v game=%v", still, f.teamA.ID, f.gameA1.ID)
	}
}

// TestService_UpsertPick_TeamAlreadyUsedInDifferentWeek confirms that
// trying to pick a team that's still actively committed to a DIFFERENT
// week (unlike the "changing your mind" test above, this pick is never
// abandoned) is rejected with a clean ErrTeamAlreadyUsed — not a raw
// unique-constraint DB error.
func TestService_UpsertPick_TeamAlreadyUsedInDifferentWeek(t *testing.T) {
	f := newFixture(t, 48*time.Hour)

	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameA1.ID, f.teamA.ID); err != nil {
		t.Fatalf("week1 pick of teamA: %v", err)
	}

	// teamA is still the live week1 pick (never abandoned) — picking it
	// again for week2 must be rejected.
	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week2.ID, f.league.Conference, f.gameA2.ID, f.teamA.ID); !errors.Is(err, ErrTeamAlreadyUsed) {
		t.Errorf("got err %v, want %v", err, ErrTeamAlreadyUsed)
	}

	// And no week2 row was created by the rejected attempt.
	if _, err := f.env.picks.GetPick(context.Background(), f.member.ID, f.week2.ID); !errors.Is(err, ErrPickNotFound) {
		t.Errorf("GetPick week2 after a rejected duplicate-team pick: got err %v, want %v", err, ErrPickNotFound)
	}
}

// TestService_ListAvailableTeams_LockedAndUsedFlags exercises the
// available-teams read against a still-open week: teamA correctly flagged
// is_used_elsewhere once committed to week1's row, is_locked reflecting
// each game's kickoff, and is_current_pick tracking the requester's own
// current selection.
func TestService_ListAvailableTeams_LockedAndUsedFlags(t *testing.T) {
	f := newFixture(t, 48*time.Hour)

	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameA1.ID, f.teamA.ID); err != nil {
		t.Fatalf("week1 pick: %v", err)
	}

	teams, currentPick, hasCurrentPick, err := f.env.picks.ListAvailableTeams(context.Background(), f.member.ID, f.week2.ID, f.league.Conference, f.league.SeasonYear)
	if err != nil {
		t.Fatalf("ListAvailableTeams (week2): %v", err)
	}
	if hasCurrentPick {
		t.Errorf("week2 hasCurrentPick = true, want false (no week2 pick made yet); currentPick=%+v", currentPick)
	}

	var teamARow *AvailableTeam
	for i := range teams {
		if teams[i].Row.TeamID == f.teamA.ID {
			teamARow = &teams[i]
		}
	}
	if teamARow == nil {
		t.Fatal("teamA not found in week2's available teams (it has a week2 game against oppY)")
	}
	if !teamARow.IsUsedElsewhere {
		t.Error("teamA.IsUsedElsewhere = false in week2's list, want true (committed to week1)")
	}
	if teamARow.IsLocked {
		t.Error("teamA.IsLocked = true in week2's list, want false (week2 kickoff is far in the future)")
	}

	// Now check week1's own list: teamA should show as the current pick,
	// not locked (far-future kickoff), and NOT used-elsewhere (that flag
	// only applies to OTHER weeks, not the week holding the pick itself).
	teams1, currentPick1, hasCurrentPick1, err := f.env.picks.ListAvailableTeams(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.league.SeasonYear)
	if err != nil {
		t.Fatalf("ListAvailableTeams (week1): %v", err)
	}
	if !hasCurrentPick1 || currentPick1.TeamID != f.teamA.ID {
		t.Errorf("week1 current pick = (has=%v, team=%v), want (true, %v)", hasCurrentPick1, currentPick1.TeamID, f.teamA.ID)
	}
	for _, tm := range teams1 {
		if tm.Row.TeamID == f.teamA.ID {
			if !tm.IsCurrentPick {
				t.Error("teamA.IsCurrentPick = false in week1's own list, want true")
			}
			if tm.IsUsedElsewhere {
				t.Error("teamA.IsUsedElsewhere = true in week1's own list, want false (used-elsewhere excludes the week holding the pick)")
			}
		}
	}
}

// TestService_ListAvailableTeams_MatchupStats covers the matchup-predictor
// merge: win probability and spread normalized to each team's own
// perspective (CFBD reports both from the home team's side), and SP+ rank
// surfaced for both a team and its opponent — none of it lock-gated, per
// the feature's explicit "decision support while deciding" design.
//
// Deliberately does NOT cover a live pick count — that was removed as a
// late-season fairness problem (see AvailableTeam's doc comment).
func TestService_ListAvailableTeams_MatchupStats(t *testing.T) {
	f := newFixture(t, 48*time.Hour)
	ctx := context.Background()

	// gameA1 is teamA (home) vs oppX (away) — see newFixture's doc comment.
	if _, err := f.env.q.UpsertGamePrediction(ctx, gen.UpsertGamePredictionParams{
		GameID: f.gameA1.ID, Spread: numericFromFloat64(t, -7.5), HomeWinProbability: numericFromFloat64(t, 0.71),
	}); err != nil {
		t.Fatalf("UpsertGamePrediction: %v", err)
	}
	if _, err := f.env.q.UpsertTeamSPRating(ctx, gen.UpsertTeamSPRatingParams{
		TeamID: f.teamA.ID, SeasonYear: f.league.SeasonYear, Rating: numericFromFloat64(t, 30), Ranking: pgtype.Int4{Int32: 5, Valid: true},
	}); err != nil {
		t.Fatalf("UpsertTeamSPRating (teamA): %v", err)
	}
	if _, err := f.env.q.UpsertTeamSPRating(ctx, gen.UpsertTeamSPRatingParams{
		TeamID: f.oppX.ID, SeasonYear: f.league.SeasonYear, Rating: numericFromFloat64(t, 5), Ranking: pgtype.Int4{Int32: 60, Valid: true},
	}); err != nil {
		t.Fatalf("UpsertTeamSPRating (oppX): %v", err)
	}

	if _, err := f.env.picks.UpsertPick(ctx, f.member.ID, f.week1.ID, f.league.Conference, f.gameA1.ID, f.teamA.ID); err != nil {
		t.Fatalf("commissioner's pick: %v", err)
	}

	teams, _, _, err := f.env.picks.ListAvailableTeams(ctx, f.member.ID, f.week1.ID, f.league.Conference, f.league.SeasonYear)
	if err != nil {
		t.Fatalf("ListAvailableTeams: %v", err)
	}

	// Matched on (TeamID, GameID), not TeamID alone: oppX also appears in
	// gameSec1 this week (see newFixture's doc comment on reusing its id),
	// tied with gameA1 on both ORDER BY columns (same kickoff_at, same
	// team name) — Postgres doesn't guarantee a stable order across ties,
	// so matching on TeamID alone nondeterministically picked up oppX's
	// unrelated gameSec1 row (no prediction set) instead of its gameA1 row
	// on some runs, making this test flaky.
	var teamA, oppX *AvailableTeam
	for i := range teams {
		switch {
		case teams[i].Row.TeamID == f.teamA.ID && teams[i].Row.GameID == f.gameA1.ID:
			teamA = &teams[i]
		case teams[i].Row.TeamID == f.oppX.ID && teams[i].Row.GameID == f.gameA1.ID:
			oppX = &teams[i]
		}
	}
	if teamA == nil || oppX == nil {
		t.Fatalf("expected both teamA and oppX rows for gameA1, got %+v", teams)
	}

	const epsilon = 1e-9
	almostEqual := func(a, b float64) bool {
		d := a - b
		return d > -epsilon && d < epsilon
	}

	if teamA.WinProbability == nil || !almostEqual(*teamA.WinProbability, 0.71) {
		t.Errorf("teamA.WinProbability = %v, want 0.71 (home team, unmodified)", teamA.WinProbability)
	}
	if teamA.Spread == nil || !almostEqual(*teamA.Spread, -7.5) {
		t.Errorf("teamA.Spread = %v, want -7.5 (home team, unmodified)", teamA.Spread)
	}
	// oppX is the away side of the same game — both values must be negated
	// relative to teamA's (CFBD reports them from the home team's side).
	if oppX.WinProbability == nil || !almostEqual(*oppX.WinProbability, 0.29) {
		t.Errorf("oppX.WinProbability = %v, want 0.29 (1 - home win probability)", oppX.WinProbability)
	}
	if oppX.Spread == nil || !almostEqual(*oppX.Spread, 7.5) {
		t.Errorf("oppX.Spread = %v, want 7.5 (negated home spread)", oppX.Spread)
	}

	if teamA.SPRank == nil || *teamA.SPRank != 5 {
		t.Errorf("teamA.SPRank = %v, want 5", teamA.SPRank)
	}
	if teamA.OpponentSPRank == nil || *teamA.OpponentSPRank != 60 {
		t.Errorf("teamA.OpponentSPRank = %v, want 60 (oppX's rank)", teamA.OpponentSPRank)
	}
	if oppX.SPRank == nil || *oppX.SPRank != 60 {
		t.Errorf("oppX.SPRank = %v, want 60", oppX.SPRank)
	}
}

// numericFromFloat64 mirrors internal/schedule's identical unexported
// helper (pgtype.Numeric.Scan only accepts a string, not a float64) — kept
// as a small test-only copy here rather than exporting schedule's version
// just for this.
func numericFromFloat64(t *testing.T, f float64) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(fmt.Sprintf("%v", f)); err != nil {
		t.Fatalf("numericFromFloat64(%v): %v", f, err)
	}
	return n
}

// TestService_ListMembershipPicksForSeason covers the season-wide history
// backing the leaderboard's expandable card: every week of the season
// appears (not just weeks with a pick — week2 here has none), in week
// order, with HasPicked/IsLocked correctly reflecting each week's actual
// state. Field-bundling privacy (hiding team/opponent/result together,
// not just team_id) is the HTTP handler's job — see
// handleListMembershipPicks — verified by curl E2E per this repo's
// established convention (no Go-level HTTP dispatch tests anywhere in
// this codebase); this test only covers what the service itself computes.
func TestService_ListMembershipPicksForSeason(t *testing.T) {
	f := newFixture(t, 48*time.Hour)

	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameA1.ID, f.teamA.ID); err != nil {
		t.Fatalf("week1 pick: %v", err)
	}
	// week2: deliberately no pick, to prove it still appears in the result.

	// f.league.SeasonYear (hardcoded 2026 by newFixture's CreateLeague
	// call) is NOT the same value as week1/week2's actual season_year
	// (newFixture seeds those from uniqueSeasonYear() instead, and
	// nothing else in this file depends on the two matching) — use the
	// week's own SeasonYear, the real value this endpoint's season-scoped
	// query needs.
	rows, err := f.env.picks.ListMembershipPicksForSeason(context.Background(), f.member.ID, f.week1.SeasonYear)
	if err != nil {
		t.Fatalf("ListMembershipPicksForSeason: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("got %d weeks, want at least 2 (week1 + week2)", len(rows))
	}
	// Season order.
	for i := 1; i < len(rows); i++ {
		if rows[i].Row.WeekNumber < rows[i-1].Row.WeekNumber {
			t.Fatalf("weeks not in ascending order: %+v", rows)
		}
	}

	var row1, row2 *MembershipWeekPick
	for i := range rows {
		switch rows[i].Row.WeekNumber {
		case f.week1.WeekNumber:
			row1 = &rows[i]
		case f.week2.WeekNumber:
			row2 = &rows[i]
		}
	}
	if row1 == nil || row2 == nil {
		t.Fatalf("expected both week1 (%d) and week2 (%d) in result, got %+v", f.week1.WeekNumber, f.week2.WeekNumber, rows)
	}
	if !row1.HasPicked || row1.Row.TeamID != f.teamA.ID {
		t.Errorf("week1: HasPicked=%v TeamID=%v, want HasPicked=true TeamID=%v", row1.HasPicked, row1.Row.TeamID, f.teamA.ID)
	}
	if row1.IsLocked {
		t.Error("week1.IsLocked = true, want false (kickoff is 48h out)")
	}
	if row2.HasPicked {
		t.Error("week2.HasPicked = true, want false (no pick made for week2)")
	}
	if row2.Row.TeamID.Valid {
		t.Errorf("week2.Row.TeamID = %v, want invalid/null (no pick this week)", row2.Row.TeamID)
	}

	setKickoffInPast(t, f.env.pool, f.gameA1.ID)

	rowsAfter, err := f.env.picks.ListMembershipPicksForSeason(context.Background(), f.member.ID, f.week1.SeasonYear)
	if err != nil {
		t.Fatalf("ListMembershipPicksForSeason (post-lock): %v", err)
	}
	for i := range rowsAfter {
		if rowsAfter[i].Row.WeekNumber == f.week1.WeekNumber {
			if !rowsAfter[i].IsLocked {
				t.Error("week1.IsLocked = false after kickoff passed, want true")
			}
		}
	}
}

// TestService_ListWeekPicks_PrivacyRule is the pick-visibility test: before
// a game kicks off, another member's game_id/team_id must not be
// resolvable from ListWeekPicks's result at all (both fields blank
// together — checked via the raw row, since the HTTP layer, not this
// service, is what actually omits the JSON fields); the requester's own
// pick is always fully visible; once the underlying game locks, the other
// member's pick becomes fully visible too.
func TestService_ListWeekPicks_PrivacyRule(t *testing.T) {
	f := newFixture(t, 48*time.Hour)
	otherUser := createTestUser(t, f.env.q, "playerb")
	otherMember, err := f.env.leagues.JoinByCode(context.Background(), f.league.ID, otherUser.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}

	if _, err := f.env.picks.UpsertPick(context.Background(), f.member.ID, f.week1.ID, f.league.Conference, f.gameA1.ID, f.teamA.ID); err != nil {
		t.Fatalf("commissioner pick: %v", err)
	}
	if _, err := f.env.picks.UpsertPick(context.Background(), otherMember.ID, f.week1.ID, f.league.Conference, f.gameB1.ID, f.teamB.ID); err != nil {
		t.Fatalf("other member pick: %v", err)
	}

	rows, err := f.env.picks.ListWeekPicks(context.Background(), f.league.ID, f.week1.ID)
	if err != nil {
		t.Fatalf("ListWeekPicks (pre-lock): %v", err)
	}
	var commRow, otherRow *MemberPickStatus
	for i := range rows {
		switch rows[i].Row.MembershipID {
		case f.member.ID:
			commRow = &rows[i]
		case otherMember.ID:
			otherRow = &rows[i]
		}
	}
	if commRow == nil || otherRow == nil {
		t.Fatalf("expected both members in ListWeekPicks result, got %d rows", len(rows))
	}
	if !commRow.HasPicked || !otherRow.HasPicked {
		t.Fatalf("expected both HasPicked=true pre-lock, got commissioner=%v other=%v", commRow.HasPicked, otherRow.HasPicked)
	}
	// Neither game has kicked off yet (48h out) — this is what
	// IsLocked=false signals; the HTTP layer uses exactly this flag (see
	// handleListWeekPicks) to decide whether to include game_id/team_id
	// for a non-own row, so asserting IsLocked here IS asserting the
	// privacy rule at this layer.
	if otherRow.IsLocked {
		t.Error("otherRow.IsLocked = true pre-lock, want false — would cause the HTTP layer to leak the pick early")
	}

	setKickoffInPast(t, f.env.pool, f.gameB1.ID)

	rowsAfter, err := f.env.picks.ListWeekPicks(context.Background(), f.league.ID, f.week1.ID)
	if err != nil {
		t.Fatalf("ListWeekPicks (post-lock): %v", err)
	}
	var otherRowAfter *MemberPickStatus
	for i := range rowsAfter {
		if rowsAfter[i].Row.MembershipID == otherMember.ID {
			otherRowAfter = &rowsAfter[i]
		}
	}
	if otherRowAfter == nil {
		t.Fatal("other member missing from post-lock ListWeekPicks result")
	}
	if !otherRowAfter.IsLocked {
		t.Error("otherRow.IsLocked = false post-lock, want true — the HTTP layer would still be hiding an already-started game's pick")
	}
	if otherRowAfter.Row.TeamID != f.teamB.ID {
		t.Errorf("otherRow.Row.TeamID post-lock = %v, want teamB (%v) — the raw data must still be correct even though the HTTP layer decides visibility", otherRowAfter.Row.TeamID, f.teamB.ID)
	}
}
