package leagues

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

var (
	// ErrLeagueNotFound is returned when a league lookup (by id or invite
	// code) finds nothing.
	ErrLeagueNotFound = errors.New("leagues: league not found")
	// ErrMembershipNotFound is returned when a membership lookup (by
	// league+user, for access checks, or by id+league, for removal) finds
	// no non-removed row.
	ErrMembershipNotFound = errors.New("leagues: membership not found")
	// ErrAlreadyMember is returned by JoinByCode when the user already has
	// an active (non-removed) membership in the target league.
	ErrAlreadyMember = errors.New("leagues: user already has an active membership in this league")
	// ErrNotEliminated is returned by BuyBackMember when the target
	// membership's current status isn't 'eliminated' — nothing to buy back,
	// whether because they're still active or were never eliminated.
	ErrNotEliminated = errors.New("leagues: membership is not currently eliminated")
	// ErrAlreadyBoughtBack is returned by BuyBackMember when the target
	// membership's bought_back flag is already true — buy-back is a
	// one-time-ever lifeline, checked against this flag rather than current
	// status history, so a member eliminated a second time after being
	// bought back does not get a second one.
	ErrAlreadyBoughtBack = errors.New("leagues: membership has already used its one-time buy-back")
)

// Service implements league CRUD, membership, and invite-code operations
// on top of the sqlc-generated queries.
type Service struct {
	queries *gen.Queries
	pool    *pgxpool.Pool
}

// NewService constructs a Service. pool is used only for the
// CreateLeague transaction (league row + the commissioner's own
// membership row must be created atomically); every other method runs a
// single statement through queries.
func NewService(queries *gen.Queries, pool *pgxpool.Pool) *Service {
	return &Service{queries: queries, pool: pool}
}

// CreateLeague creates a new league with a fresh unique invite code and,
// in the same transaction, the commissioner's own active-contestant
// membership row — per the plan's confirmed rule, there is no separate
// join step for the league's creator.
func (s *Service) CreateLeague(ctx context.Context, commissionerUserID pgtype.UUID, name string, seasonYear int32, conference string) (gen.League, gen.LeagueMembership, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.League{}, gen.LeagueMembership{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	qtx := s.queries.WithTx(tx)

	code, err := generateUniqueInviteCode(ctx, func(ctx context.Context, candidate string) (bool, error) {
		return qtx.LeagueInviteCodeExists(ctx, candidate)
	})
	if err != nil {
		return gen.League{}, gen.LeagueMembership{}, err
	}

	league, err := qtx.CreateLeague(ctx, gen.CreateLeagueParams{
		Name:               name,
		SeasonYear:         seasonYear,
		Conference:         conference,
		CommissionerUserID: commissionerUserID,
		InviteCode:         code,
	})
	if err != nil {
		return gen.League{}, gen.LeagueMembership{}, err
	}

	membership, err := qtx.CreateLeagueMembership(ctx, gen.CreateLeagueMembershipParams{
		LeagueID:     league.ID,
		UserID:       commissionerUserID,
		Role:         "commissioner",
		IsContestant: true,
	})
	if err != nil {
		return gen.League{}, gen.LeagueMembership{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return gen.League{}, gen.LeagueMembership{}, err
	}

	return league, membership, nil
}

// ListLeaguesForUser returns every league the user has a non-removed
// membership in, each row carrying the user's role/is_contestant/status
// in that league alongside the league itself.
func (s *Service) ListLeaguesForUser(ctx context.Context, userID pgtype.UUID) ([]gen.ListLeaguesForUserRow, error) {
	return s.queries.ListLeaguesForUser(ctx, userID)
}

// GetLeagueByID looks up a league by id, mapping "no rows" to
// ErrLeagueNotFound.
func (s *Service) GetLeagueByID(ctx context.Context, id pgtype.UUID) (gen.League, error) {
	league, err := s.queries.GetLeagueByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.League{}, ErrLeagueNotFound
		}
		return gen.League{}, err
	}
	return league, nil
}

// GetLeagueByInviteCode looks up a league by its current invite code,
// mapping "no rows" (including a since-regenerated old code) to
// ErrLeagueNotFound.
func (s *Service) GetLeagueByInviteCode(ctx context.Context, code string) (gen.League, error) {
	league, err := s.queries.GetLeagueByInviteCode(ctx, code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.League{}, ErrLeagueNotFound
		}
		return gen.League{}, err
	}
	return league, nil
}

