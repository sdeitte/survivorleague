package leagues

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// testDatabaseURL sources the Postgres connection string for these
// integration tests from TEST_DATABASE_URL, falling back to DATABASE_URL,
// falling back to the repo's docker-compose default. Tests in this file
// skip (not fail) when no database is reachable, so `go test ./...` still
// passes in environments with no Postgres configured (e.g. a bare `go
// test` without the local docker-compose stack running).
func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
}

func newTestService(t *testing.T) (*Service, *gen.Queries) {
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
	return NewService(q, pool), q
}

func createTestUser(t *testing.T, q *gen.Queries, label string) gen.User {
	t.Helper()
	u, err := q.CreateUser(context.Background(), gen.CreateUserParams{
		Email:        fmt.Sprintf("%s-%d@example.test", label, time.Now().UnixNano()),
		PasswordHash: "test-hash-not-a-real-argon2id-value",
		DisplayName:  label,
		IsSiteAdmin:  false,
	})
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	return u
}

func createTestLeague(t *testing.T, s *Service, commissioner gen.User) (gen.League, gen.LeagueMembership) {
	t.Helper()
	league, membership, err := s.CreateLeague(context.Background(), commissioner.ID, fmt.Sprintf("Test League %d", time.Now().UnixNano()), 2026, "SEC", "Test Team")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}
	return league, membership
}

func TestService_CreateLeague_AutoAddsCommissionerAsContestant(t *testing.T) {
	s, _ := newTestService(t)
	commissioner := createTestUser(t, s.queries, "commish")

	league, membership, err := s.CreateLeague(context.Background(), commissioner.ID, "Auto-Add Test League", 2026, "Big Ten", "Test Team")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}

	if league.CommissionerUserID != commissioner.ID {
		t.Error("league.CommissionerUserID does not match creator")
	}
	if league.Status != "active" {
		t.Errorf("league.Status = %q, want %q", league.Status, "active")
	}
	if len(league.InviteCode) != inviteCodeLength {
		t.Errorf("len(league.InviteCode) = %d, want %d", len(league.InviteCode), inviteCodeLength)
	}

	// The commissioner's own membership row must exist with no separate
	// join step — this is the "no separate join step for the creator"
	// rule from the plan.
	if membership.LeagueID != league.ID {
		t.Error("membership.LeagueID does not match created league")
	}
	if membership.UserID != commissioner.ID {
		t.Error("membership.UserID does not match commissioner")
	}
	if membership.Role != "commissioner" {
		t.Errorf("membership.Role = %q, want %q", membership.Role, "commissioner")
	}
	if !membership.IsContestant {
		t.Error("membership.IsContestant = false, want true (commissioners default to playing contestant)")
	}
	if membership.Status != "active" {
		t.Errorf("membership.Status = %q, want %q", membership.Status, "active")
	}
	if membership.RemovedAt.Valid {
		t.Error("membership.RemovedAt should not be set on a freshly created membership")
	}

	// And it should show up via the "list my leagues" path.
	rows, err := s.ListLeaguesForUser(context.Background(), commissioner.ID)
	if err != nil {
		t.Fatalf("ListLeaguesForUser: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.ID == league.ID {
			found = true
			if row.MemberRole != "commissioner" {
				t.Errorf("ListLeaguesForUser row.MemberRole = %q, want %q", row.MemberRole, "commissioner")
			}
		}
	}
	if !found {
		t.Error("newly created league not present in ListLeaguesForUser")
	}
}

