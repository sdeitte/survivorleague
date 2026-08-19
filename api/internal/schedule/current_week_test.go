package schedule

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// idCounter/nextID mirror internal/picks/service_test.go's own copy —
// each test file in this package needs its own unique external_ids for
// UpsertTeam/UpsertGame, and Go doesn't let two _test.go files in the
// same package both declare the same package-level name.
var currentWeekIDCounter = time.Now().UnixNano()

func nextCurrentWeekTestID() int64 {
	currentWeekIDCounter++
	return currentWeekIDCounter
}

func createCWTestTeam(t *testing.T, q *gen.Queries, name, conference string) gen.Team {
	t.Helper()
	team, err := q.UpsertTeam(context.Background(), gen.UpsertTeamParams{
		ExternalID: fmt.Sprintf("cw-team-%d", nextCurrentWeekTestID()),
		Name:       name,
		Conference: conference,
	})
	if err != nil {
		t.Fatalf("createCWTestTeam: %v", err)
	}
	return team
}

func createCWTestWeek(t *testing.T, q *gen.Queries, seasonYear, weekNumber int32) gen.Week {
	t.Helper()
	week, err := q.UpsertWeek(context.Background(), gen.UpsertWeekParams{SeasonYear: seasonYear, WeekNumber: weekNumber})
	if err != nil {
		t.Fatalf("createCWTestWeek: %v", err)
	}
	return week
}

func createCWTestGame(t *testing.T, q *gen.Queries, week gen.Week, home, away gen.Team, kickoffAt time.Time) gen.Game {
	t.Helper()
	game, err := q.UpsertGame(context.Background(), gen.UpsertGameParams{
		ExternalID: fmt.Sprintf("cw-game-%d", nextCurrentWeekTestID()),
		WeekID:     week.ID,
		HomeTeamID: home.ID,
		AwayTeamID: away.ID,
		KickoffAt:  pgtype.Timestamptz{Time: kickoffAt, Valid: true},
		Status:     "scheduled",
	})
	if err != nil {
		t.Fatalf("createCWTestGame: %v", err)
	}
	return game
}

// TestService_CurrentWeek covers every branch of the selection rule: a
// week whose kickoff window brackets now; the gap between two weeks
// (defaults forward, not back); before the first week (preseason);
// after the last week (season over); and no schedule data at all.
func TestService_CurrentWeek(t *testing.T) {
	q := newTestQueries(t)
	svc := NewService(q, nil) // no CFBD client needed — CurrentWeek never calls it
	year := int32(uniqueSeasonYear())
	conf := "Big Ten"

	teamA := createCWTestTeam(t, q, "CW Team A", conf)
	teamB := createCWTestTeam(t, q, "CW Team B", conf)

	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	week1 := createCWTestWeek(t, q, year, 1)
	createCWTestGame(t, q, week1, teamA, teamB, base)                    // week1 window: [base, base]
	week2 := createCWTestWeek(t, q, year, 2)
	createCWTestGame(t, q, week2, teamA, teamB, base.Add(7*24*time.Hour)) // week2 window: [base+7d, base+7d]
	week3 := createCWTestWeek(t, q, year, 3)
	createCWTestGame(t, q, week3, teamA, teamB, base.Add(14*24*time.Hour))

	t.Run("within a week's window", func(t *testing.T) {
		row, err := svc.CurrentWeek(context.Background(), year, conf, base.Add(7*24*time.Hour))
		if err != nil {
			t.Fatalf("CurrentWeek: %v", err)
		}
		if row.WeekNumber != week2.WeekNumber {
			t.Errorf("week_number = %d, want %d (week2, now is exactly its kickoff)", row.WeekNumber, week2.WeekNumber)
		}
	})

	t.Run("in the gap between two weeks defaults forward", func(t *testing.T) {
		// Tuesday between week1 (base) and week2 (base+7d): closer to
		// week1 in absolute time, but the rule must still pick week2 —
		// "forward" is a deliberate choice (the week you're heading into,
		// not the one that already happened), not "nearest".
		gap := base.Add(2 * 24 * time.Hour)
		row, err := svc.CurrentWeek(context.Background(), year, conf, gap)
		if err != nil {
			t.Fatalf("CurrentWeek: %v", err)
		}
		if row.WeekNumber != week2.WeekNumber {
			t.Errorf("week_number = %d, want %d (next upcoming week, not week1 which just finished)", row.WeekNumber, week2.WeekNumber)
		}
	})

	t.Run("before the first week falls back to the first week", func(t *testing.T) {
		row, err := svc.CurrentWeek(context.Background(), year, conf, base.Add(-30*24*time.Hour))
		if err != nil {
			t.Fatalf("CurrentWeek: %v", err)
		}
		if row.WeekNumber != week1.WeekNumber {
			t.Errorf("week_number = %d, want %d (preseason -> first week)", row.WeekNumber, week1.WeekNumber)
		}
	})

	t.Run("after the last week falls back to the last week", func(t *testing.T) {
		row, err := svc.CurrentWeek(context.Background(), year, conf, base.Add(30*24*time.Hour))
		if err != nil {
			t.Fatalf("CurrentWeek: %v", err)
		}
		if row.WeekNumber != week3.WeekNumber {
			t.Errorf("week_number = %d, want %d (season over -> last week)", row.WeekNumber, week3.WeekNumber)
		}
	})

	t.Run("no schedule data for this season/conference returns ErrNoScheduleData", func(t *testing.T) {
		emptyYear := int32(uniqueSeasonYear())
		_, err := svc.CurrentWeek(context.Background(), emptyYear, conf, base)
		if !errors.Is(err, ErrNoScheduleData) {
			t.Errorf("err = %v, want ErrNoScheduleData", err)
		}
	})

	t.Run("games from a different conference are excluded", func(t *testing.T) {
		secYear := int32(uniqueSeasonYear())
		secTeamA := createCWTestTeam(t, q, "CW SEC Team A", "SEC")
		secTeamB := createCWTestTeam(t, q, "CW SEC Team B", "SEC")
		secWeek := createCWTestWeek(t, q, secYear, 1)
		createCWTestGame(t, q, secWeek, secTeamA, secTeamB, base)

		_, err := svc.CurrentWeek(context.Background(), secYear, "Big Ten", base)
		if !errors.Is(err, ErrNoScheduleData) {
			t.Errorf("err = %v, want ErrNoScheduleData (only SEC games exist for this season)", err)
		}
	})
}

