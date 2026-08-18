// Command seed-demo populates a realistic multi-week simulated season into
// an existing local league — a local-dev convenience for trying out the
// leaderboard/picks UI with a populated season instead of an empty one.
// It is NOT a production tool: it directly fabricates game results (sets
// status='final' with made-up scores) rather than waiting for real CFBD
// data, and the next real schedule sync will overwrite that fake data
// with the real (not-yet-played) game state. Run manually, never wired
// into cmd/server or any CI job.
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
// Design: a week-by-week loop, not a hand-authored script of specific
// matchups. Each week, every still-active player picks a real, unused
// (for them) Big Ten team; each game's outcome is decided by a single
// seeded random draw (reproducible across runs); a player whose pick
// loses stops appearing in every subsequent week — a real, natural gap in
// their pick history, exactly how a real eliminated contestant looks,
// not a special-cased flag. A handful of players are designated
// "survivors" whose pick always wins, guaranteeing at least some of the
// field goes all the way through the simulated weeks — real survivor
// pools always have a few contestants who make it deep, and pure
// independent-random attrition across many weeks would eliminate
// everyone by week 6-8 (0.6^8 ≈ 2%), which doesn't tell that story.
//
// Any game a REAL (non-demo) user already has a live pick on is detected
// and protected: forced to a result that keeps that real pick a win, so
// this tool can never accidentally eliminate an actual account as a side
// effect of generating demo data.
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
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

// simulatedWeeks is how many weeks of the season to generate picks/results
// for. Leaves the remaining weeks of the real 14-week season untouched and
// open for genuine manual testing.
const simulatedWeeks = 8

// winProbability is each at-risk player's chance of surviving a given
// week's pick, per game (games are decided once, not per-player — see
// simulateWeek). Low enough that the at-risk field visibly thins out
// within a few weeks, matching a real survivor pool's early attrition.
const winProbability = 0.6

// survivorCount is how many of the roster are guaranteed to win every
// week they play, ensuring the story always has players who go all the
// way through simulatedWeeks.
const survivorCount = 3

// randSeed is fixed (not time-seeded) so re-running this tool after a
// reset-schedule produces the identical story every time — useful for a
// repeatable "wipe and reseed" local workflow.
const randSeed = 20260817

