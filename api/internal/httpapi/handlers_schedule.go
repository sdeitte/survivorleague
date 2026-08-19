package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

// handleListWeeks implements GET /weeks?season_year=&conference=
// (requireAuth). conference is optional: omitting it (as admin tooling
// does) lists every week in the season regardless of conference; a
// league's picks screen must always pass its own conference, or it'll be
// offered weeks that are a no-op for it — see
// Service.ListWeeksBySeasonYearAndConference's doc comment.
func (a *API) handleListWeeks(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("season_year")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "season_year is required")
		return
	}
	seasonYear, err := strconv.Atoi(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "season_year must be an integer")
		return
	}
	conference := r.URL.Query().Get("conference")

	var weeks []gen.Week
	if conference != "" {
		weeks, err = a.scheduleService.ListWeeksBySeasonYearAndConference(r.Context(), int32(seasonYear), conference)
	} else {
		weeks, err = a.scheduleService.ListWeeksBySeasonYear(r.Context(), int32(seasonYear))
	}
	if err != nil {
		log.Printf("list weeks: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list weeks")
		return
	}

	out := make([]weekResponse, 0, len(weeks))
	for _, week := range weeks {
		out = append(out, toWeekResponse(week))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListWeekGames implements GET /weeks/:id/games (requireAuth).
func (a *API) handleListWeekGames(w http.ResponseWriter, r *http.Request) {
	weekID, err := db.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "week not found")
		return
	}

	if _, err := a.scheduleService.GetWeekByID(r.Context(), weekID); err != nil {
		if errors.Is(err, schedule.ErrWeekNotFound) {
			writeError(w, http.StatusNotFound, "week not found")
			return
		}
		log.Printf("get week: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load week")
		return
	}

	rows, err := a.scheduleService.ListGamesByWeek(r.Context(), weekID)
	if err != nil {
		log.Printf("list week games: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list games")
		return
	}

	out := make([]gameResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toGameResponseFromListRow(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetGame implements GET /games/:id (requireAuth).
func (a *API) handleGetGame(w http.ResponseWriter, r *http.Request) {
	gameID, err := db.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "game not found")
		return
	}

	row, err := a.scheduleService.GetGameByID(r.Context(), gameID)
	if err != nil {
		if errors.Is(err, schedule.ErrGameNotFound) {
			writeError(w, http.StatusNotFound, "game not found")
			return
		}
		log.Printf("get game: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load game")
		return
	}

	writeJSON(w, http.StatusOK, toGameResponseFromGetRow(row))
}

// handleGetCurrentWeek implements GET /leagues/:id/current-week
// (requireLeagueMember) — the week that is "currently occurring"
// schedule-wise for the league's conference, per
// schedule.Service.CurrentWeek's rule. 404 if the league's season has no
// synced games for its conference yet (no sync has run, or the sync
// hasn't reached this conference).
func (a *API) handleGetCurrentWeek(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	row, err := a.scheduleService.CurrentWeek(r.Context(), lc.League.SeasonYear, lc.League.Conference, time.Now())
	if err != nil {
		if errors.Is(err, schedule.ErrNoScheduleData) {
			writeError(w, http.StatusNotFound, "no schedule data synced yet for this league's conference/season")
			return
		}
		log.Printf("get current week: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to determine current week")
		return
	}

	writeJSON(w, http.StatusOK, weekResponse{
		ID:         db.UUIDString(row.WeekID),
		SeasonYear: lc.League.SeasonYear,
		WeekNumber: row.WeekNumber,
	})
}

// handleListTeams implements GET /teams?conference= (requireAuth).
func (a *API) handleListTeams(w http.ResponseWriter, r *http.Request) {
	conference := r.URL.Query().Get("conference")

	teams, err := a.scheduleService.ListTeams(r.Context(), conference)
	if err != nil {
		log.Printf("list teams: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list teams")
		return
	}

	out := make([]teamResponse, 0, len(teams))
	for _, team := range teams {
		out = append(out, toTeamResponse(team))
	}
	writeJSON(w, http.StatusOK, out)
}
