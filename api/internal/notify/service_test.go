package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
	"github.com/sdeitte/survivor-league-api/internal/picks"
)

// testDatabaseURL mirrors every other package's own copy of this helper
// (internal/picks, internal/leagues, internal/schedule, internal/grading)
// — these integration tests self-skip (not fail) when no database is
// reachable.
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
	notify  *Service
	leagues *leagues.Service
	picks   *picks.Service
	q       *gen.Queries
	pool    *pgxpool.Pool
	push    *fakePushSender
	email   *fakeEmailSender
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

	// notification_outbox is drained by a global (not per-test-scoped)
	// query — ClaimPendingNotifications has no per-caller filter, by
	// design, since that's how the real dispatcher works. Against this
	// persistent, un-truncated local Postgres (no per-test rollback — see
	// every other package's own copy of this comment), a prior test in
	// this same run (or a prior `go test` invocation) can leave 'pending'
	// rows sitting in the table, which a later test's DispatchBatch call
	// would then also pick up, breaking that test's exact-count
	// assertions. notification_outbox/notifications_log are exclusively
	// owned by this package (nothing else in the schema writes to them),
	// so clearing them at the start of every test is safe and gives each
	// test a clean slate without needing per-test scoping in the queries
	// themselves.
	if _, err := pool.Exec(ctx, `DELETE FROM notification_outbox`); err != nil {
		pool.Close()
		t.Fatalf("clear notification_outbox: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM notifications_log`); err != nil {
		pool.Close()
		t.Fatalf("clear notifications_log: %v", err)
	}

	q := gen.New(pool)
	push := &fakePushSender{}
	email := &fakeEmailSender{}
	return testEnv{
		notify:  NewService(q, pool, push, email),
		leagues: leagues.NewService(q, pool),
		picks:   picks.NewService(q, pool),
		q:       q,
		pool:    pool,
		push:    push,
		email:   email,
	}
}

var idCounter = time.Now().UnixNano()

func nextID() int64 {
	idCounter++
	return idCounter
}

var seasonYearCounter int32 = 50000 + int32(time.Now().UnixNano()%40000)

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

// createLeague creates a Big Ten league with a fresh commissioner, who is
// auto-added as an active contestant member — mirrors internal/grading and
// internal/picks' own fixture pattern.
func createLeague(t *testing.T, env testEnv, name string) (gen.League, gen.LeagueMembership) {
	t.Helper()
	commissioner := createTestUser(t, env.q, "commish")
	league, member, err := env.leagues.CreateLeague(context.Background(), commissioner.ID, name, int32(uniqueSeasonYear()), "Big Ten", "Test Team")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}
	return league, member
}

func addPlayer(t *testing.T, env testEnv, league gen.League, label string) gen.LeagueMembership {
	t.Helper()
	user := createTestUser(t, env.q, label)
	member, err := env.leagues.JoinByCode(context.Background(), league.ID, user.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}
	return member
}

func registerDeviceToken(t *testing.T, env testEnv, userID pgtype.UUID) {
	t.Helper()
	if _, err := env.notify.RegisterDeviceToken(context.Background(), userID, fmt.Sprintf("ExponentPushToken[%d]", nextID()), "ios"); err != nil {
		t.Fatalf("RegisterDeviceToken: %v", err)
	}
}

func payloadOf(t *testing.T, row gen.NotificationOutbox) notificationPayload {
	t.Helper()
	var p notificationPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return p
}

func outboxRowsForUser(t *testing.T, env testEnv, userID pgtype.UUID) []gen.NotificationOutbox {
	t.Helper()
	rows, err := env.q.ListNotificationOutboxForUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListNotificationOutboxForUser: %v", err)
	}
	return rows
}

// --- Enqueueing ---

func TestEnqueueEliminated_InsertsPushAndEmailRows(t *testing.T) {
	env := newTestEnv(t)
	league, loser := createLeague(t, env, "Enqueue Eliminated")
	week := createTestWeek(t, env.q, uniqueSeasonYear(), 1)

	if err := env.notify.EnqueueEliminated(context.Background(), loser.ID, league.ID, week.ID); err != nil {
		t.Fatalf("EnqueueEliminated: %v", err)
	}

	rows := outboxRowsForUser(t, env, loser.UserID)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (push+email)", len(rows))
	}
	channels := map[string]bool{}
	for _, r := range rows {
		channels[r.Channel] = true
		if r.Type != TypeEliminated {
			t.Errorf("row.Type = %q, want %q", r.Type, TypeEliminated)
		}
		if payloadOf(t, r).Title == "" {
			t.Error("payload.Title is empty")
		}
	}
	if !channels[ChannelPush] || !channels[ChannelEmail] {
		t.Errorf("channels = %v, want both push and email", channels)
	}
}

