package schedule

import (
	"context"
	"testing"
	"time"
)

// TestService_IsFirstWeekPickableForConference covers the "lock joins once
// week 1 is unpickable" rule: still-open before/during week 1 while at
// least one of its games hasn't kicked off, closed the instant the last
// week-1 game kicks off (regardless of later weeks), and open by default
// when no schedule data has synced yet.
func TestService_IsFirstWeekPickableForConference(t *testing.T) {
	q := newTestQueries(t)
	svc := NewService(q, nil)
	year := int32(uniqueSeasonYear())
	conf := "Big Ten"

	teamA := createCWTestTeam(t, q, "JL Team A", conf)
	teamB := createCWTestTeam(t, q, "JL Team B", conf)
	teamC := createCWTestTeam(t, q, "JL Team C", conf)
	teamD := createCWTestTeam(t, q, "JL Team D", conf)

	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) // week1's earlier game
	late := base.Add(3 * time.Hour)                      // week1's later game (e.g. a night game)

	week1 := createCWTestWeek(t, q, year, 1)
	createCWTestGame(t, q, week1, teamA, teamB, base)
	createCWTestGame(t, q, week1, teamC, teamD, late)
	week2 := createCWTestWeek(t, q, year, 2)
	createCWTestGame(t, q, week2, teamA, teamB, base.Add(7*24*time.Hour))

	t.Run("before any week 1 game kicks off, pickable", func(t *testing.T) {
		pickable, err := svc.IsFirstWeekPickableForConference(context.Background(), year, conf, base.Add(-time.Hour))
		if err != nil {
			t.Fatalf("IsFirstWeekPickableForConference: %v", err)
		}
		if !pickable {
			t.Error("pickable = false, want true (before week 1 starts)")
		}
	})

	t.Run("after the early game but before the late one, still pickable", func(t *testing.T) {
		pickable, err := svc.IsFirstWeekPickableForConference(context.Background(), year, conf, base.Add(time.Hour))
		if err != nil {
			t.Fatalf("IsFirstWeekPickableForConference: %v", err)
		}
		if !pickable {
			t.Error("pickable = false, want true (the late week-1 game hasn't kicked off yet)")
		}
	})

	t.Run("the instant the last week 1 game kicks off, no longer pickable", func(t *testing.T) {
		pickable, err := svc.IsFirstWeekPickableForConference(context.Background(), year, conf, late)
		if err != nil {
			t.Fatalf("IsFirstWeekPickableForConference: %v", err)
		}
		if pickable {
			t.Error("pickable = true, want false (every week 1 game has kicked off — isKickedOff uses kickoff <= now)")
		}
	})

	t.Run("well into week 2, still not pickable — the rule looks at week 1 only", func(t *testing.T) {
		pickable, err := svc.IsFirstWeekPickableForConference(context.Background(), year, conf, base.Add(8*24*time.Hour))
		if err != nil {
			t.Fatalf("IsFirstWeekPickableForConference: %v", err)
		}
		if pickable {
			t.Error("pickable = true, want false (week 1 is long over, even though week 2 has its own upcoming games)")
		}
	})

	t.Run("no schedule data yet defaults to pickable", func(t *testing.T) {
		emptyYear := int32(uniqueSeasonYear())
		pickable, err := svc.IsFirstWeekPickableForConference(context.Background(), emptyYear, conf, base)
		if err != nil {
			t.Fatalf("IsFirstWeekPickableForConference: %v", err)
		}
		if !pickable {
			t.Error("pickable = false, want true (no synced schedule data should never block joining)")
		}
	})
}
