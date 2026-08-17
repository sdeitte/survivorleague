package schedule

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fixtureTeamsJSON is hand-authored to match CFBD's documented `Team`
// response schema field-for-field (confirmed against the live OpenAPI 3.1
// spec at https://api.collegefootballdata.com/api-docs.json — "College
// Football Data API" v5.24.0 — GET /teams/fbs). Extra fields CFBD really
// does return (mascot, abbreviation, alternateNames, division,
// classification, color, alternateColor, twitter, location/Venue) are
// included deliberately, to prove the client tolerates and ignores fields
// it doesn't need rather than only working against a hand-trimmed payload.
const fixtureTeamsJSON = `[
  {
    "id": 1,
    "school": "Ohio State",
    "mascot": "Buckeyes",
    "abbreviation": "OSU",
    "alternateNames": [],
    "conference": "Big Ten",
    "division": null,
    "classification": "fbs",
    "color": "#BB0000",
    "alternateColor": "#666666",
    "logos": ["https://example.com/logos/ohio-state.png"],
    "twitter": "@OhioStateFB",
    "location": {
      "id": 2148, "name": "Ohio Stadium", "city": "Columbus", "state": "OH",
      "zip": "43210", "countryCode": "US", "timezone": "America/New_York",
      "latitude": 40.0017, "longitude": -83.0197, "elevation": "224.0",
      "capacity": 102780, "constructionYear": 1922, "grass": true, "dome": false
    }
  },
  {
    "id": 2,
    "school": "Michigan",
    "mascot": "Wolverines",
    "abbreviation": "MICH",
    "alternateNames": [],
    "conference": "Big Ten",
    "division": null,
    "classification": "fbs",
    "color": "#00274C",
    "alternateColor": "#FFCB05",
    "logos": ["https://example.com/logos/michigan.png"],
    "twitter": "@UMichFootball",
    "location": null
  },
  {
    "id": 3,
    "school": "Alabama",
    "mascot": "Crimson Tide",
    "abbreviation": "ALA",
    "alternateNames": [],
    "conference": "SEC",
    "division": null,
    "classification": "fbs",
    "color": "#9E1B32",
    "alternateColor": null,
    "logos": ["https://example.com/logos/alabama.png"],
    "twitter": null,
    "location": null
  },
  {
    "id": 4,
    "school": "Army",
    "mascot": "Black Knights",
    "abbreviation": "ARMY",
    "alternateNames": [],
    "conference": "American Athletic",
    "division": null,
    "classification": "fbs",
    "color": null,
    "alternateColor": null,
    "logos": [],
    "twitter": null,
    "location": null
  }
]`

// fixtureCalendarJSON matches CFBD's `CalendarWeek` schema (GET /calendar).
// Includes a postseason entry deliberately, to prove the sync filters it
// out (the `weeks` table has no seasonType column — see cfbd_types.go).
const fixtureCalendarJSON = `[
  {"season": 2025, "week": 1, "seasonType": "regular", "startDate": "2025-08-23T00:00:00.000Z", "endDate": "2025-08-30T00:00:00.000Z"},
  {"season": 2025, "week": 2, "seasonType": "regular", "startDate": "2025-08-30T00:00:00.000Z", "endDate": "2025-09-06T00:00:00.000Z"},
  {"season": 2025, "week": 1, "seasonType": "postseason", "startDate": "2025-12-15T00:00:00.000Z", "endDate": "2025-12-22T00:00:00.000Z"}
]`

// fixtureGamesJSON matches CFBD's `Game` schema (GET /games), trimmed of
// fields this package doesn't consume (elo, lineScores, etc. are real CFBD
// fields, just irrelevant here — confirming unknown-field tolerance is
// already covered by fixtureTeamsJSON above).
const fixtureGamesJSON = `[
  {
    "id": 101, "season": 2025, "week": 1, "seasonType": "regular",
    "startDate": "2025-08-30T17:00:00.000Z", "startTimeTBD": false, "completed": false,
    "neutralSite": false, "conferenceGame": true, "attendance": null, "venueId": null, "venue": null,
    "homeId": 1, "homeTeam": "Ohio State", "homeConference": "Big Ten", "homePoints": null,
    "awayId": 2, "awayTeam": "Michigan", "awayConference": "Big Ten", "awayPoints": null
  },
  {
    "id": 102, "season": 2025, "week": 1, "seasonType": "regular",
    "startDate": "2025-08-28T00:00:00.000Z", "startTimeTBD": true, "completed": false,
    "neutralSite": false, "conferenceGame": false, "attendance": null, "venueId": null, "venue": null,
    "homeId": 3, "homeTeam": "Alabama", "homeConference": "SEC", "homePoints": null,
    "awayId": 4, "awayTeam": "Army", "awayConference": "American Athletic", "awayPoints": null
  },
  {
    "id": 104, "season": 2025, "week": 1, "seasonType": "regular",
    "startDate": "2025-08-30T20:00:00.000Z", "startTimeTBD": false, "completed": false,
    "neutralSite": false, "conferenceGame": false, "attendance": null, "venueId": null, "venue": null,
    "homeId": 999, "homeTeam": "Chattanooga", "homeConference": null, "homePoints": null,
    "awayId": 1, "awayTeam": "Ohio State", "awayConference": "Big Ten", "awayPoints": null
  }
]`

