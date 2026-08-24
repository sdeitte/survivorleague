package recap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
)

// testDatabaseURL mirrors every other package's own copy of this helper —
// self-skips (not fails) when no database is reachable.
func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
}

func newTestQueries(t *testing.T) (*gen.Queries, *pgxpool.Pool) {
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
	return gen.New(pool), pool
}

var recapTestIDCounter = time.Now().UnixNano()

func nextRecapTestID() int64 {
	recapTestIDCounter++
	return recapTestIDCounter
}

func createRecapTestUser(t *testing.T, q *gen.Queries, label string) gen.User {
	t.Helper()
	u, err := q.CreateUser(context.Background(), gen.CreateUserParams{
		Email:        fmt.Sprintf("%s-%d@example.test", label, nextRecapTestID()),
		PasswordHash: "test-hash-not-a-real-argon2id-value",
		DisplayName:  label,
		IsSiteAdmin:  false,
	})
	if err != nil {
		t.Fatalf("createRecapTestUser: %v", err)
	}
	return u
}

func createRecapTestTeam(t *testing.T, q *gen.Queries, name, conference string) gen.Team {
	t.Helper()
	team, err := q.UpsertTeam(context.Background(), gen.UpsertTeamParams{
		ExternalID: fmt.Sprintf("recap-team-%d", nextRecapTestID()),
		Name:       name,
		Conference: conference,
	})
	if err != nil {
		t.Fatalf("createRecapTestTeam: %v", err)
	}
	return team
}

// fakeTextGenerator is a canned TextGenerator — GenerateWeekRecap's
// wiring test only needs to confirm the returned text round-trips through
// storage/retrieval correctly, not exercise real prompt content (that's
// TestBuildPrompt's job, a pure function test with no DB/network
// involved).
type fakeTextGenerator struct {
	text string
	err  error
	// gotPrompt captures the last prompt passed in, so the wiring test can
	// sanity-check it's non-empty and mentions the league without
	// duplicating buildPrompt's own detailed assertions.
	gotPrompt string
}

func (f *fakeTextGenerator) GenerateText(ctx context.Context, prompt string) (string, error) {
	f.gotPrompt = prompt
	if f.err != nil {
		return "", f.err
	}
	return f.text, nil
}

func TestService_GenerateWeekRecap_StoresAndRetrieves(t *testing.T) {
	q, pool := newTestQueries(t)
	ctx := context.Background()
	leaguesSvc := leagues.NewService(q, pool)

	commissioner := createRecapTestUser(t, q, "commish")
	league, _, err := leaguesSvc.CreateLeague(ctx, commissioner.ID, "Recap Test League", 2026, "Big Ten")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}
	week, err := q.UpsertWeek(ctx, gen.UpsertWeekParams{SeasonYear: 2026, WeekNumber: 1})
	if err != nil {
		t.Fatalf("UpsertWeek: %v", err)
	}

	fake := &fakeTextGenerator{text: "  Week 1 chaos: everyone survived. Barely.  "}
	svc := NewService(q, fake)

	if err := svc.GenerateWeekRecap(ctx, league.ID, week.ID); err != nil {
		t.Fatalf("GenerateWeekRecap: %v", err)
	}
	if fake.gotPrompt == "" || !strings.Contains(fake.gotPrompt, league.Name) {
		t.Errorf("prompt sent to model = %q, want it to mention the league name %q", fake.gotPrompt, league.Name)
	}

	stored, err := svc.GetLatestRecap(ctx, league.ID)
	if err != nil {
		t.Fatalf("GetLatestRecap: %v", err)
	}
	if stored.Body != "Week 1 chaos: everyone survived. Barely." {
		t.Errorf("stored body = %q, want the trimmed generated text", stored.Body)
	}

	// Regenerating (e.g. a retry) must upsert, not create a second row —
	// same contract as every other sync-style upsert in this codebase.
	fake.text = "Revised recap."
	if err := svc.GenerateWeekRecap(ctx, league.ID, week.ID); err != nil {
		t.Fatalf("GenerateWeekRecap (regenerate): %v", err)
	}
	stored2, err := svc.GetLatestRecap(ctx, league.ID)
	if err != nil {
		t.Fatalf("GetLatestRecap (after regenerate): %v", err)
	}
	if stored2.Body != "Revised recap." || stored2.ID != stored.ID {
		t.Errorf("regenerate = %+v, want same row id with updated body", stored2)
	}
}