// TestService_JoinByCode_RejoinAfterRemoval exercises the trickiest edge
// case in the invite/membership flow: a member removed by the commissioner
// (removed_at set) must be able to rejoin via the same invite code, and
// doing so must reset their membership to a fresh active state rather than
// erroring on the UNIQUE(league_id, user_id) constraint.
func TestService_JoinByCode_RejoinAfterRemoval(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	player := createTestUser(t, q, "player")
	league, commissionerMembership := createTestLeague(t, s, commissioner)

	// First join.
	m1, err := s.JoinByCode(context.Background(), league.ID, player.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode (first join): %v", err)
	}
	if m1.Role != "player" || !m1.IsContestant || m1.Status != "active" {
		t.Fatalf("unexpected first-join membership: %+v", m1)
	}

	// Commissioner removes them.
	removed, err := s.RemoveMember(context.Background(), league.ID, m1.ID)
	if err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if !removed.RemovedAt.Valid {
		t.Fatal("expected RemovedAt to be set after RemoveMember")
	}

	// Access must now be revoked (this is what requireLeagueMember checks).
	if _, err := s.GetActiveMembership(context.Background(), league.ID, player.ID); !errors.Is(err, ErrMembershipNotFound) {
		t.Fatalf("GetActiveMembership after removal: got err %v, want %v", err, ErrMembershipNotFound)
	}

	// Rejoin via the same invite code must succeed, not fail on the
	// UNIQUE(league_id, user_id) constraint.
	m2, err := s.JoinByCode(context.Background(), league.ID, player.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode (rejoin after removal): %v", err)
	}
	if m2.ID != m1.ID {
		t.Errorf("rejoin membership id = %v, want the same row reactivated (%v)", m2.ID, m1.ID)
	}
	if m2.RemovedAt.Valid {
		t.Error("rejoined membership should have RemovedAt cleared")
	}
	if m2.Role != "player" || !m2.IsContestant || m2.Status != "active" {
		t.Errorf("rejoined membership not reset to a fresh active state: %+v", m2)
	}

	// Access restored.
	active, err := s.GetActiveMembership(context.Background(), league.ID, player.ID)
	if err != nil {
		t.Fatalf("GetActiveMembership after rejoin: %v", err)
	}
	if active.ID != m1.ID {
		t.Errorf("GetActiveMembership after rejoin returned id %v, want %v", active.ID, m1.ID)
	}

	_ = commissionerMembership // used only to construct the league above
}

func TestService_JoinByCode_AlreadyActiveMemberIsConflict(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	player := createTestUser(t, q, "player")
	league, _ := createTestLeague(t, s, commissioner)

	if _, err := s.JoinByCode(context.Background(), league.ID, player.ID, "Test Team"); err != nil {
		t.Fatalf("JoinByCode (first join): %v", err)
	}

	if _, err := s.JoinByCode(context.Background(), league.ID, player.ID, "Test Team"); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("JoinByCode (second join while still active): got err %v, want %v", err, ErrAlreadyMember)
	}
}

// A commissioner is never expected to call JoinByCode on their own league
// (they're auto-added at creation), but if they did, the upsert's WHERE
// removed_at IS NOT NULL guard must protect their commissioner membership
// from being silently downgraded to 'player'.
func TestService_JoinByCode_ProtectsCommissionerOwnMembership(t *testing.T) {
	s, _ := newTestService(t)
	commissioner := createTestUser(t, s.queries, "commish")
	league, commissionerMembership := createTestLeague(t, s, commissioner)

	if _, err := s.JoinByCode(context.Background(), league.ID, commissioner.ID, "Test Team"); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("JoinByCode by the commissioner on their own league: got err %v, want %v", err, ErrAlreadyMember)
	}

	still, err := s.GetActiveMembership(context.Background(), league.ID, commissioner.ID)
	if err != nil {
		t.Fatalf("GetActiveMembership: %v", err)
	}
	if still.Role != "commissioner" {
		t.Errorf("commissioner membership role = %q after a no-op join attempt, want unchanged %q", still.Role, "commissioner")
	}
	if still.ID != commissionerMembership.ID {
		t.Error("commissioner membership id changed after a no-op join attempt")
	}
}

