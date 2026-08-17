package grading

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sdeitte/survivor-league-api/internal/leagues"
	"github.com/sdeitte/survivor-league-api/internal/picks"
)

// TestBuyBack_FullCycle_ReEliminationAfterBuyBackRejectsSecondBuyBack is the
// important buy-back regression test called out in the Phase 6 plan: buy-
// back must be a one-time-EVER lifeline, checked against the bought_back
// flag rather than current status, so a member who is eliminated again
// after being bought back does not get a second one. This drives BOTH
// eliminations through the real grading pipeline (GradeGame +
// TryFinalizeLeagueWeek) rather than faking the status flip via direct
// SQL, so it also incidentally proves buy-back correctly reinstates a
// member into a state the grading pipeline can eliminate again later.
func TestBuyBack_FullCycle_ReEliminationAfterBuyBackRejectsSecondBuyBack(t *testing.T) {
	env := newTestEnv(t)
	league, commissioner := createLeague(t, env, "Buyback Full Cycle")
	player := addPlayer(t, env, league, "player")

	seasonYear := uniqueSeasonYear()
	week1 := createTestWeek(t, env.q, seasonYear, 1)

	// Week 1: player picks the loser, gets eliminated.
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten") // week1 winner
	teamB := createTestTeam(t, env.q, "Team B", "Big Ten") // week1 loser
	game1 := createTestGame(t, env.q, week1, teamA, teamB, time.Now().Add(48*time.Hour))
	pick(t, env, commissioner.ID, week1.ID, league.Conference, game1.ID, teamA.ID) // wins
	pick(t, env, player.ID, week1.ID, league.Conference, game1.ID, teamB.ID)       // loses
	finalizeGame(t, env.pool, game1.ID, teamA.ID, 28, 10)
	if _, err := env.grading.GradeGame(context.Background(), game1.ID); err != nil {
		t.Fatalf("GradeGame(game1): %v", err)
	}
	result1, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week1.ID)
	if err != nil || result1 == nil || result1.MassWipeout {
		t.Fatalf("TryFinalizeLeagueWeek(week1): result=%+v err=%v, want a non-mass-wipeout finalization", result1, err)
	}

	playerAfterWeek1 := getMembership(t, env, player.ID)
	if playerAfterWeek1.Status != "eliminated" {
		t.Fatalf("player status after week1 = %q, want %q", playerAfterWeek1.Status, "eliminated")
	}
	if playerAfterWeek1.EliminatedGameID != game1.ID {
		t.Fatalf("player eliminated_game_id after week1 = %v, want %v", playerAfterWeek1.EliminatedGameID, game1.ID)
	}

	// Commissioner buys the player back.
	boughtBack, err := env.leagues.BuyBackMember(context.Background(), league.ID, player.ID, commissioner.UserID)
	if err != nil {
		t.Fatalf("BuyBackMember: %v", err)
	}
	if boughtBack.Status != "active" || !boughtBack.BoughtBack {
		t.Fatalf("membership after buy-back = %+v, want active+bought_back", boughtBack)
	}

	// The leaderboard must now show the reinstated member as active with
	// bought_back=true (this is what GET /leagues/:id/leaderboard surfaces).
	rows, err := env.leagues.ListLeaderboard(context.Background(), league.ID)
	if err != nil {
		t.Fatalf("ListLeaderboard: %v", err)
	}
	var sawPlayer bool
	for _, row := range rows {
		if row.MembershipID == player.ID {
			sawPlayer = true
			if row.Status != "active" {
				t.Errorf("leaderboard row status = %q, want %q", row.Status, "active")
			}
			if !row.BoughtBack {
				t.Error("leaderboard row BoughtBack = false, want true")
			}
		}
	}
	if !sawPlayer {
		t.Fatal("reinstated player missing from leaderboard")
	}

	// Week 2: player picks the loser again (with a fresh, never-used team)
	// and the commissioner picks the winner (with a fresh team too, so
	// this isn't a mass wipeout) — this drives a REAL second elimination
	// through the grading pipeline.
	week2 := createTestWeek(t, env.q, seasonYear, 2)
	teamC := createTestTeam(t, env.q, "Team C", "Big Ten") // week2 winner
	teamD := createTestTeam(t, env.q, "Team D", "Big Ten") // week2 loser
	game2 := createTestGame(t, env.q, week2, teamC, teamD, time.Now().Add(72*time.Hour))
	pick(t, env, commissioner.ID, week2.ID, league.Conference, game2.ID, teamC.ID) // wins
	pick(t, env, player.ID, week2.ID, league.Conference, game2.ID, teamD.ID)       // loses again
	finalizeGame(t, env.pool, game2.ID, teamC.ID, 21, 17)
	if _, err := env.grading.GradeGame(context.Background(), game2.ID); err != nil {
		t.Fatalf("GradeGame(game2): %v", err)
	}
	result2, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week2.ID)
	if err != nil || result2 == nil || result2.MassWipeout {
		t.Fatalf("TryFinalizeLeagueWeek(week2): result=%+v err=%v, want a non-mass-wipeout finalization", result2, err)
	}

	playerAfterWeek2 := getMembership(t, env, player.ID)
	if playerAfterWeek2.Status != "eliminated" {
		t.Fatalf("player status after week2 = %q, want %q (real second elimination via the grading pipeline)", playerAfterWeek2.Status, "eliminated")
	}
	if playerAfterWeek2.EliminatedGameID != game2.ID {
		t.Errorf("player eliminated_game_id after week2 = %v, want %v (this week's elimination overwrites the prior one)", playerAfterWeek2.EliminatedGameID, game2.ID)
	}
	if !playerAfterWeek2.BoughtBack {
		t.Error("player BoughtBack flipped to false by the second elimination, want it to remain true (permanent record)")
	}

	// The second buy-back attempt must be rejected — the flag, not current
	// status, is what gates this.
	if _, err := env.leagues.BuyBackMember(context.Background(), league.ID, player.ID, commissioner.UserID); !errors.Is(err, leagues.ErrAlreadyBoughtBack) {
		t.Fatalf("second BuyBackMember after a real re-elimination: got err %v, want %v", err, leagues.ErrAlreadyBoughtBack)
	}

	// And the rejected attempt must be a pure no-op.
	playerFinal := getMembership(t, env, player.ID)
	if playerFinal.Status != "eliminated" {
		t.Errorf("player status after rejected second buy-back = %q, want unchanged %q", playerFinal.Status, "eliminated")
	}
}

