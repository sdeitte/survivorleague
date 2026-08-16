package httpapi

import (
	"net/http"
	"strings"
)

// RequireAuth parses `Authorization: Bearer <token>`, validates its
// signature and expiry, and attaches the caller's identity to the request
// context (retrievable via AuthFromContext). Responds 401 on any missing,
// malformed, invalid, or expired token.
func (a *API) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) || strings.TrimPrefix(header, prefix) == "" {
			writeError(w, http.StatusUnauthorized, "missing or malformed authorization header")
			return
		}

		raw := strings.TrimPrefix(header, prefix)
		claims, err := a.jwt.ParseAccessToken(raw)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}

		ctx := withAuthContext(r.Context(), AuthContext{
			UserID:      claims.Subject,
			IsSiteAdmin: claims.IsSiteAdmin,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireSiteAdmin wraps RequireAuth and additionally requires
// is_site_admin=true on the caller, responding 403 otherwise.
//
// No route uses this in Phase 1 — there are no admin endpoints yet (those
// land in Phase 8) — but it's built now per the plan's "Auth & RBAC"
// section as part of the auth foundation, since it's cheap and
// self-contained.
//
// requireLeagueMember / requireCommissioner are intentionally NOT built
// here: they depend on the league_memberships table and league routes,
// which don't exist until Phase 2.
func (a *API) RequireSiteAdmin(next http.Handler) http.Handler {
	return a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ok := AuthFromContext(r.Context())
		if !ok || !ac.IsSiteAdmin {
			writeError(w, http.StatusForbidden, "site admin access required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
