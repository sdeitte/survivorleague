package schedule

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
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
	GetPregameWinProbabilities(ctx context.Context, year int) ([]cfbdPregameWinProbability, error)
	GetSPRatings(ctx context.Context, year int) ([]cfbdTeamSP, error)
}

var _ cfbdClient = (*CFBDClient)(nil)

// excludedGameExternalIDs lists real CFBD games (keyed by CFBD's external
// game id, fmt.Sprint(g.ID)) that must never be synced/pickable despite
// being legitimate scheduled games. CFBD's calendar assigns these the same
// week_number as that team's real week-1 slate even though they're played
// nearly a week earlier as a standalone season-opener — e.g. USC played San
// José State on 2026-08-29, then Fresno State on 2026-09-04 as its real
// Big Ten week 1 game, both tagged week=1. Letting both through would
// present two "week 1" choices for the same team, or effectively strand
// the early one as a single-game week for its conference. There's no
// general signal in CFBD's data to detect this automatically (it's not
// "earliest game of the week" — plenty of weeks legitimately open with a
// Thursday game a day or two ahead of the Saturday cluster); each
// occurrence is confirmed and added here by hand.
var excludedGameExternalIDs = map[string]string{
	"401864494": "USC vs San José State (2026-08-29) — CFBD tags this Big Ten week 1, same as USC's real week 1 game (Fresno State, 2026-09-04); not a legitimate second week-1 choice",
	// The remaining four were found by auditing every conference for the
	// same signature (a team with more than one game in the same
	// season_year/week_number) after the USC case above was reported —
	// same root cause, a standalone late-August opener CFBD tags with the
	// same week_number as that team's real week 1 game 6-9 days later.
	"401864570": "Florida State vs New Mexico State (2026-08-29) — ACC/Conference USA week 1, same as Florida State's real week 1 game (SMU, 2026-09-07)",
	"401858201": "Stanford vs Hawai'i (2026-08-29) — ACC/Mountain West week 1; Stanford's real week 1 game is Miami (2026-09-05), Hawai'i's is UNLV (2026-09-06)",
	"401862693": "UNLV vs Memphis (2026-08-30) — Mountain West/American Athletic week 1; Memphis's real week 1 game is Arkansas State (2026-09-05), UNLV's is Hawai'i (2026-09-06)",
	"401866408": "Eastern Michigan vs Sacramento State (2026-08-29) — Mid-American week 1, same as Eastern Michigan's real week 1 game (San José State, 2026-09-04)",
}

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
	// WeeksUpserted is the final count of weeks actually kept — it already
	// excludes anything counted in WeeksPruned below.
	WeeksUpserted int `json:"weeks_upserted"`
	GamesUpserted int `json:"games_upserted"`

	// WeeksPruned counts weeks CFBD's calendar listed as seasonType=regular
	// (so they were initially upserted) that turned out to have zero games
	// attached once the games pull completed, and were deleted as a result
	// — e.g. a scheduling-gap week CFBD reports between the end of
	// regular-season play and conference championship week. See
	// DeleteWeekIfNoGames's doc comment for why this is always safe (a
	// week with any real games or pick history is never touched).
	WeeksPruned int `json:"weeks_pruned"`

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

		if reason, excluded := excludedGameExternalIDs[externalID]; excluded {
			result.SkippedGames = append(result.SkippedGames, SkippedGame{ExternalID: externalID, Reason: reason})
			result.GamesSkipped++
			continue
		}

		homeTeamID, homeOK := teamIDByExternalID[fmt.Sprint(g.HomeID)]
		awayTeamID, awayOK := teamIDByExternalID[fmt.Sprint(g.AwayID)]

		// Only worth resolving the missing side as a non-FBS opponent stub
		// when the OTHER side is already a confirmed FBS team we track —
		// otherwise this is some entirely irrelevant game (e.g. two FCS
		// teams playing each other) that CFBD's unfiltered GET /games
		// happens to include, and must stay skipped exactly as before.
		if !homeOK && awayOK {
			if id, ok, rerr := s.resolveNonFBSOpponent(ctx, g.HomeID, g.HomeTeam, g.HomeConference, g.HomeClassification); rerr != nil {
				return result, rerr
			} else if ok {
				homeTeamID, homeOK = id, true
				teamIDByExternalID[fmt.Sprint(g.HomeID)] = id
			}
		}
		if !awayOK && homeOK {
			if id, ok, rerr := s.resolveNonFBSOpponent(ctx, g.AwayID, g.AwayTeam, g.AwayConference, g.AwayClassification); rerr != nil {
				return result, rerr
			} else if ok {
				awayTeamID, awayOK = id, true
				teamIDByExternalID[fmt.Sprint(g.AwayID)] = id
			}
		}

		if !homeOK || !awayOK {
			// One side isn't an FBS team synced above and CFBD didn't report
			// it as a resolvable non-FBS opponent either (e.g. a game that
			// doesn't involve any team we track at all) — teams.
			// home_team_id/away_team_id are NOT NULL FKs into teams, so this
			// game cannot be represented. Skip rather than crash.
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

	// Prune any week upserted above that ended up with zero games attached
	// — CFBD's calendar can list a seasonType=regular week (e.g. a
	// scheduling-gap week before conference championship weekend) that
	// never has any games in it. See DeleteWeekIfNoGames's doc comment for
	// why this is always safe to attempt unconditionally.
	for _, weekID := range weekIDByNumber {
		rows, err := s.queries.DeleteWeekIfNoGames(ctx, weekID)
		if err != nil {
			return result, fmt.Errorf("schedule: prune empty week: %w", err)
		}
		if rows > 0 {
			result.WeeksUpserted--
			result.WeeksPruned++
		}
	}

	return result, nil
}

