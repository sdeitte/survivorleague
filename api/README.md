# api

Go module for the Survivor League REST API. Phase 1: auth
(register/login/refresh/logout), `GET`/`PATCH /me`, the
`requireAuth`/`requireSiteAdmin` middleware, sqlc-generated DB access, and
the Phase 0 Postgres-backed `/health` check. Phase 2 added league CRUD,
membership, and the invite-code join flow. Phase 3 added CFBD schedule
ingestion (`internal/schedule`), read-only `/weeks`, `/weeks/:id/games`,
`/games/:id`, `/teams` endpoints, the first `/admin/*` endpoints
(`internal/admin` — triggering/viewing a schedule sync, the first real use
of `requireSiteAdmin`), and a daily cron sync. Phase 4 adds pick
submission/retrieval (`internal/picks`) with server-enforced per-game
locking and the no-repeat-team rule. Phase 5 adds the grading/elimination
pipeline (`internal/grading`) and the adaptive live poll loop
(`internal/livepoll`). Phase 6 adds commissioner buy-back. Phase 7 adds
notifications (`internal/notify`). Phase 8 completes site-admin: see
"Admin endpoints (Phase 8)" below.

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
| `CFBD_API_KEY`         | no*      | unset                    | Bearer token for CollegeFootballData.com. *Required for a real schedule sync to succeed — without it, `POST /admin/sync/schedule` and the daily cron job still run and record a `sync_runs` row, but the CFBD call itself fails with 401. No live key exists in this repo/environment yet (the old hardcoded key was rotated/abandoned, not carried forward) — see api/internal/schedule's doc comments. |
| `CFBD_BASE_URL`        | no       | `https://api.collegefootballdata.com` | Override to point the server at a mock CFBD server (e.g. for local E2E testing without a real API key). |
| `RESEND_API_KEY`       | no*      | unset                    | Bearer token for Resend (transactional email). *Required for real email notifications to succeed — without it, notification_outbox email rows still get claimed/attempted by the dispatcher, they just fail (retried up to the attempt cap, then marked permanently `failed`; push notifications are unaffected). No live key exists in this repo/environment yet — same treatment as `CFBD_API_KEY`. See api/internal/notify's doc comments. |
| `RESEND_FROM_EMAIL`    | no       | `Survivor League <notifications@survivor-league.example>` | Sender address for outgoing email. Resend requires sending from a domain verified on the account. |
| `RESEND_BASE_URL`      | no       | `https://api.resend.com/emails` | Override to point the server at a mock Resend server (e.g. for local E2E testing without a real API key). |
| `EXPO_PUSH_BASE_URL`   | no       | `https://exp.host/--/api/v2/push/send` | Override to point the server at a mock Expo Push server (e.g. for local E2E testing). Unlike Resend, no key is required for real Expo push delivery, so this one has no "unavailable in this environment" caveat — it just can't be verified against a real physical device/APNs/FCM here. |
| `EXPO_ACCESS_TOKEN`    | no       | unset                    | Expo's optional "enhanced security" push feature. Leave unset for normal operation. |

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
`00003_notification_outbox.sql` (Phase 7) adds notification_outbox, the
pending-work queue the dispatcher drains — distinct from
notifications_log, which is the sent/audit record.

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
- `requireAuth` / `requireSiteAdmin` / `requireLeagueMember` /
  `requireCommissioner` middleware in `internal/httpapi`.

## Schedule endpoints (Phase 3)

`GET /weeks?season_year=`, `GET /weeks/:id/games`, `GET /games/:id`,
`GET /teams?conference=` — all `requireAuth`, shared across every league
(not conference-scoped; a league's conference only filters what its own
picks UI shows, in a later phase). Populated by CFBD schedule sync, not
hand-entered.

## Picks endpoints (Phase 4)

`GET`/`PUT /leagues/:id/weeks/:weekId/picks/me`,
`GET /leagues/:id/weeks/:weekId/available-teams`,
`GET /leagues/:id/weeks/:weekId/picks` — all `requireLeagueMember`; `PUT
.../picks/me` additionally requires the requester's own membership to have
`status='active'`. See `openapi/openapi.yaml` for exact request/response
shapes. Highlights:

- **Lock is per-game, checked live against `games.kickoff_at`, not a
  week-level flag.** A pick is frozen the moment the game backing the
  membership's *current* selection for that week has kicked off — even if
  other games that week are already underway. `locked` in every pick
  response is computed at request time, never stored.
- **"Used" only applies to a team currently sitting in a pick row.** Picks
  are upserted on `(league_membership_id, week_id)` (create-or-update, not
  create-a-new-row) — changing your mind before your current pick locks
  frees the abandoned team for a different week immediately.
  `UNIQUE(league_membership_id, team_id)` then does the rest: a team stays
  unavailable for a different week only while some row currently holds it.