// GetActiveMembership looks up a user's non-removed membership in a
// league, mapping "no rows" to ErrMembershipNotFound. Backs
// requireLeagueMember; note that an eliminated (status='eliminated')
// member still has a valid row here — only removed_at excludes access.
func (s *Service) GetActiveMembership(ctx context.Context, leagueID, userID pgtype.UUID) (gen.LeagueMembership, error) {
	m, err := s.queries.GetMembershipByLeagueAndUser(ctx, gen.GetMembershipByLeagueAndUserParams{
		LeagueID: leagueID,
		UserID:   userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.LeagueMembership{}, ErrMembershipNotFound
		}
		return gen.LeagueMembership{}, err
	}
	return m, nil
}

// GetMembershipByID looks up a membership directly by its own id (not
// scoped to a league/user pair), mapping "no rows" to
// ErrMembershipNotFound. Used wherever a caller already has a membership
// id in hand (e.g. tests inspecting a grading pipeline's elimination
// side-effects) rather than a (league, user) pair.
func (s *Service) GetMembershipByID(ctx context.Context, id pgtype.UUID) (gen.LeagueMembership, error) {
	m, err := s.queries.GetLeagueMembershipByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.LeagueMembership{}, ErrMembershipNotFound
		}
		return gen.LeagueMembership{}, err
	}
	return m, nil
}

// UpdateLeagueName updates a league's name. conference/season_year are
// immutable and have no corresponding update method.
func (s *Service) UpdateLeagueName(ctx context.Context, id pgtype.UUID, name string) (gen.League, error) {
	return s.queries.UpdateLeagueName(ctx, gen.UpdateLeagueNameParams{ID: id, Name: name})
}

// UpdateCommissionerIsContestant updates a single membership's
// is_contestant flag. Callers must scope this to the commissioner's own
// membership id — there is no general member-editing mechanism.
func (s *Service) UpdateCommissionerIsContestant(ctx context.Context, membershipID pgtype.UUID, isContestant bool) (gen.LeagueMembership, error) {
	return s.queries.UpdateCommissionerIsContestant(ctx, gen.UpdateCommissionerIsContestantParams{
		ID:           membershipID,
		IsContestant: isContestant,
	})
}

// ListMembers returns every non-removed member of a league, joined with
// their display name, ordered by join date.
func (s *Service) ListMembers(ctx context.Context, leagueID pgtype.UUID) ([]gen.ListActiveMembersWithUserRow, error) {
	return s.queries.ListActiveMembersWithUser(ctx, leagueID)
}

// ListLeaderboard returns every non-removed member of a league in
// standings order: active first, then eliminated members ordered by how
// late they were eliminated (survived longer ranks higher). Backs
// GET /leagues/:id/leaderboard — see ListLeaderboardForLeague's query
// comment for the exact sort contract.
func (s *Service) ListLeaderboard(ctx context.Context, leagueID pgtype.UUID) ([]gen.ListLeaderboardForLeagueRow, error) {
	return s.queries.ListLeaderboardForLeague(ctx, leagueID)
}

// RemoveMember soft-deletes a membership (sets removed_at) scoped to the
// given league. Returns ErrMembershipNotFound if membershipID doesn't
// resolve to a currently-active row in that league (wrong league, already
// removed, or nonexistent) — callers should not distinguish these cases in
// the response, to keep the error surface simple per the API contract.
func (s *Service) RemoveMember(ctx context.Context, leagueID, membershipID pgtype.UUID) (gen.LeagueMembership, error) {
	m, err := s.queries.RemoveMembership(ctx, gen.RemoveMembershipParams{
		ID:       membershipID,
		LeagueID: leagueID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.LeagueMembership{}, ErrMembershipNotFound
		}
		return gen.LeagueMembership{}, err
	}
	return m, nil
}

// RegenerateInviteCode generates a fresh unique invite code and persists
// it, overwriting (not appending to) the league's current code — the old
// code stops working immediately since it's no longer stored anywhere.
func (s *Service) RegenerateInviteCode(ctx context.Context, leagueID pgtype.UUID) (gen.League, error) {
	code, err := generateUniqueInviteCode(ctx, func(ctx context.Context, candidate string) (bool, error) {
		return s.queries.LeagueInviteCodeExists(ctx, candidate)
	})
	if err != nil {
		return gen.League{}, err
	}
	return s.queries.UpdateLeagueInviteCode(ctx, gen.UpdateLeagueInviteCodeParams{
		ID:         leagueID,
		InviteCode: code,
	})
}

