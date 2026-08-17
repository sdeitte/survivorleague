package grading

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/notify"
)

// fakePushSender/fakeEmailSender are local, minimal test doubles — mirrors
// internal/notify's own fakePushSender/fakeEmailSender (unexported to that
// package's test binary, so duplicated here rather than shared, same as
// every other small test helper duplicated across this repo's packages).
// This file's whole point is to prove Phase 7's five trigger types
// actually fire from the REAL pipeline (TryFinalizeLeagueWeek), not a
// hand-constructed outbox row — so it drives grading.Service exactly like
// internal/livepoll does, with a real *notify.Service (backed by these
// fakes instead of live Expo/Resend calls) wired in as the Notifier.
type fakePushSender struct {
	mu   sync.Mutex
	sent []notify.PushMessage
}

func (f *fakePushSender) Send(_ context.Context, msg notify.PushMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

type fakeEmailSender struct {
	mu   sync.Mutex
	sent []notify.EmailMessage
}

func (f *fakeEmailSender) Send(_ context.Context, msg notify.EmailMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

// clearNotifyTables gives each test in this file a clean slate: these two
// tables are drained/read by a caller-agnostic global query
// (notify.Service.DispatchBatch's ClaimPendingNotifications), so — same
// reasoning as internal/notify's own test helper — a prior test run's
// leftover rows in this persistent, un-truncated local Postgres could
// otherwise pollute this file's per-user row-count assertions.
func clearNotifyTables(t *testing.T, env testEnv) {
	t.Helper()
	ctx := context.Background()
	if _, err := env.pool.Exec(ctx, `DELETE FROM notification_outbox`); err != nil {
		t.Fatalf("clear notification_outbox: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `DELETE FROM notifications_log`); err != nil {
		t.Fatalf("clear notifications_log: %v", err)
	}
}

func outboxRowsForUser(t *testing.T, env testEnv, userID pgtype.UUID) []gen.NotificationOutbox {
	t.Helper()
	rows, err := env.q.ListNotificationOutboxForUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListNotificationOutboxForUser: %v", err)
	}
	return rows
}

func channelSet(rows []gen.NotificationOutbox) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[r.Channel] = true
	}
	return out
}

// TestTryFinalizeLeagueWeek_NotifiesEliminatedAndSurvived drives a normal
// (non-mass-wipeout) finalization through the REAL grading pipeline with a
// real *notify.Service wired in, and confirms: the loser gets an
// `eliminated` outbox row on both push and email, the winner gets a
// `survived` outbox row on push only (per the plan: survived is
// push-only), and neither gets a `mass_wipeout` row.
func TestTryFinalizeLeagueWeek_NotifiesEliminatedAndSurvived(t *testing.T) {
	env := newTestEnv(t)
	clearNotifyTables(t, env)
	push := &fakePushSender{}
	email := &fakeEmailSender{}
	notifySvc := notify.NewService(env.q, env.pool, push, email)
	gradingSvc := NewService(env.q, env.pool, WithNotifier(notifySvc))

	league, winner := createLeague(t, env, "Notify Normal")
	loser := addPlayer(t, env, league, "loser")

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))

	pick(t, env, winner.ID, week.ID, league.Conference, game.ID, teamA.ID)
	pick(t, env, loser.ID, week.ID, league.Conference, game.ID, teamB.ID)
	finalizeGame(t, env.pool, game.ID, teamA.ID, 28, 7)

	if _, err := gradingSvc.GradeGame(context.Background(), game.ID); err != nil {
		t.Fatalf("GradeGame: %v", err)
	}
	result, err := gradingSvc.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("TryFinalizeLeagueWeek: %v", err)
	}
	if result == nil || result.MassWipeout {
		t.Fatalf("result = %+v, want a non-mass-wipeout finalization", result)
	}

	loserRows := outboxRowsForUser(t, env, loser.UserID)
	if len(loserRows) != 2 {
		t.Fatalf("loser outbox rows = %d, want 2 (eliminated push+email)", len(loserRows))
	}
	for _, r := range loserRows {
		if r.Type != notify.TypeEliminated {
			t.Errorf("loser row type = %q, want %q", r.Type, notify.TypeEliminated)
		}
	}
	if ch := channelSet(loserRows); !ch[notify.ChannelPush] || !ch[notify.ChannelEmail] {
		t.Errorf("loser channels = %v, want both push and email", ch)
	}

	winnerRows := outboxRowsForUser(t, env, winner.UserID)
	if len(winnerRows) != 1 {
		t.Fatalf("winner outbox rows = %d, want 1 (survived, push-only)", len(winnerRows))
	}
	if winnerRows[0].Type != notify.TypeSurvived {
		t.Errorf("winner row type = %q, want %q", winnerRows[0].Type, notify.TypeSurvived)
	}
	if winnerRows[0].Channel != notify.ChannelPush {
		t.Errorf("winner row channel = %q, want push", winnerRows[0].Channel)
	}

	// Neither should have a mass_wipeout row — this was a normal
	// elimination, not a wipeout.
	for _, rows := range [][]gen.NotificationOutbox{loserRows, winnerRows} {
		for _, r := range rows {
			if r.Type == notify.TypeMassWipeout {
				t.Errorf("unexpected mass_wipeout row: %+v", r)
			}
		}
	}
}

