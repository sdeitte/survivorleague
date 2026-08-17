// Package picks handles weekly pick submission, validation (lock,
// no-repeat-team, conference eligibility), and retrieval.
//
// Phase 4. See the roadmap in
// /Users/sdeitte/.claude/plans/witty-questing-barto.md for context.
//
// Locking rule (the core thing this package has to get exactly right): a
// pick is locked the moment the game backing the membership's CURRENT
// selection for that week has kicked off (games.kickoff_at <= now()) —
// checked live at request time, not a week-level flag. Because picks are
// upserted on (league_membership_id, week_id) — see UpsertPick's ON
// CONFLICT — changing your mind before your current pick locks UPDATEs the
// same row rather than inserting a second one, so the abandoned team is
// immediately free for a different week again. UNIQUE(league_membership_id,
// team_id) is what makes "a team is used while any row anywhere holds it"
// free: no extra query needed to enforce it, just a clean error to surface
// when it fires.
package picks

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// postgresUniqueViolation mirrors internal/auth/service.go's constant —
// kept package-local rather than shared to avoid an odd cross-package
// dependency for a single SQLSTATE string.
const postgresUniqueViolation = "23505"

var (
	// ErrPickNotFound is returned by GetPick when the membership has no
	// pick for the given week yet.
	ErrPickNotFound = errors.New("picks: pick not found")
	// ErrGameNotInWeek is returned when the submitted game_id doesn't
	// belong to the specified week (or doesn't exist at all).
	ErrGameNotInWeek = errors.New("picks: game does not belong to the specified week")
	// ErrTeamNotInGame is returned when the submitted team_id is neither
	// of the game's two teams.
	ErrTeamNotInGame = errors.New("picks: team is not one of this game's two teams")
	// ErrTeamWrongConference is returned when the submitted team doesn't
	// belong to the league's locked conference.
	ErrTeamWrongConference = errors.New("picks: team does not belong to the league's conference")
	// ErrPickLocked is returned when the membership already has a pick for
	// this week and that pick's current game has already kicked off.
	ErrPickLocked = errors.New("picks: pick is already locked (its game has kicked off)")
	// ErrTeamAlreadyUsed is returned when the submitted team is already
	// committed to a different week's pick row for this membership.
	ErrTeamAlreadyUsed = errors.New("picks: team already committed to a different week")
)

// Service implements pick submission/retrieval on top of the
// sqlc-generated queries.
type Service struct {
	queries *gen.Queries
	pool    *pgxpool.Pool
}

// NewService constructs a Service. pool is used only for UpsertPick's
// transaction (the lock check and the write must happen atomically against
// a row lock); every other method runs a single statement through queries.
func NewService(queries *gen.Queries, pool *pgxpool.Pool) *Service {
	return &Service{queries: queries, pool: pool}
}

// GetPick returns a membership's pick for a week, mapping "no rows" to
// ErrPickNotFound. Backs GET .../picks/me.
func (s *Service) GetPick(ctx context.Context, membershipID, weekID pgtype.UUID) (gen.Pick, error) {
	p, err := s.queries.GetPickByMembershipAndWeek(ctx, gen.GetPickByMembershipAndWeekParams{
		LeagueMembershipID: membershipID,
		WeekID:             weekID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.Pick{}, ErrPickNotFound
		}
		return gen.Pick{}, err
	}
	return p, nil
}

// AvailableTeam is one row of GET .../available-teams: a pickable team in
// the league's conference with a game this week, annotated with whether it
// is locked (its game has kicked off) or already used elsewhere (committed
// to a different week's pick row for this membership).
type AvailableTeam struct {
	Row             gen.ListAvailableTeamsForWeekRow
	IsLocked        bool
	IsUsedElsewhere bool
	IsCurrentPick   bool
}

// ListAvailableTeams returns every pickable team for the week (league's
// conference, has a game that week) plus the membership's current pick for
// that week. hasCurrentPick is false (and currentPick is the zero value)
// when the membership hasn't picked for this week yet — that is not an
// error condition here, unlike GetPick.
func (s *Service) ListAvailableTeams(ctx context.Context, membershipID, weekID pgtype.UUID, conference string) (teams []AvailableTeam, currentPick gen.Pick, hasCurrentPick bool, err error) {
	rows, err := s.queries.ListAvailableTeamsForWeek(ctx, gen.ListAvailableTeamsForWeekParams{
		WeekID:     weekID,
		Conference: conference,
	})
	if err != nil {
		return nil, gen.Pick{}, false, err
	}

	usedRows, err := s.queries.ListUsedTeamIDsForMembershipExcludingWeek(ctx, gen.ListUsedTeamIDsForMembershipExcludingWeekParams{
		LeagueMembershipID: membershipID,
		WeekID:             weekID,
	})
	if err != nil {
		return nil, gen.Pick{}, false, err
	}
	used := make(map[pgtype.UUID]bool, len(usedRows))
	for _, id := range usedRows {
		used[id] = true
	}

	currentPick, getErr := s.GetPick(ctx, membershipID, weekID)
	if getErr != nil && !errors.Is(getErr, ErrPickNotFound) {
		return nil, gen.Pick{}, false, getErr
	}
	hasCurrentPick = getErr == nil

	out := make([]AvailableTeam, 0, len(rows))
	for _, row := range rows {
		out = append(out, AvailableTeam{
			Row:             row,
			IsLocked:        isKickedOff(row.KickoffAt),
			IsUsedElsewhere: used[row.TeamID],
			IsCurrentPick:   hasCurrentPick && currentPick.TeamID == row.TeamID,
		})
	}
	return out, currentPick, hasCurrentPick, nil
}

