package httpapi

import (
	"context"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

type authCtxKey struct{}

// AuthContext is the request-scoped identity attached by RequireAuth.
type AuthContext struct {
	UserID      string
	IsSiteAdmin bool
}

func withAuthContext(ctx context.Context, ac AuthContext) context.Context {
	return context.WithValue(ctx, authCtxKey{}, ac)
}

// AuthFromContext retrieves the AuthContext set by RequireAuth. ok is false
// if called outside a RequireAuth-protected handler.
func AuthFromContext(ctx context.Context) (AuthContext, bool) {
	ac, ok := ctx.Value(authCtxKey{}).(AuthContext)
	return ac, ok
}

type leagueCtxKey struct{}

// LeagueContext is the request-scoped league + membership state attached by
// RequireLeagueMember (and, transitively, RequireCommissioner). Handlers
// downstream of either middleware can read it instead of re-querying the
// league/membership rows already looked up during the access check.
type LeagueContext struct {
	League gen.League
	// Membership is the *requester's own* membership row in League — not
	// the target of the request (e.g. for DELETE .../members/:membershipId,
	// this is the acting commissioner's membership, not the one being
	// removed).
	Membership gen.LeagueMembership
}

func withLeagueContext(ctx context.Context, lc LeagueContext) context.Context {
	return context.WithValue(ctx, leagueCtxKey{}, lc)
}

// LeagueFromContext retrieves the LeagueContext set by RequireLeagueMember.
// ok is false if called outside a RequireLeagueMember-protected handler.
func LeagueFromContext(ctx context.Context) (LeagueContext, bool) {
	lc, ok := ctx.Value(leagueCtxKey{}).(LeagueContext)
	return lc, ok
}
