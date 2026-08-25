package leagues

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

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
	// ErrLeagueAlreadyClosed is returned by CloseLeague when the league's
	// status is already 'closed'.
	ErrLeagueAlreadyClosed = errors.New("leagues: league is already closed")
	// ErrTeamNameRequired is returned by CreateLeague/JoinByCode/
	// UpdateTeamName for a blank (or whitespace-only) team name — required
	// at every join/create going forward, per the product decision that
	// every league should have team names from here on (existing
	// memberships from before this shipped are backfilled via the
	// one-time prompt calling UpdateTeamName, not exempted from this rule
	// going forward).
	ErrTeamNameRequired = errors.New("leagues: team name cannot be empty")
	// ErrTeamNameTooLong is returned when a trimmed team name exceeds
	// maxTeamNameLength.
	ErrTeamNameTooLong = errors.New("leagues: team name too long")
	// ErrBuyBackWindowClosed is returned by BuyBackMember once the
	// league's traditional buy-back cutoff has passed — see
	// buyBackCutoffWeekNumber.
	ErrBuyBackWindowClosed = errors.New("leagues: buy-backs are no longer allowed once the cutoff week's games have begun")
)

// maxTeamNameLength is a sanity cap, not a design constraint — mirrors
// internal/chat's identical reasoning for its own message-length cap.
const maxTeamNameLength = 60

// buyBackCutoffWeekNumber is the commissioner's traditional buy-back
// deadline: a buy-back is only allowed up until this week's first game
// kicks off, after which the lifeline is off the table for the rest of
// the season regardless of when a member was eliminated.
const buyBackCutoffWeekNumber = 5

// validateTeamName trims and validates a team name, shared by
// CreateLeague, JoinByCode, and UpdateTeamName so the same rule can never
// drift between the three entry points.
func validateTeamName(teamName string) (string, error) {
	trimmed := strings.TrimSpace(teamName)
	if trimmed == "" {
		return "", ErrTeamNameRequired
	}
	if len(trimmed) > maxTeamNameLength {
		return "", ErrTeamNameTooLong
	}
	return trimmed, nil
}

// Notifier is the notification-enqueueing surface BuyBackMember calls
// into (Phase 7) once a reinstatement has succeeded. Deliberately a small
// local interface rather than an import of internal/notify — *notify.Service
// satisfies this structurally, so this package stays decoupled from
// notify's own dependencies. A nil Notifier (every pre-Phase-7 test that
// constructs a Service via NewService without WithNotifier) is a valid,
// silent no-op.
type Notifier interface {
	EnqueueBuyback(ctx context.Context, membershipID, leagueID pgtype.UUID) error
}

// Service implements league CRUD, membership, and invite-code operations
// on top of the sqlc-generated queries.
type Service struct {
	queries  *gen.Queries
	pool     *pgxpool.Pool
	notifier Notifier
}

// Option configures a Service at construction time.
type Option func(*Service)

// WithNotifier wires a Notifier into the Service — see the Notifier type
// doc comment. Omit in any test that doesn't care about the Phase 7
// notification side effects of a buy-back.
func WithNotifier(n Notifier) Option {
	return func(s *Service) { s.notifier = n }
}

