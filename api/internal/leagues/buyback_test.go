package leagues

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// eliminateTestMembership directly flips a membership to eliminated via the
// same EliminateMembership query internal/grading uses — mirrors
// TestService_ListLeaderboard_SortOrder's own use of this pattern, since
// this package has no grading dependency of its own (grading depends on
// leagues, not the reverse). Buy-back's validation-only tests here just
// need "a membership currently in eliminated status" as a starting point;
// the full grading-pipeline round trip (eliminate -> buy back -> real
// re-elimination via a graded loss -> reject second buy-back) lives in
// internal/grading, which can drive the real pipeline.
func eliminateTestMembership(t *testing.T, q *gen.Queries, week gen.Week, membershipID pgtype.UUID) gen.LeagueMembership {
	t.Helper()
	m, err := q.EliminateMembership(context.Background(), gen.EliminateMembershipParams{
		ID:     membershipID,
		WeekID: week.ID,
	})
	if err != nil {
		t.Fatalf("EliminateMembership: %v", err)
	}
	return m
}

func testWeek(t *testing.T, q *gen.Queries, weekNumber int32) gen.Week {
	t.Helper()
	seasonYear := int32(95000 + int(time.Now().UnixNano()%4000))
	week, err := q.UpsertWeek(context.Background(), gen.UpsertWeekParams{SeasonYear: seasonYear, WeekNumber: weekNumber})
	if err != nil {
		t.Fatalf("UpsertWeek: %v", err)
	}
	return week
}

// TestService_BuyBackMember_HappyPath confirms the field-update contract:
// status flips to active, bought_back/bought_back_at/bought_back_by are
// set, and eliminated_week_id/eliminated_game_id are left untouched as the
// historical record of the elimination that was bought back.
func TestService_BuyBackMember_HappyPath(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	player := createTestUser(t, q, "player")
	league, _ := createTestLeague(t, s, commissioner)

	m, err := s.JoinByCode(context.Background(), league.ID, player.ID)
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}

	week := testWeek(t, q, 1)
	eliminated := eliminateTestMembership(t, q, week, m.ID)
	if eliminated.Status != "eliminated" {
		t.Fatalf("setup: membership status = %q, want eliminated", eliminated.Status)
	}

	updated, err := s.BuyBackMember(context.Background(), league.ID, m.ID, commissioner.ID)
	if err != nil {
		t.Fatalf("BuyBackMember: %v", err)
	}
	if updated.Status != "active" {
		t.Errorf("Status = %q, want %q", updated.Status, "active")
	}
	if !updated.BoughtBack {
		t.Error("BoughtBack = false, want true")
	}
	if !updated.BoughtBackAt.Valid {
		t.Error("BoughtBackAt not set")
	}
	if updated.BoughtBackBy != commissioner.ID {
		t.Errorf("BoughtBackBy = %v, want commissioner %v", updated.BoughtBackBy, commissioner.ID)
	}
	if updated.EliminatedWeekID != week.ID {
		t.Errorf("EliminatedWeekID = %v, want preserved %v (historical record, not cleared)", updated.EliminatedWeekID, week.ID)
	}

	// And the DB reflects it too, not just the returned row.
	fresh, err := s.GetMembershipByID(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("GetMembershipByID: %v", err)
	}
	if fresh.Status != "active" || !fresh.BoughtBack {
		t.Errorf("persisted membership = %+v, want active+bought_back", fresh)
	}
}

// TestService_BuyBackMember_RejectsStillActiveMember confirms an
// un-eliminated member (never eliminated at all) is rejected with
// ErrNotEliminated — nothing to buy back.
func TestService_BuyBackMember_RejectsStillActiveMember(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	player := createTestUser(t, q, "player")
	league, _ := createTestLeague(t, s, commissioner)

	m, err := s.JoinByCode(context.Background(), league.ID, player.ID)
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}

	if _, err := s.BuyBackMember(context.Background(), league.ID, m.ID, commissioner.ID); !errors.Is(err, ErrNotEliminated) {
		t.Fatalf("BuyBackMember on active member: got err %v, want %v", err, ErrNotEliminated)
	}
}

// TestService_BuyBackMember_CrossLeagueMembershipIsNotFound mirrors
// TestService_RemoveMember_WrongLeagueIsNotFound: a membershipId that
// belongs to a different league than the one in the URL must not resolve,
// and must leave the membership completely untouched.
func TestService_BuyBackMember_CrossLeagueMembershipIsNotFound(t *testing.T) {
	s, q := newTestService(t)
	commissioner1 := createTestUser(t, q, "commish1")
	commissioner2 := createTestUser(t, q, "commish2")
	player := createTestUser(t, q, "player")

	league1, _ := createTestLeague(t, s, commissioner1)
	league2, _ := createTestLeague(t, s, commissioner2)

	m, err := s.JoinByCode(context.Background(), league1.ID, player.ID)
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}

	week := testWeek(t, q, 1)
	eliminateTestMembership(t, q, week, m.ID)

	if _, err := s.BuyBackMember(context.Background(), league2.ID, m.ID, commissioner2.ID); !errors.Is(err, ErrMembershipNotFound) {
		t.Fatalf("BuyBackMember across leagues: got err %v, want %v", err, ErrMembershipNotFound)
	}

	still, err := s.GetMembershipByID(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("GetMembershipByID: %v", err)
	}
	if still.Status != "eliminated" || still.BoughtBack {
		t.Errorf("membership after cross-league buyback attempt = %+v, want unchanged eliminated/not-bought-back", still)
	}
}

// TestService_BuyBackMember_RejectsSecondBuyBackAfterReElimination directly
// exercises the "checked via the bought_back flag itself, not current
// status" rule at the unit level: after a successful buy-back, the member
// is (here, via direct SQL — the real-pipeline version of this same
// scenario lives in internal/grading's full-cycle test) eliminated a
// second time. A second buy-back attempt must reject with
// ErrAlreadyBoughtBack, not silently succeed just because status is
// 'eliminated' again.
func TestService_BuyBackMember_RejectsSecondBuyBackAfterReElimination(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	player := createTestUser(t, q, "player")
	league, _ := createTestLeague(t, s, commissioner)

	m, err := s.JoinByCode(context.Background(), league.ID, player.ID)
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}

	week1 := testWeek(t, q, 1)
	eliminateTestMembership(t, q, week1, m.ID)

	if _, err := s.BuyBackMember(context.Background(), league.ID, m.ID, commissioner.ID); err != nil {
		t.Fatalf("first BuyBackMember: %v", err)
	}

	week2 := testWeek(t, q, 2)
	eliminateTestMembership(t, q, week2, m.ID)

	if _, err := s.BuyBackMember(context.Background(), league.ID, m.ID, commissioner.ID); !errors.Is(err, ErrAlreadyBoughtBack) {
		t.Fatalf("second BuyBackMember after re-elimination: got err %v, want %v", err, ErrAlreadyBoughtBack)
	}

	// bought_back must still be true (a rejected second attempt is a
	// pure no-op, not a reset), and status must still reflect the second
	// elimination (untouched by the rejected buy-back attempt).
	fresh, err := s.GetMembershipByID(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("GetMembershipByID: %v", err)
	}
	if !fresh.BoughtBack {
		t.Error("BoughtBack flipped to false after a rejected second buy-back attempt")
	}
	if fresh.Status != "eliminated" {
		t.Errorf("Status = %q after a rejected second buy-back attempt, want unchanged %q", fresh.Status, "eliminated")
	}
}
