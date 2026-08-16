// Package db holds the sqlc-generated database access layer (in the gen
// subpackage) plus small pgx/pgtype helpers shared across internal
// packages (UUID conversion, pool setup).
//
// Query definitions live in queries/*.sql; `sqlc generate` (run from
// api/, see sqlc.yaml) regenerates gen/*.go from them — gen/ is
// committed but never hand-edited.
package db
