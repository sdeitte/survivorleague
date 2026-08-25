package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
	"github.com/sdeitte/survivor-league-api/internal/picks"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

// isGameLocked mirrors internal/picks.Service's own lock computation
// (kickoff_at <= now()) — duplicated here rather than exported from that
// package because the two callers in this file need it against a
// gen.Game's kickoff_at (schedule reads), not a picks-package row type.
func isGameLocked(kickoffAt pgtype.Timestamptz) bool {
	return kickoffAt.Valid && !kickoffAt.Time.After(time.Now())
}

// weekIDFromRequest parses the `weekId` URL param and confirms the week
// exists, so every picks endpoint 404s cleanly on a bad/unknown week id
// rather than falling through to an empty result. Weeks are global (not
// league-scoped), matching how GET /weeks/:id/games already validates them.
func (a *API) weekIDFromRequest(w http.ResponseWriter, r *http.Request) (weekIDStr string, ok bool) {
	weekIDStr = chi.URLParam(r, "weekId")
	weekID, err := db.ParseUUID(weekIDStr)
	if err != nil {
		writeError(w, http.StatusNotFound, "week not found")
		return "", false
	}
	if _, err := a.scheduleService.GetWeekByID(r.Context(), weekID); err != nil {
		if errors.Is(err, schedule.ErrWeekNotFound) {
			writeError(w, http.StatusNotFound, "week not found")
			return "", false
		}
		log.Printf("picks: get week: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load week")
		return "", false
	}
	return weekIDStr, true
}

// handleGetMyPick implements GET /leagues/:id/weeks/:weekId/picks/me
// (requireLeagueMember). 404 (with a null-shaped body, per the API
// contract) if the requester has no pick for this week yet.
func (a *API) handleGetMyPick(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}
	weekIDStr, ok := a.weekIDFromRequest(w, r)
	if !ok {
		return
	}
	weekID, _ := db.ParseUUID(weekIDStr)

	pick, err := a.picksService.GetPick(r.Context(), lc.Membership.ID, weekID)
	if err != nil {
		if errors.Is(err, picks.ErrPickNotFound) {
			writeError(w, http.StatusNotFound, "no pick submitted for this week yet")
			return
		}
		log.Printf("get my pick: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load pick")
		return
	}

	game, err := a.scheduleService.GetGameByID(r.Context(), pick.GameID)
	if err != nil {
		log.Printf("get my pick: get game: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load pick")
		return
	}
	writeJSON(w, http.StatusOK, toPickResponse(pick, isGameLocked(game.KickoffAt)))
}

// handleUpsertMyPick implements PUT /leagues/:id/weeks/:weekId/picks/me
// (requireLeagueMember). See internal/picks.Service.UpsertPick's doc
// comment for the exact validation order this maps status codes from.
func (a *API) handleUpsertMyPick(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}
	// A membership that isn't active (nothing sets status='eliminated' yet
	// — that's Phase 5 — but removed_at is already excluded by
	// requireLeagueMember) cannot submit picks. Checked explicitly rather
	// than relying solely on the middleware, per the API contract.
	if lc.Membership.Status != "active" {
		writeError(w, http.StatusForbidden, "membership is not active")
		return
	}

	weekIDStr, ok := a.weekIDFromRequest(w, r)
	if !ok {
		return
	}
	weekID, _ := db.ParseUUID(weekIDStr)

	var req upsertPickRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	gameID, err := db.ParseUUID(req.GameID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "game_id must be a valid uuid")
		return
	}
	teamID, err := db.ParseUUID(req.TeamID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "team_id must be a valid uuid")
		return
	}

	pick, err := a.picksService.UpsertPick(r.Context(), lc.Membership.ID, weekID, lc.League.Conference, gameID, teamID)
	if err != nil {
		switch {
		case errors.Is(err, picks.ErrGameNotInWeek):
			writeError(w, http.StatusBadRequest, "game does not belong to the specified week")
		case errors.Is(err, picks.ErrTeamNotInGame):
			writeError(w, http.StatusBadRequest, "team is not one of this game's two teams")
		case errors.Is(err, picks.ErrTeamWrongConference):
			writeError(w, http.StatusBadRequest, "team does not belong to the league's conference")
		case errors.Is(err, picks.ErrPickLocked):
			writeError(w, http.StatusConflict, "your pick for this week is already locked (its game has kicked off)")
		case errors.Is(err, picks.ErrTeamAlreadyUsed):
			writeError(w, http.StatusConflict, "you have already used this team in a different week")
		default:
			log.Printf("upsert pick: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to save pick")
		}
		return
	}

	writeJSON(w, http.StatusOK, toPickResponse(pick, false)) // just written, so by definition not locked yet
}