// newFixtureCFBDServer serves the given fixture bodies for /teams/fbs,
// /calendar, and /games, and asserts every request carries the expected
// bearer-token Authorization header (confirmed via CFBD's live OpenAPI
// `apiKey` security scheme — see cfbd_client.go's doc comment). Callers get
// back the server and a *string for each fixture so tests can mutate the
// served body between requests (used to simulate CFBD data changing between
// two sync runs).
func newFixtureCFBDServer(t *testing.T, apiKey string, teamsJSON, calendarJSON, gamesJSON *string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/teams/fbs", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r, apiKey)
		writeJSONFixture(w, *teamsJSON)
	})
	mux.HandleFunc("/calendar", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r, apiKey)
		writeJSONFixture(w, *calendarJSON)
	})
	mux.HandleFunc("/games", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r, apiKey)
		if got := r.URL.Query().Get("seasonType"); got != "regular" {
			t.Errorf("GET /games seasonType = %q, want %q", got, "regular")
		}
		writeJSONFixture(w, *gamesJSON)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func checkAuth(t *testing.T, r *http.Request, apiKey string) {
	t.Helper()
	want := "Bearer " + apiKey
	if got := r.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
	if got := r.URL.Query().Get("year"); got == "" {
		t.Errorf("%s missing required year query param", r.URL.Path)
	}
}

func writeJSONFixture(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func TestCFBDClient_GetFBSTeams(t *testing.T) {
	teamsJSON, calJSON, gamesJSON := fixtureTeamsJSON, fixtureCalendarJSON, fixtureGamesJSON
	server := newFixtureCFBDServer(t, "test-key", &teamsJSON, &calJSON, &gamesJSON)
	client := NewCFBDClient(server.Client(), server.URL, "test-key")

	teams, err := client.GetFBSTeams(context.Background(), 2025)
	if err != nil {
		t.Fatalf("GetFBSTeams: %v", err)
	}
	if len(teams) != 4 {
		t.Fatalf("len(teams) = %d, want 4", len(teams))
	}
	if teams[0].ID != 1 || teams[0].School != "Ohio State" {
		t.Errorf("teams[0] = %+v, want id=1 school=Ohio State", teams[0])
	}
	if teams[0].Conference == nil || *teams[0].Conference != "Big Ten" {
		t.Errorf("teams[0].Conference = %v, want Big Ten", teams[0].Conference)
	}
	if len(teams[0].Logos) != 1 || teams[0].Logos[0] != "https://example.com/logos/ohio-state.png" {
		t.Errorf("teams[0].Logos = %v, want a single logo URL", teams[0].Logos)
	}
	// Team 2 has "location": null and no "twitter" oddity to worry about —
	// just confirming a team with fewer populated optional fields parses
	// without error.
	if teams[1].School != "Michigan" {
		t.Errorf("teams[1].School = %q, want Michigan", teams[1].School)
	}
}

func TestCFBDClient_GetCalendar(t *testing.T) {
	teamsJSON, calJSON, gamesJSON := fixtureTeamsJSON, fixtureCalendarJSON, fixtureGamesJSON
	server := newFixtureCFBDServer(t, "test-key", &teamsJSON, &calJSON, &gamesJSON)
	client := NewCFBDClient(server.Client(), server.URL, "test-key")

	weeks, err := client.GetCalendar(context.Background(), 2025)
	if err != nil {
		t.Fatalf("GetCalendar: %v", err)
	}
	if len(weeks) != 3 {
		t.Fatalf("len(weeks) = %d, want 3 (client does not filter seasonType — that's the sync layer's job)", len(weeks))
	}
}

func TestCFBDClient_GetGames(t *testing.T) {
	teamsJSON, calJSON, gamesJSON := fixtureTeamsJSON, fixtureCalendarJSON, fixtureGamesJSON
	server := newFixtureCFBDServer(t, "test-key", &teamsJSON, &calJSON, &gamesJSON)
	client := NewCFBDClient(server.Client(), server.URL, "test-key")

	games, err := client.GetGames(context.Background(), 2025)
	if err != nil {
		t.Fatalf("GetGames: %v", err)
	}
	if len(games) != 3 {
		t.Fatalf("len(games) = %d, want 3", len(games))
	}
	var tbd *cfbdGame
	for i := range games {
		if games[i].ID == 102 {
			tbd = &games[i]
		}
	}
	if tbd == nil {
		t.Fatal("game 102 (TBD kickoff) not found in response")
	}
	if !tbd.StartTimeTBD {
		t.Error("game 102 StartTimeTBD = false, want true")
	}
	if tbd.StartDate == "" {
		t.Error("game 102 StartDate is empty even though CFBD still reports a placeholder date for TBD games")
	}
}

func TestCFBDClient_NonOKStatus_ReturnsCFBDError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid bearer token"}`))
	}))
	t.Cleanup(server.Close)

	client := NewCFBDClient(server.Client(), server.URL, "bad-key")
	_, err := client.GetFBSTeams(context.Background(), 2025)
	if err == nil {
		t.Fatal("GetFBSTeams with a 401 response: got nil error, want a CFBDError")
	}
	var cfbdErr *CFBDError
	if !errors.As(err, &cfbdErr) {
		t.Fatalf("error is not a *CFBDError: %v (%T)", err, err)
	}
	if cfbdErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("CFBDError.StatusCode = %d, want 401", cfbdErr.StatusCode)
	}
	if !strings.Contains(cfbdErr.Body, "invalid bearer token") {
		t.Errorf("CFBDError.Body = %q, want it to contain the response body", cfbdErr.Body)
	}
}
