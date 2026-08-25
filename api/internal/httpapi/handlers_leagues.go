package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
	"github.com/sdeitte/survivor-league-api/internal/recap"
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

	league, membership, err := a.leaguesService.CreateLeague(r.Context(), userID, req.Name, req.SeasonYear, req.Conference, req.TeamName)
	if err != nil {
		switch {
		case errors.Is(err, leagues.ErrTeamNameRequired):
			writeError(w, http.StatusBadRequest, "team_name is required")
		case errors.Is(err, leagues.ErrTeamNameTooLong):
			writeError(w, http.StatusBadRequest, "team_name is too long")
		default:
			log.Printf("create league: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to create league")
		}
		return
	}

	writeJSON(w, http.StatusCreated, toLeagueResponse(league, membership.ID, membership.Role, membership.IsContestant, membership.Status, membership.TeamName))
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
		out = append(out, toLeagueResponse(league, row.MembershipID, row.MemberRole, row.MemberIsContestant, row.MemberStatus, row.MemberTeamName))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleGetLeague(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}
	resp := toLeagueResponse(lc.League, lc.Membership.ID, lc.Membership.Role, lc.Membership.IsContestant, lc.Membership.Status, lc.Membership.TeamName)
	seasonComplete, err := a.leaguesService.IsSeasonComplete(r.Context(), lc.League.ID)
	if err != nil {
		log.Printf("get league: check season complete: %v", err)
	} else {
		resp.SeasonComplete = seasonComplete
	}
	writeJSON(w, http.StatusOK, resp)
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

	writeJSON(w, http.StatusOK, toLeagueResponse(league, lc.Membership.ID, lc.Membership.Role, isContestant, lc.Membership.Status, lc.Membership.TeamName))
}

// handleUpdateTeamName implements PATCH /leagues/:id/team-name
// (requireLeagueMember, requireLeagueOpen) — a member setting or changing
// their own team name at any time, including via the one-time backfill
// prompt the frontend shows when membershipSummary.TeamName is empty.
func (a *API) handleUpdateTeamName(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	var req updateTeamNameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	membership, err := a.leaguesService.UpdateTeamName(r.Context(), lc.League.ID, lc.Membership.UserID, req.TeamName)
	if err != nil {
		switch {
		case errors.Is(err, leagues.ErrTeamNameRequired):
			writeError(w, http.StatusBadRequest, "team_name is required")
		case errors.Is(err, leagues.ErrTeamNameTooLong):
			writeError(w, http.StatusBadRequest, "team_name is too long")
		case errors.Is(err, leagues.ErrMembershipNotFound):
			writeError(w, http.StatusForbidden, "not a member of this league")
		default:
			log.Printf("update team name: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to update team name")
		}
		return
	}

	writeJSON(w, http.StatusOK, toLeagueResponse(lc.League, membership.ID, membership.Role, membership.IsContestant, membership.Status, membership.TeamName))
}

// handleCloseLeague implements DELETE /leagues/:id (requireCommissioner —
// deliberately not chained with requireLeagueOpen, so closing an
// already-closed league still reaches leagues.Service.CloseLeague and gets
// a clean 409 rather than the generic "league is closed" 403). This is NOT
// a hard delete — see leagues.Service.CloseLeague's doc comment; the
// league, its memberships, and its picks/history all stay in the
// database, just no longer mutable.
//
// Requires the request body's "confirm" field to exactly match
// "I want to close {league name}" — the server-side half of the web/
// mobile confirmation modal that makes the commissioner type this out by
// hand (paste disabled client-side); this check exists so a bare API call
// can't bypass that confirmation.
func (a *API) handleCloseLeague(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	var body struct {
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	expected := "I want to close " + lc.League.Name
	if strings.TrimSpace(body.Confirm) != expected {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("confirmation text must exactly match %q", expected))
		return
	}

	league, members, err := a.leaguesService.CloseLeague(r.Context(), lc.League.ID, lc.Membership.UserID)
	if err != nil {
		if errors.Is(err, leagues.ErrLeagueAlreadyClosed) {
			writeError(w, http.StatusConflict, "this league is already closed")
			return
		}
		log.Printf("close league: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to close league")
		return
	}

	if a.notifyService != nil {
		for _, m := range members {
			if m.UserID == lc.Membership.UserID {
				continue // skip the commissioner who just performed the action
			}
			if err := a.notifyService.SendLeagueClosedEmail(r.Context(), m.Email, m.DisplayName, league.Name); err != nil {
				log.Printf("close league: send closed email to %s: %v", db.UUIDString(m.UserID), err)
			}
		}
	}

	writeJSON(w, http.StatusOK, toLeagueResponse(league, lc.Membership.ID, lc.Membership.Role, lc.Membership.IsContestant, lc.Membership.Status, lc.Membership.TeamName))
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

// handleGetLatestRecap implements GET /leagues/:id/recap (requireLeagueMember)
// — the most recently generated AI weekly recap for the league (see
// internal/recap.Service.GenerateWeekRecap), regardless of which week it
// was for. 404 if no week has finalized yet (a brand-new league).
func (a *API) handleGetLatestRecap(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	rec, err := a.recapService.GetLatestRecap(r.Context(), lc.League.ID)
	if err != nil {
		if errors.Is(err, recap.ErrNoRecapYet) {
			writeError(w, http.StatusNotFound, "no recap generated yet for this league")
			return
		}
		log.Printf("get latest recap: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load recap")
		return
	}

	writeJSON(w, http.StatusOK, weekRecapResponse{
		Body:        rec.Body,
		GeneratedAt: formatTimestamp(rec.GeneratedAt),
	})
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
		case errors.Is(err, leagues.ErrBuyBackWindowClosed):
			writeError(w, http.StatusConflict, "buy-backs are no longer allowed — the cutoff week's games have already begun")
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
	joinable, err := a.isLeagueJoinable(r.Context(), lc.League)
	if err != nil {
		log.Printf("get invite code: check joinable: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load invite code")
		return
	}
	writeJSON(w, http.StatusOK, inviteCodeResponse{InviteCode: lc.League.InviteCode, Joinable: joinable})
}

func (a *API) handleRegenerateInviteCode(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}
	joinable, err := a.isLeagueJoinable(r.Context(), lc.League)
	if err != nil {
		log.Printf("regenerate invite code: check joinable: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to regenerate invite code")
		return
	}
	if !joinable {
		writeError(w, http.StatusForbidden, "this league has already started and isn't accepting new members")
		return
	}

	updated, err := a.leaguesService.RegenerateInviteCode(r.Context(), lc.League.ID)
	if err != nil {
		log.Printf("regenerate invite code: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to regenerate invite code")
		return
	}
	writeJSON(w, http.StatusOK, inviteCodeResponse{InviteCode: updated.InviteCode, Joinable: joinable})
}