func TestService_RemoveMember_WrongLeagueIsNotFound(t *testing.T) {
	s, q := newTestService(t)
	commissioner1 := createTestUser(t, q, "commish1")
	commissioner2 := createTestUser(t, q, "commish2")
	player := createTestUser(t, q, "player")

	league1, _ := createTestLeague(t, s, commissioner1)
	league2, _ := createTestLeague(t, s, commissioner2)

	m, err := s.JoinByCode(context.Background(), league1.ID, player.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}

	// Attempting to remove a league1 membership scoped to league2 must not
	// succeed.
	if _, err := s.RemoveMember(context.Background(), league2.ID, m.ID); !errors.Is(err, ErrMembershipNotFound) {
		t.Fatalf("RemoveMember across leagues: got err %v, want %v", err, ErrMembershipNotFound)
	}

	// It's still active in the correct league.
	active, err := s.GetActiveMembership(context.Background(), league1.ID, player.ID)
	if err != nil {
		t.Fatalf("GetActiveMembership: %v", err)
	}
	if active.RemovedAt.Valid {
		t.Error("membership should still be active after a cross-league removal attempt")
	}
}

func TestService_ListMembers_ExcludesRemoved(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	playerB := createTestUser(t, q, "playerb")
	playerC := createTestUser(t, q, "playerc")
	league, _ := createTestLeague(t, s, commissioner)

	mb, err := s.JoinByCode(context.Background(), league.ID, playerB.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode B: %v", err)
	}
	if _, err := s.JoinByCode(context.Background(), league.ID, playerC.ID, "Test Team"); err != nil {
		t.Fatalf("JoinByCode C: %v", err)
	}

	if _, err := s.RemoveMember(context.Background(), league.ID, mb.ID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	members, err := s.ListMembers(context.Background(), league.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}

	var sawB, sawC bool
	for _, m := range members {
		if m.UserID == playerB.ID {
			sawB = true
		}
		if m.UserID == playerC.ID {
			sawC = true
		}
	}
	if sawB {
		t.Error("removed member B should not appear in ListMembers")
	}
	if !sawC {
		t.Error("active member C should appear in ListMembers")
	}
}

func TestService_RegenerateInviteCode_InvalidatesOldCode(t *testing.T) {
	s, _ := newTestService(t)
	commissioner := createTestUser(t, s.queries, "commish")
	league, _ := createTestLeague(t, s, commissioner)
	oldCode := league.InviteCode

	updated, err := s.RegenerateInviteCode(context.Background(), league.ID)
	if err != nil {
		t.Fatalf("RegenerateInviteCode: %v", err)
	}
	if updated.InviteCode == oldCode {
		t.Fatal("regenerated invite code is identical to the old one")
	}

	if _, err := s.GetLeagueByInviteCode(context.Background(), oldCode); !errors.Is(err, ErrLeagueNotFound) {
		t.Fatalf("GetLeagueByInviteCode(oldCode): got err %v, want %v", err, ErrLeagueNotFound)
	}

	found, err := s.GetLeagueByInviteCode(context.Background(), updated.InviteCode)
	if err != nil {
		t.Fatalf("GetLeagueByInviteCode(newCode): %v", err)
	}
	if found.ID != league.ID {
		t.Error("new invite code resolves to the wrong league")
	}
}

func TestService_UpdateLeagueName_LeavesImmutableFieldsAlone(t *testing.T) {
	s, _ := newTestService(t)
	commissioner := createTestUser(t, s.queries, "commish")
	league, _ := createTestLeague(t, s, commissioner)

	updated, err := s.UpdateLeagueName(context.Background(), league.ID, "Renamed League")
	if err != nil {
		t.Fatalf("UpdateLeagueName: %v", err)
	}
	if updated.Name != "Renamed League" {
		t.Errorf("Name = %q, want %q", updated.Name, "Renamed League")
	}
	if updated.Conference != league.Conference {
		t.Error("Conference changed by UpdateLeagueName, should be immutable")
	}
	if updated.SeasonYear != league.SeasonYear {
		t.Error("SeasonYear changed by UpdateLeagueName, should be immutable")
	}
}

func TestService_UpdateCommissionerIsContestant(t *testing.T) {
	s, _ := newTestService(t)
	commissioner := createTestUser(t, s.queries, "commish")
	_, membership := createTestLeague(t, s, commissioner)

	updated, err := s.UpdateCommissionerIsContestant(context.Background(), membership.ID, false)
	if err != nil {
		t.Fatalf("UpdateCommissionerIsContestant: %v", err)
	}
	if updated.IsContestant {
		t.Error("IsContestant = true after setting it to false")
	}
	if updated.Role != "commissioner" {
		t.Errorf("Role changed unexpectedly to %q", updated.Role)
	}
}

// TestService_CloseLeague_SetsStatusAndPreservesData is the core
// regression guard for the close-league feature's core design decision:
// closing must NOT delete anything — the league row, its memberships, and
// the member list must all survive untouched apart from status.
func TestService_CloseLeague_SetsStatusAndPreservesData(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, s.queries, "commish")
	league, commMembership := createTestLeague(t, s, commissioner)

	player := createTestUser(t, q, "player")
	if _, err := s.JoinByCode(context.Background(), league.ID, player.ID, "Test Team"); err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}

	closed, members, err := s.CloseLeague(context.Background(), league.ID, commMembership.UserID)
	if err != nil {
		t.Fatalf("CloseLeague: %v", err)
	}
	if closed.Status != "closed" {
		t.Errorf("closed.Status = %q, want %q", closed.Status, "closed")
	}
	if closed.Name != league.Name {
		t.Error("CloseLeague must not change the league name")
	}
	if len(members) != 2 {
		t.Fatalf("len(members) = %d, want 2 (commissioner + player)", len(members))
	}

	// The league row itself must still be fetchable — this is a status
	// flip, not a delete.
	stillThere, err := s.GetLeagueByID(context.Background(), league.ID)
	if err != nil {
		t.Fatalf("GetLeagueByID after close: %v", err)
	}
	if stillThere.Status != "closed" {
		t.Errorf("re-fetched league.Status = %q, want %q", stillThere.Status, "closed")
	}

	// Memberships must survive untouched — closing is not a mass member
	// removal.
	activeMembers, err := s.ListMembers(context.Background(), league.ID)
	if err != nil {
		t.Fatalf("ListMembers after close: %v", err)
	}
	if len(activeMembers) != 2 {
		t.Errorf("len(activeMembers) after close = %d, want 2 — closing must not remove memberships", len(activeMembers))
	}

	// Closing an already-closed league must fail cleanly, not silently
	// no-op or corrupt state.
	if _, _, err := s.CloseLeague(context.Background(), league.ID, commMembership.UserID); !errors.Is(err, ErrLeagueAlreadyClosed) {
		t.Errorf("second CloseLeague error = %v, want ErrLeagueAlreadyClosed", err)
	}
}

