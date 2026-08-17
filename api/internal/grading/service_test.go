package grading

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
	"github.com/sdeitte/survivor-league-api/internal/picks"
)

// testDatabaseURL mirrors every other package's own copy of this helper
// (internal/picks, internal/leagues, internal/schedule) — these
// integration tests self-skip (not fail) when no database is reachable,
// so `go test ./...` still passes without the local docker-compose
// Postgres running.
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
	grading *Service
	leagues *leagues.Service
	picks   *picks.Service
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
		grading: NewService(q, pool),
		leagues: leagues.NewService(q, pool),
		picks:   picks.NewService(q, pool),
		q:       q,
		pool:    pool,
	}
}

// idCounter hands out ever-increasing suffixes for fixture uniqueness —
// mirrors internal/picks/service_test.go's own copy.
var idCounter = time.Now().UnixNano()

func nextID() int64 {
	idCounter++
	return idCounter
}

var seasonYearCounter int32 = 93000

func uniqueSeasonYear() int32 {
	seasonYearCounter++
	return seasonYearCounter
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

// finalizeGame directly rewrites a game's status/scores/winner to simulate
// CFBD reporting it final — there's no live game clock in tests, so this
// (along with setKickoffInPast-style direct writes elsewhere in this repo)
// is the established way to simulate "the game just finished".
func finalizeGame(t *testing.T, pool *pgxpool.Pool, gameID, winnerTeamID pgtype.UUID, homeScore, awayScore int32) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE games SET status = 'final', winner_team_id = $1, home_score = $2, away_score = $3, updated_at = now() WHERE id = $4`,
		winnerTeamID, homeScore, awayScore, gameID)
	if err != nil {
		t.Fatalf("finalizeGame: %v", err)
	}
}

// setGameStatus directly rewrites a game's status — used to simulate
// postponed/canceled/still-in-progress games.
func setGameStatus(t *testing.T, pool *pgxpool.Pool, gameID pgtype.UUID, status string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE games SET status = $1, updated_at = now() WHERE id = $2`, status, gameID)
	if err != nil {
		t.Fatalf("setGameStatus: %v", err)
	}
}

func getMembership(t *testing.T, env testEnv, id pgtype.UUID) gen.LeagueMembership {
	t.Helper()
	m, err := env.leagues.GetMembershipByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetMembershipByID: %v", err)
	}
	return m
}

// createLeague creates a Big Ten league with a fresh commissioner, who is
// auto-added as an active contestant member — mirrors internal/picks'
// fixture pattern.
func createLeague(t *testing.T, env testEnv, name string) (gen.League, gen.LeagueMembership) {
	t.Helper()
	commissioner := createTestUser(t, env.q, "commish")
	league, member, err := env.leagues.CreateLeague(context.Background(), commissioner.ID, name, 2026, "Big Ten")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}
	return league, member
}

// addPlayer joins a fresh user to the league as a regular active
// contestant.
func addPlayer(t *testing.T, env testEnv, league gen.League, label string) gen.LeagueMembership {
	t.Helper()
	user := createTestUser(t, env.q, label)
	member, err := env.leagues.JoinByCode(context.Background(), league.ID, user.ID)
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}
	return member
}

// pick submits membershipID's pick for weekID/gameID/teamID, far enough
// before kickoff not to be locked (callers create games far in the
// future, then finalizeGame simulates the result directly).
func pick(t *testing.T, env testEnv, membershipID, weekID pgtype.UUID, conference string, gameID, teamID pgtype.UUID) {
	t.Helper()
	if _, err := env.picks.UpsertPick(context.Background(), membershipID, weekID, conference, gameID, teamID); err != nil {
		t.Fatalf("UpsertPick: %v", err)
	}
}

// --- GradeGame ---

