package grading

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

var (
	// ErrGameNotFound is returned by GradeGame when gameID doesn't exist.
	ErrGameNotFound = errors.New("grading: game not found")
	// ErrLeagueNotFound is returned by TryFinalizeLeagueWeek when leagueID
	// doesn't exist.
	ErrLeagueNotFound = errors.New("grading: league not found")
)

// LeagueWeekPair identifies one league's stake in one week — what
// GradeGame reports as "touched" so the caller (internal/livepoll) knows
// which TryFinalizeLeagueWeek calls to attempt next.
type LeagueWeekPair struct {
	LeagueID pgtype.UUID
	WeekID   pgtype.UUID
}

// Service implements the grading/elimination pipeline on top of the
// sqlc-generated queries.
type Service struct {
	queries *gen.Queries
	pool    *pgxpool.Pool
}

// NewService constructs a Service. pool is used for GradeGame's and
// TryFinalizeLeagueWeek's transactions (both need row-locking/atomic
// idempotency guards); every other read runs a single statement through
// queries.
func NewService(queries *gen.Queries, pool *pgxpool.Pool) *Service {
	return &Service{queries: queries, pool: pool}
}

// GradeGame grades every pending pick on gameID (result='win' if
// pick.team_id == game.winner_team_id, 'loss' otherwise) and marks the
// game graded, all in one transaction guarded by games.graded_at IS NULL
// — see the package doc comment. Returns the distinct (league_id,
// week_id) pairs touched by picks on this game so the caller can attempt
// TryFinalizeLeagueWeek for each.
//
// Safe to call repeatedly for the same game: any call after the first (or
// one that raced against a concurrent caller and lost the row lock) is a
// clean no-op — (nil, nil), not an error — since the live poll loop calls
// this unconditionally for every game it observes as final on every tick,
// and TryFinalizeLeagueWeek's self-heal path may also call it for a game
// that reached 'final' via the plain schedule-sync cron. A game that
// exists but isn't status='final' yet, or is final with no determined
// winner (an undetermined tie — deliberately never guessed, see
// internal/schedule/sync.go), is likewise a no-op, not an error: only a
// nonexistent gameID is an error (ErrGameNotFound).
func (s *Service) GradeGame(ctx context.Context, gameID pgtype.UUID) ([]LeagueWeekPair, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("grading: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	qtx := s.queries.WithTx(tx)

	game, err := qtx.GetGameForGradingForUpdate(ctx, gameID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrGameNotFound
		}
		return nil, fmt.Errorf("grading: get game: %w", err)
	}

	if game.GradedAt.Valid {
		return nil, nil // already graded — idempotent no-op
	}
	if game.Status != "final" {
		return nil, nil // nothing to grade yet
	}
	if !game.WinnerTeamID.Valid {
		// final but no determined winner (a tie). Cannot grade win/loss
		// without one; leave ungraded for Phase 8 admin resolution, same
		// spirit as a postponed/canceled game at the league-week level.
		return nil, nil
	}

	if err := qtx.GradePicksForGame(ctx, gen.GradePicksForGameParams{
		WinnerTeamID: game.WinnerTeamID,
		GameID:       gameID,
	}); err != nil {
		return nil, fmt.Errorf("grading: grade picks for game %s: %w", db.UUIDString(gameID), err)
	}

	leagueIDs, err := qtx.ListLeagueIDsWithPicksForGame(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("grading: list leagues touched by game %s: %w", db.UUIDString(gameID), err)
	}

	if err := qtx.MarkGameGraded(ctx, gameID); err != nil {
		return nil, fmt.Errorf("grading: mark game %s graded: %w", db.UUIDString(gameID), err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("grading: commit grading game %s: %w", db.UUIDString(gameID), err)
	}

	pairs := make([]LeagueWeekPair, 0, len(leagueIDs))
	for _, lid := range leagueIDs {
		pairs = append(pairs, LeagueWeekPair{LeagueID: lid, WeekID: game.WeekID})
	}
	return pairs, nil
}

// TryFinalizeLeagueWeek attempts to finalize one league's week per the
// plan's "Confirmed Product Rules" (missed pick counts as a loss;
// mass-wipeout eliminates nobody). This is deliberately a separate step
// from GradeGame — a week can contain multiple games, and a league's week
// can't be judged until every game relevant to that league's conference
// that week has concluded.
//
// Returns the finalized league_week_results row, or (nil, nil) if the
// week could not be finalized yet: a conference-relevant game is
// postponed/canceled (left for Phase 8 admin handling), a
// conference-relevant game hasn't reached 'final' yet, or another
// concurrent/earlier call already finalized this league/week. None of
// these are error conditions — TryFinalizeLeagueWeek is meant to be called
// speculatively and often (the live poll loop calls it after every week
// refresh), so "not ready yet" and "already done" are both expected,
// silent no-ops.
func (s *Service) TryFinalizeLeagueWeek(ctx context.Context, leagueID, weekID pgtype.UUID) (*gen.LeagueWeekResult, error) {
	league, err := s.queries.GetLeagueByID(ctx, leagueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLeagueNotFound
		}
		return nil, fmt.Errorf("grading: get league %s: %w", db.UUIDString(leagueID), err)
	}

	games, err := s.queries.ListConferenceRelevantGamesForWeek(ctx, gen.ListConferenceRelevantGamesForWeekParams{
		WeekID:     weekID,
		Conference: league.Conference,
	})
	if err != nil {
		return nil, fmt.Errorf("grading: list conference-relevant games: %w", err)
	}

	for _, g := range games {
		if g.Status == "postponed" || g.Status == "canceled" {
			return nil, nil // intentionally left unresolved for Phase 8
		}
	}
	for _, g := range games {
		if g.Status != "final" {
			return nil, nil // not all results are in yet
		}
	}

	// Self-heal: a game can reach status='final' via the daily
	// schedule-sync cron (internal/schedule, Phase 3) without ever passing
	// through the live poll loop's GradeGame call (e.g. the poll loop was
	// down when the game actually finished). Make sure every
	// conference-relevant final game has actually been graded before
	// reading pick results off it below, so a stale 'pending' pick can
	// never be silently mis-treated as a loss/missed pick.
	for _, g := range games {
		if !g.GradedAt.Valid {
			if _, err := s.GradeGame(ctx, g.ID); err != nil {
				return nil, fmt.Errorf("grading: self-heal grade game %s: %w", db.UUIDString(g.ID), err)
			}
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("grading: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed
	qtx := s.queries.WithTx(tx)

	contestants, err := qtx.ListActiveContestantMembershipsForLeague(ctx, leagueID)
	if err != nil {
		return nil, fmt.Errorf("grading: list active contestants: %w", err)
	}

	type outcome struct {
		membershipID pgtype.UUID
		eliminate    bool
		gameID       pgtype.UUID // zero value (Valid=false) for a missed pick
	}
	outcomes := make([]outcome, 0, len(contestants))
	anyWin := false
	for _, m := range contestants {
		pick, err := qtx.GetPickByMembershipAndWeek(ctx, gen.GetPickByMembershipAndWeekParams{
			LeagueMembershipID: m.ID,
			WeekID:             weekID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Missed pick — counts as a loss per the confirmed product rule.
			outcomes = append(outcomes, outcome{membershipID: m.ID, eliminate: true})
		case err != nil:
			return nil, fmt.Errorf("grading: get pick for membership %s: %w", db.UUIDString(m.ID), err)
		case pick.Result == "win":
			anyWin = true
		case pick.Result == "loss":
			outcomes = append(outcomes, outcome{membershipID: m.ID, eliminate: true, gameID: pick.GameID})
		default:
			// "pending" (or "void") should never survive the self-heal
			// pass above, but bail out rather than guess if it somehow
			// does — a later call will retry once grading genuinely
			// catches up.
			return nil, nil
		}
	}

	massWipeout := !anyWin

	result, err := qtx.InsertLeagueWeekResultIfAbsent(ctx, gen.InsertLeagueWeekResultIfAbsentParams{
		LeagueID:    leagueID,
		WeekID:      weekID,
		MassWipeout: massWipeout,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Another concurrent call already finalized this league/week.
			return nil, nil
		}
		return nil, fmt.Errorf("grading: insert league_week_results: %w", err)
	}

	if !massWipeout {
		for _, o := range outcomes {
			if !o.eliminate {
				continue
			}
			if _, err := qtx.EliminateMembership(ctx, gen.EliminateMembershipParams{
				ID:     o.membershipID,
				WeekID: weekID,
				GameID: o.gameID,
			}); err != nil {
				return nil, fmt.Errorf("grading: eliminate membership %s: %w", db.UUIDString(o.membershipID), err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("grading: commit finalizing league %s week %s: %w", db.UUIDString(leagueID), db.UUIDString(weekID), err)
	}

	return &result, nil
}

// ListLeagueIDsForWeek returns every league with at least one pick for the
// given week — used by internal/livepoll to know which leagues to attempt
// TryFinalizeLeagueWeek for after refreshing a week's games. See
// ListLeagueIDsWithPicksForWeek's query comment for the one known scope
// gap (a league whose contestants ALL missed their pick that week has no
// picks row to be discovered by).
func (s *Service) ListLeagueIDsForWeek(ctx context.Context, weekID pgtype.UUID) ([]pgtype.UUID, error) {
	return s.queries.ListLeagueIDsWithPicksForWeek(ctx, weekID)
}
