// Command seed-demo populates realistic-looking fake players, picks, and
// results into an existing local league — a local-dev convenience for
// trying out the leaderboard/picks UI with a populated season instead of
// an empty one. It is NOT a production tool: it directly fabricates game
// results (sets status='final' with made-up scores) rather than waiting
// for real CFBD data, and the next real schedule sync will overwrite that
// fake data with the real (not-yet-played) game state. Run manually,
// never wired into cmd/server or any CI job.
//
// It reuses the app's real internal/auth, internal/leagues, internal/picks,
// and internal/grading services for every state change (registering
// players, joining the league, submitting picks, grading, and
// finalization) rather than hand-writing SQL for any of that — the
// simulated season is exactly as internally consistent as a real one
// (correct pick results, correct eliminations, correct mass-wipeout
// detection) because it goes through the identical code paths a real
// server would use.
//
// Every team/game is resolved by NAME at runtime (via the same
// ListAvailableTeamsForWeek query the picks screen itself uses), not
// hardcoded UUIDs — hand-transcribing team/game ids for a whole season is
// exactly the kind of thing that's easy to get subtly wrong (e.g.
// confusing two different games' ids), and a resolver that fails loudly
// on a name typo beats a silent wrong-team bug.
//
// Important: TryFinalizeLeagueWeek only finalizes a league-week once
// EVERY conference-relevant game that week is final (see
// internal/grading's doc comment) — not just the games players happened
// to pick. So every week here fabricates a result for the league's
// ENTIRE conference slate that week, defaulting to a home win for any
// game no player's story cares about.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/auth"
	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/grading"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
	"github.com/sdeitte/survivor-league-api/internal/picks"
)

// The target league: "Steven's LOCAL B1G League" (Big Ten, season 2026).
const targetLeagueID = "e3946a77-55e1-47a2-b010-7790e60df212"

type player struct {
	email       string
	displayName string
	userID      pgtype.UUID
	membership  gen.LeagueMembership
	eliminated  bool
}

// weekPick declares one player's intended pick for a week, by team name —
// resolved against that week's real schedule at runtime. wins is whether
// the PICKED team should win (the home/away bookkeeping needed to
// fabricate a matching game result is computed automatically, not
// hand-specified).
type weekPick struct {
	player   *player
	teamName string
	wins     bool
}

// namedResult forces a specific team to win, independent of any player's
// pick — used for the one game (Wisconsin/Notre Dame, week 1) where a
// real user already has a live pick, so their result must come out
// consistent with the story regardless of whether any demo player is
// involved. Every OTHER conference game that week not covered by a pick
// or a namedResult defaults to a home win.
type namedResult struct {
	teamName string
	wins     bool
}