// maxInvitesPerRequest caps a single POST .../invite/send batch — plenty
// of headroom for this app's scale (friends-and-family leagues), small
// enough to keep one commissioner action from turning into a mass-mail
// blast.
const maxInvitesPerRequest = 25

// handleSendInvites implements POST /leagues/:id/invite/send
// (requireCommissioner, requireLeagueOpen). Sends the league's existing
// shareable invite code/link by email to a batch of name+email pairs — no
// new per-recipient tracking state, just reuses lc.League.InviteCode via
// notify.Service.SendLeagueInviteEmail. Deliberately best-effort per
// recipient rather than all-or-nothing: one bad email in a batch of 20
// shouldn't silently swallow the other 19, so this always responds 200
// with a per-recipient sent/error breakdown once the request itself is
// well-formed (only an empty or oversized batch is a 400).
func (a *API) handleSendInvites(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}
	joinable, err := a.isLeagueJoinable(r.Context(), lc.League)
	if err != nil {
		log.Printf("send invites: check joinable: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to send invites")
		return
	}
	if !joinable {
		writeError(w, http.StatusForbidden, "this league has already started and isn't accepting new members")
		return
	}

	var body struct {
		Invites []struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"invites"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.Invites) == 0 {
		writeError(w, http.StatusBadRequest, "at least one invite is required")
		return
	}
	if len(body.Invites) > maxInvitesPerRequest {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("too many invites in one request (max %d)", maxInvitesPerRequest))
		return
	}
	if a.notifyService == nil {
		writeError(w, http.StatusInternalServerError, "email delivery is not configured")
		return
	}

	results := make([]inviteSendResultResponse, 0, len(body.Invites))
	seen := make(map[string]bool, len(body.Invites))
	for _, inv := range body.Invites {
		email := strings.TrimSpace(inv.Email)
		name := strings.TrimSpace(inv.Name)

		if email == "" {
			results = append(results, inviteSendResultResponse{Email: email, Sent: false, Error: "email is required"})
			continue
		}
		if _, err := mail.ParseAddress(email); err != nil {
			results = append(results, inviteSendResultResponse{Email: email, Sent: false, Error: "invalid email address"})
			continue
		}
		normalized := strings.ToLower(email)
		if seen[normalized] {
			results = append(results, inviteSendResultResponse{Email: email, Sent: false, Error: "duplicate in this request"})
			continue
		}
		seen[normalized] = true

		if err := a.notifyService.SendLeagueInviteEmail(r.Context(), email, name, lc.League.Name, lc.League.Conference, lc.League.SeasonYear, lc.League.InviteCode); err != nil {
			log.Printf("send invite email to %s: %v", email, err)
			results = append(results, inviteSendResultResponse{Email: email, Sent: false, Error: "failed to send email"})
			continue
		}
		results = append(results, inviteSendResultResponse{Email: email, Sent: true})
	}

	writeJSON(w, http.StatusOK, results)
}

