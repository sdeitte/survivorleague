// Package httpapi wires chi routes, middleware (RequireAuth/
// RequireSiteAdmin/RequireLeagueMember/RequireCommissioner), and HTTP
// handlers on top of internal/auth, internal/leagues, internal/schedule,
// and the sqlc-generated internal/db/gen layer.
package httpapi