// TestService_ListWeeksBySeasonYearAndConference guards against the
// production bug where a week that's a no-op for a league's conference
// (e.g. a standalone Army-Navy game occupying its own global week number
// after every other conference's season has ended) showed up as a
// selectable-but-empty week on that league's picks screen — weeks are
// global/shared across conferences, so listing "the season's weeks" without
// filtering by conference always leaked this.
func TestService_ListWeeksBySeasonYearAndConference(t *testing.T) {
	q := newTestQueries(t)
	svc := NewService(q, nil)
	year := int32(uniqueSeasonYear())
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	bigTenA := createCWTestTeam(t, q, "LW Big Ten A", "Big Ten")
	bigTenB := createCWTestTeam(t, q, "LW Big Ten B", "Big Ten")
	week1 := createCWTestWeek(t, q, year, 1)
	createCWTestGame(t, q, week1, bigTenA, bigTenB, base)
	week2 := createCWTestWeek(t, q, year, 2)
	createCWTestGame(t, q, week2, bigTenA, bigTenB, base.Add(7*24*time.Hour))

	// A standalone week, in the same season, with only an AAC game —
	// mirrors the real Army-Navy case: a real global week row that has no
	// bearing on a Big Ten league.
	aacA := createCWTestTeam(t, q, "LW AAC A", "American Athletic Conference")
	aacB := createCWTestTeam(t, q, "LW AAC B", "American Athletic Conference")
	week15 := createCWTestWeek(t, q, year, 15)
	createCWTestGame(t, q, week15, aacA, aacB, base.Add(90*24*time.Hour))

	weeks, err := svc.ListWeeksBySeasonYearAndConference(context.Background(), year, "Big Ten")
	if err != nil {
		t.Fatalf("ListWeeksBySeasonYearAndConference: %v", err)
	}
	if len(weeks) != 2 {
		t.Fatalf("got %d weeks, want 2 (week15's AAC-only game must not appear for Big Ten)", len(weeks))
	}
	if weeks[0].WeekNumber != week1.WeekNumber || weeks[1].WeekNumber != week2.WeekNumber {
		t.Errorf("weeks = %d, %d; want %d, %d in order", weeks[0].WeekNumber, weeks[1].WeekNumber, week1.WeekNumber, week2.WeekNumber)
	}

	aacWeeks, err := svc.ListWeeksBySeasonYearAndConference(context.Background(), year, "American Athletic Conference")
	if err != nil {
		t.Fatalf("ListWeeksBySeasonYearAndConference: %v", err)
	}
	if len(aacWeeks) != 1 || aacWeeks[0].WeekNumber != week15.WeekNumber {
		t.Fatalf("AAC weeks = %+v, want exactly week %d", aacWeeks, week15.WeekNumber)
	}
}