func TestEnqueueSurvived_PushOnly(t *testing.T) {
	env := newTestEnv(t)
	league, winner := createLeague(t, env, "Enqueue Survived")
	week := createTestWeek(t, env.q, uniqueSeasonYear(), 1)

	if err := env.notify.EnqueueSurvived(context.Background(), winner.ID, league.ID, week.ID); err != nil {
		t.Fatalf("EnqueueSurvived: %v", err)
	}

	rows := outboxRowsForUser(t, env, winner.UserID)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (push only)", len(rows))
	}
	if rows[0].Channel != ChannelPush {
		t.Errorf("channel = %q, want push", rows[0].Channel)
	}
	if rows[0].Type != TypeSurvived {
		t.Errorf("type = %q, want %q", rows[0].Type, TypeSurvived)
	}
}

func TestEnqueueMassWipeout_InsertsPushAndEmailRows(t *testing.T) {
	env := newTestEnv(t)
	league, member := createLeague(t, env, "Enqueue MassWipeout")
	week := createTestWeek(t, env.q, uniqueSeasonYear(), 1)

	if err := env.notify.EnqueueMassWipeout(context.Background(), member.ID, league.ID, week.ID); err != nil {
		t.Fatalf("EnqueueMassWipeout: %v", err)
	}

	rows := outboxRowsForUser(t, env, member.UserID)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (push+email)", len(rows))
	}
	for _, r := range rows {
		if r.Type != TypeMassWipeout {
			t.Errorf("type = %q, want %q", r.Type, TypeMassWipeout)
		}
	}
}

func TestEnqueueBuyback_InsertsPushAndEmailRowsWithoutWeek(t *testing.T) {
	env := newTestEnv(t)
	league, member := createLeague(t, env, "Enqueue Buyback")

	if err := env.notify.EnqueueBuyback(context.Background(), member.ID, league.ID); err != nil {
		t.Fatalf("EnqueueBuyback: %v", err)
	}

	rows := outboxRowsForUser(t, env, member.UserID)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (push+email)", len(rows))
	}
	for _, r := range rows {
		if r.Type != TypeBuyback {
			t.Errorf("type = %q, want %q", r.Type, TypeBuyback)
		}
		if r.WeekID.Valid {
			t.Errorf("week_id = %v, want NULL (buyback isn't week-scoped)", r.WeekID)
		}
	}
}

// TestEnqueueEliminated_DedupeKeyPreventsDoubleEnqueue drives the same
// enqueue call twice (simulating a self-heal re-grade or a re-fired
// caller) and confirms it's still exactly 2 rows, not 4 — the
// ON CONFLICT (dedupe_key) DO NOTHING guarantee every trigger site relies
// on.
func TestEnqueueEliminated_DedupeKeyPreventsDoubleEnqueue(t *testing.T) {
	env := newTestEnv(t)
	league, loser := createLeague(t, env, "Enqueue Dedupe")
	week := createTestWeek(t, env.q, uniqueSeasonYear(), 1)

	for i := 0; i < 2; i++ {
		if err := env.notify.EnqueueEliminated(context.Background(), loser.ID, league.ID, week.ID); err != nil {
			t.Fatalf("EnqueueEliminated (call %d): %v", i, err)
		}
	}

	rows := outboxRowsForUser(t, env, loser.UserID)
	if len(rows) != 2 {
		t.Fatalf("len(rows) after 2 calls = %d, want 2 (dedupe key prevents a second insert)", len(rows))
	}
}

// --- Dispatch ---

func TestDispatchBatch_SendsPendingRow_MarksSentAndLogs(t *testing.T) {
	env := newTestEnv(t)
	league, member := createLeague(t, env, "Dispatch Sent")
	week := createTestWeek(t, env.q, uniqueSeasonYear(), 1)
	registerDeviceToken(t, env, member.UserID)

	if err := env.notify.EnqueueEliminated(context.Background(), member.ID, league.ID, week.ID); err != nil {
		t.Fatalf("EnqueueEliminated: %v", err)
	}

	n, err := env.notify.DispatchBatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("DispatchBatch: %v", err)
	}
	if n != 2 {
		t.Fatalf("DispatchBatch processed = %d, want 2", n)
	}

	if got := env.push.sentCount(); got != 1 {
		t.Errorf("push sends = %d, want 1", got)
	}
	if got := env.email.sentCount(); got != 1 {
		t.Errorf("email sends = %d, want 1", got)
	}

	rows := outboxRowsForUser(t, env, member.UserID)
	for _, r := range rows {
		if r.Status != "sent" {
			t.Errorf("row (channel=%s) status = %q, want sent", r.Channel, r.Status)
		}
		if !r.SentAt.Valid {
			t.Errorf("row (channel=%s) sent_at is not set", r.Channel)
		}
	}

	var logStatus string
	err = env.pool.QueryRow(context.Background(),
		`SELECT status FROM notifications_log WHERE dedupe_key = $1`, rows[0].DedupeKey).Scan(&logStatus)
	if err != nil {
		t.Fatalf("query notifications_log: %v", err)
	}
	if logStatus != "sent" {
		t.Errorf("notifications_log status = %q, want sent", logStatus)
	}
}