// TestGradeGame_GradesWinAndLossAndReturnsTouchedLeague is the normal
// single-game case: two picks on the same game, one on the winner, one on
// the loser, both within one league. Confirms picks.result is set
// correctly and GradeGame reports the touched (league, week) pair.
func TestGradeGame_GradesWinAndLossAndReturnsTouchedLeague(t *testing.T) {
	env := newTestEnv(t)
	league, commissioner := createLeague(t, env, "Grading Normal")
	player := addPlayer(t, env, league, "player")

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))

	pick(t, env, commissioner.ID, week.ID, league.Conference, game.ID, teamA.ID) // will win
	pick(t, env, player.ID, week.ID, league.Conference, game.ID, teamB.ID)       // will lose

	finalizeGame(t, env.pool, game.ID, teamA.ID, 30, 10)

	pairs, err := env.grading.GradeGame(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("GradeGame: %v", err)
	}
	if len(pairs) != 1 || pairs[0].LeagueID != league.ID || pairs[0].WeekID != week.ID {
		t.Fatalf("GradeGame pairs = %+v, want exactly one pair for (league=%v, week=%v)", pairs, league.ID, week.ID)
	}

	winPick, err := env.picks.GetPick(context.Background(), commissioner.ID, week.ID)
	if err != nil {
		t.Fatalf("GetPick (winner): %v", err)
	}
	if winPick.Result != "win" {
		t.Errorf("winner pick.Result = %q, want %q", winPick.Result, "win")
	}
	lossPick, err := env.picks.GetPick(context.Background(), player.ID, week.ID)
	if err != nil {
		t.Fatalf("GetPick (loser): %v", err)
	}
	if lossPick.Result != "loss" {
		t.Errorf("loser pick.Result = %q, want %q", lossPick.Result, "loss")
	}

	game2, err := env.q.GetGameByIDWithTeams(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("GetGameByIDWithTeams: %v", err)
	}
	if !game2.GradedAt.Valid {
		t.Error("game.GradedAt not set after GradeGame")
	}
}

// TestGradeGame_Idempotent confirms calling GradeGame twice on the same
// game doesn't double-grade, doesn't error, and the second call reports no
// touched pairs (since there's nothing new to report).
func TestGradeGame_Idempotent(t *testing.T) {
	env := newTestEnv(t)
	league, commissioner := createLeague(t, env, "Grading Idempotent")

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))
	pick(t, env, commissioner.ID, week.ID, league.Conference, game.ID, teamA.ID)
	finalizeGame(t, env.pool, game.ID, teamA.ID, 21, 14)

	first, err := env.grading.GradeGame(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("first GradeGame: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first GradeGame pairs = %+v, want 1", first)
	}

	second, err := env.grading.GradeGame(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("second GradeGame: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second (re-fired) GradeGame pairs = %+v, want none (already graded, idempotent no-op)", second)
	}

	winPick, err := env.picks.GetPick(context.Background(), commissioner.ID, week.ID)
	if err != nil {
		t.Fatalf("GetPick: %v", err)
	}
	if winPick.Result != "win" {
		t.Errorf("pick.Result after double grading = %q, want %q (must not flip/duplicate)", winPick.Result, "win")
	}
}

// TestGradeGame_NotFinalYetIsANoOp confirms grading a game that hasn't
// reached status='final' does nothing and returns no error.
func TestGradeGame_NotFinalYetIsANoOp(t *testing.T) {
	env := newTestEnv(t)
	_, commissioner := createLeague(t, env, "Grading Not Final")
	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))

	pairs, err := env.grading.GradeGame(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("GradeGame: %v", err)
	}
	if len(pairs) != 0 {
		t.Errorf("GradeGame pairs for a not-yet-final game = %+v, want none", pairs)
	}
	_ = commissioner
}

// --- TryFinalizeLeagueWeek ---