// TestTryFinalizeLeagueWeek_NotifiesMassWipeoutForEveryActiveMember_NotEliminated
// forces the mass-wipeout scenario (every active contestant loses/misses)
// through the real pipeline and confirms every active contestant — not
// just some — gets a `mass_wipeout` row on both channels, and crucially
// that NONE of them get an `eliminated` row (mass-wipeout eliminates
// nobody, per the plan's confirmed product rule).
func TestTryFinalizeLeagueWeek_NotifiesMassWipeoutForEveryActiveMember_NotEliminated(t *testing.T) {
	env := newTestEnv(t)
	clearNotifyTables(t, env)
	push := &fakePushSender{}
	email := &fakeEmailSender{}
	notifySvc := notify.NewService(env.q, env.pool, push, email)
	gradingSvc := NewService(env.q, env.pool, WithNotifier(notifySvc))

	league, commissioner := createLeague(t, env, "Notify MassWipeout")
	player2 := addPlayer(t, env, league, "player2")
	player3 := addPlayer(t, env, league, "player3") // misses the pick entirely

	seasonYear := uniqueSeasonYear()
	week := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten") // wins
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten") // loses
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(48*time.Hour))

	pick(t, env, commissioner.ID, week.ID, league.Conference, game.ID, teamB.ID)
	pick(t, env, player2.ID, week.ID, league.Conference, game.ID, teamB.ID)
	// player3 misses the pick.
	finalizeGame(t, env.pool, game.ID, teamA.ID, 35, 3)

	if _, err := gradingSvc.GradeGame(context.Background(), game.ID); err != nil {
		t.Fatalf("GradeGame: %v", err)
	}
	result, err := gradingSvc.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("TryFinalizeLeagueWeek: %v", err)
	}
	if result == nil || !result.MassWipeout {
		t.Fatalf("result = %+v, want a mass-wipeout finalization", result)
	}

	for _, m := range []struct {
		label string
		id    pgtype.UUID
	}{
		{"commissioner", commissioner.UserID},
		{"player2", player2.UserID},
		{"player3 (missed)", player3.UserID},
	} {
		rows := outboxRowsForUser(t, env, m.id)
		if len(rows) != 2 {
			t.Fatalf("%s outbox rows = %d, want 2 (mass_wipeout push+email)", m.label, len(rows))
		}
		for _, r := range rows {
			if r.Type != notify.TypeMassWipeout {
				t.Errorf("%s row type = %q, want %q (mass-wipeout eliminates nobody)", m.label, r.Type, notify.TypeMassWipeout)
			}
		}
		if ch := channelSet(rows); !ch[notify.ChannelPush] || !ch[notify.ChannelEmail] {
			t.Errorf("%s channels = %v, want both push and email", m.label, ch)
		}
	}
}