// maxBroadcastMessageLength is a sanity cap, not a design constraint —
// mirrors internal/chat's identical reasoning for its own message-length
// cap.
const maxBroadcastMessageLength = 5000

// handleListMemberEmails implements GET /leagues/:id/members/emails
// (requireCommissioner). The commissioner-only address book: every
// non-removed member's email, backing both the frontend's "copy all
// emails" button and the compose-broadcast screen. Deliberately a
// separate endpoint from GET .../members (member-visible, no email
// column) rather than an added field there — a member's email is not
// something every other member should be able to see.
func (a *API) handleListMemberEmails(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	rows, err := a.leaguesService.ListMemberEmails(r.Context(), lc.League.ID)
	if err != nil {
		log.Printf("list member emails: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list member emails")
		return
	}

	out := make([]memberEmailResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, memberEmailResponse{
			MembershipID: db.UUIDString(row.MembershipID),
			Email:        row.Email,
			DisplayName:  row.DisplayName,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleBroadcastEmail implements POST /leagues/:id/broadcast-email
// (requireCommissioner). Sends one email to every current member of the
// league from a fixed noreply address (see notify.Service.
// SendLeagueBroadcastEmail) — deliberately not gated by RequireLeagueOpen,
// since a commissioner may well want to email participants after closing
// the league (e.g. a wrap-up message). Best-effort per recipient, same
// reasoning as handleSendInvites: one bad/bounced address must not
// silently swallow the rest of the league's emails.
func (a *API) handleBroadcastEmail(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	var req broadcastEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Subject = strings.TrimSpace(req.Subject)
	req.Message = strings.TrimSpace(req.Message)
	if req.Subject == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if len(req.Message) > maxBroadcastMessageLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("message is too long (max %d characters)", maxBroadcastMessageLength))
		return
	}
	if a.notifyService == nil {
		writeError(w, http.StatusInternalServerError, "email delivery is not configured")
		return
	}

	members, err := a.leaguesService.ListMemberEmails(r.Context(), lc.League.ID)
	if err != nil {
		log.Printf("broadcast email: list members: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load league members")
		return
	}

	results := make([]inviteSendResultResponse, 0, len(members))
	for _, m := range members {
		if err := a.notifyService.SendLeagueBroadcastEmail(r.Context(), m.Email, m.DisplayName, lc.League.Name, req.Subject, req.Message); err != nil {
			log.Printf("broadcast email to %s: %v", m.Email, err)
			results = append(results, inviteSendResultResponse{Email: m.Email, Sent: false, Error: "failed to send email"})
			continue
		}
		results = append(results, inviteSendResultResponse{Email: m.Email, Sent: true})
	}

	writeJSON(w, http.StatusOK, results)
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

	joinable, err := a.isLeagueJoinable(r.Context(), league)
	if err != nil {
		log.Printf("preview invite: check joinable: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to look up invite")
		return
	}

	writeJSON(w, http.StatusOK, invitePreviewResponse{
		LeagueName: league.Name,
		Conference: league.Conference,
		SeasonYear: league.SeasonYear,
		Joinable:   joinable,
	})
}

// isLeagueJoinable is shared by handlePreviewInvite (so an anonymous
// visitor sees "this league has already started" up front) and
// handleJoinByCode (which enforces it). A league stops accepting new
// members once it's closed, or once its conference's week 1 has no
// pickable games left — see schedule.Service.IsFirstWeekPickableForConference's
// doc comment for why that's the chosen "season has started" line rather
// than "any game anywhere has kicked off".
func (a *API) isLeagueJoinable(ctx context.Context, league gen.League) (bool, error) {
	if league.Status == "closed" {
		return false, nil
	}
	return a.scheduleService.IsFirstWeekPickableForConference(ctx, league.SeasonYear, league.Conference, time.Now())
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

	var req joinByCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
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
	if league.Status == "closed" {
		writeError(w, http.StatusForbidden, "this league is closed")
		return
	}
	joinable, err := a.isLeagueJoinable(r.Context(), league)
	if err != nil {
		log.Printf("join by code: check joinable: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to check league schedule")
		return
	}
	if !joinable {
		writeError(w, http.StatusForbidden, "this league has already started and isn't accepting new members")
		return
	}

	membership, err := a.leaguesService.JoinByCode(r.Context(), league.ID, userID, req.TeamName)
	if err != nil {
		switch {
		case errors.Is(err, leagues.ErrAlreadyMember):
			writeError(w, http.StatusConflict, "already a member of this league")
		case errors.Is(err, leagues.ErrTeamNameRequired):
			writeError(w, http.StatusBadRequest, "team_name is required")
		case errors.Is(err, leagues.ErrTeamNameTooLong):
			writeError(w, http.StatusBadRequest, "team_name is too long")
		default:
			log.Printf("join by code: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to join league")
		}
		return
	}

	writeJSON(w, http.StatusOK, toLeagueResponse(league, membership.ID, membership.Role, membership.IsContestant, membership.Status, membership.TeamName))
}
