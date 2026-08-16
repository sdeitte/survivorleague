# api

Go module for the Survivor League REST API. Phase 1: auth
(register/login/refresh/logout), `GET`/`PATCH /me`, the
`requireAuth`/`requireSiteAdmin` middleware, sqlc-generated DB access, and
the Phase 0 Postgres-backed `/health` check.

See the full plan: `/Users/sdeitte/.claude/plans/witty-questing-barto.md`.

## Requirements

- Go 1.23+
- Postgres 16 (the repo-root `docker-compose.yml` provides one locally)
- `sqlc` CLI, dev-time only (`brew install sqlc` or
  `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`) — needed only when
  changing queries, not to build/run the server

## Environment variables

| Var                    | Required | Default                 | Notes                                                                 |
|------------------------|----------|--------------------------|------------------------------------------------------------------------|
| `DATABASE_URL`         | yes      | —                        | e.g. `postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable` |
| `JWT_SECRET`           | yes      | —                        | HS256 signing secret for access tokens. Dev-only default in `.env.example` — never reuse it anywhere real. |
| `APP_ENV`              | no       | `development`            | `development` \| `production`. Gates the refresh-token cookie's `Secure` flag (local dev over http can't set `Secure` cookies). |
| `ADMIN_EMAIL`          | no       | unset                    | If set, a `POST /auth/register` whose `email` case-insensitively matches this gets `is_site_admin=true` auto-set — the site-admin bootstrap path (no manual DB surgery needed). |
| `CORS_ALLOWED_ORIGIN`  | no       | `http://localhost:5173`  | Single exact origin allowed to call the API with credentials (cookies). Wildcards don't work with `credentials: 'include'` per browser spec. |
| `PORT`                 | no       | `8080`                   | HTTP port the server listens on |

## Run the server

```sh
export DATABASE_URL="postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
export JWT_SECRET="dev-only-insecure-secret-change-me"
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

`00001_init.sql` creates the full schema (users, leagues,
league_memberships, league_invites, teams, weeks, games, picks,
league_week_results, device_tokens, notification_preferences,
notifications_log, refresh_tokens, audit_log, sync_runs) — see the plan's
"Data Model" section for rationale on the unique constraints.

## Database access (sqlc)

Query access goes through [sqlc](https://sqlc.dev), not an ORM or
hand-written query strings in handlers — this schema leans on unique
constraints, row-locking, and `SKIP LOCKED`, which read most naturally as
plain SQL.

- Annotated SQL lives in `internal/db/queries/*.sql`.
- `sqlc generate` (run from `api/`, config in `sqlc.yaml`) regenerates
  `internal/db/gen/*.go` — generated, committed, never hand-edited.
- `internal/db/uuid.go` has small `pgtype.UUID` <-> `string` helpers shared
  by every package that talks to the generated layer.

```sh
cd api
sqlc generate
```

Re-run `sqlc generate` any time `internal/db/queries/*.sql` or the schema
in `migrations/` changes.

## Auth endpoints (Phase 1)

`POST /auth/register`, `POST /auth/login`, `POST /auth/refresh`,
`POST /auth/logout`, `GET /me`, `PATCH /me` — see `openapi/openapi.yaml`
for exact request/response shapes. Highlights:

- Passwords hashed with argon2id (`github.com/alexedwards/argon2id`).
- Access tokens: JWT, HS256, 15 min, claims include `sub` (user id) and
  `is_site_admin`.
- Refresh tokens: random 32-byte opaque token, stored server-side as a
  SHA-256 hash with `expires_at`/`revoked_at` in `refresh_tokens`, rotated
  on every use (old row revoked, new row issued) — reusing a rotated token
  always fails.
- Web receives the refresh token only via an httpOnly `refresh_token`
  cookie (`SameSite=Strict`, `Secure` when `APP_ENV=production`); mobile
  has no cookie jar and sends/receives it in the JSON body instead.
- `requireAuth` / `requireSiteAdmin` middleware in `internal/httpapi`.
  `requireLeagueMember` / `requireCommissioner` are not built yet — they
  need `league_memberships` and league routes, which land in Phase 2.

## Build / vet / test

```sh
go build ./...
go vet ./...
go test ./...
```

## Layout

```
cmd/server/    HTTP server entrypoint — reads env config, wires deps, calls httpapi.NewRouter
cmd/migrate/   goose migration runner wrapper
internal/
  auth/        password hashing, JWT issuance/verification, refresh-token
               rotation, the Service that register/login/refresh/logout
               and GET/PATCH /me are built on
  db/          sqlc config output (gen/) plus pgtype/UUID + pool helpers
  httpapi/     chi routes, middleware (requireAuth/requireSiteAdmin),
               HTTP handlers, request/response DTOs
  leagues, picks, schedule, grading, notify, admin — empty stubs, filled in
               phase-by-phase per the roadmap
migrations/    goose SQL migrations
openapi/       openapi.yaml, the source-of-truth API spec
sqlc.yaml      sqlc codegen config
```