// TestTryFinalizeLeagueWeek_NormalCase: one winner stays active, one loser
// is eliminated with the correct eliminated_week_id/eliminated_game_id.
func TestTryFinalizeLeagueWeek_NormalCase(t *testing.T) {
	env := newTestEnv(t)
	league, winner := createLeague(t, env, "Finalize Normal")
	loser := addPlayer(t, env, league, "loser")

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))

	pick(t, env, winner.ID, week.ID, league.Conference, game.ID, teamA.ID)
	pick(t, env, loser.ID, week.ID, league.Conference, game.ID, teamB.ID)
	finalizeGame(t, env.pool, game.ID, teamA.ID, 28, 7)

	if _, err := env.grading.GradeGame(context.Background(), game.ID); err != nil {
		t.Fatalf("GradeGame: %v", err)
	}

	result, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("TryFinalizeLeagueWeek: %v", err)
	}
	if result == nil {
		t.Fatal("TryFinalizeLeagueWeek returned nil, want a finalized result")
	}
	if result.MassWipeout {
		t.Error("MassWipeout = true, want false (someone won)")
	}

	winnerAfter := getMembership(t, env, winner.ID)
	if winnerAfter.Status != "active" {
		t.Errorf("winner status = %q, want %q", winnerAfter.Status, "active")
	}

	loserAfter := getMembership(t, env, loser.ID)
	if loserAfter.Status != "eliminated" {
		t.Errorf("loser status = %q, want %q", loserAfter.Status, "eliminated")
	}
	if loserAfter.EliminatedWeekID != week.ID {
		t.Errorf("loser eliminated_week_id = %v, want %v", loserAfter.EliminatedWeekID, week.ID)
	}
	if !loserAfter.EliminatedGameID.Valid || loserAfter.EliminatedGameID != game.ID {
		t.Errorf("loser eliminated_game_id = %v, want %v", loserAfter.EliminatedGameID, game.ID)
	}
}

// TestTryFinalizeLeagueWeek_MissedPickEliminatedWithNullGame is the
// missed-pick scenario the plan explicitly calls out: a contestant with no
// pick row for the week is eliminated when the week finalizes, with
// eliminated_game_id left NULL (nothing to point it at).
func TestTryFinalizeLeagueWeek_MissedPickEliminatedWithNullGame(t *testing.T) {
	env := newTestEnv(t)
	league, winner := createLeague(t, env, "Finalize Missed Pick")
	missed := addPlayer(t, env, league, "missed")

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))

	pick(t, env, winner.ID, week.ID, league.Conference, game.ID, teamA.ID)
	// `missed` never picks at all.
	finalizeGame(t, env.pool, game.ID, teamA.ID, 24, 17)
	if _, err := env.grading.GradeGame(context.Background(), game.ID); err != nil {
		t.Fatalf("GradeGame: %v", err)
	}

	result, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("TryFinalizeLeagueWeek: %v", err)
	}
	if result == nil || result.MassWipeout {
		t.Fatalf("result = %+v, want a non-mass-wipeout finalization", result)
	}

	missedAfter := getMembership(t, env, missed.ID)
	if missedAfter.Status != "eliminated" {
		t.Errorf("missed-pick member status = %q, want %q", missedAfter.Status, "eliminated")
	}
	if missedAfter.EliminatedWeekID != week.ID {
		t.Errorf("missed-pick member eliminated_week_id = %v, want %v", missedAfter.EliminatedWeekID, week.ID)
	}
	if missedAfter.EliminatedGameID.Valid {
		t.Errorf("missed-pick member eliminated_game_id = %v, want NULL (no pick to point at)", missedAfter.EliminatedGameID)
	}
}

// TestTryFinalizeLeagueWeek_MassWipeoutEliminatesNobody forces a scenario
// where every active contestant picked losing teams — confirms
// mass_wipeout=true and that NOBODY's status changed to eliminated. Called
// out explicitly in the plan's roadmap as a required test scenario.
func TestTryFinalizeLeagueWeek_MassWipeoutEliminatesNobody(t *testing.T) {
	env := newTestEnv(t)
	league, commissioner := createLeague(t, env, "Mass Wipeout")
	player2 := addPlayer(t, env, league, "player2")
	player3 := addPlayer(t, env, league, "player3") // will miss the pick entirely

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten") // will win
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten") // will lose
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))

	// Every active contestant either picks the loser or misses entirely —
	// zero wins among them.
	pick(t, env, commissioner.ID, week.ID, league.Conference, game.ID, teamB.ID)
	pick(t, env, player2.ID, week.ID, league.Conference, game.ID, teamB.ID)
	// player3 misses the pick.
	finalizeGame(t, env.pool, game.ID, teamA.ID, 35, 3)

	if _, err := env.grading.GradeGame(context.Background(), game.ID); err != nil {
		t.Fatalf("GradeGame: %v", err)
	}

	result, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("TryFinalizeLeagueWeek: %v", err)
	}
	if result == nil {
		t.Fatal("TryFinalizeLeagueWeek returned nil, want a finalized (mass-wipeout) result")
	}
	if !result.MassWipeout {
		t.Fatal("MassWipeout = false, want true (nobody won)")
	}

	for _, m := range []struct {
		label string
		id    pgtype.UUID
	}{
		{"commissioner", commissioner.ID},
		{"player2", player2.ID},
		{"player3 (missed)", player3.ID},
	} {
		after := getMembership(t, env, m.id)
		if after.Status != "active" {
			t.Errorf("%s status after mass wipeout = %q, want %q (nobody eliminated)", m.label, after.Status, "active")
		}
		if after.EliminatedWeekID.Valid {
			t.Errorf("%s eliminated_week_id after mass wipeout = %v, want unset", m.label, after.EliminatedWeekID)
		}
	}
}

