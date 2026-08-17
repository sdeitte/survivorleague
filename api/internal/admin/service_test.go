package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

// testDatabaseURL mirrors the pattern used by internal/leagues and
// internal/schedule's integration tests — self-skip (not fail) when no
// database is reachable.
func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
}

func newTestDeps(t *testing.T) (*Service, *gen.Queries, *pgxpool.Pool) {
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

	// A minimal but schema-accurate CFBD fixture server — this package's
	// tests only care about sync_runs/audit_log bookkeeping around
	// SyncSeason, not the sync's own field-by-field correctness (that's
	// internal/schedule's job), so the fixture is intentionally tiny.
	mux := http.NewServeMux()
	mux.HandleFunc("/teams/fbs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id": 1, "school": "Test State", "conference": "Big Ten", "logos": []}]`))
	})
	mux.HandleFunc("/calendar", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"season": 2025, "week": 1, "seasonType": "regular", "startDate": "2025-08-23T00:00:00.000Z", "endDate": "2025-08-30T00:00:00.000Z"}]`))
	})
	mux.HandleFunc("/games", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	cfbdClient := schedule.NewCFBDClient(server.Client(), server.URL, "test-key")
	scheduleService := schedule.NewService(q, cfbdClient)
	return NewService(q, scheduleService), q, pool
}

func createTestUser(t *testing.T, q *gen.Queries) gen.User {
	t.Helper()
	u, err := q.CreateUser(context.Background(), gen.CreateUserParams{
		Email:        "admin-test-" + time.Now().Format("150405.000000000") + "@example.test",
		PasswordHash: "test-hash-not-a-real-argon2id-value",
		DisplayName:  "Admin Test",
		IsSiteAdmin:  true,
	})
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	return u
}

// uniqueSeasonYear avoids colliding with internal/schedule's own test suite
// (a separate package/process, but sharing the same dev database) or with
// other runs of this file.
var seasonYearCounter = 80000

func uniqueSeasonYear() int {
	seasonYearCounter++
	return seasonYearCounter
}

func countAuditLogRows(t *testing.T, pool *pgxpool.Pool, syncRunID pgtype.UUID) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'schedule_sync' AND target_id = $1`,
		syncRunID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count audit_log rows: %v", err)
	}
	return count
}

func TestService_TriggerScheduleSync_ManualTriggerWritesAuditLog(t *testing.T) {
	svc, q, pool := newTestDeps(t)
	admin := createTestUser(t, q)
	year := uniqueSeasonYear()

	run, err := svc.TriggerScheduleSync(context.Background(), admin.ID, TriggerManual, year)
	if err != nil {
		t.Fatalf("TriggerScheduleSync: %v", err)
	}

	if run.Kind != syncKindSchedule {
		t.Errorf("run.Kind = %q, want %q", run.Kind, syncKindSchedule)
	}
	if run.Status != "success" {
		t.Errorf("run.Status = %q, want %q", run.Status, "success")
	}
	if !run.FinishedAt.Valid {
		t.Error("run.FinishedAt not set after a completed sync")
	}
	if run.Error.Valid {
		t.Errorf("run.Error = %q, want unset on success", run.Error.String)
	}

	var details syncRunDetails
	if err := json.Unmarshal(run.Details, &details); err != nil {
		t.Fatalf("unmarshal run.Details: %v", err)
	}
	if details.Trigger != TriggerManual {
		t.Errorf("details.Trigger = %q, want %q", details.Trigger, TriggerManual)
	}
	if details.TriggeredBy == nil {
		t.Fatal("details.TriggeredBy is nil, want the acting admin's user id")
	}
	if details.Result == nil || details.Result.TeamsUpserted != 1 {
		t.Errorf("details.Result = %+v, want TeamsUpserted=1", details.Result)
	}

	// Privileged action -> audit_log row, per the plan's "every
	// commissioner/admin privileged action writes a row here" rule.
	if got := countAuditLogRows(t, pool, run.ID); got != 1 {
		t.Errorf("audit_log rows for this sync run = %d, want 1", got)
	}
}

func TestService_TriggerScheduleSync_CronTriggerSkipsAuditLog(t *testing.T) {
	svc, _, pool := newTestDeps(t)
	year := uniqueSeasonYear()

	run, err := svc.TriggerScheduleSync(context.Background(), pgtype.UUID{}, TriggerCron, year)
	if err != nil {
		t.Fatalf("TriggerScheduleSync: %v", err)
	}

	var details syncRunDetails
	if err := json.Unmarshal(run.Details, &details); err != nil {
		t.Fatalf("unmarshal run.Details: %v", err)
	}
	if details.Trigger != TriggerCron {
		t.Errorf("details.Trigger = %q, want %q", details.Trigger, TriggerCron)
	}
	if details.TriggeredBy != nil {
		t.Errorf("details.TriggeredBy = %v, want nil for a cron-triggered run", *details.TriggeredBy)
	}

	// No acting user -> no audit_log row (there's no actor_user_id to log).
	if got := countAuditLogRows(t, pool, run.ID); got != 0 {
		t.Errorf("audit_log rows for a cron-triggered sync run = %d, want 0", got)
	}
}

func TestService_ListSyncRuns_NewestFirst(t *testing.T) {
	svc, q, _ := newTestDeps(t)
	admin := createTestUser(t, q)

	first, err := svc.TriggerScheduleSync(context.Background(), admin.ID, TriggerManual, uniqueSeasonYear())
	if err != nil {
		t.Fatalf("first TriggerScheduleSync: %v", err)
	}
	second, err := svc.TriggerScheduleSync(context.Background(), admin.ID, TriggerManual, uniqueSeasonYear())
	if err != nil {
		t.Fatalf("second TriggerScheduleSync: %v", err)
	}

	runs, err := svc.ListSyncRuns(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSyncRuns: %v", err)
	}
	if len(runs) < 2 {
		t.Fatalf("len(runs) = %d, want at least 2", len(runs))
	}

	firstIdx, secondIdx := -1, -1
	for i, r := range runs {
		if r.ID == first.ID {
			firstIdx = i
		}
		if r.ID == second.ID {
			secondIdx = i
		}
	}
	if firstIdx == -1 || secondIdx == -1 {
		t.Fatal("one or both triggered runs not found in ListSyncRuns output")
	}
	if secondIdx > firstIdx {
		t.Errorf("second (later) run at index %d, first (earlier) run at index %d — want newest first", secondIdx, firstIdx)
	}
}
