package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/auth"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/grading"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
	"github.com/sdeitte/survivor-league-api/internal/picks"
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
	gradingService := grading.NewService(q, pool)
	return NewService(q, scheduleService, gradingService), q, pool
}

// --- Fixtures/helpers shared with internal/grading's own test suite
// pattern (each package's tests keep their own small copy rather than
// sharing test-only code across package boundaries) ---

// idCounter hands out ever-increasing suffixes for fixture uniqueness.
var idCounter = time.Now().UnixNano()

func nextID() int64 {
	idCounter++
	return idCounter
}

func createTestTeamWithExternalID(t *testing.T, q *gen.Queries, externalID int64, name, conference string) gen.Team {
	t.Helper()
	team, err := q.UpsertTeam(context.Background(), gen.UpsertTeamParams{
		ExternalID: fmt.Sprintf("%d", externalID),
		Name:       name,
		Conference: conference,
	})
	if err != nil {
		t.Fatalf("createTestTeamWithExternalID: %v", err)
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

func createTestGameWithExternalID(t *testing.T, q *gen.Queries, externalID int64, week gen.Week, home, away gen.Team, kickoffAt time.Time) gen.Game {
	t.Helper()
	game, err := q.UpsertGame(context.Background(), gen.UpsertGameParams{
		ExternalID: fmt.Sprintf("%d", externalID),
		WeekID:     week.ID,
		HomeTeamID: home.ID,
		AwayTeamID: away.ID,
		KickoffAt:  pgtype.Timestamptz{Time: kickoffAt, Valid: true},
		Status:     "scheduled",
	})
	if err != nil {
		t.Fatalf("createTestGameWithExternalID: %v", err)
	}
	return game
}

// finalizeGame directly rewrites a game's status/scores/winner to simulate
// CFBD reporting it final — mirrors internal/grading/service_test.go's
// identical helper.
func finalizeGame(t *testing.T, pool *pgxpool.Pool, gameID, winnerTeamID pgtype.UUID, homeScore, awayScore int32) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE games SET status = 'final', winner_team_id = $1, home_score = $2, away_score = $3, updated_at = now() WHERE id = $4`,
		winnerTeamID, homeScore, awayScore, gameID)
	if err != nil {
		t.Fatalf("finalizeGame: %v", err)
	}
}

// setGameStatus directly rewrites a game's status — used to simulate a
// postponed game the way internal/grading/service_test.go's own copy does.
func setGameStatus(t *testing.T, pool *pgxpool.Pool, gameID pgtype.UUID, status string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE games SET status = $1, updated_at = now() WHERE id = $2`, status, gameID)
	if err != nil {
		t.Fatalf("setGameStatus: %v", err)
	}
}

// createTestLeague creates a Big Ten league with a fresh commissioner, who
// is auto-added as an active contestant member.
func createTestLeague(t *testing.T, leaguesSvc *leagues.Service, q *gen.Queries, name string) (gen.League, gen.LeagueMembership) {
	t.Helper()
	commissioner := createTestUser(t, q)
	league, member, err := leaguesSvc.CreateLeague(context.Background(), commissioner.ID, name, int32(uniqueSeasonYear()), "Big Ten")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}
	return league, member
}

func addTestPlayer(t *testing.T, leaguesSvc *leagues.Service, q *gen.Queries, league gen.League, label string) gen.LeagueMembership {
	t.Helper()
	user := createTestUser(t, q)
	_ = label
	member, err := leaguesSvc.JoinByCode(context.Background(), league.ID, user.ID)
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}
	return member
}

func submitPick(t *testing.T, picksSvc *picks.Service, membershipID, weekID pgtype.UUID, conference string, gameID, teamID pgtype.UUID) {
	t.Helper()
	if _, err := picksSvc.UpsertPick(context.Background(), membershipID, weekID, conference, gameID, teamID); err != nil {
		t.Fatalf("UpsertPick: %v", err)
	}
}