// PredictionSyncResult summarizes one SyncPredictions run.
type PredictionSyncResult struct {
	Upserted int
	// Skipped counts a prediction row whose gameId doesn't resolve to a
	// game synced by SyncSeason — not an error, since CFBD's win-
	// probability model can reference a game (e.g. a postseason/exhibition
	// matchup) outside what SyncSeason ever pulls in.
	Skipped int
}

// SyncPredictions pulls CFBD's pregame win-probability/spread model for
// the whole season (GET /metrics/wp/pregame?year=) and upserts it into
// game_predictions, keyed by matching CFBD's gameId to games.external_id.
// Safe to call repeatedly (plain upsert on game_id) — called alongside
// SyncSeason on the same twice-daily cron (see admin.Service.
// TriggerScheduleSync), independently since CFBD only populates this data
// in the ~1 week before kickoff, so most calls will only touch the
// current/near-term weeks' rows regardless of how far the season has
// progressed.
func (s *Service) SyncPredictions(ctx context.Context, year int) (PredictionSyncResult, error) {
	var result PredictionSyncResult

	rows, err := s.cfbd.GetPregameWinProbabilities(ctx, year)
	if err != nil {
		return result, fmt.Errorf("schedule: get pregame win probabilities: %w", err)
	}

	for _, row := range rows {
		game, err := s.queries.GetGameByExternalID(ctx, fmt.Sprint(row.GameID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				result.Skipped++
				continue
			}
			return result, fmt.Errorf("schedule: get game for prediction %d: %w", row.GameID, err)
		}

		if _, err := s.queries.UpsertGamePrediction(ctx, gen.UpsertGamePredictionParams{
			GameID:             game.ID,
			Spread:             numericFromFloat64(row.Spread),
			HomeWinProbability: numericFromFloat64(row.HomeWinProbability),
		}); err != nil {
			return result, fmt.Errorf("schedule: upsert prediction for game %d: %w", row.GameID, err)
		}
		result.Upserted++
	}

	return result, nil
}

// SPRatingSyncResult summarizes one SyncSPRatings run.
type SPRatingSyncResult struct {
	Upserted int
	// Skipped counts a row that couldn't be matched to a synced team — CFBD's
	// synthetic "nationalAverages" pseudo-team row always falls here (see
	// cfbdTeamSP's doc comment), along with any real team name CFBD returns
	// that doesn't match a currently-synced team.
	Skipped int
}

// SyncSPRatings pulls CFBD's SP+ power ratings for the whole season
// (GET /ratings/sp?year=) and upserts them into team_sp_ratings, keyed by
// matching CFBD's team name to teams.name. Unlike SyncPredictions this is
// available for the whole season up front, so a call early in the season
// already populates every team's rating (which then just gets refreshed
// on the same twice-daily cadence as everything else).
func (s *Service) SyncSPRatings(ctx context.Context, year int) (SPRatingSyncResult, error) {
	var result SPRatingSyncResult

	rows, err := s.cfbd.GetSPRatings(ctx, year)
	if err != nil {
		return result, fmt.Errorf("schedule: get SP+ ratings: %w", err)
	}

	for _, row := range rows {
		team, err := s.queries.GetTeamByName(ctx, row.Team)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Expected for CFBD's "nationalAverages" pseudo-team row,
				// and harmless for any other unmatched name.
				result.Skipped++
				continue
			}
			return result, fmt.Errorf("schedule: get team %q for SP+ rating: %w", row.Team, err)
		}

		var ranking pgtype.Int4
		if row.Ranking != nil {
			ranking = pgtype.Int4{Int32: int32(*row.Ranking), Valid: true}
		}
		if _, err := s.queries.UpsertTeamSPRating(ctx, gen.UpsertTeamSPRatingParams{
			TeamID:     team.ID,
			SeasonYear: int32(year),
			Rating:     numericFromFloat64(row.Rating),
			Ranking:    ranking,
		}); err != nil {
			return result, fmt.Errorf("schedule: upsert SP+ rating for %q: %w", row.Team, err)
		}
		result.Upserted++
	}

	return result, nil
}

