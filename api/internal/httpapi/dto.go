package httpapi

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type updateMeRequest struct {
	DisplayName string `json:"display_name"`
}

type userResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsSiteAdmin bool   `json:"is_site_admin"`
}

// sessionResponse covers all three token-issuing endpoints. RefreshToken
// is populated on all of them: web clients ignore it (they rely on the
// httpOnly cookie, which is also always set), while mobile clients — which
// have no cookie jar — read it from here and store it via
// expo-secure-store. Deliberate deviation from a literal reading of the
// Phase 1 spec (which only documented refresh_token in the /auth/refresh
// body): without it, mobile would have no way to obtain a refresh token at
// all on initial register/login, since it can't read Set-Cookie.
type sessionResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token,omitempty"`
	User         userResponse `json:"user"`
}

func toUserResponse(u gen.User) userResponse {
	return userResponse{
		ID:          db.UUIDString(u.ID),
		Email:       u.Email,
		DisplayName: u.DisplayName,
		IsSiteAdmin: u.IsSiteAdmin,
	}
}

func formatTimestamp(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

// --- Leagues ---

type createLeagueRequest struct {
	Name       string `json:"name"`
	SeasonYear int32  `json:"season_year"`
	Conference string `json:"conference"`
}

// updateLeagueRequest covers the two fields PATCH /leagues/:id may change.
// conference/season_year are deliberately absent here — the handler
// decodes the body separately as a raw map first specifically to detect
// (and reject with 400) any attempt to set those immutable fields, before
// ever reaching this struct.
type updateLeagueRequest struct {
	Name                     *string `json:"name"`
	CommissionerIsContestant *bool   `json:"commissioner_is_contestant"`
}

// membershipSummary is the requester's own role/status within a league, as
// embedded in leagueResponse.
type membershipSummary struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	IsContestant bool   `json:"is_contestant"`
	Status       string `json:"status"`
}

// leagueResponse is returned by every endpoint that hands back a league
// together with the requester's membership in it (create, list, get,
// update, join). It deliberately omits invite_code — that's only exposed
// via the dedicated GET/POST /leagues/:id/invite... endpoints.
type leagueResponse struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	SeasonYear         int32             `json:"season_year"`
	Conference         string            `json:"conference"`
	CommissionerUserID string            `json:"commissioner_user_id"`
	Status             string            `json:"status"`
	CreatedAt          string            `json:"created_at"`
	Membership         membershipSummary `json:"membership"`
}

func toLeagueResponse(league gen.League, membershipID pgtype.UUID, role string, isContestant bool, status string) leagueResponse {
	return leagueResponse{
		ID:                 db.UUIDString(league.ID),
		Name:               league.Name,
		SeasonYear:         league.SeasonYear,
		Conference:         league.Conference,
		CommissionerUserID: db.UUIDString(league.CommissionerUserID),
		Status:             league.Status,
		CreatedAt:          formatTimestamp(league.CreatedAt),
		Membership: membershipSummary{
			ID:           db.UUIDString(membershipID),
			Role:         role,
			IsContestant: isContestant,
			Status:       status,
		},
	}
}

type memberResponse struct {
	MembershipID string `json:"membership_id"`
	UserID       string `json:"user_id"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	IsContestant bool   `json:"is_contestant"`
	Status       string `json:"status"`
	JoinedAt     string `json:"joined_at"`
}

type inviteCodeResponse struct {
	InviteCode string `json:"invite_code"`
}

type invitePreviewResponse struct {
	LeagueName string `json:"league_name"`
	Conference string `json:"conference"`
	SeasonYear int32  `json:"season_year"`
}