// handleListAvailableTeams implements
// GET /leagues/:id/weeks/:weekId/available-teams (requireLeagueMember).
func (a *API) handleListAvailableTeams(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}
	weekIDStr, ok := a.weekIDFromRequest(w, r)
	if !ok {
		return
	}
	weekID, _ := db.ParseUUID(weekIDStr)

	teams, currentPick, hasCurrentPick, err := a.picksService.ListAvailableTeams(r.Context(), lc.Membership.ID, lc.League.ID, weekID, lc.League.Conference, lc.League.SeasonYear)
	if err != nil {
		log.Printf("list available teams: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list available teams")
		return
	}

	out := availableTeamsResponse{Teams: make([]availableTeamResponse, 0, len(teams))}
	currentPickLocked := false
	for _, t := range teams {
		out.Teams = append(out.Teams, availableTeamResponse{
			TeamID:          db.UUIDString(t.Row.TeamID),
			TeamName:        t.Row.TeamName,
			TeamLogoURL:     t.Row.TeamLogoUrl.String,
			OpponentTeamID:  db.UUIDString(t.Row.OpponentTeamID),
			OpponentName:    t.Row.OpponentName,
			OpponentLogoURL: t.Row.OpponentLogoUrl.String,
			GameID:          db.UUIDString(t.Row.GameID),
			KickoffAt:       formatTimestamp(t.Row.KickoffAt),
			IsHome:          t.Row.IsHome,
			IsLocked:        t.IsLocked,
			IsUsedElsewhere: t.IsUsedElsewhere,
			IsCurrentPick:   t.IsCurrentPick,
			WinProbability:  t.WinProbability,
			Spread:          t.Spread,
			SPPlusRank:      t.SPRank,
			OpponentSPRank:  t.OpponentSPRank,
			PickCount:       t.PickCount,
		})
		// The current pick's team is always one of this week's available
		// teams (its game was validated to belong to this week and its
		// conference to this league at submission time), so its lock
		// status is already computed right here — no extra game lookup
		// needed to populate CurrentPick below.
		if t.IsCurrentPick {
			currentPickLocked = t.IsLocked
		}
	}
	if hasCurrentPick {
		resp := toPickResponse(currentPick, currentPickLocked)
		out.CurrentPick = &resp
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListWeekPicks implements GET /leagues/:id/weeks/:weekId/picks
// (requireLeagueMember). Privacy rule: every OTHER member's game_id/team_id
// are included only once that pick's game has kicked off; the requester's
// own row always includes them when has_picked is true.
func (a *API) handleListWeekPicks(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}
	weekIDStr, ok := a.weekIDFromRequest(w, r)
	if !ok {
		return
	}
	weekID, _ := db.ParseUUID(weekIDStr)

	rows, err := a.picksService.ListWeekPicks(r.Context(), lc.League.ID, weekID)
	if err != nil {
		log.Printf("list week picks: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list picks")
		return
	}

	out := make([]memberPickStatusResponse, 0, len(rows))
	for _, row := range rows {
		resp := memberPickStatusResponse{
			MembershipID: db.UUIDString(row.Row.MembershipID),
			DisplayName:  row.Row.DisplayName,
			TeamName:     row.Row.TeamName.String,
			HasPicked:    row.HasPicked,
		}
		isOwn := row.Row.MembershipID == lc.Membership.ID
		if row.HasPicked && (isOwn || row.IsLocked) {
			resp.GameID = db.UUIDString(row.Row.GameID)
			resp.TeamID = db.UUIDString(row.Row.TeamID)
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListMembershipPicks implements GET
// /leagues/:id/members/:membershipId/picks (requireLeagueMember) — the
// leaderboard's per-contestant expandable pick history: every week of the
// league's season for one membership. Privacy rule: identical to
// handleListWeekPicks', just applied across a season of weeks for one
// membership instead of one week for every membership — the requester's
// own membership always shows full pick detail; another member's pick
// shows full detail only once its game has kicked off, otherwise just
// has_picked.
func (a *API) handleListMembershipPicks(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	membershipID, err := db.ParseUUID(chi.URLParam(r, "membershipId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid membership id")
		return
	}

	target, err := a.leaguesService.GetMembershipByID(r.Context(), membershipID)
	if err != nil {
		if errors.Is(err, leagues.ErrMembershipNotFound) {
			writeError(w, http.StatusBadRequest, "membership not found in this league")
			return
		}
		log.Printf("get membership for picks history: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load membership")
		return
	}
	if target.LeagueID != lc.League.ID {
		writeError(w, http.StatusBadRequest, "membership not found in this league")
		return
	}

	rows, err := a.picksService.ListMembershipPicksForSeason(r.Context(), membershipID, lc.League.SeasonYear)
	if err != nil {
		log.Printf("list membership picks for season: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list picks")
		return
	}

	// A currently-eliminated membership never picks again — showing a
	// long tail of "not picked" rows all the way through the rest of the
	// season reads as if they kept playing and simply forgot every week,
	// not as "they were out." Truncate at (inclusive of) their
	// elimination week instead. A bought-back membership is active again
	// (status flips back to 'active', though eliminated_week_id stays as
	// history per the buy-back contract — see leagues.BuyBackMember), so
	// this deliberately checks current Status, not whether
	// EliminatedWeekID is set.
	if target.Status == "eliminated" && target.EliminatedWeekID.Valid {
		var eliminatedAtWeekNumber int32 = -1
		for _, row := range rows {
			if row.Row.WeekID == target.EliminatedWeekID {
				eliminatedAtWeekNumber = row.Row.WeekNumber
				break
			}
		}
		if eliminatedAtWeekNumber >= 0 {
			truncated := rows[:0:0]
			for _, row := range rows {
				if row.Row.WeekNumber <= eliminatedAtWeekNumber {
					truncated = append(truncated, row)
				}
			}
			rows = truncated
		}
	}

	isOwn := membershipID == lc.Membership.ID
	out := make([]membershipWeekPickResponse, 0, len(rows))
	for _, row := range rows {
		resp := membershipWeekPickResponse{
			WeekNumber: row.Row.WeekNumber,
			HasPicked:  row.HasPicked,
			IsLocked:   row.IsLocked,
		}
		if row.HasPicked && (isOwn || row.IsLocked) {
			resp.GameID = db.UUIDString(row.Row.GameID)
			resp.TeamID = db.UUIDString(row.Row.TeamID)
			resp.TeamName = row.Row.TeamName.String
			resp.TeamLogoURL = row.Row.TeamLogoUrl.String
			resp.OpponentName = row.Row.OpponentName.String
			resp.OpponentLogoURL = row.Row.OpponentLogoUrl.String
			resp.IsHome = row.Row.HomeTeamID.Valid && row.Row.HomeTeamID == row.Row.TeamID
			resp.KickoffAt = formatTimestamp(row.Row.KickoffAt)
			resp.Result = row.Row.Result.String
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, out)
}
