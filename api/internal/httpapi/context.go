package httpapi

import "context"

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