// resolveNonFBSOpponent looks up/creates a teamID for a game participant
// that isn't one of the FBS teams this sync already knows about. If CFBD
// explicitly reports this participant as non-FBS (classification is
// non-empty and not "fbs" — e.g. "fcs"), a minimal stub team row is
// upserted (see UpsertNonFBSOpponentTeam) so the FBS side's game can still
// be stored — the common "cupcake" case, e.g. an FBS team's week-1 game
// against an FCS opponent. classification == "" (some older CFBD payloads
// omit it) deliberately returns ok=false rather than guessing, so callers
// keep the pre-existing skip behavior for genuinely ambiguous data.
func (s *Service) resolveNonFBSOpponent(ctx context.Context, externalID int, name string, conference *string, classification string) (teamID pgtype.UUID, ok bool, err error) {
	if classification == "" || strings.EqualFold(classification, "fbs") {
		return pgtype.UUID{}, false, nil
	}

	conf := "FCS"
	if conference != nil && *conference != "" {
		conf = *conference
	}

	team, err := s.queries.UpsertNonFBSOpponentTeam(ctx, gen.UpsertNonFBSOpponentTeamParams{
		ExternalID: fmt.Sprint(externalID),
		Name:       name,
		Conference: conf,
	})
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("schedule: upsert non-FBS opponent team %d: %w", externalID, err)
	}
	return team.ID, true, nil
}