// NewService constructs a Service. pool is used only for the
// CreateLeague transaction (league row + the commissioner's own
// membership row must be created atomically); every other method runs a
// single statement through queries.
func NewService(queries *gen.Queries, pool *pgxpool.Pool, opts ...Option) *Service {
	s := &Service{queries: queries, pool: pool}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreateLeague creates a new league with a fresh unique invite code and,
// in the same transaction, the commissioner's own active-contestant
// membership row — per the plan's confirmed rule, there is no separate
// join step for the league's creator. teamName is required (see
// validateTeamName) — the commissioner sets their own team name at
// creation time, same as any other joiner does via JoinByCode.
func (s *Service) CreateLeague(ctx context.Context, commissionerUserID pgtype.UUID, name string, seasonYear int32, conference string, teamName string) (gen.League, gen.LeagueMembership, error) {
	teamName, err := validateTeamName(teamName)
	if err != nil {
		return gen.League{}, gen.LeagueMembership{}, err
	}

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
		TeamName:     pgtype.Text{String: teamName, Valid: true},
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

// ListMemberEmails returns every non-removed member's user id/email/
// display name for a league — the commissioner-only surface backing both
// "copy all emails" and the league-wide broadcast email feature. Reuses
// the same query CloseLeague already relies on for its closed-league
// notification email.
func (s *Service) ListMemberEmails(ctx context.Context, leagueID pgtype.UUID) ([]gen.ListLeagueMemberEmailsRow, error) {
	return s.queries.ListLeagueMemberEmails(ctx, leagueID)
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

// IsSeasonComplete reports whether every conference-relevant game synced
// for leagueID's own season/conference has reached exactly 'final' — the
// same terminal-status bar internal/grading.TryFinalizeLeagueWeek applies
// per week, just checked across the whole season at once. Drives the
// frontend's co-champions banner: per the product's confirmed tiebreaker
// rule, a season that ends with more than one contestant still active
// makes them all co-champions rather than triggering a sudden-death
// week. Returns false (not complete) whenever no games are synced yet at
// all for this season/conference — "no data" is never "season over."
func (s *Service) IsSeasonComplete(ctx context.Context, leagueID pgtype.UUID) (bool, error) {
	league, err := s.GetLeagueByID(ctx, leagueID)
	if err != nil {
		return false, err
	}
	counts, err := s.queries.CountUnfinishedConferenceGamesForSeason(ctx, gen.CountUnfinishedConferenceGamesForSeasonParams{
		SeasonYear: league.SeasonYear,
		Conference: league.Conference,
	})
	if err != nil {
		return false, err
	}
	return counts.Total > 0 && counts.Unfinished == 0, nil
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
func (s *Service) JoinByCode(ctx context.Context, leagueID, userID pgtype.UUID, teamName string) (gen.LeagueMembership, error) {
	teamName, err := validateTeamName(teamName)
	if err != nil {
		return gen.LeagueMembership{}, err
	}

	m, err := s.queries.UpsertLeagueMembershipOnJoin(ctx, gen.UpsertLeagueMembershipOnJoinParams{
		LeagueID: leagueID,
		UserID:   userID,
		TeamName: pgtype.Text{String: teamName, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.LeagueMembership{}, ErrAlreadyMember
		}
		return gen.LeagueMembership{}, err
	}
	return m, nil
}

// UpdateTeamName sets or changes a member's own team name in leagueID at
// any time — backs PATCH /leagues/:id/team-name. Used both for the
// one-time backfill prompt (a pre-existing membership with team_name
// still NULL) and ordinary renaming later; there's no distinction between
// the two at this layer. ErrMembershipNotFound if userID has no non-removed
// membership in leagueID.
func (s *Service) UpdateTeamName(ctx context.Context, leagueID, userID pgtype.UUID, teamName string) (gen.LeagueMembership, error) {
	teamName, err := validateTeamName(teamName)
	if err != nil {
		return gen.LeagueMembership{}, err
	}

	m, err := s.queries.UpdateTeamName(ctx, gen.UpdateTeamNameParams{
		LeagueID: leagueID,
		UserID:   userID,
		TeamName: pgtype.Text{String: teamName, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.LeagueMembership{}, ErrMembershipNotFound
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
//  4. the league's traditional buy-back window (before
//     buyBackCutoffWeekNumber's first kickoff) hasn't closed yet
//     (ErrBuyBackWindowClosed otherwise) — checked against the league's
//     own season/conference schedule, not wall-clock week numbers, so it
//     lines up exactly with when that league's members actually see week
//     5 games lock. A season/conference with no synced week-5 schedule
//     data yet is treated as still open, same permissive default as
//     internal/schedule's IsFirstWeekPickableForConference.
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

	league, err := s.GetLeagueByID(ctx, leagueID)
	if err != nil {
		return gen.LeagueMembership{}, err
	}
	open, err := s.isBuyBackWindowOpen(ctx, league.SeasonYear, league.Conference)
	if err != nil {
		return gen.LeagueMembership{}, err
	}
	if !open {
		return gen.LeagueMembership{}, ErrBuyBackWindowClosed
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

	// Phase 7: enqueue the buyback notification strictly after the update
	// and audit_log write above have succeeded. Deliberately non-fatal —
	// see grading.Service.TryFinalizeLeagueWeek's identical reasoning for
	// why a notify failure must never make an otherwise-successful
	// buy-back look like it failed to the caller.
	if s.notifier != nil {
		if err := s.notifier.EnqueueBuyback(ctx, membershipID, leagueID); err != nil {
			log.Printf("leagues: enqueue buyback notification for membership %s: %v", db.UUIDString(membershipID), err)
		}
	}

	return updated, nil
}

// isBuyBackWindowOpen reports whether buyBackCutoffWeekNumber's first game
// (for this season/conference) has yet to kick off, as of now. No synced
// schedule data for that week (season hasn't reached it yet, or hasn't
// been synced) is treated as still open — never block a commissioner
// action for a reason nobody could see coming, same call as
// internal/schedule's IsFirstWeekPickableForConference.
func (s *Service) isBuyBackWindowOpen(ctx context.Context, seasonYear int32, conference string) (bool, error) {
	rows, err := s.queries.ListWeekKickoffRangesForConference(ctx, gen.ListWeekKickoffRangesForConferenceParams{
		SeasonYear: seasonYear,
		Conference: conference,
	})
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if r.WeekNumber == buyBackCutoffWeekNumber {
			return time.Now().Before(r.MinKickoff.Time), nil
		}
	}
	return true, nil
}

// CloseLeague closes a league: sets status='closed'. This is NOT a
// delete — the league row, its memberships, picks, and full history all
// stay in the database exactly as they were. What "closed" actually
// changes:
//   - httpapi.RequireLeagueOpen rejects every mutating league/pick/join
//     route (new picks, buy-backs, member removal, invite regeneration,
//     joining by code) once a league's status is 'closed'.
//   - Read-only routes (get league, members, leaderboard, pick history)
//     keep working — a closed league stays visible, rendered as disabled
//     in the UI, rather than disappearing.
//
// There is no reopen endpoint exposed to commissioners — from their side
// this is one-way — but the data itself is fully intact underneath and
// could be restored (status flipped back) via direct DB access or a
// future admin tool, deliberately unlike a hard delete.
//
// Returns the closed league and its member list (loaded before the
// close) so the caller can notify former members after the transaction
// commits, or ErrLeagueAlreadyClosed if the league was already closed —
// note this also covers "league doesn't exist", but every caller of this
// method sits behind RequireCommissioner, which already 404s a
// nonexistent league before this is ever reached.
func (s *Service) CloseLeague(ctx context.Context, leagueID, actorUserID pgtype.UUID) (gen.League, []gen.ListLeagueMemberEmailsRow, error) {
	members, err := s.queries.ListLeagueMemberEmails(ctx, leagueID)
	if err != nil {
		return gen.League{}, nil, fmt.Errorf("leagues: list members before close: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.League{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	qtx := s.queries.WithTx(tx)

	closed, err := qtx.CloseLeague(ctx, leagueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.League{}, nil, ErrLeagueAlreadyClosed
		}
		return gen.League{}, nil, err
	}

	metadata := map[string]any{
		"name":         closed.Name,
		"conference":   closed.Conference,
		"season_year":  closed.SeasonYear,
		"member_count": len(members),
	}
	metadataJSON, merr := json.Marshal(metadata)
	if merr != nil {
		return gen.League{}, nil, fmt.Errorf("leagues: marshal close_league audit_log metadata: %w", merr)
	}
	if _, err := qtx.CreateAuditLog(ctx, gen.CreateAuditLogParams{
		ActorUserID: actorUserID,
		LeagueID:    leagueID,
		Action:      "close_league",
		TargetType:  pgtype.Text{String: "league", Valid: true},
		TargetID:    leagueID,
		Metadata:    metadataJSON,
	}); err != nil {
		return gen.League{}, nil, fmt.Errorf("leagues: write close_league audit_log row: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return gen.League{}, nil, err
	}

	return closed, members, nil
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
