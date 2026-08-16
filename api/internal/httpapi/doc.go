// Package httpapi wires chi routes, middleware (auth/site-admin — league
// member/commissioner middleware lands in Phase 2), and HTTP handlers on
// top of internal/auth and the sqlc-generated internal/db/gen layer.
package httpapi
