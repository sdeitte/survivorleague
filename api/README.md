# api

Go module for the Survivor League REST API. Phase 0 scaffolding: chi router,
a Postgres-backed `/health` check, goose migrations, and empty `internal/`
package stubs for later phases. No feature logic yet.

See the full plan: `/Users/sdeitte/.claude/plans/witty-questing-barto.md`.

## Requirements

- Go 1.23+
- Postgres 16 (the repo-root `docker-compose.yml` provides one locally)

## Environment variables

| Var            | Required | Default | Notes                                   |
|----------------|----------|---------|------------------------------------------|
| `DATABASE_URL` | yes      | —       | e.g. `postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable` |
| `PORT`         | no       | `8080`  | HTTP port the server listens on          |

## Run the server

```sh
export DATABASE_URL="postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
go run ./cmd/server
# GET http://localhost:8080/health -> {"status":"ok","db":"ok"}
```

## Migrations

Migrations live in `migrations/` and run via `goose` (through the
`cmd/migrate` wrapper, so no separate goose install is required).

```sh
export DATABASE_URL="postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"

go run ./cmd/migrate up       # apply all pending migrations
go run ./cmd/migrate status   # show applied/pending state
go run ./cmd/migrate down     # roll back the most recent migration
```

`00001_init.sql` creates the full Phase 0 schema (users, leagues,
league_memberships, league_invites, teams, weeks, games, picks,
league_week_results, device_tokens, notification_preferences,
notifications_log, refresh_tokens, audit_log, sync_runs) — see the plan's
"Data Model" section for rationale on the unique constraints.

## Build / vet

```sh
go build ./...
go vet ./...
```

## Layout

```
cmd/server/    HTTP server entrypoint (chi router, /health)
cmd/migrate/   goose migration runner wrapper
internal/      auth, leagues, picks, schedule, grading, notify, admin,
               httpapi, db — empty package stubs for now, filled in
               phase-by-phase per the roadmap
migrations/    goose SQL migrations
openapi/       openapi.yaml, the source-of-truth API spec
```