// MemberPickStatus is one row of GET .../picks: a league member's pick
// status for the week, with game_id/team_id populated only when the caller
// is entitled to see them (own row always; others' rows only once the
// underlying game has kicked off) — the privacy rule from the API
// contract. Applying that rule is the HTTP layer's job (it alone knows
// which membership is "own"); this type just carries the raw joined data
// plus convenience HasPicked/IsLocked flags.
type MemberPickStatus struct {
	Row       gen.ListPicksByWeekForLeagueRow
	HasPicked bool
	IsLocked  bool
}

// ListWeekPicks returns every non-removed league member's pick status for
// the week. Backs GET .../picks.
func (s *Service) ListWeekPicks(ctx context.Context, leagueID, weekID pgtype.UUID) ([]MemberPickStatus, error) {
	rows, err := s.queries.ListPicksByWeekForLeague(ctx, gen.ListPicksByWeekForLeagueParams{
		WeekID:   weekID,
		LeagueID: leagueID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]MemberPickStatus, 0, len(rows))
	for _, row := range rows {
		out = append(out, MemberPickStatus{
			Row:       row,
			HasPicked: row.PickID.Valid,
			IsLocked:  isKickedOff(row.KickoffAt),
		})
	}
	return out, nil
}

// isKickedOff reports whether a (possibly-null, e.g. a member with no pick
// yet) kickoff_at is set and has already passed — the single definition of
// "locked" this whole package works from, per its doc comment.
func isKickedOff(kickoffAt pgtype.Timestamptz) bool {
	return kickoffAt.Valid && !kickoffAt.Time.After(time.Now())
}

// UpsertPick validates and creates-or-updates a membership's pick for a
// week, in this order (matching the API contract exactly):
//
//  1. gameID belongs to weekID (ErrGameNotInWeek — also covers a
//     nonexistent gameID, since it then trivially fails to belong to any
//     week).
//  2. teamID is one of the game's two teams (ErrTeamNotInGame).
//  3. that team belongs to the league's locked conference
//     (ErrTeamWrongConference) — only the picked team needs to; the
//     opponent doesn't (that's how non-conference games work).
//  4. if a pick already exists for this week and its CURRENT game has
//     already kicked off, reject (ErrPickLocked) — checked inside a
//     transaction with the existing row locked (FOR UPDATE) so a
//     concurrent request can't race past this check.
//  5. upsert. A unique-violation on the (league_membership_id, team_id)
//     constraint (team already committed to a different week) is caught
//     and mapped to ErrTeamAlreadyUsed rather than surfaced raw.
func (s *Service) UpsertPick(ctx context.Context, membershipID, weekID pgtype.UUID, leagueConference string, gameID, teamID pgtype.UUID) (gen.Pick, error) {
	game, err := s.queries.GetGameByIDWithTeams(ctx, gameID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.Pick{}, ErrGameNotInWeek
		}
		return gen.Pick{}, err
	}
	if game.WeekID != weekID {
		return gen.Pick{}, ErrGameNotInWeek
	}

	var teamConference string
	switch teamID {
	case game.HomeTeamID:
		teamConference = game.HomeTeamConference
	case game.AwayTeamID:
		teamConference = game.AwayTeamConference
	default:
		return gen.Pick{}, ErrTeamNotInGame
	}
	if teamConference != leagueConference {
		return gen.Pick{}, ErrTeamWrongConference
	}

	// The target game itself must still be open. This isn't spelled out as
	// a separate numbered step in the API contract (which only explicitly
	// guards against changing an *existing, already-locked* pick), but it
	// follows directly from the plan's confirmed product rule ("Pick locks
	// the moment that game's kickoff passes — enforced server-side at
	// request time") and from "swap to a different (still-open) game" in
	// the locking rule: a brand-new pick into a game that has already
	// kicked off (or even finished) would let someone pick with knowledge
	// of the outcome, which no version of this rule intends to allow.
	// Reuses ErrPickLocked's "already locked" framing/status code since
	// it's the same class of rejection from the caller's point of view.
	if isKickedOff(game.KickoffAt) {
		return gen.Pick{}, ErrPickLocked
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.Pick{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	qtx := s.queries.WithTx(tx)

	existing, err := qtx.GetPickByMembershipAndWeekForUpdate(ctx, gen.GetPickByMembershipAndWeekForUpdateParams{
		LeagueMembershipID: membershipID,
		WeekID:             weekID,
	})
	switch {
	case err == nil:
		existingGame, err := qtx.GetGameByIDWithTeams(ctx, existing.GameID)
		if err != nil {
			return gen.Pick{}, err
		}
		if isKickedOff(existingGame.KickoffAt) {
			return gen.Pick{}, ErrPickLocked
		}
	case errors.Is(err, pgx.ErrNoRows):
		// No existing pick for this week — nothing to lock-check.
	default:
		return gen.Pick{}, err
	}

	pick, err := qtx.UpsertPick(ctx, gen.UpsertPickParams{
		LeagueMembershipID: membershipID,
		WeekID:             weekID,
		GameID:             gameID,
		TeamID:             teamID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation {
			return gen.Pick{}, ErrTeamAlreadyUsed
		}
		return gen.Pick{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return gen.Pick{}, err
	}
	return pick, nil
}