// numericFromFloat64 converts a plain float64 into a valid pgtype.Numeric
// via its decimal text representation — pgtype.Numeric.Scan only accepts a
// string (or nil), not a float64 directly.
func numericFromFloat64(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	// Scan's error return is only non-nil for a malformed string, which
	// strconv.FormatFloat never produces.
	_ = n.Scan(strconv.FormatFloat(f, 'f', -1, 64))
	return n
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

		if reason, excluded := excludedGameExternalIDs[externalID]; excluded {
			result.SkippedGames = append(result.SkippedGames, SkippedGame{ExternalID: externalID, Reason: reason})
			result.GamesSkipped++
			continue
		}

		homeTeam, homeErr := s.queries.GetTeamByExternalID(ctx, fmt.Sprint(g.HomeID))
		homeFound := homeErr == nil
		if homeErr != nil && !errors.Is(homeErr, pgx.ErrNoRows) {
			return result, fmt.Errorf("schedule: get home team for game %s: %w", externalID, homeErr)
		}

		awayTeam, awayErr := s.queries.GetTeamByExternalID(ctx, fmt.Sprint(g.AwayID))
		awayFound := awayErr == nil
		if awayErr != nil && !errors.Is(awayErr, pgx.ErrNoRows) {
			return result, fmt.Errorf("schedule: get away team for game %s: %w", externalID, awayErr)
		}

		// Same "only resolve the missing side as a non-FBS opponent stub
		// when the other side is a real, already-synced FBS team" gating as
		// SyncSeason — see its comment above for why.
		if !homeFound && awayFound && awayTeam.IsFbs {
			if id, ok, rerr := s.resolveNonFBSOpponent(ctx, g.HomeID, g.HomeTeam, g.HomeConference, g.HomeClassification); rerr != nil {
				return result, rerr
			} else if ok {
				homeTeam.ID, homeFound = id, true
			}
		}
		if !awayFound && homeFound && homeTeam.IsFbs {
			if id, ok, rerr := s.resolveNonFBSOpponent(ctx, g.AwayID, g.AwayTeam, g.AwayConference, g.AwayClassification); rerr != nil {
				return result, rerr
			} else if ok {
				awayTeam.ID, awayFound = id, true
			}
		}

		if !homeFound {
			result.SkippedGames = append(result.SkippedGames, SkippedGame{
				ExternalID: externalID,
				Reason:     "home team not found (not previously synced by a full SyncSeason)",
			})
			result.GamesSkipped++
			continue
		}
		if !awayFound {
			result.SkippedGames = append(result.SkippedGames, SkippedGame{
				ExternalID: externalID,
				Reason:     "away team not found (not previously synced by a full SyncSeason)",
			})
			result.GamesSkipped++
			continue
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

// RefreshGame re-fetches one specific game (identified by its games.id
// primary key, not its CFBD external_id) from CFBD and upserts it via the
// same UpsertGame path (and the same status/score/winner mapping — see
// buildGameUpsertParams) as SyncSeason/RefreshWeek. This is what
// POST /admin/games/:id/resync (Phase 8) calls to unblock a single game
// whose status is stuck postponed/canceled/stale, without paying for a
// whole week's refresh.
//
// The game must already exist (created by an earlier SyncSeason/
// RefreshWeek) — RefreshGame never creates a new game row, only updates
// one, and it re-resolves home/away teams by CFBD's reported homeId/awayId
// (via GetTeamByExternalID) rather than trusting the existing row's
// home_team_id/away_team_id, the same defensive approach RefreshWeek
// takes. Returns ErrGameNotFound if gameID doesn't resolve to an existing
// game, or ErrGameNotFoundInCFBD if CFBD's response for that game's
// season/week no longer contains a game with this external_id.
func (s *Service) RefreshGame(ctx context.Context, gameID pgtype.UUID) (gen.Game, error) {
	game, err := s.queries.GetGame(ctx, gameID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.Game{}, ErrGameNotFound
		}
		return gen.Game{}, fmt.Errorf("schedule: get game: %w", err)
	}

	week, err := s.queries.GetWeekByID(ctx, game.WeekID)
	if err != nil {
		return gen.Game{}, fmt.Errorf("schedule: get week for game %s: %w", game.ExternalID, err)
	}

	cfbdGames, err := s.cfbd.GetGamesForWeek(ctx, int(week.SeasonYear), int(week.WeekNumber))
	if err != nil {
		return gen.Game{}, fmt.Errorf("schedule: get games for week %d/%d: %w", week.SeasonYear, week.WeekNumber, err)
	}

	for _, g := range cfbdGames {
		if fmt.Sprint(g.ID) != game.ExternalID {
			continue
		}

		homeTeam, homeErr := s.queries.GetTeamByExternalID(ctx, fmt.Sprint(g.HomeID))
		if homeErr != nil && !errors.Is(homeErr, pgx.ErrNoRows) {
			return gen.Game{}, fmt.Errorf("schedule: get home team for game %s: %w", game.ExternalID, homeErr)
		}
		awayTeam, awayErr := s.queries.GetTeamByExternalID(ctx, fmt.Sprint(g.AwayID))
		if awayErr != nil && !errors.Is(awayErr, pgx.ErrNoRows) {
			return gen.Game{}, fmt.Errorf("schedule: get away team for game %s: %w", game.ExternalID, awayErr)
		}

		// Same non-FBS-opponent-stub fallback as SyncSeason/RefreshWeek —
		// see resolveNonFBSOpponent's doc comment.
		if homeErr != nil && awayErr == nil && awayTeam.IsFbs {
			id, ok, rerr := s.resolveNonFBSOpponent(ctx, g.HomeID, g.HomeTeam, g.HomeConference, g.HomeClassification)
			if rerr != nil {
				return gen.Game{}, rerr
			}
			if !ok {
				return gen.Game{}, fmt.Errorf("schedule: get home team for game %s: %w", game.ExternalID, homeErr)
			}
			homeTeam.ID = id
		} else if homeErr != nil {
			return gen.Game{}, fmt.Errorf("schedule: get home team for game %s: %w", game.ExternalID, homeErr)
		}
		if awayErr != nil && homeTeam.IsFbs {
			id, ok, rerr := s.resolveNonFBSOpponent(ctx, g.AwayID, g.AwayTeam, g.AwayConference, g.AwayClassification)
			if rerr != nil {
				return gen.Game{}, rerr
			}
			if !ok {
				return gen.Game{}, fmt.Errorf("schedule: get away team for game %s: %w", game.ExternalID, awayErr)
			}
			awayTeam.ID = id
		} else if awayErr != nil {
			return gen.Game{}, fmt.Errorf("schedule: get away team for game %s: %w", game.ExternalID, awayErr)
		}

		params, _, err := buildGameUpsertParams(g, week.ID, homeTeam.ID, awayTeam.ID)
		if err != nil {
			return gen.Game{}, fmt.Errorf("schedule: build upsert params for game %s: %w", game.ExternalID, err)
		}

		updated, err := s.queries.UpsertGame(ctx, params)
		if err != nil {
			return gen.Game{}, fmt.Errorf("schedule: upsert game %s: %w", game.ExternalID, err)
		}
		return updated, nil
	}

	return gen.Game{}, ErrGameNotFoundInCFBD
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