// resyncTestEnv wires a Service against a mock CFBD server whose /games
// response is controllable per-test via setGamesJSON — needed for
// TestService_ResyncGame_UnblocksPostponedGameFinalization, which must
// drive a real postponed-game-blocks-then-resync-unblocks scenario through
// the actual ResyncGame -> RefreshGame -> GetGamesForWeek path, not a
// direct DB write.
type resyncTestEnv struct {
	admin        *Service
	leagues      *leagues.Service
	picks        *picks.Service
	auth         *auth.Service
	q            *gen.Queries
	pool         *pgxpool.Pool
	setGamesJSON func(string)
}

func newResyncTestEnv(t *testing.T) resyncTestEnv {
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

	var mu sync.Mutex
	gamesJSON := "[]"
	setGamesJSON := func(body string) {
		mu.Lock()
		defer mu.Unlock()
		gamesJSON = body
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/games", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		body := gamesJSON
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	cfbdClient := schedule.NewCFBDClient(server.Client(), server.URL, "test-key")
	scheduleService := schedule.NewService(q, cfbdClient)
	gradingService := grading.NewService(q, pool)
	adminService := NewService(q, scheduleService, gradingService)
	leaguesService := leagues.NewService(q, pool)
	picksService := picks.NewService(q, pool)
	jwtIssuer := auth.NewJWTIssuer("test-secret")
	authService := auth.NewService(q, jwtIssuer, "")

	return resyncTestEnv{
		admin:        adminService,
		leagues:      leaguesService,
		picks:        picksService,
		auth:         authService,
		q:            q,
		pool:         pool,
		setGamesJSON: setGamesJSON,
	}
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

// --- ListLeagues / ListUsers pagination ---

func TestService_ListLeagues_Pagination(t *testing.T) {
	env := newResyncTestEnv(t)

	var created []pgtype.UUID
	for i := 0; i < 3; i++ {
		league, _ := createTestLeague(t, env.leagues, env.q, fmt.Sprintf("Pagination League %d", nextID()))
		created = append(created, league.ID)
	}

	firstPage, total, err := env.admin.ListLeagues(context.Background(), 2, 0)
	if err != nil {
		t.Fatalf("ListLeagues (page 1): %v", err)
	}
	if len(firstPage) != 2 {
		t.Fatalf("len(firstPage) = %d, want 2", len(firstPage))
	}
	if total < int64(len(created)) {
		t.Errorf("total = %d, want at least %d", total, len(created))
	}

	// A far-out offset must return an empty slice, not an error.
	emptyPage, _, err := env.admin.ListLeagues(context.Background(), 10, int32(total)+1000)
	if err != nil {
		t.Fatalf("ListLeagues (empty page): %v", err)
	}
	if len(emptyPage) != 0 {
		t.Errorf("len(emptyPage) = %d, want 0", len(emptyPage))
	}

	// Every league this test created must be findable somewhere across
	// enough pages, and each row must carry a commissioner + member_count.
	found := make(map[pgtype.UUID]bool)
	for offset := int32(0); offset < int32(total); offset += 50 {
		page, _, err := env.admin.ListLeagues(context.Background(), 50, offset)
		if err != nil {
			t.Fatalf("ListLeagues (offset=%d): %v", offset, err)
		}
		for _, row := range page {
			found[row.ID] = true
			if row.CommissionerDisplayName == "" || row.CommissionerEmail == "" {
				t.Errorf("league %v missing commissioner display_name/email", row.ID)
			}
			if row.MemberCount < 1 {
				t.Errorf("league %v member_count = %d, want at least 1 (the commissioner)", row.ID, row.MemberCount)
			}
		}
		if len(page) == 0 {
			break
		}
	}
	for _, id := range created {
		if !found[id] {
			t.Errorf("league %v created by this test not found in ListLeagues output", id)
		}
	}
}

func TestService_ListUsers_Pagination(t *testing.T) {
	env := newResyncTestEnv(t)

	var created []pgtype.UUID
	for i := 0; i < 3; i++ {
		u := createTestUser(t, env.q)
		created = append(created, u.ID)
	}

	firstPage, total, err := env.admin.ListUsers(context.Background(), 2, 0)
	if err != nil {
		t.Fatalf("ListUsers (page 1): %v", err)
	}
	if len(firstPage) != 2 {
		t.Fatalf("len(firstPage) = %d, want 2", len(firstPage))
	}
	if total < int64(len(created)) {
		t.Errorf("total = %d, want at least %d", total, len(created))
	}

	emptyPage, _, err := env.admin.ListUsers(context.Background(), 10, int32(total)+1000)
	if err != nil {
		t.Fatalf("ListUsers (empty page): %v", err)
	}
	if len(emptyPage) != 0 {
		t.Errorf("len(emptyPage) = %d, want 0", len(emptyPage))
	}

	found := make(map[pgtype.UUID]bool)
	for offset := int32(0); offset < int32(total); offset += 50 {
		page, _, err := env.admin.ListUsers(context.Background(), 50, offset)
		if err != nil {
			t.Fatalf("ListUsers (offset=%d): %v", offset, err)
		}
		for _, row := range page {
			found[row.ID] = true
		}
		if len(page) == 0 {
			break
		}
	}
	for _, id := range created {
		if !found[id] {
			t.Errorf("user %v created by this test not found in ListUsers output", id)
		}
	}
}

// --- Disable/enable user: the actual login-blocking effect, driven
// through internal/auth.Service.Login, not a shortcut against user.Status
// directly. ---

func TestService_DisableUser_BlocksLogin_EnableUser_RestoresIt(t *testing.T) {
	env := newResyncTestEnv(t)
	admin := createTestUser(t, env.q)

	email := fmt.Sprintf("disable-test-%d@example.test", nextID())
	const password = "correct horse battery staple"
	session, err := env.auth.Register(context.Background(), email, password, "Disable Target")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	target := session.User

	// Sanity: login works before any disable.
	if _, err := env.auth.Login(context.Background(), email, password); err != nil {
		t.Fatalf("Login before disable: %v", err)
	}

	updated, err := env.admin.DisableUser(context.Background(), admin.ID, target.ID)
	if err != nil {
		t.Fatalf("DisableUser: %v", err)
	}
	if updated.Status != UserStatusDisabled {
		t.Errorf("updated.Status = %q, want %q", updated.Status, UserStatusDisabled)
	}

	if _, err := env.auth.Login(context.Background(), email, password); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("Login after disable: err = %v, want auth.ErrInvalidCredentials", err)
	}

	if got := countAuditLogRowsForTarget(t, env.pool, actionDisableUser, target.ID); got != 1 {
		t.Errorf("audit_log rows for disable_user on target = %d, want 1", got)
	}

	reenabled, err := env.admin.EnableUser(context.Background(), admin.ID, target.ID)
	if err != nil {
		t.Fatalf("EnableUser: %v", err)
	}
	if reenabled.Status != UserStatusActive {
		t.Errorf("reenabled.Status = %q, want %q", reenabled.Status, UserStatusActive)
	}

	if _, err := env.auth.Login(context.Background(), email, password); err != nil {
		t.Errorf("Login after enable: %v, want success", err)
	}

	if got := countAuditLogRowsForTarget(t, env.pool, actionEnableUser, target.ID); got != 1 {
		t.Errorf("audit_log rows for enable_user on target = %d, want 1", got)
	}
}

func TestService_DisableUser_UnknownUser(t *testing.T) {
	env := newResyncTestEnv(t)
	admin := createTestUser(t, env.q)

	_, err := env.admin.DisableUser(context.Background(), admin.ID, pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true})
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("DisableUser(unknown user): err = %v, want ErrUserNotFound", err)
	}
}

