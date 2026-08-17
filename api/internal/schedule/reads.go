package schedule

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// ErrWeekNotFound is returned by GetWeekByID when no week matches.
var ErrWeekNotFound = errors.New("schedule: week not found")

// ErrGameNotFound is returned by GetGameByID when no game matches.
var ErrGameNotFound = errors.New("schedule: game not found")

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