func main() {
	ctx := context.Background()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	q := gen.New(pool)

	jwt := auth.NewJWTIssuer("seed-demo-unused-secret")
	authSvc := auth.NewService(q, pool, jwt, "")
	leaguesSvc := leagues.NewService(q, pool)
	picksSvc := picks.NewService(q, pool)
	gradingSvc := grading.NewService(q, pool)

	leagueID, err := db.ParseUUID(targetLeagueID)
	if err != nil {
		log.Fatalf("parse league id: %v", err)
	}
	league, err := leaguesSvc.GetLeagueByID(ctx, leagueID)
	if err != nil {
		log.Fatalf("league %s not found: %v — is this still the right league id?", targetLeagueID, err)
	}
	fmt.Printf("Target league: %q (%s, season %d)\n", league.Name, league.Conference, league.SeasonYear)

	// --- Register 7 fake players and join them to the league ---
	names := []string{
		"Betty Buckeye", "Will Wolverine", "Nora Nittany", "Hugo Hawkeye",
		"Bree Badger", "Steve Spartan", "Bea Boilermaker",
	}
	players := make([]*player, 0, len(names))
	for _, name := range names {
		email := fmt.Sprintf("demo.%s@seed.local", slug(name))
		sess, err := authSvc.Register(ctx, email, "demo12345", name)
		if err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
		membership, err := leaguesSvc.JoinByCode(ctx, leagueID, sess.User.ID)
		if err != nil {
			log.Fatalf("join %s: %v", name, err)
		}
		players = append(players, &player{email: email, displayName: name, userID: sess.User.ID, membership: membership})
		fmt.Printf("  joined: %-16s membership=%s\n", name, db.UUIDString(membership.ID))
	}
	betty, will, nora, hugo, bree, steve, bea := players[0], players[1], players[2], players[3], players[4], players[5], players[6]

	// --- Week 1: 7 players pick, 2 lose (upsets) ---
	fmt.Println("\n=== Week 1 ===")
	week1Picks := []weekPick{
		{betty, "Ohio State", true},
		{will, "Michigan", true},
		{nora, "Penn State", true},
		{hugo, "Oregon", true},
		{bree, "Washington", false}, // upset
		{steve, "Indiana", false},   // upset
		{bea, "Iowa", true},
	}
	// The real admin account already has a live pick on Wisconsin this
	// week — force it to a win so this demo data can't accidentally
	// eliminate a real user's own account as a side effect.
	simulateWeek(ctx, q, picksSvc, gradingSvc, leagueID, league.Conference, league.SeasonYear, 1, week1Picks, []namedResult{{"Wisconsin", true}})
	printWeekResult(week1Picks)

	// --- Week 2: 5 remaining players pick (unused teams), 2 lose ---
	fmt.Println("\n=== Week 2 ===")
	week2Picks := []weekPick{
		{betty, "Michigan", true},
		{will, "Ohio State", true},
		{nora, "Iowa", true},
		{hugo, "Penn State", false}, // upset
		{bea, "Purdue", false},      // upset
	}
	simulateWeek(ctx, q, picksSvc, gradingSvc, leagueID, league.Conference, league.SeasonYear, 2, week2Picks, nil)
	printWeekResult(week2Picks)

	// --- Week 3: 3 remaining players ALL pick losers -> mass wipeout ---
	fmt.Println("\n=== Week 3 (engineered mass-wipeout) ===")
	week3Picks := []weekPick{
		{betty, "Michigan State", false},
		{will, "Purdue", false},
		{nora, "USC", false},
	}
	simulateWeek(ctx, q, picksSvc, gradingSvc, leagueID, league.Conference, league.SeasonYear, 3, week3Picks, nil)
	printWeekResult(week3Picks)

	// --- Buy back Steve (eliminated week 1) ---
	fmt.Println("\n=== Buy-back ===")
	admin, err := q.GetUserByEmail(ctx, "admin@survivorleague.football")
	if err != nil {
		log.Fatalf("find admin user: %v", err)
	}
	if _, err := leaguesSvc.BuyBackMember(ctx, leagueID, steve.membership.ID, admin.ID); err != nil {
		log.Fatalf("buy back steve: %v", err)
	}
	fmt.Printf("  Steve Spartan bought back by admin@survivorleague.football\n")

	fmt.Println("\n=== Final story ===")
	fmt.Println("  Active:     Betty, Will, Nora (survived 3 weeks incl. a mass-wipeout), Steve (bought back)")
	fmt.Println("  Eliminated: Bree (wk1, Washington upset), Hugo (wk2, Penn State upset), Bea (wk2, Purdue upset)")
	fmt.Println("\nAll 7 demo accounts use password: demo12345 (emails: demo.<name>@seed.local)")
	fmt.Println("NOTE: this is fabricated local data. The next real schedule sync will overwrite these games' fake final scores with their real (not-yet-played) state.")
}

