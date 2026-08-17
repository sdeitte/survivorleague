// Package schedule handles CFBD ingestion and read access to teams, weeks,
// and games.
//
// Phase 2 added only the canonical FBS conference list (conferences.go),
// needed by league creation before CFBD sync existed.
//
// Phase 3 adds the CFBD client (cfbd_client.go, cfbd_types.go) and the
// idempotent upsert sync (sync.go, Service.SyncSeason) — pulling FBS teams,
// the regular-season calendar, and regular-season games. Read-only access
// for the HTTP layer (GET /weeks, /weeks/:id/games, /games/:id, /teams)
// lives in internal/httpapi, backed directly by sqlc queries rather than
// this package, matching how internal/leagues exposes reads.
//
// Explicitly NOT in this phase: live in-game score polling and the
// grading/elimination pipeline (Phase 5). Games synced before they're
// played get status='scheduled'; games CFBD already reports final (e.g.
// syncing mid-season) are stored as status='final' with scores, but nothing
// here watches for a scheduled game transitioning to final in real time —
// that's the Phase 5 poll loop's job. See the roadmap in
// /Users/sdeitte/.claude/plans/witty-questing-barto.md.
package schedule