type player struct {
	email       string
	displayName string
	userID      pgtype.UUID
	membership  gen.LeagueMembership
	usedTeams   map[pgtype.UUID]bool
	isSurvivor  bool
	eliminated  bool
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
	rng := rand.New(rand.NewSource(randSeed))

	leagueID, err := db.ParseUUID(targetLeagueID)
	if err != nil {
		log.Fatalf("parse league id: %v", err)
	}
	league, err := leaguesSvc.GetLeagueByID(ctx, leagueID)
	if err != nil {
		log.Fatalf("league %s not found: %v — is this still the right league id?", targetLeagueID, err)
	}
	fmt.Printf("Target league: %q (%s, season %d)\n", league.Name, league.Conference, league.SeasonYear)

	// --- Register 10 fake players and join them to the league ---
	names := []string{
		"Betty Buckeye", "Will Wolverine", "Nora Nittany", "Hugo Hawkeye",
		"Bree Badger", "Steve Spartan", "Bea Boilermaker", "Wendy Wildcat",
		"Gus Golden Gopher", "Terry Terrapin",
	}
	players := make([]*player, 0, len(names))
	for i, name := range names {
		email := fmt.Sprintf("demo.%s@seed.local", slug(name))
		sess, err := authSvc.Register(ctx, email, "demo12345", name)
		if err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
		membership, err := leaguesSvc.JoinByCode(ctx, leagueID, sess.User.ID)
		if err != nil {
			log.Fatalf("join %s: %v", name, err)
		}
		p := &player{
			email:       email,
			displayName: name,
			userID:      sess.User.ID,
			membership:  membership,
			usedTeams:   map[pgtype.UUID]bool{},
			isSurvivor:  i < survivorCount, // first N in the roster go all the way
		}
		players = append(players, p)
		role := "at-risk"
		if p.isSurvivor {
			role = "SURVIVOR"
		}
		fmt.Printf("  joined: %-18s (%s) membership=%s\n", name, role, db.UUIDString(membership.ID))
	}

	memberIDs := map[pgtype.UUID]bool{}
	for _, p := range players {
		memberIDs[p.membership.ID] = true
	}

	// Any OTHER active contestant in this league (a real account, not one
	// of the players registered above) must never be missing a pick in a
	// simulated week — a real miss is a genuine loss per the actual game
	// rules, and this tool has twice now accidentally eliminated the real
	// admin account as a side effect of simulating weeks they hadn't
	// personally picked yet, permanently burning their one-time buy-back
	// both times. Rather than try to undo that after the fact (which
	// isn't even possible once buy-back is used), every week
	// auto-fills — and force-wins — a pick for any real member who
	// doesn't already have one, so they can never be eliminated as a
	// side effect of this tool. realUserUsedTeams is seeded from each
	// real member's actual season-to-date pick history (so an
	// auto-filled pick never repeats a team they already used for real)
	// and persists across weeks as more auto-fills happen.
	realUserUsedTeams := map[pgtype.UUID]map[pgtype.UUID]bool{}
	allMembers, err := leaguesSvc.ListMembers(ctx, leagueID)
	if err != nil {
		log.Fatalf("list league members: %v", err)
	}
	for _, m := range allMembers {
		if memberIDs[m.MembershipID] || !m.IsContestant || m.Status != "active" {
			continue
		}
		history, err := picksSvc.ListMembershipPicksForSeason(ctx, m.MembershipID, league.SeasonYear)
		if err != nil {
			log.Fatalf("load pick history for real member %s: %v", m.DisplayName, err)
		}
		used := map[pgtype.UUID]bool{}
		for _, h := range history {
			if h.HasPicked {
				used[h.Row.TeamID] = true
			}
		}
		realUserUsedTeams[m.MembershipID] = used
		fmt.Printf("  real member detected: %-18s (%d team(s) already used) — will auto-fill any week they haven't picked\n", m.DisplayName, len(used))
	}

	admin, err := q.GetUserByEmail(ctx, "admin@survivorleague.football")
	if err != nil {
		log.Fatalf("find admin user: %v", err)
	}
	boughtBack := false

	for week := int32(1); week <= simulatedWeeks; week++ {
		fmt.Printf("\n=== Week %d ===\n", week)
		simulateWeek(ctx, q, picksSvc, gradingSvc, leagueID, league.Conference, league.SeasonYear, week, players, memberIDs, realUserUsedTeams, rng)
		anyActive := false
		for _, p := range players {
			status := "still active"
			if p.eliminated {
				status = "ELIMINATED"
			} else {
				anyActive = true
			}
			fmt.Printf("  %-18s -> %s\n", p.displayName, status)
		}
		if !anyActive {
			fmt.Println("  (everyone eliminated — stopping early)")
			break
		}

		// Buy back the first newly-eliminated non-survivor immediately —
		// in the very next week, not some later arbitrary week. No gap
		// weeks: a real commissioner reinstating someone wouldn't leave
		// them sitting out for a few weeks first, and a gap would just
		// reproduce the exact "long stretch with no pick" look this
		// tool is trying to avoid. Once used, this is a one-time
		// lifeline (real rule), so a SECOND elimination later leaves
		// them out for good — no further buy-backs happen.
		if !boughtBack && week < simulatedWeeks {
			for _, p := range players {
				if p.eliminated && !p.isSurvivor {
					if _, err := leaguesSvc.BuyBackMember(ctx, leagueID, p.membership.ID, admin.ID); err != nil {
						log.Fatalf("buy back %s: %v", p.displayName, err)
					}
					p.eliminated = false // rejoin the active pool starting next week — zero gap
					fmt.Printf("  -- %s bought back by admin@survivorleague.football, picks again starting week %d --\n", p.displayName, week+1)
					boughtBack = true
					break
				}
			}
		}
	}
	if !boughtBack {
		fmt.Println("\n(no eliminated player was available to buy back)")
	}

	fmt.Println("\n=== Final story ===")
	for _, p := range players {
		status := "eliminated"
		if !p.eliminated {
			status = "active"
		}
		fmt.Printf("  %-18s %s\n", p.displayName, status)
	}
	fmt.Println("\nAll demo accounts use password: demo12345 (emails: demo.<name>@seed.local)")
	fmt.Println("NOTE: this is fabricated local data. The next real schedule sync will overwrite these games' fake final scores with their real (not-yet-played) state.")
}