// TestTryFinalizeLeagueWeek_Idempotent calls TryFinalizeLeagueWeek twice
// for the same already-processed league/week — confirms no
// double-elimination, no duplicate league_week_results row, and no error.
func TestTryFinalizeLeagueWeek_Idempotent(t *testing.T) {
	env := newTestEnv(t)
	league, winner := createLeague(t, env, "Finalize Idempotent")
	loser := addPlayer(t, env, league, "loser")

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))
	pick(t, env, winner.ID, week.ID, league.Conference, game.ID, teamA.ID)
	pick(t, env, loser.ID, week.ID, league.Conference, game.ID, teamB.ID)
	finalizeGame(t, env.pool, game.ID, teamA.ID, 20, 6)
	if _, err := env.grading.GradeGame(context.Background(), game.ID); err != nil {
		t.Fatalf("GradeGame: %v", err)
	}

	first, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil || first == nil {
		t.Fatalf("first TryFinalizeLeagueWeek: result=%+v err=%v", first, err)
	}

	second, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("second TryFinalizeLeagueWeek: %v", err)
	}
	if second != nil {
		t.Errorf("second (re-fired) TryFinalizeLeagueWeek = %+v, want nil (already finalized, no-op)", second)
	}

	// Exactly one league_week_results row — the DB's own UNIQUE constraint
	// is what actually guarantees this, but assert the read-back too.
	row, err := env.q.GetLeagueWeekResultByLeagueAndWeek(context.Background(), gen.GetLeagueWeekResultByLeagueAndWeekParams{
		LeagueID: league.ID, WeekID: week.ID,
	})
	if err != nil {
		t.Fatalf("GetLeagueWeekResultByLeagueAndWeek: %v", err)
	}
	if row.ID != first.ID {
		t.Errorf("league_week_results row id = %v, want the first call's row id %v (no duplicate row)", row.ID, first.ID)
	}

	loserAfter := getMembership(t, env, loser.ID)
	if loserAfter.Status != "eliminated" {
		t.Errorf("loser status = %q, want %q", loserAfter.Status, "eliminated")
	}
}

