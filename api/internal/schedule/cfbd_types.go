package schedule

// This file mirrors the subset of CollegeFootballData.com's (CFBD) documented
// response schema this package actually consumes. Field names/types were
// confirmed against CFBD's live OpenAPI 3.1 spec (fetched from
// https://api.collegefootballdata.com/api-docs.json — no API key required
// to fetch the spec itself, "College Football Data API" v5.24.0) rather than
// assumed from memory. Every field below matches the spec's `Team`,
// `CalendarWeek`, and `Game` component schemas exactly (name and JSON
// casing) — see internal/schedule/doc.go for the full source list.
//
// Only fields this package reads are included; CFBD's real payloads carry
// many more (advanced-stats fields, elo, line scores, etc.) that are simply
// ignored by Go's default JSON decoding of unknown fields.

// cfbdTeam is CFBD's Team schema (GET /teams/fbs).
type cfbdTeam struct {
	ID         int      `json:"id"`
	School     string   `json:"school"`
	Conference *string  `json:"conference"`
	Logos      []string `json:"logos"`
}

// cfbdCalendarWeek is CFBD's CalendarWeek schema (GET /calendar).
type cfbdCalendarWeek struct {
	Season     int    `json:"season"`
	Week       int    `json:"week"`
	SeasonType string `json:"seasonType"`
}

// cfbdGame is CFBD's Game schema (GET /games), trimmed to the fields this
// sync needs.
//
// HomeClassification/AwayClassification ("fbs", "fcs", "ii", "iii") back
// resolveNonFBSOpponent's decision to synthesize a minimal opponent team
// row rather than skip the game outright — see sync.go. They're absent
// (empty string) in some older/hand-authored test fixtures, which
// deliberately keeps resolveNonFBSOpponent conservative (skip, the
// pre-existing behavior) rather than guessing.
type cfbdGame struct {
	ID                 int     `json:"id"`
	Season             int     `json:"season"`
	Week               int     `json:"week"`
	SeasonType         string  `json:"seasonType"`
	StartDate          string  `json:"startDate"`
	StartTimeTBD       bool    `json:"startTimeTBD"`
	Completed          bool    `json:"completed"`
	HomeID             int     `json:"homeId"`
	HomeTeam           string  `json:"homeTeam"`
	HomeConference     *string `json:"homeConference"`
	HomeClassification string  `json:"homeClassification"`
	HomePoints         *int    `json:"homePoints"`
	AwayID             int     `json:"awayId"`
	AwayTeam           string  `json:"awayTeam"`
	AwayConference     *string `json:"awayConference"`
	AwayClassification string  `json:"awayClassification"`
	AwayPoints         *int    `json:"awayPoints"`
}

// cfbdPregameWinProbability is CFBD's PregameWinProbability schema
// (GET /metrics/wp/pregame), confirmed against the live OpenAPI spec.
// spread/homeWinProbability are both from the home team's perspective —
// callers normalize per-team at read time (see internal/picks/service.go).
type cfbdPregameWinProbability struct {
	GameID             int     `json:"gameId"`
	HomeTeam           string  `json:"homeTeam"`
	AwayTeam           string  `json:"awayTeam"`
	Spread             float64 `json:"spread"`
	HomeWinProbability float64 `json:"homeWinProbability"`
}

// cfbdTeamSP is CFBD's TeamSP schema (GET /ratings/sp), trimmed to the
// fields this sync needs. CFBD's response always includes a synthetic
// "nationalAverages" pseudo-team row with no real team behind it — callers
// must skip it (see SyncSPRatings's doc comment in sync.go).
type cfbdTeamSP struct {
	Team    string  `json:"team"`
	Rating  float64 `json:"rating"`
	Ranking *int    `json:"ranking"`
}

// cfbdScoreboardGame is CFBD's ScoreboardGame schema (GET /scoreboard) —
// confirmed against the live OpenAPI spec. Distinct from cfbdGame/GET
// /games: that bulk endpoint only ever reports a game as "not completed"
// or "completed" with no in-progress score at all, which is why this
// package's existing sync never had a way to show a score before a game
// finishes. /scoreboard is CFBD's actual live-game feed — status, period,
// and clock are populated while a game is in progress, not just at
// kickoff/final. Trimmed to the fields internal/schedule.Service.
// RefreshLiveScores needs; CFBD's real payload carries more (venue,
// weather, betting, per-quarter line scores) that this feature has no use
// for.
type cfbdScoreboardGame struct {
	ID     int                      `json:"id"`
	Status string                   `json:"status"`
	Period *int                     `json:"period"`
	Clock  *string                  `json:"clock"`
	Home   cfbdScoreboardTeamPoints `json:"homeTeam"`
	Away   cfbdScoreboardTeamPoints `json:"awayTeam"`
}

// cfbdScoreboardTeamPoints is the per-side object nested in
// cfbdScoreboardGame's homeTeam/awayTeam — trimmed to just the id (so a
// scoreboard row can be matched back to one of OUR two team ids, since the
// two sides aren't otherwise labeled) and points.
type cfbdScoreboardTeamPoints struct {
	ID     int  `json:"id"`
	Points *int `json:"points"`
}

// seasonTypeRegular is the only CFBD seasonType this phase syncs — see the
// plan's Phase 3 scope ("season games (regular season)"). Postseason/bowl
// games are out of scope for now; also, the `weeks` table has no seasonType
// column (UNIQUE(season_year, week_number) only), so mixing in postseason
// "week 1" would collide with regular-season week 1.
const seasonTypeRegular = "regular"