// TestBuyBack_UsedTeamsStayLockedAfterReinstatement confirms the plan's
// explicitly-called-out "zero new code" claim: a team the member already
// used before their original elimination stays locked after buy-back,
// purely because of the pre-existing UNIQUE(league_membership_id, team_id)
// constraint Phase 4's picks.UpsertPick already relies on — no buy-back-
// specific logic is needed to enforce this. Also confirms a fresh,
// never-used team can still be picked post-reinstatement.
func TestBuyBack_UsedTeamsStayLockedAfterReinstatement(t *testing.T) {
	env := newTestEnv(t)
	league, commissioner := createLeague(t, env, "Buyback Used Teams Locked")
	player := addPlayer(t, env, league, "player")

	seasonYear := uniqueSeasonYear()
	week1 := createTestWeek(t, env.q, seasonYear, 1)
	teamA := createTestTeam(t, env.q, "Team A", "Big Ten")
	usedTeam := createTestTeam(t, env.q, "Used Team", "Big Ten")
	game1 := createTestGame(t, env.q, week1, teamA, usedTeam, time.Now().Add(48*time.Hour))

	pick(t, env, commissioner.ID, week1.ID, league.Conference, game1.ID, teamA.ID)
	pick(t, env, player.ID, week1.ID, league.Conference, game1.ID, usedTeam.ID) // this is the team that stays "used"
	finalizeGame(t, env.pool, game1.ID, teamA.ID, 24, 20)
	if _, err := env.grading.GradeGame(context.Background(), game1.ID); err != nil {
		t.Fatalf("GradeGame: %v", err)
	}
	result, err := env.grading.TryFinalizeLeagueWeek(context.Background(), league.ID, week1.ID)
	if err != nil || result == nil || result.MassWipeout {
		t.Fatalf("TryFinalizeLeagueWeek: result=%+v err=%v, want a non-mass-wipeout finalization", result, err)
	}
	if getMembership(t, env, player.ID).Status != "eliminated" {
		t.Fatal("setup: player not eliminated after week1")
	}

	if _, err := env.leagues.BuyBackMember(context.Background(), league.ID, player.ID, commissioner.UserID); err != nil {
		t.Fatalf("BuyBackMember: %v", err)
	}

	// Week 2: a new game features usedTeam again (as the away side this
	// time) alongside a brand-new, never-picked team.
	week2 := createTestWeek(t, env.q, seasonYear, 2)
	freshTeam := createTestTeam(t, env.q, "Fresh Team", "Big Ten")
	game2 := createTestGame(t, env.q, week2, freshTeam, usedTeam, time.Now().Add(72*time.Hour))

	// Re-picking the already-used team must be rejected with
	// ErrTeamAlreadyUsed, exactly as it would for any other repeat pick —
	// buy-back grants no exception here.
	if _, err := env.picks.UpsertPick(context.Background(), player.ID, week2.ID, league.Conference, game2.ID, usedTeam.ID); !errors.Is(err, picks.ErrTeamAlreadyUsed) {
		t.Fatalf("UpsertPick(usedTeam) after reinstatement: got err %v, want %v", err, picks.ErrTeamAlreadyUsed)
	}

	// But a fresh, never-used team must be pickable normally.
	freshPick, err := env.picks.UpsertPick(context.Background(), player.ID, week2.ID, league.Conference, game2.ID, freshTeam.ID)
	if err != nil {
		t.Fatalf("UpsertPick(freshTeam) after reinstatement: %v", err)
	}
	if freshPick.TeamID != freshTeam.ID {
		t.Errorf("freshPick.TeamID = %v, want %v", freshPick.TeamID, freshTeam.ID)
	}
}
