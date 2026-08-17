package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

// handleListWeeks implements GET /weeks?season_year= (requireAuth).
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

	weeks, err := a.scheduleService.ListWeeksBySeasonYear(r.Context(), int32(seasonYear))
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