// simulateWeek resolves every team name in picksList and forced against
// the week's REAL schedule (via the same ListAvailableTeamsForWeek query
// the picks screen itself uses), submits each still-active player's pick,
// then fabricates a result for the league's ENTIRE conference slate that
// week — every named/picked game gets its declared outcome, every other
// game defaults to a home win — and runs the whole batch through the real
// grading pipeline (GradeGame + TryFinalizeLeagueWeek).
func simulateWeek(ctx context.Context, q *gen.Queries, picksSvc *picks.Service, gradingSvc *grading.Service, leagueID pgtype.UUID, conference string, seasonYear int32, weekNumber int32, picksList []weekPick, forced []namedResult) {
	week, err := q.GetWeekBySeasonAndNumber(ctx, gen.GetWeekBySeasonAndNumberParams{SeasonYear: seasonYear, WeekNumber: weekNumber})
	if err != nil {
		log.Fatalf("get week %d: %v", weekNumber, err)
	}
	teams, err := q.ListAvailableTeamsForWeek(ctx, gen.ListAvailableTeamsForWeekParams{WeekID: week.ID, Conference: conference})
	if err != nil {
		log.Fatalf("list available teams for week %d: %v", weekNumber, err)
	}
	byName := make(map[string]gen.ListAvailableTeamsForWeekRow, len(teams))
	for _, t := range teams {
		byName[t.TeamName] = t
	}
	resolve := func(teamName string) gen.ListAvailableTeamsForWeekRow {
		row, ok := byName[teamName]
		if !ok {
			log.Fatalf("week %d: no game found for team %q (typo, or this team has a bye?)", weekNumber, teamName)
		}
		return row
	}

	// Every conference-relevant game this week defaults to a home win,
	// then gets overridden by forced results and picked results in turn
	// (picks always win a conflict — that's the actual story).
	results := map[pgtype.UUID]bool{} // gameID -> homeWins
	for _, t := range teams {
		results[t.GameID] = true
	}
	for _, nr := range forced {
		row := resolve(nr.teamName)
		results[row.GameID] = (nr.wins == row.IsHome) // team wins == home wins iff the forced team IS home
	}

	for _, wp := range picksList {
		if wp.player.eliminated {
			continue
		}
		row := resolve(wp.teamName)
		if _, err := picksSvc.UpsertPick(ctx, wp.player.membership.ID, week.ID, conference, row.GameID, row.TeamID); err != nil {
			log.Fatalf("pick %q for %s: %v", wp.teamName, wp.player.displayName, err)
		}
		results[row.GameID] = (wp.wins == row.IsHome)
	}

	// Fabricate every game's result: kickoff in the past, status final, a
	// made-up score matching the intended winner.
	for gameID, homeWins := range results {
		homeScore, awayScore := 24, 17
		if !homeWins {
			homeScore, awayScore = 17, 24
		}
		if _, err := q.SeedFinalizeGame(ctx, gen.SeedFinalizeGameParams{
			ID:        gameID,
			KickoffAt: pgtype.Timestamptz{Time: time.Now().Add(-2 * time.Hour), Valid: true},
			HomeScore: pgtype.Int4{Int32: int32(homeScore), Valid: true},
			AwayScore: pgtype.Int4{Int32: int32(awayScore), Valid: true},
			HomeWins:  homeWins,
		}); err != nil {
			log.Fatalf("fabricate result for game %s: %v", db.UUIDString(gameID), err)
		}
	}

	// Grade every fabricated game, then finalize every league/week pair it
	// touched — plus this specific league/week explicitly, since a filler
	// game with no picks on it never appears in GradeGame's returned
	// pairs but can still be the last conference-relevant game
	// TryFinalizeLeagueWeek was waiting on.
	finalizedLeagueWeeks := map[[2]pgtype.UUID]bool{{leagueID, week.ID}: true}
	for gameID := range results {
		pairs, err := gradingSvc.GradeGame(ctx, gameID)
		if err != nil {
			log.Fatalf("grade game %s: %v", db.UUIDString(gameID), err)
		}
		for _, p := range pairs {
			finalizedLeagueWeeks[[2]pgtype.UUID{p.LeagueID, p.WeekID}] = true
		}
	}
	for pair := range finalizedLeagueWeeks {
		if _, err := gradingSvc.TryFinalizeLeagueWeek(ctx, pair[0], pair[1]); err != nil {
			log.Fatalf("finalize league week: %v", err)
		}
	}

	// Refresh each player's eliminated status from the DB so the next
	// week's caller (and this function's own printWeekResult) sees the
	// real post-grading state.
	for _, wp := range picksList {
		m, err := q.GetLeagueMembershipByID(ctx, wp.player.membership.ID)
		if err != nil {
			log.Fatalf("reload membership: %v", err)
		}
		wp.player.membership = m
		wp.player.eliminated = m.Status == "eliminated"
	}
}

func printWeekResult(picksList []weekPick) {
	for _, wp := range picksList {
		status := "still active"
		if wp.player.eliminated {
			status = "ELIMINATED"
		}
		fmt.Printf("  %-16s -> %s\n", wp.player.displayName, status)
	}
}

func slug(name string) string {
	out := make([]byte, 0, len(name))
	for _, r := range name {
		if r == ' ' {
			out = append(out, '.')
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		out = append(out, byte(r))
	}
	return string(out)
}