- `PUT .../picks/me` validates, in order: game belongs to the week (400);
  team is one of that game's two teams (400); team belongs to the league's
  locked conference (400); the target game (and, if a pick already exists
  for the week, that existing pick's current game) must not have already
  kicked off (409); a team already committed to a different week is caught
  as a clean 409, not a raw DB constraint error.
- `GET .../picks` (all members' status for a week) hides `game_id`/`team_id`
  together (never just one) for any other member's pick whose game hasn't
  kicked off yet — the requester's own entry is always fully visible.
- `internal/picks` implements all of this; `internal/db/queries/picks.sql`
  holds the underlying SQL (no schema migration was needed — Phase 0's
  `picks` table already had everything required).

## Admin endpoints (Phase 3)

`POST /admin/sync/schedule` (body: `{"season_year": 2025}`, required, no
implicit default) and `GET /admin/sync/runs` — both `requireSiteAdmin`. The
sync endpoint runs synchronously, upserts teams/weeks/games via
`internal/schedule`'s `Service.SyncSeason`, and always records a
`sync_runs` row plus (on success) an `audit_log` row
(`action=schedule_sync`). A daily cron job (`robfig/cron/v3`, wired in
`cmd/server/main.go`, 6:00 AM America/New_York) runs the same sync
automatically for "the current season" (see `currentSeasonYear` in
`main.go`) — additive to the manual endpoint, not a replacement, and
cleanly stopped on server shutdown (`SIGINT`/`SIGTERM`).

CFBD's API schema (teams/calendar/games field names, auth via a bearer
token) was confirmed against the live OpenAPI 3.1 spec at
https://api.collegefootballdata.com/api-docs.json — no API key is needed to
fetch the spec itself. `internal/schedule`'s tests mock CFBD entirely via
`httptest.Server` with hand-authored fixture JSON matching that schema; no
live network calls happen in `go test`.

## Admin endpoints (Phase 8)

All `requireSiteAdmin`, all under `/admin`:

- `GET /admin/leagues`, `GET /admin/users` — every league/user in the
  system (unlike every other league/user endpoint, not scoped to the
  requester), paginated via `limit`/`offset` query params (default 25, max
  100 — clamped, not rejected, on out-of-range input).
- `POST /admin/users/:id/disable` / `.../enable` — sets `users.status` to
  `disabled`/`active`. `internal/auth.Service.Login` already rejects any
  non-`active` status, so disabling is what actually blocks a user's next
  login attempt; an already-issued access token stays valid until its own
  15-minute expiry (no access-token blocklist anywhere in this API).
  Rejects with 403 if the target is the acting admin's own account — no
  self-lockout. Both write an `audit_log` row.
- `POST /admin/games/:id/resync` — re-fetches one game from CFBD
  (`internal/schedule`'s `Service.RefreshGame`, reusing the Phase 3
  `CFBDClient` — no second client) and upserts it. This is the unblock
  mechanism for a game `internal/grading` left `postponed`/`canceled`
  (grading deliberately never auto-resolves those — see
  `internal/grading`'s package doc comment). If the resync brings the game
  to `status=final`, this runs the exact same grading pass
  `internal/livepoll`'s poll loop would (`GradeGame`, then
  `TryFinalizeLeagueWeek` for every league with picks that week) and
  reports which league-weeks actually finalized as a result. Always writes
  an `audit_log` row once the game itself has been upserted.
- `GET /admin/audit-log` — paginated, newest first, with optional
  `action`/`actor_user_id` equality filters.

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
  httpapi/     chi routes, middleware (requireAuth/requireSiteAdmin/
               requireLeagueMember/requireCommissioner), HTTP handlers,
               request/response DTOs
  leagues/     league CRUD, membership, invite-code join flow (Phase 2)
  schedule/    CFBD client + idempotent teams/weeks/games sync, canonical
               FBS conference list + CFBD conference-name normalization,
               read access for GET /weeks, /weeks/:id/games, /games/:id,
               /teams (Phase 3)
  admin/       site-admin: schedule-sync trigger + sync_runs bookkeeping
               (Phase 3); cross-league leagues/users listing, user
               disable/enable, single-game CFBD resync, audit log viewer
               (Phase 8)
  picks/       pick submission/retrieval, per-game locking, no-repeat-team
               rule (Phase 4)
  grading/     grade-on-final + weekly elimination/mass-wipeout pipeline (Phase 5)
  livepoll/    adaptive live-score poll loop that drives grading (Phase 5)
  notify/      device tokens/preferences, notification_outbox dispatcher,
               Expo Push + Resend delivery (Phase 7)
migrations/    goose SQL migrations
openapi/       openapi.yaml, the source-of-truth API spec
sqlc.yaml      sqlc codegen config
```
