package schedule

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// cfbdClient is the subset of *CFBDClient Service depends on — an interface
// so tests can inject a fake without going through HTTPDoer/httptest at all
// when they only care about sync/upsert logic, not HTTP framing.
type cfbdClient interface {
	GetFBSTeams(ctx context.Context, year int) ([]cfbdTeam, error)
	GetCalendar(ctx context.Context, year int) ([]cfbdCalendarWeek, error)
	GetGames(ctx context.Context, year int) ([]cfbdGame, error)
	GetGamesForWeek(ctx context.Context, year, week int) ([]cfbdGame, error)
}

var _ cfbdClient = (*CFBDClient)(nil)

// SkippedGame records a CFBD game this sync could not upsert, and why.
type SkippedGame struct {
	ExternalID string `json:"external_id"`
	Reason     string `json:"reason"`
}

// SyncResult summarizes one SyncSeason run — returned to the caller and
// also what backs the sync_runs.details JSONB written by internal/admin.
type SyncResult struct {
	SeasonYear int `json:"season_year"`

	TeamsUpserted int `json:"teams_upserted"`
	WeeksUpserted int `json:"weeks_upserted"`
	GamesUpserted int `json:"games_upserted"`

	// GamesSkipped is len(SkippedGames) — a game whose home/away team or
	// week couldn't be resolved (e.g. an FCS opponent never synced as a
	// team, since only FBS teams are stored) or whose start date was
	// entirely missing/unparseable. These are not upserted at all.
	GamesSkipped int           `json:"games_skipped"`
	SkippedGames []SkippedGame `json:"skipped_games,omitempty"`

	// DeferredKickoffGames counts games CFBD marked startTimeTBD=true.
	// These ARE upserted (using whatever start date CFBD provided — see
	// the plan's Phase 3 scope note on TBD kickoff times), just flagged
	// here since their kickoff_at may still move before CFBD finalizes it.
	DeferredKickoffGames int `json:"deferred_kickoff_games"`

	// UnmappedConferences lists distinct raw CFBD conference strings with
	// no entry in cfbdConferenceNormalization — see NormalizeConference's
	// doc comment. Non-empty here means the normalization table in
	// conferences.go needs a new entry.
	UnmappedConferences []string `json:"unmapped_conferences,omitempty"`
}

// Service runs CFBD schedule ingestion against the database.
type Service struct {
	queries *gen.Queries
	cfbd    cfbdClient
}

// NewService constructs a Service. cfbd is accepted as the cfbdClient
// interface (not the concrete *CFBDClient) so tests can substitute a fake.
func NewService(queries *gen.Queries, cfbd cfbdClient) *Service {
	return &Service{queries: queries, cfbd: cfbd}
}

