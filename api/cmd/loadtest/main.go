// Command loadtest hammers PUT /leagues/:id/weeks/:weekId/picks/me with
// real concurrency against a live API server + real Postgres instance —
// Phase 10's "concurrent-pick load test" roadmap item.
//
// This is a standalone program, not a `go test` — `go test ./...` never
// builds or runs it, and it requires a running server + database to do
// anything useful. Nothing about its behavior is theoretical: it seeds its
// own throwaway fixtures directly in Postgres (teams/weeks/games under a
// dedicated, auto-selected season_year so it never collides with real or
// other test data), drives every pick submission through the real HTTP
// API (so the full middleware/handler/transaction stack is exercised, not
// bypassed), and — critically — re-queries Postgres afterward to prove no
// constraint was ever silently violated under the concurrency it just
// generated.
//
// The specific risks under test, per the plan's Phase 10 roadmap line:
//   - a race on the UNIQUE(league_membership_id, team_id) constraint
//     (never let a member end up with the same team committed twice, or a
//     team "vanish" from the constraint's protection under concurrent
//     writers racing across two different weeks)
//   - the per-game/per-week lock check under concurrent requests to the
//     SAME (membership, week) pair (never let a member end up with two
//     rows for one week, and never corrupt the row under concurrent
//     "change my mind" writes)
//
// Usage:
//
//	docker-compose up -d
//	# terminal 1:
//	DATABASE_URL=postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable \
//	  JWT_SECRET=dev-only-insecure-secret-change-me \
//	  PORT=8090 go run ./cmd/server
//	# terminal 2:
//	DATABASE_URL=postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable \
//	  BASE_URL=http://localhost:8090 go run ./cmd/loadtest
//
// Flags (all optional, see -h): -members, -phase1-rounds, -phase1-workers,
// -phase2-members, -phase2-racers, -phase3-members, -no-cleanup.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	baseURL := flag.String("base-url", getenv("BASE_URL", "http://localhost:8090"), "base URL of a running survivor-league-api server")
	databaseURL := flag.String("database-url", getenv("DATABASE_URL", "postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"), "Postgres connection string (same DB the server is using)")
	nGeneral := flag.Int("members", 20, "members used for phase 1 (broad concurrency churn)")
	phase1Rounds := flag.Int("phase1-rounds", 10, "PUT requests per phase-1 member")
	phase1Workers := flag.Int("phase1-workers", 60, "max concurrent in-flight requests during phase 1")
	nSelfRace := flag.Int("phase2-members", 10, "members used for phase 2 (same-membership race)")
	selfRaceRacers := flag.Int("phase2-racers", 15, "concurrent goroutines racing per phase-2 member")
	nTeamRace := flag.Int("phase3-members", 10, "members used for phase 3 (team-reuse-across-weeks race)")
	teamRaceRacersPerSide := flag.Int("phase3-racers-per-side", 5, "concurrent goroutines per side (week1 vs week2) per phase-3 member")
	noCleanup := flag.Bool("no-cleanup", false, "skip deleting seeded fixtures/data at the end (for manual inspection)")
	flag.Parse()

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping database: %v (is docker-compose up and migrations applied?)", err)
	}

	if resp, err := http.Get(strings.TrimRight(*baseURL, "/") + "/health"); err != nil || resp.StatusCode != http.StatusOK {
		log.Fatalf("server health check failed at %s/health: err=%v (is `go run ./cmd/server` running against the same DATABASE_URL?)", *baseURL, err)
	}

	runTag := fmt.Sprintf("lt%d", time.Now().UnixNano())
	client := &httpClient{base: strings.TrimRight(*baseURL, "/"), c: &http.Client{Timeout: 30 * time.Second}}

	fmt.Printf("=== Phase 10 concurrent-pick load test (run tag %s) ===\n", runTag)

	totalMembers := *nGeneral + *nSelfRace + *nTeamRace
	fixtures := seedFixtures(ctx, pool, runTag, totalMembers)
	fmt.Printf("seeded: season_year=%d conference=%q teams=%d week1=%s week2=%s games/week=%d\n",
		fixtures.seasonYear, fixtures.conference, len(fixtures.teamIDs), fixtures.week1ID.String(), fixtures.week2ID.String(), len(fixtures.week1Games))

	// leagues.season_year is validated to a "reasonable 4-digit year"
	// (2000-2100) by POST /leagues — unlike fixtures.seasonYear (which
	// drives weeks.season_year and is deliberately a large, distinctive,
	// collision-free value picked by pickUnusedSeasonYear). Nothing in the
	// picks pipeline actually cross-checks league.season_year against the
	// week's own season_year (weeks are global/shared-across-leagues per
	// the plan, filtered only by conference) — the league's season_year
	// here is just a valid placeholder to satisfy that one field's own
	// input validation.
	const leagueSeasonYear = 2026
	commissioner := registerAndLogin(client, runTag, "commish")
	league := createLeague(client, commissioner, "Loadtest League "+runTag, leagueSeasonYear, fixtures.conference)
	fmt.Printf("league created: id=%s conference=%s\n", league.ID, league.Conference)

	inviteCode := getInviteCode(client, commissioner, league.ID)

	// Registration hashes each password with argon2id (deliberately
	// expensive — see internal/auth/password.go), so setup itself is
	// bounded to a modest concurrency here rather than firing all
	// registrations at once: that's CPU-bound account-creation cost, not
	// the thing this load test exists to measure, and letting it run
	// fully unbounded just self-inflicts request queueing/timeouts
	// unrelated to the picks endpoint under test below.
	const registrationConcurrency = 12
	members := make([]*member, totalMembers)
	var wg sync.WaitGroup
	regSem := make(chan struct{}, registrationConcurrency)
	for i := 0; i < totalMembers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			regSem <- struct{}{}
			defer func() { <-regSem }()
			u := registerAndLogin(client, runTag, fmt.Sprintf("member%d", i))
			membershipID := joinLeague(client, u, inviteCode)
			members[i] = &member{user: u, membershipID: membershipID}
		}(i)
	}
	wg.Wait()
	fmt.Printf("registered + joined %d members\n", totalMembers)

	generalMembers := members[:*nGeneral]
	selfRaceMembers := members[*nGeneral : *nGeneral+*nSelfRace]
	teamRaceMembers := members[*nGeneral+*nSelfRace:]

	// --- Phase 1: broad concurrency churn ---
	p1 := runPhase1(client, league.ID, fixtures.week1ID.String(), fixtures.week1Games, generalMembers, *phase1Rounds, *phase1Workers)
	fmt.Println()
	fmt.Println("--- Phase 1: broad concurrency (many members, many rapid pick changes) ---")
	p1.report()

	// --- Phase 2: same-membership race (row-lock / no-duplicate-row check) ---
	p2 := runPhase2(client, league.ID, fixtures.week1ID.String(), fixtures.week1Games, selfRaceMembers, *selfRaceRacers)
	fmt.Println()
	fmt.Println("--- Phase 2: same-membership concurrent race (UNIQUE(membership,week) / row-lock stress) ---")
	p2.report()

	// --- Phase 3: team-reuse-across-weeks race (UNIQUE(membership,team_id) check) ---
	p3 := runPhase3(client, league.ID, fixtures, teamRaceMembers, *teamRaceRacersPerSide)
	fmt.Println()
	fmt.Println("--- Phase 3: team-reuse-across-weeks concurrent race (UNIQUE(membership,team_id) stress) ---")
	p3.report()

	// --- Integrity verification directly against Postgres ---
	fmt.Println()
	fmt.Println("--- Data integrity verification (direct Postgres queries) ---")
	allMembershipIDs := make([]string, 0, totalMembers)
	for _, m := range members {
		allMembershipIDs = append(allMembershipIDs, m.membershipID)
	}
	verifyIntegrity(ctx, pool, allMembershipIDs)

	if *noCleanup {
		fmt.Println()
		fmt.Printf("skipping cleanup (-no-cleanup set) — fixtures tagged %q left in place\n", runTag)
		return
	}

	fmt.Println()
	fmt.Println("--- Cleanup ---")
	cleanupFixtures(ctx, pool, league.ID, fixtures.seasonYear, runTag)
	fmt.Println("cleanup complete: league, memberships, picks, games, weeks, teams, and users created by this run have been deleted")
}

