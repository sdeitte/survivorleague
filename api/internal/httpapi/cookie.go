package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/sdeitte/survivor-league-api/internal/auth"
)

const refreshCookieName = "refresh_token"

// maxRefreshBodyBytes bounds the body read in extractRefreshToken — the
// body is only ever a tiny {"refresh_token": "..."} object.
const maxRefreshBodyBytes = 1 << 12 // 4KB

// setRefreshCookie sets the rotating refresh token as an httpOnly cookie.
// Secure is only set when appEnv is "production" — local dev over plain
// http can't set/receive Secure cookies, and SameSite=Strict already
// prevents cross-site sending.
func (a *API) setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.appEnv == "production",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(auth.RefreshTokenTTL.Seconds()),
	})
}

func (a *API) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.appEnv == "production",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// extractRefreshToken reads the refresh token from the refresh_token
// cookie if present (the web client's path — it never sees the raw
// token), else falls back to a JSON body {"refresh_token": "..."} (the
// mobile client's path — no cookie jar, so it sends the token it has in
// secure storage explicitly). Returns "" if neither is present.
func extractRefreshToken(r *http.Request) string {
	if c, err := r.Cookie(refreshCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	if r.Body == nil {
		return ""
	}
	var body refreshRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRefreshBodyBytes))
	if err := dec.Decode(&body); err != nil {
		return ""
	}
	return body.RefreshToken
}