func countAuditLogRowsForTarget(t *testing.T, pool *pgxpool.Pool, action string, targetID pgtype.UUID) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2`,
		action, targetID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count audit_log rows: %v", err)
	}
	return count
}

// --- Audit log filtering ---

func TestService_ListAuditLog_FiltersByActionAndActor(t *testing.T) {
	env := newResyncTestEnv(t)
	adminA := createTestUser(t, env.q)
	adminB := createTestUser(t, env.q)
	targetA := createTestUser(t, env.q)
	targetB := createTestUser(t, env.q)

	if _, err := env.admin.DisableUser(context.Background(), adminA.ID, targetA.ID); err != nil {
		t.Fatalf("DisableUser (adminA -> targetA): %v", err)
	}
	if _, err := env.admin.DisableUser(context.Background(), adminB.ID, targetB.ID); err != nil {
		t.Fatalf("DisableUser (adminB -> targetB): %v", err)
	}
	if _, err := env.admin.EnableUser(context.Background(), adminA.ID, targetA.ID); err != nil {
		t.Fatalf("EnableUser (adminA -> targetA): %v", err)
	}

	// Filter by action only: every disable_user row, regardless of actor,
	// must include both of the ones just written.
	action := actionDisableUser
	rows, _, err := env.admin.ListAuditLog(context.Background(), 100, 0, &action, pgtype.UUID{})
	if err != nil {
		t.Fatalf("ListAuditLog (action filter): %v", err)
	}
	seenTargets := make(map[pgtype.UUID]bool)
	for _, r := range rows {
		if r.Action != actionDisableUser {
			t.Errorf("row with action %q returned under action=disable_user filter", r.Action)
		}
		seenTargets[r.TargetID] = true
	}
	if !seenTargets[targetA.ID] || !seenTargets[targetB.ID] {
		t.Errorf("expected both disable_user targets in filtered results, got targets=%v", seenTargets)
	}

	// Filter by actor only: adminA acted twice (disable + enable) on
	// targetA; adminB's disable_user row on targetB must not appear.
	rows, _, err = env.admin.ListAuditLog(context.Background(), 100, 0, nil, adminA.ID)
	if err != nil {
		t.Fatalf("ListAuditLog (actor filter): %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("len(rows) = %d, want at least 2 (adminA's disable + enable)", len(rows))
	}
	for _, r := range rows {
		if r.ActorUserID != adminA.ID {
			t.Errorf("row with actor_user_id %v returned under actor_user_id=%v filter", r.ActorUserID, adminA.ID)
		}
		if r.TargetID == targetB.ID {
			t.Errorf("adminB's action on targetB leaked into adminA's actor-filtered results")
		}
	}

	// Combined action+actor filter: only adminA's disable_user row on
	// targetA.
	rows, _, err = env.admin.ListAuditLog(context.Background(), 100, 0, &action, adminA.ID)
	if err != nil {
		t.Fatalf("ListAuditLog (action+actor filter): %v", err)
	}
	for _, r := range rows {
		if r.Action != actionDisableUser || r.ActorUserID != adminA.ID {
			t.Errorf("row action=%q actor=%v does not match combined filter", r.Action, r.ActorUserID)
		}
	}
	foundCombined := false
	for _, r := range rows {
		if r.TargetID == targetA.ID {
			foundCombined = true
		}
	}
	if !foundCombined {
		t.Error("expected adminA's disable_user row on targetA in the combined filter's results")
	}
}

// --- The main event: a postponed game blocking a league-week's
// finalization, unblocked through the real POST /admin/games/:id/resync
// path (ResyncGame -> schedule.RefreshGame -> a mock CFBD server -> the
// same GradeGame/TryFinalizeLeagueWeek path internal/livepoll's poll loop
// uses) — not a direct grading.Service call bypassing the resync logic. ---

func TestService_ResyncGame_UnblocksPostponedGameFinalization(t *testing.T) {
	env := newResyncTestEnv(t)
	admin := createTestUser(t, env.q)

	league, playerA := createTestLeague(t, env.leagues, env.q, fmt.Sprintf("Resync Unblock %d", nextID()))
	playerB := addTestPlayer(t, env.leagues, env.q, league, "playerB")

	week := createTestWeek(t, env.q, int32(uniqueSeasonYear()), 1)

	teamAExtID, teamBExtID, teamCExtID, teamDExtID := nextID(), nextID(), nextID(), nextID()
	teamA := createTestTeamWithExternalID(t, env.q, teamAExtID, "Team A", league.Conference)
	teamB := createTestTeamWithExternalID(t, env.q, teamBExtID, "Team B", league.Conference)
	teamC := createTestTeamWithExternalID(t, env.q, teamCExtID, "Team C", league.Conference)
	teamD := createTestTeamWithExternalID(t, env.q, teamDExtID, "Team D", league.Conference)

	finalGameExtID := nextID()
	postponedGameExtID := nextID()
	finalGame := createTestGameWithExternalID(t, env.q, finalGameExtID, week, teamA, teamB, time.Now().Add(48*time.Hour))
	postponedGame := createTestGameWithExternalID(t, env.q, postponedGameExtID, week, teamC, teamD, time.Now().Add(48*time.Hour))

	submitPick(t, env.picks, playerA.ID, week.ID, league.Conference, finalGame.ID, teamA.ID)
	submitPick(t, env.picks, playerB.ID, week.ID, league.Conference, postponedGame.ID, teamC.ID)

	finalizeGame(t, env.pool, finalGame.ID, teamA.ID, 10, 0)
	setGameStatus(t, env.pool, postponedGame.ID, "postponed")

	gradingService := grading.NewService(env.q, env.pool)
	if _, err := gradingService.GradeGame(context.Background(), finalGame.ID); err != nil {
		t.Fatalf("GradeGame(finalGame): %v", err)
	}
	blockedResult, err := gradingService.TryFinalizeLeagueWeek(context.Background(), league.ID, week.ID)
	if err != nil {
		t.Fatalf("TryFinalizeLeagueWeek (should be blocked): %v", err)
	}
	if blockedResult != nil {
		t.Fatalf("TryFinalizeLeagueWeek = %+v, want nil (blocked by the postponed game)", blockedResult)
	}

	// Configure the mock CFBD server to now report the postponed game as
	// final: teamC (home) 24, teamD (away) 10 — teamC wins, matching
	// playerB's pick, so this scenario is a normal finalize (nobody
	// eliminated on this game), not a second mass-wipeout.
	gamesJSON := fmt.Sprintf(
		`[{"id": %d, "season": 2099, "week": 1, "seasonType": "regular", "startDate": "2025-09-06T17:00:00Z", "startTimeTBD": false, "completed": true, "homeId": %d, "homeTeam": "Team C", "homePoints": 24, "awayId": %d, "awayTeam": "Team D", "awayPoints": 10}]`,
		postponedGameExtID, teamCExtID, teamDExtID,
	)
	env.setGamesJSON(gamesJSON)

	result, err := env.admin.ResyncGame(context.Background(), admin.ID, postponedGame.ID)
	if err != nil {
		t.Fatalf("ResyncGame: %v", err)
	}
	if result.Game.Status != "final" {
		t.Fatalf("result.Game.Status = %q, want %q", result.Game.Status, "final")
	}
	if !result.Game.WinnerTeamID.Valid || result.Game.WinnerTeamID != teamC.ID {
		t.Errorf("result.Game.WinnerTeamID = %v, want teamC (%v)", result.Game.WinnerTeamID, teamC.ID)
	}

	// The core assertion: this specific league/week must show up as
	// finalized in ResyncGame's own summary.
	foundFinalized := false
	for _, f := range result.FinalizedLeagueWeeks {
		if f.LeagueID == league.ID && f.WeekID == week.ID {
			foundFinalized = true
			if f.MassWipeout {
				t.Error("MassWipeout = true, want false (both players picked winners)")
			}
		}
	}
	if !foundFinalized {
		t.Fatalf("league %v week %v not present in ResyncGame's FinalizedLeagueWeeks = %+v", league.ID, week.ID, result.FinalizedLeagueWeeks)
	}

	// Confirm it's really finalized in the DB, not just reported as such.
	lwr, err := env.q.GetLeagueWeekResultByLeagueAndWeek(context.Background(), gen.GetLeagueWeekResultByLeagueAndWeekParams{
		LeagueID: league.ID, WeekID: week.ID,
	})
	if err != nil {
		t.Fatalf("GetLeagueWeekResultByLeagueAndWeek: %v (expected a row — league-week should now be finalized)", err)
	}
	if lwr.MassWipeout {
		t.Error("league_week_results.mass_wipeout = true, want false")
	}

	// Both players picked the eventual winner of their respective games —
	// neither should be eliminated.
	membershipA, err := env.leagues.GetMembershipByID(context.Background(), playerA.ID)
	if err != nil {
		t.Fatalf("GetMembershipByID(playerA): %v", err)
	}
	if membershipA.Status != "active" {
		t.Errorf("playerA status = %q, want %q", membershipA.Status, "active")
	}
	membershipB, err := env.leagues.GetMembershipByID(context.Background(), playerB.ID)
	if err != nil {
		t.Fatalf("GetMembershipByID(playerB): %v", err)
	}
	if membershipB.Status != "active" {
		t.Errorf("playerB status = %q, want %q (picked the game this resync just resolved)", membershipB.Status, "active")
	}

	// The resync itself must be audit-logged.
	if got := countAuditLogRowsForTarget(t, env.pool, actionResyncGame, postponedGame.ID); got != 1 {
		t.Errorf("audit_log rows for resync_game on game %v = %d, want 1", postponedGame.ID, got)
	}
}

// TestService_ResyncGame_NotFinalYetTriggersNoFinalization confirms
// ResyncGame doesn't attempt any grading/finalization when the resynced
// game hasn't reached 'final' — a resync that merely updates a still-
// in-progress game's score, say.
func TestService_ResyncGame_NotFinalYetTriggersNoFinalization(t *testing.T) {
	env := newResyncTestEnv(t)
	admin := createTestUser(t, env.q)

	week := createTestWeek(t, env.q, int32(uniqueSeasonYear()), 1)
	teamAExtID, teamBExtID := nextID(), nextID()
	teamA := createTestTeamWithExternalID(t, env.q, teamAExtID, "Team A2", "Big Ten")
	teamB := createTestTeamWithExternalID(t, env.q, teamBExtID, "Team B2", "Big Ten")
	gameExtID := nextID()
	game := createTestGameWithExternalID(t, env.q, gameExtID, week, teamA, teamB, time.Now().Add(48*time.Hour))
	setGameStatus(t, env.pool, game.ID, "postponed")

	gamesJSON := fmt.Sprintf(
		`[{"id": %d, "season": 2099, "week": 1, "seasonType": "regular", "startDate": "2025-09-06T17:00:00Z", "startTimeTBD": false, "completed": false, "homeId": %d, "homeTeam": "Team A2", "homePoints": null, "awayId": %d, "awayTeam": "Team B2", "awayPoints": null}]`,
		gameExtID, teamAExtID, teamBExtID,
	)
	env.setGamesJSON(gamesJSON)

	result, err := env.admin.ResyncGame(context.Background(), admin.ID, game.ID)
	if err != nil {
		t.Fatalf("ResyncGame: %v", err)
	}
	if result.Game.Status != "scheduled" {
		t.Errorf("result.Game.Status = %q, want %q (completed=false maps to 'scheduled')", result.Game.Status, "scheduled")
	}
	if len(result.FinalizedLeagueWeeks) != 0 {
		t.Errorf("FinalizedLeagueWeeks = %+v, want empty (game isn't final yet)", result.FinalizedLeagueWeeks)
	}
	if got := countAuditLogRowsForTarget(t, env.pool, actionResyncGame, game.ID); got != 1 {
		t.Errorf("audit_log rows for resync_game on game %v = %d, want 1 (still recorded even though nothing finalized)", game.ID, got)
	}
}
