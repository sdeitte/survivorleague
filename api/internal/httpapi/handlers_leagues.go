package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
)

func (a *API) handleListConferences(w http.ResponseWriter, r *http.Request) {
	conferences, err := a.scheduleService.ListEligibleConferences(r.Context())
	if err != nil {
		log.Printf("list eligible conferences: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list conferences")
		return
	}
	// nil vs [] both marshal to "null" vs "[]" in Go's encoding/json — force
	// an empty slice (not nil) so a no-sync-yet response is a valid "[]",
	// not "null", for clients that don't special-case null arrays.
	if conferences == nil {
		conferences = []string{}
	}
	writeJSON(w, http.StatusOK, conferences)
}

func (a *API) handleCreateLeague(w http.ResponseWriter, r *http.Request) {
	ac, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createLeagueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !validateSeasonYear(req.SeasonYear) {
		writeError(w, http.StatusBadRequest, "season_year must be a reasonable 4-digit year")
		return
	}
	eligible, err := a.scheduleService.IsEligibleConference(r.Context(), req.Conference)
	if err != nil {
		log.Printf("check eligible conference: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to validate conference")
		return
	}
	if !eligible {
		writeError(w, http.StatusBadRequest, "conference is not eligible for a league (unrecognized, FBS Independents, or fewer than 13 synced teams)")
		return
	}

	userID, err := db.ParseUUID(ac.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	league, membership, err := a.leaguesService.CreateLeague(r.Context(), userID, req.Name, req.SeasonYear, req.Conference)
	if err != nil {
		log.Printf("create league: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create league")
		return
	}

	writeJSON(w, http.StatusCreated, toLeagueResponse(league, membership.ID, membership.Role, membership.IsContestant, membership.Status))
}

func (a *API) handleListLeagues(w http.ResponseWriter, r *http.Request) {
	ac, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID, err := db.ParseUUID(ac.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	rows, err := a.leaguesService.ListLeaguesForUser(r.Context(), userID)
	if err != nil {
		log.Printf("list leagues: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list leagues")
		return
	}

	out := make([]leagueResponse, 0, len(rows))
	for _, row := range rows {
		league := gen.League{
			ID:                 row.ID,
			Name:               row.Name,
			SeasonYear:         row.SeasonYear,
			Conference:         row.Conference,
			CommissionerUserID: row.CommissionerUserID,
			InviteCode:         row.InviteCode,
			Status:             row.Status,
			CreatedAt:          row.CreatedAt,
			UpdatedAt:          row.UpdatedAt,
		}
		out = append(out, toLeagueResponse(league, row.MembershipID, row.MemberRole, row.MemberIsContestant, row.MemberStatus))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleGetLeague(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}
	writeJSON(w, http.StatusOK, toLeagueResponse(lc.League, lc.Membership.ID, lc.Membership.Role, lc.Membership.IsContestant, lc.Membership.Status))
}

// handleUpdateLeague implements PATCH /leagues/:id. `name` renames the
// league; `commissioner_is_contestant` updates *the acting commissioner's
// own* league_memberships.is_contestant flag — this is intentionally not a
// general member-editing mechanism. `conference` and `season_year` are
// immutable: any attempt to set either (even to their current value) is
// rejected with 400, checked against the raw request body before the
// typed updateLeagueRequest (which has no fields for them at all) ever
// comes into play.
func (a *API) handleUpdateLeague(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, present := raw["conference"]; present {
		writeError(w, http.StatusBadRequest, "conference cannot be changed after league creation")
		return
	}
	if _, present := raw["season_year"]; present {
		writeError(w, http.StatusBadRequest, "season_year cannot be changed after league creation")
		return
	}

	league := lc.League
	isContestant := lc.Membership.IsContestant

	if nameRaw, present := raw["name"]; present {
		var name string
		if err := json.Unmarshal(nameRaw, &name); err != nil {
			writeError(w, http.StatusBadRequest, "invalid name")
			return
		}
		name = strings.TrimSpace(name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		updated, err := a.leaguesService.UpdateLeagueName(r.Context(), league.ID, name)
		if err != nil {
			log.Printf("update league name: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to update league")
			return
		}
		league = updated
	}

	if cicRaw, present := raw["commissioner_is_contestant"]; present {
		var value bool
		if err := json.Unmarshal(cicRaw, &value); err != nil {
			writeError(w, http.StatusBadRequest, "invalid commissioner_is_contestant")
			return
		}
		updatedMembership, err := a.leaguesService.UpdateCommissionerIsContestant(r.Context(), lc.Membership.ID, value)
		if err != nil {
			log.Printf("update commissioner is_contestant: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to update league")
			return
		}
		isContestant = updatedMembership.IsContestant
	}

	writeJSON(w, http.StatusOK, toLeagueResponse(league, lc.Membership.ID, lc.Membership.Role, isContestant, lc.Membership.Status))
}

func (a *API) handleListMembers(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	rows, err := a.leaguesService.ListMembers(r.Context(), lc.League.ID)
	if err != nil {
		log.Printf("list members: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	out := make([]memberResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, memberResponse{
			MembershipID: db.UUIDString(row.MembershipID),
			UserID:       db.UUIDString(row.UserID),
			DisplayName:  row.DisplayName,
			Role:         row.Role,
			IsContestant: row.IsContestant,
			Status:       row.Status,
			BoughtBack:   row.BoughtBack,
			JoinedAt:     formatTimestamp(row.JoinedAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetLeaderboard implements GET /leagues/:id/leaderboard
// (requireLeagueMember). See internal/leagues.Service.ListLeaderboard /
// ListLeaderboardForLeague's query comment for the exact sort contract:
// active members first, then eliminated members ordered by how late they
// were eliminated (survived longer ranks higher).
func (a *API) handleGetLeaderboard(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	rows, err := a.leaguesService.ListLeaderboard(r.Context(), lc.League.ID)
	if err != nil {
		log.Printf("get leaderboard: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load leaderboard")
		return
	}

	out := make([]leaderboardEntryResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toLeaderboardEntryResponse(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRemoveMember implements DELETE /leagues/:id/members/:membershipId.
// Soft-deletes (sets removed_at) the target membership. Two rejection
// cases, both drawn from the API contract: 403 if the commissioner targets
// their own membership (self-removal isn't allowed through this endpoint),
// 400 if membershipId doesn't resolve to a currently-active row scoped to
// this league (wrong league, already removed, or nonexistent — these are
// deliberately not distinguished in the response).
func (a *API) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
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

	if membershipID == lc.Membership.ID {
		writeError(w, http.StatusForbidden, "cannot remove your own commissioner membership")
		return
	}

	if _, err := a.leaguesService.RemoveMember(r.Context(), lc.League.ID, membershipID); err != nil {
		if errors.Is(err, leagues.ErrMembershipNotFound) {
			writeError(w, http.StatusBadRequest, "membership not found in this league")
			return
		}
		log.Printf("remove member: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleBuyBackMember implements
// POST /leagues/:id/members/:membershipId/buyback (Phase 6, requireCommissioner).
// Reinstates an eliminated member on their one-time buy-back lifeline. See
// leagues.Service.BuyBackMember for the exact validation order/error
// mapping; the response is the updated membership record in full
// (membershipResponse), not the trimmed membershipSummary used elsewhere.
func (a *API) handleBuyBackMember(w http.ResponseWriter, r *http.Request) {
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

	updated, err := a.leaguesService.BuyBackMember(r.Context(), lc.League.ID, membershipID, lc.Membership.UserID)
	if err != nil {
		switch {
		case errors.Is(err, leagues.ErrMembershipNotFound):
			writeError(w, http.StatusBadRequest, "membership not found in this league")
			return
		case errors.Is(err, leagues.ErrNotEliminated):
			writeError(w, http.StatusBadRequest, "member is not currently eliminated — nothing to buy back")
			return
		case errors.Is(err, leagues.ErrAlreadyBoughtBack):
			writeError(w, http.StatusConflict, "member has already used their one-time buy-back")
			return
		}
		log.Printf("buy back member: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to buy back member")
		return
	}

	writeJSON(w, http.StatusOK, toMembershipResponse(updated))
}

func (a *API) handleGetInviteCode(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}
	writeJSON(w, http.StatusOK, inviteCodeResponse{InviteCode: lc.League.InviteCode})
}

func (a *API) handleRegenerateInviteCode(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	updated, err := a.leaguesService.RegenerateInviteCode(r.Context(), lc.League.ID)
	if err != nil {
		log.Printf("regenerate invite code: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to regenerate invite code")
		return
	}
	writeJSON(w, http.StatusOK, inviteCodeResponse{InviteCode: updated.InviteCode})
}

func (a *API) handlePreviewInvite(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	league, err := a.leaguesService.GetLeagueByInviteCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, leagues.ErrLeagueNotFound) {
			writeError(w, http.StatusNotFound, "invite code not found")
			return
		}
		log.Printf("preview invite: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to look up invite")
		return
	}

	writeJSON(w, http.StatusOK, invitePreviewResponse{
		LeagueName: league.Name,
		Conference: league.Conference,
		SeasonYear: league.SeasonYear,
	})
}

func (a *API) handleJoinByCode(w http.ResponseWriter, r *http.Request) {
	ac, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID, err := db.ParseUUID(ac.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	code := chi.URLParam(r, "code")
	league, err := a.leaguesService.GetLeagueByInviteCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, leagues.ErrLeagueNotFound) {
			writeError(w, http.StatusNotFound, "invite code not found")
			return
		}
		log.Printf("join by code: get league: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to look up invite")
		return
	}

	membership, err := a.leaguesService.JoinByCode(r.Context(), league.ID, userID)
	if err != nil {
		if errors.Is(err, leagues.ErrAlreadyMember) {
			writeError(w, http.StatusConflict, "already a member of this league")
			return
		}
		log.Printf("join by code: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to join league")
		return
	}

	writeJSON(w, http.StatusOK, toLeagueResponse(league, membership.ID, membership.Role, membership.IsContestant, membership.Status))
}
