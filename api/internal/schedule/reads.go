package schedule

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// ErrWeekNotFound is returned by GetWeekByID when no week matches.
var ErrWeekNotFound = errors.New("schedule: week not found")

// ErrGameNotFound is returned by GetGameByID when no game matches.
var ErrGameNotFound = errors.New("schedule: game not found")

// ErrGameNotFoundInCFBD is returned by RefreshGame when the game's
// season/week no longer contains a CFBD game with this game's external_id
// — a CFBD-side data issue (or a genuinely wrong external_id), not
// something a resync can resolve.
var ErrGameNotFoundInCFBD = errors.New("schedule: game not found in CFBD response for its week")

// ListWeeksBySeasonYear lists a season's weeks, ordered by week_number.
// Backs GET /weeks?season_year=.
func (s *Service) ListWeeksBySeasonYear(ctx context.Context, seasonYear int32) ([]gen.Week, error) {
	return s.queries.ListWeeksBySeasonYear(ctx, seasonYear)
}

// GetWeekByID looks up a week by id, mapping "no rows" to ErrWeekNotFound.
// Used by GET /weeks/:id/games to 404 on an unknown week id before listing
// its (necessarily empty) games.
func (s *Service) GetWeekByID(ctx context.Context, id pgtype.UUID) (gen.Week, error) {
	week, err := s.queries.GetWeekByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.Week{}, ErrWeekNotFound
		}
		return gen.Week{}, err
	}
	return week, nil
}

// ListGamesByWeek lists a week's games joined with both teams' name/
// conference/logo. Backs GET /weeks/:id/games.
func (s *Service) ListGamesByWeek(ctx context.Context, weekID pgtype.UUID) ([]gen.ListGamesByWeekWithTeamsRow, error) {
	return s.queries.ListGamesByWeekWithTeams(ctx, weekID)
}

// GetGameByID looks up a single game (joined with both teams), mapping "no
// rows" to ErrGameNotFound. Backs GET /games/:id.
func (s *Service) GetGameByID(ctx context.Context, id pgtype.UUID) (gen.GetGameByIDWithTeamsRow, error) {
	row, err := s.queries.GetGameByIDWithTeams(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.GetGameByIDWithTeamsRow{}, ErrGameNotFound
		}
		return gen.GetGameByIDWithTeamsRow{}, err
	}
	return row, nil
}

// ListTeams lists teams, optionally filtered to an exact conference match.
// A nil/empty conference lists every team. Backs GET /teams?conference=.
func (s *Service) ListTeams(ctx context.Context, conference string) ([]gen.Team, error) {
	var arg pgtype.Text
	if conference != "" {
		arg = pgtype.Text{String: conference, Valid: true}
	}
	return s.queries.ListTeams(ctx, arg)
}

// MinConferenceTeamsForLeague is the minimum number of currently-synced
// FBS teams a conference must have to be offered as a league's conference
// at creation time. See ListEligibleConferences.
const MinConferenceTeamsForLeague = 13

// ListEligibleConferences returns the conferences a league can be created
// for: real conferences (never "FBS Independents" — its members don't
// share a fixed schedule) with at least MinConferenceTeamsForLeague
// currently-synced teams. Computed live from teams.conference rather than
// FBSConferences' static list, so it stays correct through future
// conference realignment without a code change. Backs GET /conferences
// and POST /leagues' conference validation. Returns an empty slice (not
// an error) if no schedule sync has run yet — see this method's callers
// for how that's surfaced.
func (s *Service) ListEligibleConferences(ctx context.Context) ([]string, error) {
	return s.queries.ListEligibleConferences(ctx, MinConferenceTeamsForLeague)
}

// IsEligibleConference reports whether name is currently in
// ListEligibleConferences' result.
func (s *Service) IsEligibleConference(ctx context.Context, name string) (bool, error) {
	conferences, err := s.ListEligibleConferences(ctx)
	if err != nil {
		return false, err
	}
	for _, c := range conferences {
		if c == name {
			return true, nil
		}
	}
	return false, nil
}

// ListLiveWindowWeeks returns the distinct (season_year, week_number)
// pairs among games currently inside the live poll window: kicked off
// on-or-before now, but not so long ago that they've fallen out the far
// end of the window (kickoff_at >= now - liveWindow), and not yet
// status='final'. This is the Phase 5 live poll loop's cheap "is there
// anything to even check" gate — an empty result means the loop makes no
// CFBD call at all this tick.
func (s *Service) ListLiveWindowWeeks(ctx context.Context, now time.Time, liveWindow time.Duration) ([]gen.ListLiveWindowWeeksRow, error) {
	return s.queries.ListLiveWindowWeeks(ctx, gen.ListLiveWindowWeeksParams{
		Now:         pgtype.Timestamptz{Time: now, Valid: true},
		WindowStart: pgtype.Timestamptz{Time: now.Add(-liveWindow), Valid: true},
	})
}