// TestDispatchBatch_OptedOut_SkipsAndDoesNotRetry confirms an opted-out
// preference terminally skips the row (no send attempt) rather than
// retrying it forever.
func TestDispatchBatch_OptedOut_SkipsAndDoesNotRetry(t *testing.T) {
	env := newTestEnv(t)
	league, member := createLeague(t, env, "Dispatch OptOut")
	week := createTestWeek(t, env.q, uniqueSeasonYear(), 1)
	registerDeviceToken(t, env, member.UserID)

	// Opt out of `survived` specifically (push channel stays enabled) —
	// mirrors the E2E scenario's "opt out of survived" step.
	if _, err := env.notify.UpdatePreferences(context.Background(), member.UserID, gen.UpsertNotificationPreferencesParams{
		PickReminder: true, Eliminated: true, Survived: false, MassWipeout: true, Buyback: true,
		EmailEnabled: true, PushEnabled: true,
	}); err != nil {
		t.Fatalf("set preferences: %v", err)
	}

	if err := env.notify.EnqueueSurvived(context.Background(), member.ID, league.ID, week.ID); err != nil {
		t.Fatalf("EnqueueSurvived: %v", err)
	}

	if _, err := env.notify.DispatchBatch(context.Background(), 10); err != nil {
		t.Fatalf("DispatchBatch: %v", err)
	}

	if got := env.push.sentCount(); got != 0 {
		t.Errorf("push sends = %d, want 0 (opted out)", got)
	}

	rows := outboxRowsForUser(t, env, member.UserID)
	if len(rows) != 1 || rows[0].Status != "skipped" {
		t.Fatalf("rows = %+v, want exactly 1 row with status=skipped", rows)
	}

	// A second dispatch tick must not pick this row up again — it's no
	// longer 'pending'.
	if _, err := env.notify.DispatchBatch(context.Background(), 10); err != nil {
		t.Fatalf("DispatchBatch (2nd tick): %v", err)
	}
	if got := env.push.sentCount(); got != 0 {
		t.Errorf("push sends after 2nd tick = %d, want still 0", got)
	}
}

// TestDispatchBatch_FailingSend_RetriesThenPermanentlyFails drives a
// send that always fails through DefaultMaxAttempts dispatch ticks and
// confirms: attempts increments each time, the row stays 'pending' (and
// thus gets reclaimed) until the cap, and only then flips to 'failed' with
// a matching notifications_log row.
func TestDispatchBatch_FailingSend_RetriesThenPermanentlyFails(t *testing.T) {
	env := newTestEnv(t)
	league, member := createLeague(t, env, "Dispatch Failing")
	registerDeviceToken(t, env, member.UserID)

	env.push.failCount = 1000 // always fail

	if err := env.notify.EnqueueBuyback(context.Background(), member.ID, league.ID); err != nil {
		t.Fatalf("EnqueueBuyback: %v", err)
	}

	// Isolate the push row so batch accounting below is unambiguous.
	rowsBefore := outboxRowsForUser(t, env, member.UserID)
	var pushRowID pgtype.UUID
	for _, r := range rowsBefore {
		if r.Channel == ChannelPush {
			pushRowID = r.ID
		}
	}
	if !pushRowID.Valid {
		t.Fatal("no push row found among enqueued rows")
	}

	for attempt := int32(1); attempt <= DefaultMaxAttempts; attempt++ {
		if _, err := env.notify.DispatchBatch(context.Background(), 10); err != nil {
			t.Fatalf("DispatchBatch (attempt %d): %v", attempt, err)
		}
		row := getOutboxRow(t, env, pushRowID)
		if row.Attempts != attempt {
			t.Errorf("after dispatch tick %d: attempts = %d, want %d", attempt, row.Attempts, attempt)
		}
		if attempt < DefaultMaxAttempts {
			if row.Status != "pending" {
				t.Errorf("after dispatch tick %d: status = %q, want pending (not yet at cap)", attempt, row.Status)
			}
		} else {
			if row.Status != "failed" {
				t.Errorf("after dispatch tick %d (cap reached): status = %q, want failed", attempt, row.Status)
			}
		}
	}

	var logStatus string
	row := getOutboxRow(t, env, pushRowID)
	err := env.pool.QueryRow(context.Background(),
		`SELECT status FROM notifications_log WHERE dedupe_key = $1`, row.DedupeKey).Scan(&logStatus)
	if err != nil {
		t.Fatalf("query notifications_log: %v", err)
	}
	if logStatus != "failed" {
		t.Errorf("notifications_log status = %q, want failed", logStatus)
	}

	// One more tick must be a no-op for this row (no longer pending) —
	// attempts must not keep climbing past the cap.
	if _, err := env.notify.DispatchBatch(context.Background(), 10); err != nil {
		t.Fatalf("DispatchBatch (post-cap tick): %v", err)
	}
	row = getOutboxRow(t, env, pushRowID)
	if row.Attempts != DefaultMaxAttempts {
		t.Errorf("attempts after post-cap tick = %d, want unchanged at %d", row.Attempts, DefaultMaxAttempts)
	}
}