// TestService_ListLeaderboard_SortOrder is a Phase 5 test: active members
// must sort ahead of eliminated ones, and among the eliminated, the one
// eliminated LATER (survived longer) must rank higher — confirmed here by
// directly writing elimination state via the same EliminateMembership
// query internal/grading uses, since this package has no grading
// dependency of its own.
func TestService_ListLeaderboard_SortOrder(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish") // stays active
	player4 := createTestUser(t, q, "player4")      // stays active
	player2 := createTestUser(t, q, "player2")      // eliminated week 1 (earlier)
	player3 := createTestUser(t, q, "player3")      // eliminated week 2 (later — survived longer)

	league, commissionerMembership := createTestLeague(t, s, commissioner)

	m4, err := s.JoinByCode(context.Background(), league.ID, player4.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode player4: %v", err)
	}
	m2, err := s.JoinByCode(context.Background(), league.ID, player2.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode player2: %v", err)
	}
	m3, err := s.JoinByCode(context.Background(), league.ID, player3.ID, "Test Team")
	if err != nil {
		t.Fatalf("JoinByCode player3: %v", err)
	}

	seasonYear := int32(92500 + int(time.Now().UnixNano()%2000))
	week1, err := q.UpsertWeek(context.Background(), gen.UpsertWeekParams{SeasonYear: seasonYear, WeekNumber: 1})
	if err != nil {
		t.Fatalf("UpsertWeek 1: %v", err)
	}
	week2, err := q.UpsertWeek(context.Background(), gen.UpsertWeekParams{SeasonYear: seasonYear, WeekNumber: 2})
	if err != nil {
		t.Fatalf("UpsertWeek 2: %v", err)
	}

	if _, err := q.EliminateMembership(context.Background(), gen.EliminateMembershipParams{ID: m2.ID, WeekID: week1.ID}); err != nil {
		t.Fatalf("EliminateMembership player2: %v", err)
	}
	if _, err := q.EliminateMembership(context.Background(), gen.EliminateMembershipParams{ID: m3.ID, WeekID: week2.ID}); err != nil {
		t.Fatalf("EliminateMembership player3: %v", err)
	}

	rows, err := s.ListLeaderboard(context.Background(), league.ID)
	if err != nil {
		t.Fatalf("ListLeaderboard: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("ListLeaderboard returned %d rows, want 4", len(rows))
	}

	// The two active members (order between them is a display_name
	// tie-break, "commish" < "player4") come first, then eliminated
	// members ordered by eliminated_week_id descending: player3 (week2,
	// eliminated later) ranks above player2 (week1).
	wantOrder := []struct {
		membershipID pgtype.UUID
		status       string
	}{
		{commissionerMembership.ID, "active"},
		{m4.ID, "active"},
		{m3.ID, "eliminated"},
		{m2.ID, "eliminated"},
	}
	for i, want := range wantOrder {
		if rows[i].MembershipID != want.membershipID {
			t.Errorf("row %d membership_id = %v, want %v (status=%s)", i, rows[i].MembershipID, want.membershipID, want.status)
		}
		if rows[i].Status != want.status {
			t.Errorf("row %d status = %q, want %q", i, rows[i].Status, want.status)
		}
	}
	if rows[2].EliminatedWeekID != week2.ID {
		t.Errorf("row 2 (player3) eliminated_week_id = %v, want week2 (%v)", rows[2].EliminatedWeekID, week2.ID)
	}
	if rows[3].EliminatedWeekID != week1.ID {
		t.Errorf("row 3 (player2) eliminated_week_id = %v, want week1 (%v)", rows[3].EliminatedWeekID, week1.ID)
	}
	if rows[0].Role != "commissioner" {
		t.Errorf("row 0 (commissioner) role = %q, want %q — the league overview page's member-management list depends on this to gate remove/commissioner-only UI", rows[0].Role, "commissioner")
	}
	if rows[1].Role != "player" {
		t.Errorf("row 1 (player4) role = %q, want %q", rows[1].Role, "player")
	}
	for _, row := range rows {
		if row.BoughtBack {
			t.Errorf("row %s BoughtBack = true, want false (nobody in this fixture was bought back)", row.MembershipID)
		}
	}
}

func TestService_CreateLeague_RequiresTeamName(t *testing.T) {
	s, _ := newTestService(t)
	commissioner := createTestUser(t, s.queries, "commish")

	if _, _, err := s.CreateLeague(context.Background(), commissioner.ID, "No Team Name League", 2026, "Big Ten", "   "); !errors.Is(err, ErrTeamNameRequired) {
		t.Fatalf("CreateLeague with blank team name: got err %v, want %v", err, ErrTeamNameRequired)
	}

	tooLong := ""
	for i := 0; i < maxTeamNameLength+1; i++ {
		tooLong += "a"
	}
	if _, _, err := s.CreateLeague(context.Background(), commissioner.ID, "Too Long Team Name League", 2026, "Big Ten", tooLong); !errors.Is(err, ErrTeamNameTooLong) {
		t.Fatalf("CreateLeague with too-long team name: got err %v, want %v", err, ErrTeamNameTooLong)
	}

	_, membership, err := s.CreateLeague(context.Background(), commissioner.ID, "Trimmed Team Name League", 2026, "Big Ten", "  My Squad  ")
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}
	if membership.TeamName.String != "My Squad" {
		t.Errorf("membership.TeamName = %q, want trimmed %q", membership.TeamName.String, "My Squad")
	}
}