// SyncSeason pulls FBS teams, the regular-season calendar, and
// regular-season games for year from CFBD and idempotently upserts them
// into teams/weeks/games. Safe to call repeatedly for the same year — every
// write is an upsert keyed on a stable identity (external_id for
// teams/games, (season_year, week_number) for weeks), so re-running against
// unchanged CFBD data never creates duplicate rows, only updates existing
// ones. See SyncResult's doc comments for what's tracked and why a game
// might be skipped rather than upserted.
func (s *Service) SyncSeason(ctx context.Context, year int) (SyncResult, error) {
	result := SyncResult{SeasonYear: year}

	// --- Teams ---
	cfbdTeams, err := s.cfbd.GetFBSTeams(ctx, year)
	if err != nil {
		return result, fmt.Errorf("schedule: get FBS teams: %w", err)
	}

	teamIDByExternalID := make(map[string]pgtype.UUID, len(cfbdTeams))
	unmapped := make(map[string]bool)
	for _, t := range cfbdTeams {
		raw := ""
		if t.Conference != nil {
			raw = *t.Conference
		}
		conference, ok := NormalizeConference(raw)
		if !ok && raw != "" {
			unmapped[raw] = true
		}

		var logoURL pgtype.Text
		if len(t.Logos) > 0 && t.Logos[0] != "" {
			logoURL = pgtype.Text{String: t.Logos[0], Valid: true}
		}

		externalID := fmt.Sprint(t.ID)
		team, err := s.queries.UpsertTeam(ctx, gen.UpsertTeamParams{
			ExternalID: externalID,
			Name:       t.School,
			Conference: conference,
			LogoUrl:    logoURL,
		})
		if err != nil {
			return result, fmt.Errorf("schedule: upsert team %s: %w", externalID, err)
		}
		teamIDByExternalID[externalID] = team.ID
		result.TeamsUpserted++
	}
	for raw := range unmapped {
		result.UnmappedConferences = append(result.UnmappedConferences, raw)
	}
	sort.Strings(result.UnmappedConferences)

	// --- Weeks (calendar, regular season only) ---
	calendar, err := s.cfbd.GetCalendar(ctx, year)
	if err != nil {
		return result, fmt.Errorf("schedule: get calendar: %w", err)
	}

	weekIDByNumber := make(map[int]pgtype.UUID)
	for _, cw := range calendar {
		if cw.SeasonType != seasonTypeRegular {
			continue
		}
		week, err := s.queries.UpsertWeek(ctx, gen.UpsertWeekParams{
			SeasonYear: int32(year),
			WeekNumber: int32(cw.Week),
		})
		if err != nil {
			return result, fmt.Errorf("schedule: upsert week %d: %w", cw.Week, err)
		}
		weekIDByNumber[cw.Week] = week.ID
		result.WeeksUpserted++
	}

	// --- Games (regular season only — GetGames already scopes to it) ---
	games, err := s.cfbd.GetGames(ctx, year)
	if err != nil {
		return result, fmt.Errorf("schedule: get games: %w", err)
	}

	for _, g := range games {
		externalID := fmt.Sprint(g.ID)

		homeTeamID, homeOK := teamIDByExternalID[fmt.Sprint(g.HomeID)]
		awayTeamID, awayOK := teamIDByExternalID[fmt.Sprint(g.AwayID)]
		if !homeOK || !awayOK {
			// One side isn't an FBS team synced above (e.g. an FBS-vs-FCS
			// non-conference game) — teams.home_team_id/away_team_id are
			// NOT NULL FKs into teams, and only FBS teams are stored, so
			// this game cannot be represented. Skip rather than crash.
			result.SkippedGames = append(result.SkippedGames, SkippedGame{
				ExternalID: externalID,
				Reason:     "home or away team is not an FBS team in this sync (likely a non-FBS opponent)",
			})
			result.GamesSkipped++
			continue
		}

		weekID, weekOK := weekIDByNumber[g.Week]
		if !weekOK {
			// Defensive: shouldn't happen since the calendar is synced
			// first and games.WeekID is required, but CFBD's game.week and
			// calendar.week are independently reported fields — don't
			// assume they can never disagree.
			result.SkippedGames = append(result.SkippedGames, SkippedGame{
				ExternalID: externalID,
				Reason:     fmt.Sprintf("no synced week found for week_number=%d", g.Week),
			})
			result.GamesSkipped++
			continue
		}

		params, deferredKickoff, err := buildGameUpsertParams(g, weekID, homeTeamID, awayTeamID)
		if err != nil {
			// CFBD hasn't finalized this game's date/time at all (empty or
			// unparseable startDate) — don't crash the whole sync over one
			// game, defer it instead.
			result.SkippedGames = append(result.SkippedGames, SkippedGame{
				ExternalID: externalID,
				Reason:     fmt.Sprintf("start date missing or unparseable: %q", g.StartDate),
			})
			result.GamesSkipped++
			continue
		}
		if deferredKickoff {
			result.DeferredKickoffGames++
		}

		if _, err := s.queries.UpsertGame(ctx, params); err != nil {
			return result, fmt.Errorf("schedule: upsert game %s: %w", externalID, err)
		}
		result.GamesUpserted++
	}

	return result, nil
}

// buildGameUpsertParams translates one CFBD game (already resolved to a
// weekID/homeTeamID/awayTeamID) into UpsertGame's params — the exact
// status/score/winner-determination logic SyncSeason and RefreshWeek both
// need, kept in one place so a narrow week refresh (Phase 5's live poll
// loop) can never drift from what a full season sync computes for the same
// fields. Returns an error only when the game's start date is missing or
// unparseable (see parseCFBDTime) — callers skip/defer that one game
// rather than failing the whole sync/refresh over it.
func buildGameUpsertParams(g cfbdGame, weekID, homeTeamID, awayTeamID pgtype.UUID) (params gen.UpsertGameParams, deferredKickoff bool, err error) {
	kickoffAt, err := parseCFBDTime(g.StartDate)
	if err != nil {
		return gen.UpsertGameParams{}, false, err
	}

	status := "scheduled"
	var homeScore, awayScore pgtype.Int4
	var winnerTeamID pgtype.UUID
	if g.Completed {
		status = "final"
		if g.HomePoints != nil {
			homeScore = pgtype.Int4{Int32: int32(*g.HomePoints), Valid: true}
		}
		if g.AwayPoints != nil {
			awayScore = pgtype.Int4{Int32: int32(*g.AwayPoints), Valid: true}
		}
		if g.HomePoints != nil && g.AwayPoints != nil {
			if *g.HomePoints > *g.AwayPoints {
				winnerTeamID = homeTeamID
			} else if *g.AwayPoints > *g.HomePoints {
				winnerTeamID = awayTeamID
			}
			// A tie (rare/impossible in modern FBS overtime rules) is
			// deliberately left with winnerTeamID unset rather than
			// guessing.
		}
	}

	return gen.UpsertGameParams{
		ExternalID:   fmt.Sprint(g.ID),
		WeekID:       weekID,
		HomeTeamID:   homeTeamID,
		AwayTeamID:   awayTeamID,
		KickoffAt:    kickoffAt,
		Status:       status,
		HomeScore:    homeScore,
		AwayScore:    awayScore,
		WinnerTeamID: winnerTeamID,
	}, g.StartTimeTBD, nil
}