// TestTryFinalizeLeagueWeek_CrossLeagueGradingIsIndependent: the same game
// is picked in two different (same-conference) leagues. Each league must
// finalize/eliminate based only on its own contestants, unaffected by the
// other league's outcome.
func TestTryFinalizeLeagueWeek_CrossLeagueGradingIsIndependent(t *testing.T) {
	env := newTestEnv(t)
	leagueX, playerX := createLeague(t, env, "League X")
	leagueY, playerY := createLeague(t, env, "League Y")

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))

	// League X's sole contestant picks the eventual winner; League Y's
	// sole contestant picks the eventual loser.
	pick(t, env, playerX.ID, week.ID, leagueX.Conference, game.ID, teamA.ID)
	pick(t, env, playerY.ID, week.ID, leagueY.Conference, game.ID, teamB.ID)
	finalizeGame(t, env.pool, game.ID, teamA.ID, 17, 14)

	pairs, err := env.grading.GradeGame(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("GradeGame: %v", err)
	}
	touched := map[pgtype.UUID]bool{}
	for _, p := range pairs {
		touched[p.LeagueID] = true
	}
	if !touched[leagueX.ID] || !touched[leagueY.ID] {
		t.Fatalf("GradeGame pairs = %+v, want both leagueX (%v) and leagueY (%v) touched", pairs, leagueX.ID, leagueY.ID)
	}

	resultX, err := env.grading.TryFinalizeLeagueWeek(context.Background(), leagueX.ID, week.ID)
	if err != nil || resultX == nil || resultX.MassWipeout {
		t.Fatalf("TryFinalizeLeagueWeek(leagueX): result=%+v err=%v, want a non-mass-wipeout finalization", resultX, err)
	}
	resultY, err := env.grading.TryFinalizeLeagueWeek(context.Background(), leagueY.ID, week.ID)
	if err != nil || resultY == nil {
		t.Fatalf("TryFinalizeLeagueWeek(leagueY): result=%+v err=%v", resultY, err)
	}
	// League Y's only contestant lost — with nobody else in that league,
	// that's a mass wipeout (zero wins), not a plain elimination.
	if !resultY.MassWipeout {
		t.Fatalf("leagueY MassWipeout = false, want true (its sole contestant lost)")
	}

	playerXAfter := getMembership(t, env, playerX.ID)
	if playerXAfter.Status != "active" {
		t.Errorf("leagueX player status = %q, want %q", playerXAfter.Status, "active")
	}
	playerYAfter := getMembership(t, env, playerY.ID)
	if playerYAfter.Status != "active" {
		t.Errorf("leagueY player status = %q, want %q (mass wipeout, not eliminated)", playerYAfter.Status, "active")
	}
}

// TestTryFinalizeLeagueWeek_PostponedGameBlocksFinalization: a week with
// one final game and one postponed game must NOT finalize — nobody's
// status changes and no league_week_results row is written until the
// postponement is resolved (Phase 8, out of scope here).
func TestTryFinalizeLeagueWeek_PostponedGameBlocksFinalization(t *testing.T) {
	env := newTestEnv(t)
	league, playerA := createLeague(t, env, "Postponed Blocks")
	playerB := addPlayer(t, env, league, "playerB")

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	teamC := createTestTeam(t, env.q, "Team C", "Big Ten")
	teamD := createTestTeam(t, env.q, "Team D", "Big Ten")
	finalGame := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))
	postponedGame := createTestGame(t, env.q, week, teamC, teamD, time.Now().Add(48*time.Hour))

	pick(t, env, playerA.ID, week.ID, league.Conference, finalGame.ID, teamA.ID)
	pick(t, env, playerB.ID, week.ID, league.Conference, postponedGame.ID, teamC.ID)

	finalizeGame(t, env.pool, finalGame.ID, teamA.ID, 10, 0)
	setGameStatus(t, env.pool, postponedGame.ID, "postponed")

	if _, err := env.grading.GradeGame(context.Background(), finalGame.ID); err != nil {
		t.Fatalf("GradeGame: %v", err)
	}

	result, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("TryFinalizeLeagueWeek: %v", err)
	}
	if result != nil {
		t.Fatalf("TryFinalizeLeagueWeek = %+v, want nil (blocked by the postponed game)", result)
	}

	_, err = env.q.GetLeagueWeekResultByLeagueAndWeek(context.Background(), gen.GetLeagueWeekResultByLeagueAndWeekParams{
		LeagueID: league.ID, WeekID: week.ID,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetLeagueWeekResultByLeagueAndWeek err = %v, want pgx.ErrNoRows (no row should exist yet)", err)
	}

	for _, id := range []pgtype.UUID{playerA.ID, playerB.ID} {
		m := getMembership(t, env, id)
		if m.Status != "active" {
			t.Errorf("membership %v status = %q, want %q (unchanged while blocked)", id, m.Status, "active")
		}
	}
}