// JoinByCode creates (or, for a previously-removed member, revives) an
// active player membership for userID in leagueID. Returns
// ErrAlreadyMember if the user already has a non-removed membership in
// that league — see UpsertLeagueMembershipOnJoin's doc comment in
// internal/db/queries/league_memberships.sql for exactly how the
// rejoin-after-removal case is handled against the
// UNIQUE(league_id, user_id) constraint.
func (s *Service) JoinByCode(ctx context.Context, leagueID, userID pgtype.UUID) (gen.LeagueMembership, error) {
	m, err := s.queries.UpsertLeagueMembershipOnJoin(ctx, gen.UpsertLeagueMembershipOnJoinParams{
		LeagueID: leagueID,
		UserID:   userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.LeagueMembership{}, ErrAlreadyMember
		}
		return gen.LeagueMembership{}, err
	}
	return m, nil
}

// BuyBackMember reinstates an eliminated member (status -> 'active') on
// the commissioner's one-time-per-member buy-back lifeline. Validates, in
// order:
//  1. membershipID resolves to a currently non-removed member of leagueID
//     (ErrMembershipNotFound otherwise — same cross-league/nonexistent/
//     already-removed collapse as RemoveMember).
//  2. that member's current status is 'eliminated' (ErrNotEliminated
//     otherwise — nothing to buy back, whether still active or never
//     eliminated).
//  3. that member's bought_back flag is still false (ErrAlreadyBoughtBack
//     otherwise — buy-back is one-time-ever, checked against this flag,
//     not current status, so a second elimination after a buy-back never
//     grants a second one).
//
// On success, updates status/bought_back/bought_back_at/bought_back_by in
// one statement (BuyBackMembership's WHERE guard also protects against a
// concurrent double-buy-back race slipping past steps 2/3 above) and
// writes an audit_log row, following the same commissioner-privileged-
// action pattern as internal/admin's TriggerScheduleSync.
// eliminated_week_id/eliminated_game_id are left untouched by the update
// — they remain the historical record of the elimination that was bought
// back.
func (s *Service) BuyBackMember(ctx context.Context, leagueID, membershipID, actorUserID pgtype.UUID) (gen.LeagueMembership, error) {
	current, err := s.queries.GetMembershipByIDAndLeague(ctx, gen.GetMembershipByIDAndLeagueParams{
		ID:       membershipID,
		LeagueID: leagueID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.LeagueMembership{}, ErrMembershipNotFound
		}
		return gen.LeagueMembership{}, err
	}
	if current.Status != "eliminated" {
		return gen.LeagueMembership{}, ErrNotEliminated
	}
	if current.BoughtBack {
		return gen.LeagueMembership{}, ErrAlreadyBoughtBack
	}

	updated, err := s.queries.BuyBackMembership(ctx, gen.BuyBackMembershipParams{
		ID:           membershipID,
		LeagueID:     leagueID,
		BoughtBackBy: actorUserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Lost a race against a concurrent buy-back attempt (or removal)
			// between the checks above and this update.
			return gen.LeagueMembership{}, ErrAlreadyBoughtBack
		}
		return gen.LeagueMembership{}, err
	}

	metadata := map[string]any{
		"eliminated_game_id": pgUUIDStringOrNil(current.EliminatedGameID),
	}
	if current.EliminatedWeekID.Valid {
		metadata["eliminated_week_id"] = db.UUIDString(current.EliminatedWeekID)
		if week, werr := s.queries.GetWeekByID(ctx, current.EliminatedWeekID); werr == nil {
			metadata["eliminated_week_number"] = week.WeekNumber
			metadata["season_year"] = week.SeasonYear
		}
	}
	metadataJSON, merr := json.Marshal(metadata)
	if merr != nil {
		return gen.LeagueMembership{}, fmt.Errorf("leagues: marshal buyback audit_log metadata: %w", merr)
	}
	if _, err := s.queries.CreateAuditLog(ctx, gen.CreateAuditLogParams{
		ActorUserID: actorUserID,
		LeagueID:    leagueID,
		Action:      "buyback",
		TargetType:  pgtype.Text{String: "league_membership", Valid: true},
		TargetID:    membershipID,
		Metadata:    metadataJSON,
	}); err != nil {
		return gen.LeagueMembership{}, fmt.Errorf("leagues: write buyback audit_log row: %w", err)
	}

	return updated, nil
}

// pgUUIDStringOrNil returns the UUID's string form, or nil for a JSON
// metadata value when the UUID is unset (e.g. eliminated_game_id for a
// missed-pick elimination, which has no game to point at).
func pgUUIDStringOrNil(v pgtype.UUID) any {
	if !v.Valid {
		return nil
	}
	return db.UUIDString(v)
}
