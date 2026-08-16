package httpapi

import (
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
