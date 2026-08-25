package leagues

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// seasonTestGame creates a single game for seasonYear/conference in the
// given status — everything IsSeasonComplete's check needs, without a
// full picks-style fixture (this package has no need for the games
// themselves to be pickable, just present with a status).
func seasonTestGame(t *testing.T, q *gen.Queries, seasonYear, weekNumber int32, conference, status string) {
	t.Helper()
	week, err := q.UpsertWeek(context.Background(), gen.UpsertWeekParams{SeasonYear: seasonYear, WeekNumber: weekNumber})
	if err != nil {
		t.Fatalf("UpsertWeek: %v", err)
	}
	id := time.Now().UnixNano()
	home, err := q.UpsertTeam(context.Background(), gen.UpsertTeamParams{
		ExternalID: fmt.Sprintf("season-home-%d", id), Name: fmt.Sprintf("Season Home %d", id), Conference: conference,
	})
	if err != nil {
		t.Fatalf("UpsertTeam (home): %v", err)
	}
	away, err := q.UpsertTeam(context.Background(), gen.UpsertTeamParams{
		ExternalID: fmt.Sprintf("season-away-%d", id), Name: fmt.Sprintf("Season Away %d", id), Conference: conference,
	})
	if err != nil {
		t.Fatalf("UpsertTeam (away): %v", err)
	}
	if _, err := q.UpsertGame(context.Background(), gen.UpsertGameParams{
		ExternalID: fmt.Sprintf("season-game-%d", id), WeekID: week.ID, HomeTeamID: home.ID, AwayTeamID: away.ID,
		KickoffAt: pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}, Status: status,
	}); err != nil {
		t.Fatalf("UpsertGame: %v", err)
	}
}

// TestService_IsSeasonComplete_NoGamesSyncedIsNotComplete confirms "no
// data yet" is never mistaken for "season over" — a brand-new league
// (or one whose schedule sync hasn't run yet) must not show a
// co-champions banner.
func TestService_IsSeasonComplete_NoGamesSyncedIsNotComplete(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	seasonYear := int32(99000 + int(time.Now().UnixNano()%1000))
	league, _, err := s.CreateLeague(context.Background(), commissioner.ID, "No Games League", seasonYear, "Big Ten", "Test Team")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}

	complete, err := s.IsSeasonComplete(context.Background(), league.ID)
	if err != nil {
		t.Fatalf("IsSeasonComplete: %v", err)
	}
	if complete {
		t.Error("IsSeasonComplete = true with no synced games, want false")
	}
}

// TestService_IsSeasonComplete_UnfinishedGameBlocksCompletion confirms a
// single non-final conference-relevant game (scheduled, or stuck
// postponed/canceled — same bar as TryFinalizeLeagueWeek) keeps the
// season from being reported complete.
func TestService_IsSeasonComplete_UnfinishedGameBlocksCompletion(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	seasonYear := int32(91000 + int(time.Now().UnixNano()%1000))
	league, _, err := s.CreateLeague(context.Background(), commissioner.ID, "Unfinished Games League", seasonYear, "Big Ten", "Test Team")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}

	seasonTestGame(t, q, seasonYear, 1, "Big Ten", "final")
	seasonTestGame(t, q, seasonYear, 2, "Big Ten", "scheduled")

	complete, err := s.IsSeasonComplete(context.Background(), league.ID)
	if err != nil {
		t.Fatalf("IsSeasonComplete: %v", err)
	}
	if complete {
		t.Error("IsSeasonComplete = true with a still-scheduled game, want false")
	}
}

// TestService_IsSeasonComplete_AllFinalIsComplete is the happy path: every
// conference-relevant game synced for the season has reached 'final'.
func TestService_IsSeasonComplete_AllFinalIsComplete(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	seasonYear := int32(92000 + int(time.Now().UnixNano()%1000))
	league, _, err := s.CreateLeague(context.Background(), commissioner.ID, "Complete Season League", seasonYear, "Big Ten", "Test Team")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}

	seasonTestGame(t, q, seasonYear, 1, "Big Ten", "final")
	seasonTestGame(t, q, seasonYear, 2, "Big Ten", "final")

	complete, err := s.IsSeasonComplete(context.Background(), league.ID)
	if err != nil {
		t.Fatalf("IsSeasonComplete: %v", err)
	}
	if !complete {
		t.Error("IsSeasonComplete = false with every game final, want true")
	}
}