func TestService_GetLatestRecap_NoneYet(t *testing.T) {
	q, pool := newTestQueries(t)
	ctx := context.Background()
	leaguesSvc := leagues.NewService(q, pool)

	commissioner := createRecapTestUser(t, q, "commish")
	league, _, err := leaguesSvc.CreateLeague(ctx, commissioner.ID, "No Recap Yet League", 2026, "SEC")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}

	svc := NewService(q, &fakeTextGenerator{})
	if _, err := svc.GetLatestRecap(ctx, league.ID); !errors.Is(err, ErrNoRecapYet) {
		t.Errorf("err = %v, want ErrNoRecapYet", err)
	}
}

// TestBuildPrompt_FactFormatting is a pure unit test (no DB/network) of
// the trickiest part of this package: score orientation (must reflect the
// PICKING member's team, not always "home first"), the eliminated-this-
// week vs. eliminated-in-an-earlier-week distinction, mass-wipeout
// phrasing, and the missed-pick case.
func TestBuildPrompt_FactFormatting(t *testing.T) {
	league := gen.League{Name: "Test League", Conference: "Big Ten"}
	week := gen.Week{ID: newUUID(t, 1), WeekNumber: 3}
	otherWeek := newUUID(t, 2)

	facts := []gen.ListWeekRecapFactsForLeagueRow{
		{
			// Winner, picked the home team — score must print home-first.
			DisplayName: "Alice", TeamID: newUUID(t, 10), TeamName: pgtype.Text{String: "Ohio State", Valid: true},
			OpponentName: pgtype.Text{String: "Michigan", Valid: true}, PickResult: pgtype.Text{String: "win", Valid: true},
			HomeTeamID: newUUID(t, 10), HomeScore: pgtype.Int4{Int32: 30, Valid: true}, AwayScore: pgtype.Int4{Int32: 20, Valid: true},
		},
		{
			// Loser, picked the AWAY team — score must print away-first
			// (their team's score first), eliminated THIS week.
			DisplayName: "Bob", TeamID: newUUID(t, 11), TeamName: pgtype.Text{String: "Rutgers", Valid: true},
			OpponentName: pgtype.Text{String: "Iowa", Valid: true}, PickResult: pgtype.Text{String: "loss", Valid: true},
			HomeTeamID: newUUID(t, 12), HomeScore: pgtype.Int4{Int32: 24, Valid: true}, AwayScore: pgtype.Int4{Int32: 10, Valid: true},
			EliminatedWeekID: week.ID,
		},
		{
			// Loser, but eliminated in an EARLIER week (this loss was
			// during a mass-wipeout, or they'd already been bought back
			// and lost again without re-elimination tracking here) —
			// must NOT be phrased as "eliminated" this week.
			DisplayName: "Carol", TeamID: newUUID(t, 13), TeamName: pgtype.Text{String: "Purdue", Valid: true},
			OpponentName: pgtype.Text{String: "Illinois", Valid: true}, PickResult: pgtype.Text{String: "loss", Valid: true},
			HomeTeamID: newUUID(t, 13), HomeScore: pgtype.Int4{Int32: 14, Valid: true}, AwayScore: pgtype.Int4{Int32: 21, Valid: true},
			EliminatedWeekID: otherWeek,
		},
		{
			// Missed pick entirely.
			DisplayName: "Dave",
		},
	}

	standings := []gen.ListLeaderboardForLeagueRow{
		{DisplayName: "Alice", Status: "active"},
		{DisplayName: "Bob", Status: "eliminated"},
	}
	pickCounts := []gen.ListPickCountsForWeekRow{
		{TeamID: newUUID(t, 10), PickCount: 1},
		{TeamID: newUUID(t, 11), PickCount: 1},
		{TeamID: newUUID(t, 13), PickCount: 1},
	}

	prompt := buildPrompt(league, week, facts, pickCounts, 3, standings)

	checks := []string{
		"Test League",                                    // league name present
		"week 3",                                          // week number present
		"Alice picked Ohio State (beat Michigan, 30-20)",  // home-picker score orientation
		"Bob picked Rutgers (lost to Iowa, 10-24)",         // away-picker score orientation (their score first)
		"eliminated.",                                     // Bob's actual elimination this week
		"Dave did not make a pick this week.",              // missed pick
		"Ohio State: 1 of 3",                               // pick-count breakdown uses real team names
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing expected fragment %q\n\nfull prompt:\n%s", want, prompt)
		}
	}

	// Carol lost but was eliminated in an earlier week — the prompt must
	// explain that distinction, not just say "eliminated" a second time
	// for a stale reason.
	if !strings.Contains(prompt, "Carol picked Purdue") || !strings.Contains(prompt, "mass-wipeout") {
		t.Errorf("Carol's earlier-week elimination not phrased correctly:\n%s", prompt)
	}

	if !strings.Contains(prompt, "invent") {
		t.Errorf("prompt must explicitly instruct the model not to invent facts")
	}
}

func newUUID(t *testing.T, seed byte) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	u.Bytes[15] = seed
	u.Valid = true
	return u
}