// RefreshWeekResult summarizes one RefreshWeek call.
type RefreshWeekResult struct {
	SeasonYear int
	WeekNumber int
	WeekID     pgtype.UUID

	GamesUpserted int
	GamesSkipped  int
	SkippedGames  []SkippedGame
}

// RefreshWeek re-fetches just one season/week's games from CFBD and
// upserts them via the same UpsertGame path (and the same status/score/
// winner mapping — see buildGameUpsertParams) as SyncSeason, without the
// full season's team/calendar/game pull. This is what the Phase 5 live
// poll loop calls every ~90s instead of a full SyncSeason, which would be
// far too heavy to run that often.
//
// The target week must already exist (created by an earlier full
// SyncSeason) — RefreshWeek never creates a week itself, only games within
// one. Teams referenced by this week's games must likewise already be
// synced; a game whose home/away team can't be resolved is skipped (same
// non-fatal treatment as SyncSeason), not an error for the whole call.
func (s *Service) RefreshWeek(ctx context.Context, seasonYear, weekNumber int) (RefreshWeekResult, error) {
	result := RefreshWeekResult{SeasonYear: seasonYear, WeekNumber: weekNumber}

	week, err := s.queries.GetWeekBySeasonAndNumber(ctx, gen.GetWeekBySeasonAndNumberParams{
		SeasonYear: int32(seasonYear),
		WeekNumber: int32(weekNumber),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrWeekNotFound
		}
		return result, fmt.Errorf("schedule: get week %d/%d: %w", seasonYear, weekNumber, err)
	}
	result.WeekID = week.ID

	games, err := s.cfbd.GetGamesForWeek(ctx, seasonYear, weekNumber)
	if err != nil {
		return result, fmt.Errorf("schedule: get games for week %d/%d: %w", seasonYear, weekNumber, err)
	}

	for _, g := range games {
		externalID := fmt.Sprint(g.ID)

		homeTeam, err := s.queries.GetTeamByExternalID(ctx, fmt.Sprint(g.HomeID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				result.SkippedGames = append(result.SkippedGames, SkippedGame{
					ExternalID: externalID,
					Reason:     "home team not found (not previously synced by a full SyncSeason)",
				})
				result.GamesSkipped++
				continue
			}
			return result, fmt.Errorf("schedule: get home team for game %s: %w", externalID, err)
		}
		awayTeam, err := s.queries.GetTeamByExternalID(ctx, fmt.Sprint(g.AwayID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				result.SkippedGames = append(result.SkippedGames, SkippedGame{
					ExternalID: externalID,
					Reason:     "away team not found (not previously synced by a full SyncSeason)",
				})
				result.GamesSkipped++
				continue
			}
			return result, fmt.Errorf("schedule: get away team for game %s: %w", externalID, err)
		}

		params, _, err := buildGameUpsertParams(g, week.ID, homeTeam.ID, awayTeam.ID)
		if err != nil {
			result.SkippedGames = append(result.SkippedGames, SkippedGame{
				ExternalID: externalID,
				Reason:     fmt.Sprintf("start date missing or unparseable: %q", g.StartDate),
			})
			result.GamesSkipped++
			continue
		}

		if _, err := s.queries.UpsertGame(ctx, params); err != nil {
			return result, fmt.Errorf("schedule: upsert game %s: %w", externalID, err)
		}
		result.GamesUpserted++
	}

	return result, nil
}

// parseCFBDTime parses CFBD's ISO-8601 startDate field into a
// pgtype.Timestamptz. CFBD documents this field as RFC3339 date-time; games
// with a genuinely blank date (as opposed to merely startTimeTBD=true, which
// still carries a real date) return an error so the caller can skip/defer
// that one game instead of failing the whole sync.
func parseCFBDTime(raw string) (pgtype.Timestamptz, error) {
	if raw == "" {
		return pgtype.Timestamptz{}, fmt.Errorf("empty start date")
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return pgtype.Timestamptz{}, err
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, nil
}