// --- HTTP client + API helpers ---

type httpClient struct {
	base string
	c    *http.Client
}

type apiResult struct {
	status   int
	body     []byte
	err      error
	duration time.Duration
}

func (h *httpClient) do(method, path, token string, body any) apiResult {
	start := time.Now()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, h.base+path, reader)
	if err != nil {
		return apiResult{err: err, duration: time.Since(start)}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := h.c.Do(req)
	dur := time.Since(start)
	if err != nil {
		return apiResult{err: err, duration: dur}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return apiResult{status: resp.StatusCode, body: b, duration: dur}
}

type user struct {
	email       string
	accessToken string
	id          string
}

type member struct {
	user         *user
	membershipID string
}

func registerAndLogin(client *httpClient, runTag, label string) *user {
	email := fmt.Sprintf("loadtest-%s-%s@example.test", runTag, label)
	res := client.do(http.MethodPost, "/auth/register", "", map[string]string{
		"email":        email,
		"password":     "LoadTest123!Password",
		"display_name": "Loadtest " + label,
	})
	if res.err != nil || res.status != http.StatusCreated {
		log.Fatalf("register %s failed: status=%d err=%v body=%s", email, res.status, res.err, string(res.body))
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(res.body, &parsed); err != nil {
		log.Fatalf("parse register response for %s: %v (body=%s)", email, err, string(res.body))
	}
	return &user{email: email, accessToken: parsed.AccessToken, id: parsed.User.ID}
}

type league struct {
	ID         string `json:"id"`
	Conference string `json:"conference"`
}

func createLeague(client *httpClient, commissioner *user, name string, seasonYear int32, conference string) league {
	res := client.do(http.MethodPost, "/leagues", commissioner.accessToken, map[string]any{
		"name":        name,
		"season_year": seasonYear,
		"conference":  conference,
	})
	if res.err != nil || res.status != http.StatusCreated {
		log.Fatalf("create league failed: status=%d err=%v body=%s", res.status, res.err, string(res.body))
	}
	var l league
	if err := json.Unmarshal(res.body, &l); err != nil {
		log.Fatalf("parse create league response: %v", err)
	}
	return l
}

func getInviteCode(client *httpClient, commissioner *user, leagueID string) string {
	res := client.do(http.MethodGet, "/leagues/"+leagueID+"/invite", commissioner.accessToken, nil)
	if res.err != nil || res.status != http.StatusOK {
		log.Fatalf("get invite code failed: status=%d err=%v body=%s", res.status, res.err, string(res.body))
	}
	var parsed struct {
		InviteCode string `json:"invite_code"`
	}
	if err := json.Unmarshal(res.body, &parsed); err != nil {
		log.Fatalf("parse invite code response: %v", err)
	}
	return parsed.InviteCode
}

func joinLeague(client *httpClient, u *user, code string) string {
	res := client.do(http.MethodPost, "/invites/"+code+"/join", u.accessToken, nil)
	if res.err != nil || res.status != http.StatusOK {
		log.Fatalf("join league failed for %s: status=%d err=%v body=%s", u.email, res.status, res.err, string(res.body))
	}
	var parsed struct {
		Membership struct {
			ID string `json:"id"`
		} `json:"membership"`
	}
	if err := json.Unmarshal(res.body, &parsed); err != nil {
		log.Fatalf("parse join response for %s: %v (body=%s)", u.email, err, string(res.body))
	}
	return parsed.Membership.ID
}

func putPick(client *httpClient, leagueID, weekID string, u *user, gameID, teamID string) apiResult {
	return client.do(http.MethodPut, fmt.Sprintf("/leagues/%s/weeks/%s/picks/me", leagueID, weekID), u.accessToken, map[string]string{
		"game_id": gameID,
		"team_id": teamID,
	})
}

// --- Fixtures ---

type gamePair struct {
	gameID       pgtype.UUID
	homeTeamID   pgtype.UUID
	awayTeamID   pgtype.UUID
	homeTeamName string
	awayTeamName string
}

type fixtures struct {
	seasonYear int32
	conference string
	teamIDs    []pgtype.UUID
	week1ID    pgtype.UUID
	week2ID    pgtype.UUID
	week1Games []gamePair
	week2Games []gamePair // same team pairings as week1Games, index-aligned, different game_id
}

// seedFixtures creates a dedicated, collision-free season_year plus enough
// teams/weeks/games directly in Postgres for every phase below. Teams in
// week1Games[i] and week2Games[i] are the SAME two teams (different
// game_id, different week) — exactly what phase 3 needs to race a single
// team across two weeks.
func seedFixtures(ctx context.Context, pool *pgxpool.Pool, runTag string, totalMembers int) fixtures {
	seasonYear := pickUnusedSeasonYear(ctx, pool)
	const conference = "Mountain West Conference"

	// Enough teams for every member across every phase to have its own
	// dedicated pair, plus headroom.
	nPairs := totalMembers + 20
	nTeams := nPairs * 2

	teamIDs := make([]pgtype.UUID, nTeams)
	teamNames := make([]string, nTeams)
	batch := &pgx.Batch{}
	for i := 0; i < nTeams; i++ {
		teamNames[i] = fmt.Sprintf("Loadtest Team %s-%d", runTag, i)
		batch.Queue(`INSERT INTO teams (external_id, name, conference) VALUES ($1,$2,$3) RETURNING id`,
			fmt.Sprintf("LOADTEST-%s-TEAM-%d", runTag, i), teamNames[i], conference)
	}
	br := pool.SendBatch(ctx, batch)
	for i := 0; i < nTeams; i++ {
		if err := br.QueryRow().Scan(&teamIDs[i]); err != nil {
			log.Fatalf("seed teams: %v", err)
		}
	}
	if err := br.Close(); err != nil {
		log.Fatalf("seed teams batch close: %v", err)
	}

	var week1ID, week2ID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO weeks (season_year, week_number) VALUES ($1,1) RETURNING id`, seasonYear).Scan(&week1ID); err != nil {
		log.Fatalf("seed week1: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO weeks (season_year, week_number) VALUES ($1,2) RETURNING id`, seasonYear).Scan(&week2ID); err != nil {
		log.Fatalf("seed week2: %v", err)
	}

	week1Kickoff := time.Now().Add(48 * time.Hour)
	week2Kickoff := time.Now().Add(72 * time.Hour)

	week1Games := make([]gamePair, nPairs)
	week2Games := make([]gamePair, nPairs)
	gbatch := &pgx.Batch{}
	for i := 0; i < nPairs; i++ {
		home, away := teamIDs[2*i], teamIDs[2*i+1]
		gbatch.Queue(`INSERT INTO games (external_id, week_id, home_team_id, away_team_id, kickoff_at, status) VALUES ($1,$2,$3,$4,$5,'scheduled') RETURNING id`,
			fmt.Sprintf("LOADTEST-%s-W1-GAME-%d", runTag, i), week1ID, home, away, week1Kickoff)
		gbatch.Queue(`INSERT INTO games (external_id, week_id, home_team_id, away_team_id, kickoff_at, status) VALUES ($1,$2,$3,$4,$5,'scheduled') RETURNING id`,
			fmt.Sprintf("LOADTEST-%s-W2-GAME-%d", runTag, i), week2ID, home, away, week2Kickoff)
	}
	gbr := pool.SendBatch(ctx, gbatch)
	for i := 0; i < nPairs; i++ {
		var g1, g2 pgtype.UUID
		if err := gbr.QueryRow().Scan(&g1); err != nil {
			log.Fatalf("seed week1 game %d: %v", i, err)
		}
		if err := gbr.QueryRow().Scan(&g2); err != nil {
			log.Fatalf("seed week2 game %d: %v", i, err)
		}
		home, away := teamIDs[2*i], teamIDs[2*i+1]
		week1Games[i] = gamePair{gameID: g1, homeTeamID: home, awayTeamID: away, homeTeamName: teamNames[2*i], awayTeamName: teamNames[2*i+1]}
		week2Games[i] = gamePair{gameID: g2, homeTeamID: home, awayTeamID: away, homeTeamName: teamNames[2*i], awayTeamName: teamNames[2*i+1]}
	}
	if err := gbr.Close(); err != nil {
		log.Fatalf("seed games batch close: %v", err)
	}

	return fixtures{
		seasonYear: seasonYear,
		conference: conference,
		teamIDs:    teamIDs,
		week1ID:    week1ID,
		week2ID:    week2ID,
		week1Games: week1Games,
		week2Games: week2Games,
	}
}

// pickUnusedSeasonYear finds a season_year with zero existing weeks rows,
// starting from a distinctive base far outside any real or unit-test
// season range, so this program never collides with real data or with
// another package's integration-test fixtures (several of which use
// small/near-present-day season years like 2026 or 2099) sharing the same
// dev database.
func pickUnusedSeasonYear(ctx context.Context, pool *pgxpool.Pool) int32 {
	for candidate := int32(900001); candidate < 900001+1000; candidate++ {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM weeks WHERE season_year = $1`, candidate).Scan(&count); err != nil {
			log.Fatalf("pickUnusedSeasonYear: %v", err)
		}
		if count == 0 {
			return candidate
		}
	}
	log.Fatal("pickUnusedSeasonYear: exhausted candidate range — is the dev DB unusually full of test data?")
	return 0
}

// --- Latency/throughput reporting ---

type outcome struct {
	status   int
	duration time.Duration
	err      error
}

type phaseStats struct {
	name      string
	outcomes  []outcome
	wallClock time.Duration
}

func (p *phaseStats) add(o outcome) {
	p.outcomes = append(p.outcomes, o)
}

func (p *phaseStats) report() {
	n := len(p.outcomes)
	if n == 0 {
		fmt.Println("  (no requests)")
		return
	}
	byStatus := map[int]int{}
	var errs int
	durs := make([]time.Duration, 0, n)
	for _, o := range p.outcomes {
		if o.err != nil {
			errs++
			continue
		}
		byStatus[o.status]++
		durs = append(durs, o.duration)
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	pct := func(p float64) time.Duration {
		if len(durs) == 0 {
			return 0
		}
		idx := int(float64(len(durs)-1) * p)
		return durs[idx]
	}
	throughput := float64(n) / p.wallClock.Seconds()
	fmt.Printf("  requests=%d wall_clock=%s throughput=%.1f req/s\n", n, p.wallClock.Round(time.Millisecond), throughput)
	fmt.Printf("  latency: min=%s p50=%s p95=%s p99=%s max=%s\n",
		durs[0].Round(time.Millisecond), pct(0.50).Round(time.Millisecond), pct(0.95).Round(time.Millisecond), pct(0.99).Round(time.Millisecond), durs[len(durs)-1].Round(time.Millisecond))
	statusKeys := make([]int, 0, len(byStatus))
	for k := range byStatus {
		statusKeys = append(statusKeys, k)
	}
	sort.Ints(statusKeys)
	for _, k := range statusKeys {
		fmt.Printf("  status %d: %d\n", k, byStatus[k])
	}
	if errs > 0 {
		fmt.Printf("  transport errors: %d\n", errs)
	}
}

// --- Phase 1: broad concurrency churn ---

func runPhase1(client *httpClient, leagueID, weekID string, games []gamePair, members []*member, rounds, maxWorkers int) *phaseStats {
	stats := &phaseStats{name: "phase1"}
	var mu sync.Mutex
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	start := time.Now()
	for _, m := range members {
		for round := 0; round < rounds; round++ {
			wg.Add(1)
			go func(m *member) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				g := games[rand.Intn(len(games))]
				teamID := g.homeTeamID
				if rand.Intn(2) == 0 {
					teamID = g.awayTeamID
				}
				res := putPick(client, leagueID, weekID, m.user, g.gameID.String(), teamID.String())
				mu.Lock()
				stats.add(outcome{status: res.status, duration: res.duration, err: res.err})
				mu.Unlock()
			}(m)
		}
	}
	wg.Wait()
	stats.wallClock = time.Since(start)
	return stats
}

// --- Phase 2: same-membership race ---

func runPhase2(client *httpClient, leagueID, weekID string, games []gamePair, members []*member, racersPerMember int) *phaseStats {
	stats := &phaseStats{name: "phase2"}
	var mu sync.Mutex
	var wg sync.WaitGroup

	start := time.Now()
	for mi, m := range members {
		startCh := make(chan struct{})
		for r := 0; r < racersPerMember; r++ {
			wg.Add(1)
			go func(m *member, r int) {
				defer wg.Done()
				g := games[(mi*racersPerMember+r)%len(games)]
				teamID := g.homeTeamID
				if r%2 == 1 {
					teamID = g.awayTeamID
				}
				<-startCh // all racers for this member fire together
				res := putPick(client, leagueID, weekID, m.user, g.gameID.String(), teamID.String())
				mu.Lock()
				stats.add(outcome{status: res.status, duration: res.duration, err: res.err})
				mu.Unlock()
			}(m, r)
		}
		close(startCh)
	}
	wg.Wait()
	stats.wallClock = time.Since(start)
	return stats
}

// --- Phase 3: team-reuse-across-weeks race ---

func runPhase3(client *httpClient, leagueID string, f fixtures, members []*member, racersPerSide int) *phaseStats {
	stats := &phaseStats{name: "phase3"}
	var mu sync.Mutex
	var wg sync.WaitGroup

	start := time.Now()
	for i, m := range members {
		g1 := f.week1Games[i]
		g2 := f.week2Games[i] // same two teams as g1, different week/game_id
		team := g1.homeTeamID // race this exact team across week1 vs week2

		startCh := make(chan struct{})
		for r := 0; r < racersPerSide; r++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-startCh
				res := putPick(client, leagueID, f.week1ID.String(), m.user, g1.gameID.String(), team.String())
				mu.Lock()
				stats.add(outcome{status: res.status, duration: res.duration, err: res.err})
				mu.Unlock()
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-startCh
				res := putPick(client, leagueID, f.week2ID.String(), m.user, g2.gameID.String(), team.String())
				mu.Lock()
				stats.add(outcome{status: res.status, duration: res.duration, err: res.err})
				mu.Unlock()
			}()
		}
		close(startCh)
	}
	wg.Wait()
	stats.wallClock = time.Since(start)
	return stats
}

// --- Integrity verification ---

func verifyIntegrity(ctx context.Context, pool *pgxpool.Pool, membershipIDs []string) {
	var violations int32

	// 1. No membership ever has more than one pick row for the same week
	// (UNIQUE(league_membership_id, week_id) — this is DB-enforced, so a
	// violation here would mean the constraint itself failed, not just the
	// app).
	rows, err := pool.Query(ctx, `
		SELECT league_membership_id, week_id, count(*)
		FROM picks
		WHERE league_membership_id = ANY($1::uuid[])
		GROUP BY league_membership_id, week_id
		HAVING count(*) > 1`, membershipIDs)
	if err != nil {
		log.Fatalf("integrity check 1 (duplicate week rows): query failed: %v", err)
	}
	dupWeeks := 0
	for rows.Next() {
		dupWeeks++
		var membershipID, weekID pgtype.UUID
		var n int
		if err := rows.Scan(&membershipID, &weekID, &n); err != nil {
			log.Fatalf("scan: %v", err)
		}
		fmt.Printf("  VIOLATION: membership %s has %d picks rows for week %s (should be at most 1)\n", membershipID, n, weekID)
		atomic.AddInt32(&violations, 1)
	}
	rows.Close()
	if dupWeeks == 0 {
		fmt.Println("  [PASS] no membership has more than one pick row for the same week")
	}

	// 2. No membership ever has the same team committed to more than one
	// pick row (UNIQUE(league_membership_id, team_id)).
	rows2, err := pool.Query(ctx, `
		SELECT league_membership_id, team_id, count(*)
		FROM picks
		WHERE league_membership_id = ANY($1::uuid[])
		GROUP BY league_membership_id, team_id
		HAVING count(*) > 1`, membershipIDs)
	if err != nil {
		log.Fatalf("integrity check 2 (duplicate team commitments): query failed: %v", err)
	}
	dupTeams := 0
	for rows2.Next() {
		dupTeams++
		var membershipID, teamID pgtype.UUID
		var n int
		if err := rows2.Scan(&membershipID, &teamID, &n); err != nil {
			log.Fatalf("scan: %v", err)
		}
		fmt.Printf("  VIOLATION: membership %s has team %s committed to %d pick rows (should be at most 1)\n", membershipID, teamID, n)
		atomic.AddInt32(&violations, 1)
	}
	rows2.Close()
	if dupTeams == 0 {
		fmt.Println("  [PASS] no membership has the same team committed across more than one week")
	}

	// 3. Every pick's team_id is actually one of its game's two teams, and
	// its game_id actually belongs to its week_id — catches any
	// "corrupted" row a broken transaction might have left behind (should
	// be structurally impossible given UpsertPick's validation, but this
	// is the load test's job to prove, not assume).
	var badRows int
	err = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM picks p
		JOIN games g ON g.id = p.game_id
		WHERE p.league_membership_id = ANY($1::uuid[])
		  AND (g.week_id <> p.week_id OR (p.team_id <> g.home_team_id AND p.team_id <> g.away_team_id))`,
		membershipIDs).Scan(&badRows)
	if err != nil {
		log.Fatalf("integrity check 3 (row consistency): query failed: %v", err)
	}
	if badRows > 0 {
		fmt.Printf("  VIOLATION: %d pick rows have a team/game/week mismatch\n", badRows)
		atomic.AddInt32(&violations, int32(badRows))
	} else {
		fmt.Println("  [PASS] every pick row's team belongs to its game, and its game belongs to its week")
	}

	if violations == 0 {
		fmt.Println()
		fmt.Println("RESULT: zero data-integrity violations detected under concurrency.")
	} else {
		fmt.Println()
		fmt.Printf("RESULT: %d data-integrity violations detected — see above.\n", violations)
		os.Exit(1)
	}
}

// --- Cleanup ---

func cleanupFixtures(ctx context.Context, pool *pgxpool.Pool, leagueID string, seasonYear int32, runTag string) {
	// Order matters for the FK RESTRICT edges (games -> weeks/teams,
	// leagues.commissioner_user_id -> users): delete the league first
	// (cascades to league_memberships, which cascades to picks), then
	// games, then weeks/teams, then the users this run created. The league
	// is deleted by id (not by season_year — its season_year is a fixed
	// valid placeholder, see the comment where it's created, and is not
	// unique to this run) while weeks/games/teams are deleted by the
	// distinctive fixture season_year / external_id tag that IS unique to
	// this run.
	tag := "loadtest-" + runTag + "-%@example.test"

	exec := func(sql string, args ...any) {
		tag, err := pool.Exec(ctx, sql, args...)
		if err != nil {
			log.Fatalf("cleanup %q: %v", sql, err)
		}
		fmt.Printf("  %s (%d rows)\n", strings.SplitN(sql, "\n", 2)[0], tag.RowsAffected())
	}

	exec(`DELETE FROM leagues WHERE id = $1`, leagueID)
	exec(`DELETE FROM games WHERE week_id IN (SELECT id FROM weeks WHERE season_year = $1)`, seasonYear)
	exec(`DELETE FROM weeks WHERE season_year = $1`, seasonYear)
	exec(`DELETE FROM teams WHERE external_id LIKE $1`, "LOADTEST-"+runTag+"-%")
	exec(`DELETE FROM users WHERE email LIKE $1`, tag)

	// Sanity: confirm nothing tagged with this run's identifiers survives.
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email LIKE $1`, tag).Scan(&remaining); err != nil {
		log.Fatalf("cleanup verification query: %v", err)
	}
	if remaining != 0 {
		log.Fatalf("cleanup verification FAILED: %d users matching this run's tag still exist", remaining)
	}
	var remainingWeeks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM weeks WHERE season_year = $1`, seasonYear).Scan(&remainingWeeks); err != nil {
		log.Fatalf("cleanup verification query: %v", err)
	}
	if remainingWeeks != 0 {
		log.Fatalf("cleanup verification FAILED: %d weeks for season_year=%d still exist", remainingWeeks, seasonYear)
	}
	fmt.Println("  cleanup verification: 0 rows remain tagged with this run's identifiers")
}
