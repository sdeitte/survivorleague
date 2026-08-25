package leagues

import (
	"context"
	"sync"
	"testing"

	"github.com/sdeitte/survivor-league-api/internal/notify"
)

// fakePushSender/fakeEmailSender mirror internal/notify's own test doubles
// (unexported to that package's test binary, so duplicated here — same
// convention as every other small test helper duplicated across this
// repo's packages, e.g. buyback_test.go's own testWeek/
// eliminateTestMembership doc comment).
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

func clearNotifyTables(t *testing.T, base *Service) {
	t.Helper()
	// Same reasoning as internal/notify and internal/grading's own copies
	// of this helper: notification_outbox is drained by a global query, so
	// leftover rows from an earlier test/run in this persistent Postgres
	// could otherwise pollute this file's per-user row-count assertions.
	if _, err := base.pool.Exec(context.Background(), `DELETE FROM notification_outbox`); err != nil {
		t.Fatalf("clear notification_outbox: %v", err)
	}
	if _, err := base.pool.Exec(context.Background(), `DELETE FROM notifications_log`); err != nil {
		t.Fatalf("clear notifications_log: %v", err)
	}
}

// TestBuyBackMember_NotifiesReinstatedPlayer drives a buy-back through the
// REAL leagues.Service.BuyBackMember endpoint (not a hand-constructed
// outbox row) with a real *notify.Service wired in as the Notifier, and
// confirms the reinstated player — not the commissioner who performed the
// buy-back — gets a `buyback` outbox row on both push and email.
func TestBuyBackMember_NotifiesReinstatedPlayer(t *testing.T) {
	base, q := newTestService(t)
	clearNotifyTables(t, base)

	push := &fakePushSender{}
	email := &fakeEmailSender{}
	notifySvc := notify.NewService(q, base.pool, push, email)
	s := NewService(q, base.pool, WithNotifier(notifySvc))

	commissioner := createTestUser(t, q, "commish")
	player := createTestUser(t, q, "player")
	league, _ := createTestLeague(t, s, commissioner)

	m, err := s.JoinByCode(context.Background(), league.ID, player.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}

	week := testWeek(t, q, 1)
	eliminateTestMembership(t, q, week, m.ID)

	if _, err := s.BuyBackMember(context.Background(), league.ID, m.ID, commissioner.ID); err != nil {
		t.Fatalf("BuyBackMember: %v", err)
	}

	rows, err := q.ListNotificationOutboxForUser(context.Background(), player.ID)
	if err != nil {
		t.Fatalf("ListNotificationOutboxForUser: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("player outbox rows = %d, want 2 (buyback push+email)", len(rows))
	}
	channels := map[string]bool{}
	for _, r := range rows {
		channels[r.Channel] = true
		if r.Type != notify.TypeBuyback {
			t.Errorf("row type = %q, want %q", r.Type, notify.TypeBuyback)
		}
		if r.WeekID.Valid {
			t.Errorf("row week_id = %v, want NULL (buyback isn't week-scoped)", r.WeekID)
		}
	}
	if !channels[notify.ChannelPush] || !channels[notify.ChannelEmail] {
		t.Errorf("channels = %v, want both push and email", channels)
	}

	// The commissioner who performed the buy-back must NOT get their own
	// notification from this event.
	commissionerRows, err := q.ListNotificationOutboxForUser(context.Background(), commissioner.ID)
	if err != nil {
		t.Fatalf("ListNotificationOutboxForUser (commissioner): %v", err)
	}
	if len(commissionerRows) != 0 {
		t.Errorf("commissioner outbox rows = %d, want 0", len(commissionerRows))
	}
}
