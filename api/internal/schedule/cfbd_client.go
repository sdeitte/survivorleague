package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// DefaultCFBDBaseURL is collegefootballdata.com's production API host.
const DefaultCFBDBaseURL = "https://api.collegefootballdata.com"

// HTTPDoer is the subset of *http.Client this package depends on. Accepting
// this interface (rather than a concrete *http.Client) is what makes
// CFBDClient testable against an httptest.Server, or any other RoundTripper,
// with zero live network calls.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// CFBDClient talks to the CollegeFootballData.com REST API. Auth is a
// bearer token per CFBD's documented `apiKey` security scheme (confirmed
// against the live OpenAPI spec — see cfbd_types.go's doc comment).
type CFBDClient struct {
	httpClient HTTPDoer
	baseURL    string
	apiKey     string
}

// NewCFBDClient constructs a client. baseURL is configurable (rather than
// hardcoded to DefaultCFBDBaseURL) specifically so it can be pointed at a
// mock HTTP server in tests and in the local E2E pass described in the
// Phase 3 verification plan — there is no live CFBD API key in this
// environment yet. apiKey may be empty in that case; requests will simply
// fail against the real API with 401 until a real key is configured via
// CFBD_API_KEY.
func NewCFBDClient(httpClient HTTPDoer, baseURL, apiKey string) *CFBDClient {
	if baseURL == "" {
		baseURL = DefaultCFBDBaseURL
	}
	return &CFBDClient{httpClient: httpClient, baseURL: baseURL, apiKey: apiKey}
}

// CFBDError wraps a non-200 response from CFBD with enough context (status
// code, endpoint, response body) to debug an auth failure or an unexpected
// schema change without a live-network repro.
type CFBDError struct {
	StatusCode int
	Endpoint   string
	Body       string
}

func (e *CFBDError) Error() string {
	return fmt.Sprintf("cfbd: %s returned %d: %s", e.Endpoint, e.StatusCode, e.Body)
}

func (c *CFBDClient) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("cfbd: build request for %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cfbd: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cfbd: read response body for %s: %w", path, err)
	}

	if resp.StatusCode != http.StatusOK {
		return &CFBDError{StatusCode: resp.StatusCode, Endpoint: path, Body: string(body)}
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("cfbd: decode response for %s: %w", path, err)
	}
	return nil
}

// GetFBSTeams fetches every FBS team for the given season year via
// GET /teams/fbs?year=. CFBD's classification filtering happens
// server-side on this endpoint — unlike the generic /teams endpoint, it
// only ever returns classification=fbs teams, which is exactly the "store
// all FBS teams across all conferences" requirement.
func (c *CFBDClient) GetFBSTeams(ctx context.Context, year int) ([]cfbdTeam, error) {
	var teams []cfbdTeam
	q := url.Values{"year": {fmt.Sprint(year)}}
	if err := c.get(ctx, "/teams/fbs", q, &teams); err != nil {
		return nil, err
	}
	return teams, nil
}

// GetCalendar fetches the season's week calendar via GET /calendar?year=.
// Callers should filter to seasonType == seasonTypeRegular themselves —
// CFBD returns all season types (regular, postseason, ...) in one response
// and this endpoint has no server-side seasonType filter.
func (c *CFBDClient) GetCalendar(ctx context.Context, year int) ([]cfbdCalendarWeek, error) {
	var weeks []cfbdCalendarWeek
	q := url.Values{"year": {fmt.Sprint(year)}}
	if err := c.get(ctx, "/calendar", q, &weeks); err != nil {
		return nil, err
	}
	return weeks, nil
}

// GetGames fetches regular-season games for the given year via
// GET /games?year=&seasonType=regular.
func (c *CFBDClient) GetGames(ctx context.Context, year int) ([]cfbdGame, error) {
	var games []cfbdGame
	q := url.Values{"year": {fmt.Sprint(year)}, "seasonType": {seasonTypeRegular}}
	if err := c.get(ctx, "/games", q, &games); err != nil {
		return nil, err
	}
	return games, nil
}

// GetGamesForWeek fetches regular-season games for a single week via
// GET /games?year=&week=&seasonType=regular — the narrow query the Phase 5
// live poll loop uses to refresh just the in-progress week's games instead
// of paying for a full SyncSeason call every ~90s.
func (c *CFBDClient) GetGamesForWeek(ctx context.Context, year, week int) ([]cfbdGame, error) {
	var games []cfbdGame
	q := url.Values{"year": {fmt.Sprint(year)}, "week": {fmt.Sprint(week)}, "seasonType": {seasonTypeRegular}}
	if err := c.get(ctx, "/games", q, &games); err != nil {
		return nil, err
	}
	return games, nil
}

// GetPregameWinProbabilities fetches CFBD's pregame win-probability/spread
// model for the whole season via GET /metrics/wp/pregame?year= — omitting
// `week` returns every week CFBD currently has data for in one call
// (confirmed against the live API, same "whole season in one call"
// convention as GetGames). CFBD only publishes this in the ~1 week before
// a week's games kick off, so a season with mostly-future weeks legitimately
// returns far fewer rows than GetGames does — that's expected, not an error.
func (c *CFBDClient) GetPregameWinProbabilities(ctx context.Context, year int) ([]cfbdPregameWinProbability, error) {
	var rows []cfbdPregameWinProbability
	q := url.Values{"year": {fmt.Sprint(year)}}
	if err := c.get(ctx, "/metrics/wp/pregame", q, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// GetSPRatings fetches CFBD's SP+ power ratings for the whole season via
// GET /ratings/sp?year= — unlike pregame win probability, this is
// available for the full season up front (confirmed against the live
// API), so it's the fallback matchup-strength signal for weeks too far
// out for a real win probability yet.
func (c *CFBDClient) GetSPRatings(ctx context.Context, year int) ([]cfbdTeamSP, error) {
	var rows []cfbdTeamSP
	q := url.Values{"year": {fmt.Sprint(year)}}
	if err := c.get(ctx, "/ratings/sp", q, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