func getOutboxRow(t *testing.T, env testEnv, id pgtype.UUID) gen.NotificationOutbox {
	t.Helper()
	var row gen.NotificationOutbox
	err := env.pool.QueryRow(context.Background(),
		`SELECT id, user_id, league_id, week_id, type, channel, payload, dedupe_key, status, attempts, created_at, sent_at
		 FROM notification_outbox WHERE id = $1`, id).
		Scan(&row.ID, &row.UserID, &row.LeagueID, &row.WeekID, &row.Type, &row.Channel, &row.Payload, &row.DedupeKey, &row.Status, &row.Attempts, &row.CreatedAt, &row.SentAt)
	if err != nil {
		t.Fatalf("getOutboxRow: %v", err)
	}
	return row
}

// TestDispatchBatch_ConcurrentDispatch_ExactlyOnce is the concurrency
// proof the plan explicitly calls for: many pending rows, several
// DispatchBatch calls racing against the same live Postgres concurrently,
// and a fakePushSender that records every send — every row must be sent
// exactly once (FOR UPDATE SKIP LOCKED's job), never zero, never twice.
func TestDispatchBatch_ConcurrentDispatch_ExactlyOnce(t *testing.T) {
	env := newTestEnv(t)
	league, _ := createLeague(t, env, "Dispatch Concurrency")

	const numRows = 24
	for i := 0; i < numRows; i++ {
		member := addPlayer(t, env, league, fmt.Sprintf("racer%d", i))
		registerDeviceToken(t, env, member.UserID)
		if err := env.notify.EnqueueBuyback(context.Background(), member.ID, league.ID); err != nil {
			t.Fatalf("EnqueueBuyback (%d): %v", i, err)
		}
	}
	// numRows push rows + numRows email rows pending.

	const numDispatchers = 6
	errCh := make(chan error, numDispatchers)
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		var wg sync.WaitGroup
		wg.Add(numDispatchers)
		for i := 0; i < numDispatchers; i++ {
			go func() {
				defer wg.Done()
				// Each goroutine ticks repeatedly until nothing pending is
				// left claimable by it — mirrors several real dispatcher
				// processes/ticks racing each other.
				for tries := 0; tries < 20; tries++ {
					n, err := env.notify.DispatchBatch(context.Background(), 5)
					if err != nil {
						errCh <- err
						return
					}
					if n == 0 {
						return
					}
				}
			}()
		}
		wg.Wait()
	}()
	<-doneCh
	close(errCh)
	for err := range errCh {
		t.Fatalf("DispatchBatch (concurrent): %v", err)
	}

	pushMessages := env.push.messages()
	emailMessages := env.email.messages()
	if len(pushMessages) != numRows {
		t.Errorf("push sends = %d, want exactly %d (no duplicates, none dropped)", len(pushMessages), numRows)
	}
	if len(emailMessages) != numRows {
		t.Errorf("email sends = %d, want exactly %d (no duplicates, none dropped)", len(emailMessages), numRows)
	}

	// Every outbox row must have converged to 'sent'.
	var pendingCount int64
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM notification_outbox WHERE status <> 'sent'`).Scan(&pendingCount); err != nil {
		t.Fatalf("count non-sent rows: %v", err)
	}
	if pendingCount != 0 {
		t.Errorf("non-sent outbox rows remaining = %d, want 0", pendingCount)
	}
}

// --- ScanPickReminders ---

// TestScanPickReminders_EnqueuesBothWindowsForUnpickedMember drives a
// realistic scenario through the real reminder-scan query path (not a
// hand-constructed outbox row): a game kicking off in 2 hours (inside both
// the 24h and 3h windows) with one member who has already picked (no
// reminder) and one who hasn't (both windows' push+email rows).
func TestScanPickReminders_EnqueuesBothWindowsForUnpickedMember(t *testing.T) {
	env := newTestEnv(t)
	league, picked := createLeague(t, env, "Reminder Scan")
	unpicked := addPlayer(t, env, league, "unpicked")

	// Must share the league's own season_year (createLeague picks a fresh
	// one internally) — GetNearestUnpickedGameForMembership joins weeks on
	// season_year, so a week fixture under a different season_year would
	// simply never match.
	week := createTestWeek(t, env.q, league.SeasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	game := createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(2*time.Hour))

	if _, err := env.picks.UpsertPick(context.Background(), picked.ID, week.ID, league.Conference, game.ID, teamA.ID); err != nil {
		t.Fatalf("UpsertPick: %v", err)
	}

	if err := env.notify.ScanPickReminders(context.Background()); err != nil {
		t.Fatalf("ScanPickReminders: %v", err)
	}

	if rows := outboxRowsForUser(t, env, picked.UserID); len(rows) != 0 {
		t.Errorf("picked member outbox rows = %d, want 0 (already picked, no reminder)", len(rows))
	}

	rows := outboxRowsForUser(t, env, unpicked.UserID)
	if len(rows) != 4 {
		t.Fatalf("unpicked member outbox rows = %d, want 4 (24h push+email, 3h push+email)", len(rows))
	}
	seen := map[string]int{}
	for _, r := range rows {
		if r.Type != TypePickReminder {
			t.Errorf("row.Type = %q, want %q", r.Type, TypePickReminder)
		}
		seen[r.Channel]++
	}
	if seen[ChannelPush] != 2 || seen[ChannelEmail] != 2 {
		t.Errorf("channel counts = %v, want 2 push + 2 email (one per window)", seen)
	}

	// A second scan tick (the hourly cron firing again while the
	// condition still holds) must not double-enqueue — the dedupe key,
	// not scan-side state, is what prevents that.
	if err := env.notify.ScanPickReminders(context.Background()); err != nil {
		t.Fatalf("ScanPickReminders (2nd tick): %v", err)
	}
	if rows := outboxRowsForUser(t, env, unpicked.UserID); len(rows) != 4 {
		t.Errorf("unpicked member outbox rows after 2nd scan = %d, want still 4", len(rows))
	}
}

// TestScanPickReminders_OutsideWindow_NoReminder confirms a deadline more
// than 24h out enqueues nothing yet.
func TestScanPickReminders_OutsideWindow_NoReminder(t *testing.T) {
	env := newTestEnv(t)
	league, _ := createLeague(t, env, "Reminder Scan Far")
	unpicked := addPlayer(t, env, league, "unpicked")

	week := createTestWeek(t, env.q, league.SeasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(72*time.Hour))

	if err := env.notify.ScanPickReminders(context.Background()); err != nil {
		t.Fatalf("ScanPickReminders: %v", err)
	}

	if rows := outboxRowsForUser(t, env, unpicked.UserID); len(rows) != 0 {
		t.Errorf("outbox rows = %d, want 0 (deadline is 72h out, outside both windows)", len(rows))
	}
}

// TestScanPickReminders_24hOnly_NotYetInside3h confirms a deadline inside
// the 24h window but not yet inside the 3h window gets only the 24h
// reminder.
func TestScanPickReminders_24hOnly_NotYetInside3h(t *testing.T) {
	env := newTestEnv(t)
	league, _ := createLeague(t, env, "Reminder Scan 24h")
	unpicked := addPlayer(t, env, league, "unpicked")

	week := createTestWeek(t, env.q, league.SeasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten")
	createTestGame(t, env.q, week, teamA, teamB, time.Now().Add(10*time.Hour))

	if err := env.notify.ScanPickReminders(context.Background()); err != nil {
		t.Fatalf("ScanPickReminders: %v", err)
	}

	rows := outboxRowsForUser(t, env, unpicked.UserID)
	if len(rows) != 2 {
		t.Fatalf("outbox rows = %d, want 2 (24h push+email only)", len(rows))
	}
	for _, r := range rows {
		var p notificationPayload
		if err := json.Unmarshal(r.Payload, &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if p.Body == "" {
			t.Error("payload.Body is empty")
		}
	}
}
