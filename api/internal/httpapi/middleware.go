package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
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
// No route uses this yet — there are no admin endpoints yet (those land in
// Phase 8) — but it's built now per the plan's "Auth & RBAC" section as
// part of the auth foundation, since it's cheap and self-contained.
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

// leagueIDURLParam is the URL param name every /leagues/{id}/... route
// uses for the league id, read by RequireLeagueMember.
const leagueIDURLParam = "id"

// RequireLeagueMember wraps RequireAuth. It resolves the league from the
// `id` URL param, requires the caller to have a non-removed
// league_memberships row for it (403 otherwise), and attaches both to the
// request context as a LeagueContext for downstream handlers.
//
// A membership with status='eliminated' still counts as access — only
// removed_at (a commissioner removing the member, distinct from gameplay
// elimination) blocks access. If the league itself doesn't exist, this
// responds 404 rather than 403, so callers can tell "wrong league id" from
// "not a member of this league".
func (a *API) RequireLeagueMember(next http.Handler) http.Handler {
	return a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ok := AuthFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		leagueID, err := db.ParseUUID(chi.URLParam(r, leagueIDURLParam))
		if err != nil {
			writeError(w, http.StatusNotFound, "league not found")
			return
		}
		userID, err := db.ParseUUID(ac.UserID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		league, err := a.leaguesService.GetLeagueByID(r.Context(), leagueID)
		if err != nil {
			if errors.Is(err, leagues.ErrLeagueNotFound) {
				writeError(w, http.StatusNotFound, "league not found")
				return
			}
			log.Printf("requireLeagueMember: get league: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load league")
			return
		}

		membership, err := a.leaguesService.GetActiveMembership(r.Context(), leagueID, userID)
		if err != nil {
			if errors.Is(err, leagues.ErrMembershipNotFound) {
				writeError(w, http.StatusForbidden, "not a member of this league")
				return
			}
			log.Printf("requireLeagueMember: get membership: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load membership")
			return
		}

		ctx := withLeagueContext(r.Context(), LeagueContext{League: league, Membership: membership})
		next.ServeHTTP(w, r.WithContext(ctx))
	}))
}

// RequireCommissioner wraps RequireLeagueMember, additionally requiring
// role='commissioner' on the caller's membership (403 otherwise). Since it
// wraps RequireLeagueMember, any route carrying RequireCommissioner also
// satisfies "at least requireLeagueMember".
func (a *API) RequireCommissioner(next http.Handler) http.Handler {
	return a.RequireLeagueMember(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lc, ok := LeagueFromContext(r.Context())
		if !ok || lc.Membership.Role != "commissioner" {
			writeError(w, http.StatusForbidden, "commissioner access required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