// simulateWeek assigns each still-active player an unused Big Ten team for
// this week (round-robin over the week's real schedule, resolved live —
// never hardcoded), decides one outcome per game touched (protecting any
// real user's existing pick first, then a survivor's pick, then a random
// draw for everyone else, then a default home win for every remaining
// conference-relevant game so TryFinalizeLeagueWeek can actually
// finalize), fabricates and grades those results, and updates each
// player's eliminated flag from the real post-grading DB state.
func simulateWeek(
	ctx context.Context,
	q *gen.Queries,
	picksSvc *picks.Service,
	gradingSvc *grading.Service,
	leagueID pgtype.UUID,
	conference string,
	seasonYear int32,
	weekNumber int32,
	players []*player,
	memberIDs map[pgtype.UUID]bool,
	realUserUsedTeams map[pgtype.UUID]map[pgtype.UUID]bool,
	rng *rand.Rand,
) {
	week, err := q.GetWeekBySeasonAndNumber(ctx, gen.GetWeekBySeasonAndNumberParams{SeasonYear: seasonYear, WeekNumber: weekNumber})
	if err != nil {
		log.Fatalf("get week %d: %v", weekNumber, err)
	}
	teams, err := q.ListAvailableTeamsForWeek(ctx, gen.ListAvailableTeamsForWeekParams{WeekID: week.ID, Conference: conference})
	if err != nil {
		log.Fatalf("list available teams for week %d: %v", weekNumber, err)
	}
	if len(teams) == 0 {
		log.Fatalf("week %d: no %s games this week — reduce simulatedWeeks or pick a different range", weekNumber, conference)
	}

	// Every conference-relevant game this week defaults to a home win,
	// then gets overridden below in priority order: a real user's
	// existing pick > a survivor's pick > a random draw for an at-risk
	// pick > the default.
	outcomes := map[pgtype.UUID]bool{} // gameID -> homeWins
	for _, t := range teams {
		outcomes[t.GameID] = true
	}

	// Protect any REAL (non-demo) user's existing pick this week — force
	// that game to a result matching what they picked, so this tool can
	// never accidentally eliminate an actual account.
	existing, err := picksSvc.ListWeekPicks(ctx, leagueID, week.ID)
	if err != nil {
		log.Fatalf("list existing week picks for week %d: %v", weekNumber, err)
	}
	protectedGames := map[pgtype.UUID]bool{}
	for _, row := range existing {
		if !row.HasPicked || memberIDs[row.Row.MembershipID] {
			continue // not a real user's pick (either no pick, or it's one of ours)
		}
		var teamIsHome bool
		for _, t := range teams {
			if t.GameID == row.Row.GameID {
				teamIsHome = t.TeamID == row.Row.TeamID
				break
			}
		}
		outcomes[row.Row.GameID] = teamIsHome
		protectedGames[row.Row.GameID] = true
	}

	// Auto-fill a forced-win pick for any real member who doesn't already
	// have one this week (realUserUsedTeams is nil/empty for a member the
	// caller didn't detect as real — nothing to do for the rest, i.e. our
	// own demo players and anyone already covered by the protection pass
	// above). See main()'s comment on realUserUsedTeams for why this
	// exists: a genuine missed pick is a real elimination, and this tool
	// must never cause that as a side effect for an account it doesn't
	// own.
	for _, row := range existing {
		used, isRealMember := realUserUsedTeams[row.Row.MembershipID]
		if !isRealMember || row.HasPicked {
			continue
		}
		var chosen gen.ListAvailableTeamsForWeekRow
		found := false
		for _, t := range teams {
			if !used[t.TeamID] && !protectedGames[t.GameID] {
				chosen, found = t, true
				break
			}
		}
		if !found {
			log.Fatalf("week %d: no unused team available to auto-fill for real member %s (membership %s) — they'd be missing a pick and could be eliminated; reduce simulatedWeeks", weekNumber, row.Row.DisplayName, db.UUIDString(row.Row.MembershipID))
		}
		if _, err := picksSvc.UpsertPick(ctx, row.Row.MembershipID, week.ID, conference, chosen.GameID, chosen.TeamID); err != nil {
			log.Fatalf("auto-fill pick for real member %s (week %d): %v", row.Row.DisplayName, weekNumber, err)
		}
		used[chosen.TeamID] = true
		outcomes[chosen.GameID] = chosen.IsHome // force this real member's auto-filled pick to win
		protectedGames[chosen.GameID] = true
		fmt.Printf("  auto-filled a pick for real member %s: %s (forced win)\n", row.Row.DisplayName, chosen.TeamName)
	}

	// Assign each still-active player a team, survivors first (so their
	// forced wins claim their game before any other player's outcome for
	// that same game is decided) — round-robin starting at an index
	// derived from the player+week so different players get variety,
	// skipping teams that player has already used and games already
	// claimed this week (by an earlier survivor, or by a real user's
	// protected pick — claimedThisWeek is pre-seeded with every
	// protected game below, so no demo player can ever be assigned into
	// one and no branch here needs to re-check protectedGames itself).
	claimedThisWeek := map[pgtype.UUID]bool{}
	for gameID := range protectedGames {
		claimedThisWeek[gameID] = true
	}
	assign := func(p *player) (row gen.ListAvailableTeamsForWeekRow, ok bool) {
		start := (int(weekNumber)*7 + len(p.displayName)) % len(teams)
		for i := 0; i < len(teams); i++ {
			t := teams[(start+i)%len(teams)]
			if p.usedTeams[t.TeamID] || claimedThisWeek[t.GameID] {
				continue
			}
			return t, true
		}
		return gen.ListAvailableTeamsForWeekRow{}, false
	}

	assignAndSubmit := func(p *player) {
		row, ok := assign(p)
		if !ok {
			if p.isSurvivor {
				// A guaranteed survivor running out of teams is a real
				// design problem (too many simulatedWeeks for the
				// available team pool), not a normal in-story event —
				// fail loudly rather than silently break the "goes all
				// the way" guarantee.
				log.Fatalf("week %d: no unused team left for SURVIVOR %s — reduce simulatedWeeks or survivorCount", weekNumber, p.displayName)
			}
			// An at-risk player running out of unused teams for this
			// week's slate (e.g. their remaining teams all have a bye)
			// is a legitimate real scenario — treat it exactly like a
			// real missed pick: submit nothing, let the real
			// missed-pick-counts-as-loss rule (internal/grading) do the
			// rest once the week finalizes.
			fmt.Printf("  (no unused team available for %s this week — treated as a missed pick)\n", p.displayName)
			return
		}
		if _, err := picksSvc.UpsertPick(ctx, p.membership.ID, week.ID, conference, row.GameID, row.TeamID); err != nil {
			log.Fatalf("pick for %s (week %d): %v", p.displayName, weekNumber, err)
		}
		p.usedTeams[row.TeamID] = true
		claimedThisWeek[row.GameID] = true
		if p.isSurvivor {
			outcomes[row.GameID] = row.IsHome // force this survivor's team to win
		} else {
			survives := rng.Float64() < winProbability
			outcomes[row.GameID] = (survives == row.IsHome)
		}
	}

	for _, p := range players {
		if p.eliminated {
			continue
		}
		if p.isSurvivor {
			assignAndSubmit(p)
		}
	}
	for _, p := range players {
		if p.eliminated || p.isSurvivor {
			continue
		}
		assignAndSubmit(p)
	}

	// Fabricate every game's result: kickoff in the past, status final, a
	// made-up score matching the decided outcome.
	for gameID, homeWins := range outcomes {
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
	for gameID := range outcomes {
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

	// Refresh each player's eliminated status from the real post-grading
	// DB state.
	for _, p := range players {
		m, err := q.GetLeagueMembershipByID(ctx, p.membership.ID)
		if err != nil {
			log.Fatalf("reload membership for %s: %v", p.displayName, err)
		}
		p.membership = m
		p.eliminated = m.Status == "eliminated"
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
