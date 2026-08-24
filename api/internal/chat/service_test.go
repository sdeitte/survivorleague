package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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

var chatTestIDCounter = time.Now().UnixNano()

func nextChatTestID() int64 {
	chatTestIDCounter++
	return chatTestIDCounter
}

func createChatTestUser(t *testing.T, q *gen.Queries, label string) gen.User {
	t.Helper()
	u, err := q.CreateUser(context.Background(), gen.CreateUserParams{
		Email:        fmt.Sprintf("%s-%d@example.test", label, nextChatTestID()),
		PasswordHash: "test-hash-not-a-real-argon2id-value",
		DisplayName:  label,
		IsSiteAdmin:  false,
	})
	if err != nil {
		t.Fatalf("createChatTestUser: %v", err)
	}
	return u
}

func TestService_PostMessage_HappyPath(t *testing.T) {
	q, pool := newTestQueries(t)
	leaguesSvc := leagues.NewService(q, pool)
	svc := NewService(q)
	ctx := context.Background()

	commissioner := createChatTestUser(t, q, "commish")
	league, _, err := leaguesSvc.CreateLeague(ctx, commissioner.ID, "Chat Test League", 2026, "Big Ten")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}

	msg, err := svc.PostMessage(ctx, league.ID, commissioner.ID, "  gg everyone, tough week  ")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if msg.Body != "gg everyone, tough week" {
		t.Errorf("Body = %q, want trimmed", msg.Body)
	}

	rows, err := svc.ListRecentMessages(ctx, league.ID)
	if err != nil {
		t.Fatalf("ListRecentMessages: %v", err)
	}
	if len(rows) != 1 || rows[0].Body != msg.Body || rows[0].DisplayName != "commish" {
		t.Fatalf("ListRecentMessages = %+v, want one row from commish with the posted body", rows)
	}
}

func TestService_PostMessage_RejectsEmptyAndOverLongBodies(t *testing.T) {
	q, _ := newTestQueries(t)
	svc := NewService(q)
	ctx := context.Background()
	user := createChatTestUser(t, q, "poster")
	leagueID := user.ID // any non-nil UUID works — validation happens before the DB is touched

	if _, err := svc.PostMessage(ctx, leagueID, user.ID, "   "); !errors.Is(err, ErrEmptyMessageBody) {
		t.Errorf("err = %v, want ErrEmptyMessageBody", err)
	}
	if _, err := svc.PostMessage(ctx, leagueID, user.ID, strings.Repeat("x", maxMessageBodyLength+1)); !errors.Is(err, ErrMessageBodyTooLong) {
		t.Errorf("err = %v, want ErrMessageBodyTooLong", err)
	}
}

func TestService_ListRecentMessages_ExcludesMessagesOlderThanSevenDays(t *testing.T) {
	q, pool := newTestQueries(t)
	leaguesSvc := leagues.NewService(q, pool)
	svc := NewService(q)
	ctx := context.Background()

	commissioner := createChatTestUser(t, q, "commish")
	league, _, err := leaguesSvc.CreateLeague(ctx, commissioner.ID, "Chat TTL Test League", 2026, "SEC")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}

	fresh, err := svc.PostMessage(ctx, league.ID, commissioner.ID, "still within the window")
	if err != nil {
		t.Fatalf("PostMessage (fresh): %v", err)
	}
	stale, err := svc.PostMessage(ctx, league.ID, commissioner.ID, "eight days old")
	if err != nil {
		t.Fatalf("PostMessage (stale): %v", err)
	}
	// Backdate the "stale" message directly — PostMessage always stamps
	// now(), so the only way to test the TTL boundary is to manipulate
	// created_at after the fact, same pattern other packages' tests use
	// for raw state setup (see e.g. internal/notify/service_test.go).
	if _, err := pool.Exec(ctx, `UPDATE league_messages SET created_at = $1 WHERE id = $2`,
		time.Now().Add(-8*24*time.Hour), stale.ID); err != nil {
		t.Fatalf("backdate stale message: %v", err)
	}

	rows, err := svc.ListRecentMessages(ctx, league.ID)
	if err != nil {
		t.Fatalf("ListRecentMessages: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != fresh.ID {
		t.Fatalf("ListRecentMessages = %+v, want exactly the fresh message (8-day-old one excluded)", rows)
	}
}

func TestService_DeleteMessage(t *testing.T) {
	q, pool := newTestQueries(t)
	leaguesSvc := leagues.NewService(q, pool)
	svc := NewService(q)
	ctx := context.Background()

	commissioner := createChatTestUser(t, q, "commish")
	leagueA, _, err := leaguesSvc.CreateLeague(ctx, commissioner.ID, "Chat Delete League A", 2026, "Big Ten")
	if err != nil {
		t.Fatalf("CreateLeague A: %v", err)
	}
	leagueB, _, err := leaguesSvc.CreateLeague(ctx, commissioner.ID, "Chat Delete League B", 2026, "SEC")
	if err != nil {
		t.Fatalf("CreateLeague B: %v", err)
	}

	msg, err := svc.PostMessage(ctx, leagueA.ID, commissioner.ID, "delete me")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Deleting via the WRONG league must not succeed — a message can only
	// be deleted by its own league's commissioner.
	if err := svc.DeleteMessage(ctx, leagueB.ID, msg.ID); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("DeleteMessage (wrong league) err = %v, want ErrMessageNotFound", err)
	}
	rows, err := svc.ListRecentMessages(ctx, leagueA.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("message should still exist after the wrong-league delete attempt: rows=%+v err=%v", rows, err)
	}

	if err := svc.DeleteMessage(ctx, leagueA.ID, msg.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	rows, err = svc.ListRecentMessages(ctx, leagueA.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("message should be gone after delete: rows=%+v err=%v", rows, err)
	}

	if err := svc.DeleteMessage(ctx, leagueA.ID, msg.ID); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("DeleteMessage (already deleted) err = %v, want ErrMessageNotFound", err)
	}
}