// TestTryFinalizeLeagueWeek_WaitsForAllConferenceGamesFinal: only one of
// two conference-relevant games in the week is final — TryFinalizeLeagueWeek
// must wait, then succeed once the second game is also final.
func TestTryFinalizeLeagueWeek_WaitsForAllConferenceGamesFinal(t *testing.T) {
	env := newTestEnv(t)
	league, playerA := createLeague(t, env, "Waits For All Final")
	playerB := addPlayer(t, env, league, "playerB")

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	teamC := createTestTeam(t, env.q, "Team C", "Big Ten")
	teamD := createTestTeam(t, env.q, "Team D", "Big Ten")
	gameOne := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))
	gameTwo := createTestGame(t, env.q, week, teamC, teamD, time.Now().Add(48*time.Hour))

	pick(t, env, playerA.ID, week.ID, league.Conference, gameOne.ID, teamA.ID)
	pick(t, env, playerB.ID, week.ID, league.Conference, gameTwo.ID, teamC.ID)

	finalizeGame(t, env.pool, gameOne.ID, teamA.ID, 14, 3)
	// gameTwo is still 'scheduled' — not final yet.

	if _, err := env.grading.GradeGame(context.Background(), gameOne.ID); err != nil {
		t.Fatalf("GradeGame(gameOne): %v", err)
	}

	result, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("TryFinalizeLeagueWeek (should wait): %v", err)
	}
	if result != nil {
		t.Fatalf("TryFinalizeLeagueWeek = %+v, want nil (gameTwo not final yet)", result)
	}

	// Now gameTwo finishes too.
	finalizeGame(t, env.pool, gameTwo.ID, teamC.ID, 21, 10)
	if _, err := env.grading.GradeGame(context.Background(), gameTwo.ID); err != nil {
		t.Fatalf("GradeGame(gameTwo): %v", err)
	}

	result2, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("TryFinalizeLeagueWeek (after both final): %v", err)
	}
	if result2 == nil {
		t.Fatal("TryFinalizeLeagueWeek = nil, want a finalized result now that both games are final")
	}
}

// TestTryFinalizeLeagueWeek_NonContestantCommissionerExcluded: a
// manage-only commissioner (is_contestant=false) with no pick at all must
// never be eliminated, and must not affect the mass-wipeout calculation.
func TestTryFinalizeLeagueWeek_NonContestantCommissionerExcluded(t *testing.T) {
	env := newTestEnv(t)
	commissionerUser := createTestUser(t, env.q, "manage-only-commish")
	league, commissionerMember, err := env.leagues.CreateLeague(context.Background(), commissionerUser.ID, "Non-Contestant Commish", 2026, "Big Ten")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}
	// Flip the commissioner to manage-only, per the toggle the plan
	// describes (UpdateCommissionerIsContestant).
	if _, err := env.leagues.UpdateCommissionerIsContestant(context.Background(), commissionerMember.ID, false); err != nil {
		t.Fatalf("UpdateCommissionerIsContestant: %v", err)
	}

	activePlayer := addPlayer(t, env, league, "active-player")

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))

	// The only real contestant picks (and wins) — the commissioner never
	// picks at all, since they're manage-only.
	pick(t, env, activePlayer.ID, week.ID, league.Conference, game.ID, teamA.ID)
	finalizeGame(t, env.pool, game.ID, teamA.ID, 31, 20)
	if _, err := env.grading.GradeGame(context.Background(), game.ID); err != nil {
		t.Fatalf("GradeGame: %v", err)
	}

	result, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("TryFinalizeLeagueWeek: %v", err)
	}
	if result == nil {
		t.Fatal("TryFinalizeLeagueWeek returned nil, want a finalized result")
	}
	if result.MassWipeout {
		t.Error("MassWipeout = true, want false — the active player won, and the non-contestant commissioner must not have dragged this into a wipeout")
	}

	commAfter := getMembership(t, env, commissionerMember.ID)
	if commAfter.Status != "active" {
		t.Errorf("non-contestant commissioner status = %q, want %q (never eliminated)", commAfter.Status, "active")
	}
	if commAfter.EliminatedWeekID.Valid {
		t.Errorf("non-contestant commissioner eliminated_week_id = %v, want unset", commAfter.EliminatedWeekID)
	}

	playerAfter := getMembership(t, env, activePlayer.ID)
	if playerAfter.Status != "active" {
		t.Errorf("active player status = %q, want %q", playerAfter.Status, "active")
	}
}