func TestService_JoinByCode_RequiresTeamName(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	player := createTestUser(t, q, "player")
	league, _ := createTestLeague(t, s, commissioner)

	if _, err := s.JoinByCode(context.Background(), league.ID, player.ID, ""); !errors.Is(err, ErrTeamNameRequired) {
		t.Fatalf("JoinByCode with blank team name: got err %v, want %v", err, ErrTeamNameRequired)
	}
}

// A player who was removed and later rejoins can set a different team name
// than they had before — the upsert's reactivate branch must overwrite
// team_name, not just leave the previous one in place.
func TestService_JoinByCode_RejoinSetsNewTeamName(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	player := createTestUser(t, q, "player")
	league, _ := createTestLeague(t, s, commissioner)

	m1, err := s.JoinByCode(context.Background(), league.ID, player.ID, "Original Squad")
	if err != nil {
		t.Fatalf("JoinByCode (first join): %v", err)
	}
	if m1.TeamName.String != "Original Squad" {
		t.Errorf("first-join TeamName = %q, want %q", m1.TeamName.String, "Original Squad")
	}

	if _, err := s.RemoveMember(context.Background(), league.ID, m1.ID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	m2, err := s.JoinByCode(context.Background(), league.ID, player.ID, "New Squad")
	if err != nil {
		t.Fatalf("JoinByCode (rejoin): %v", err)
	}
	if m2.TeamName.String != "New Squad" {
		t.Errorf("rejoin TeamName = %q, want %q", m2.TeamName.String, "New Squad")
	}
}

func TestService_UpdateTeamName(t *testing.T) {
	s, q := newTestService(t)
	commissioner := createTestUser(t, q, "commish")
	player := createTestUser(t, q, "player")
	league, _ := createTestLeague(t, s, commissioner)

	if _, err := s.JoinByCode(context.Background(), league.ID, player.ID, "Old Name"); err != nil {
		t.Fatalf("JoinByCode: %v", err)
	}

	updated, err := s.UpdateTeamName(context.Background(), league.ID, player.ID, "  Better Name  ")
	if err != nil {
		t.Fatalf("UpdateTeamName: %v", err)
	}
	if updated.TeamName.String != "Better Name" {
		t.Errorf("UpdateTeamName result TeamName = %q, want trimmed %q", updated.TeamName.String, "Better Name")
	}

	active, err := s.GetActiveMembership(context.Background(), league.ID, player.ID)
	if err != nil {
		t.Fatalf("GetActiveMembership: %v", err)
	}
	if active.TeamName.String != "Better Name" {
		t.Errorf("GetActiveMembership TeamName = %q, want %q", active.TeamName.String, "Better Name")
	}

	if _, err := s.UpdateTeamName(context.Background(), league.ID, player.ID, ""); !errors.Is(err, ErrTeamNameRequired) {
		t.Fatalf("UpdateTeamName with blank name: got err %v, want %v", err, ErrTeamNameRequired)
	}

	stranger := createTestUser(t, q, "stranger")
	if _, err := s.UpdateTeamName(context.Background(), league.ID, stranger.ID, "Doesn't Matter"); !errors.Is(err, ErrMembershipNotFound) {
		t.Fatalf("UpdateTeamName for a non-member: got err %v, want %v", err, ErrMembershipNotFound)
	}
}
