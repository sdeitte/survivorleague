# Launch Checklist

Phase 10 ("Launch readiness") runbook. Three of the plan's five Phase 10
items were completed and verified locally against the docker-compose
Postgres + a real running API server — see the bottom of this document for
what was run and where the evidence lives. The other two — **finalized
production secrets** and **DNS cutover** — genuinely require a real
DigitalOcean account, a real domain, and real third-party credentials that
don't exist in a local dev environment. This document is the concrete,
step-by-step runbook for those two, written against the actual env vars
this codebase reads today (re-derived by grepping `api/` for
`os.Getenv`/`os.LookupEnv`, not from memory of what earlier phases
originally planned) and the actual `infra/app.yaml` spec.

Nothing in this document has been executed. No `doctl` command has been
run, no DO resources exist, and there is no git remote to push to.

---

## 1. Production secrets checklist

Do these in order — several later steps depend on values from earlier ones.

### 1.1 `api/` — every env var the server reads

Confirmed via `grep -rn "os.Getenv\|os.LookupEnv" api --include="*.go"`
against the current `api/cmd/server/main.go` and `api/cmd/migrate/main.go`
(this list is the ground truth as of Phase 10 — re-run that grep if this
checklist is used again after a later phase adds new config):

| Var | Required? | Default if unset | Status |
|---|---|---|---|
| `DATABASE_URL` | **Yes** — server fails to boot without it | none (fatal) | Comes from DO Managed Postgres, step 1.4 below |
| `JWT_SECRET` | **Yes** — server fails to boot without it | none (fatal) | **Must be freshly generated** — never the repo's dev value, step 1.2 below |
| `PORT` | No | `8080` | App Platform sets this automatically; `infra/app.yaml` also pins `8080` |
| `APP_ENV` | No | `development` | **Must be `production`** — gates the refresh-token cookie's `Secure` flag (see `api/internal/httpapi/cookie.go`); leaving this at the dev default in prod means the refresh cookie is sent over plain HTTP |
| `ADMIN_EMAIL` | No, but you want it set | none (no auto-bootstrap) | The email that gets `is_site_admin=true` auto-set on matching registration — see step 1.3 |
| `CORS_ALLOWED_ORIGIN` | No, but you need it correct | `http://localhost:5173` | Must be the exact scheme+host of the deployed web app, step 1.6 |
| `CFBD_API_KEY` | No, but sync fails without it | none (sync calls 401) | Flagged open since Phase 3 — step 1.5 |
| `CFBD_BASE_URL` | No | `https://api.collegefootballdata.com` | Leave default in production |
| `RESEND_API_KEY` | No, but email notifications fail without it | none (sends fail, retried, then permanently marked failed) | Flagged open since Phase 7 — step 1.5 |
| `RESEND_FROM_EMAIL` | No | `Survivor League <notifications@survivor-league.example>` | Must be on a domain verified in the Resend account, or email sends will fail even with a valid API key |
| `RESEND_BASE_URL` | No | `https://api.resend.com/emails` | Leave default in production |
| `EXPO_PUSH_BASE_URL` | No | `https://exp.host/--/api/v2/push/send` | Leave default in production |
| `EXPO_ACCESS_TOKEN` | No | none | Optional — Expo's "enhanced security" feature; push delivery works without it |

### 1.2 Generate a real `JWT_SECRET`

The repo's `.env.example` value (`dev-only-insecure-secret-change-me`) is
committed, public, and must never be used outside a local machine. Generate
a long random secret and store it only in DO's encrypted secret store —
never in git:

```bash
openssl rand -base64 48
```

### 1.3 Decide the real `ADMIN_EMAIL`

This is a security-relevant value: whoever registers with this exact email
address (case-insensitive match, see `api/internal/auth/service.go`) gets
`is_site_admin=true` automatically on registration. Set it to the real
address you'll register with, and register that account **immediately**
after the first deploy, before announcing the app to anyone else — the
bootstrap only fires once, on that first matching registration.

### 1.4 Provision DO Managed PostgreSQL

Per `infra/app.yaml`'s trailing comment block, Postgres is provisioned as
a standalone DO Managed Database resource, **not** an App Platform
`databases:` component — this keeps the DB alive independently of app
redeploys/teardowns.

```bash
doctl databases create survivor-league-db --engine pg --version 16 \
  --region nyc --size db-s-1vcpu-1gb
```

Then run the goose migrations in `api/migrations/` against it once
(`go run ./cmd/migrate up` with `DATABASE_URL` pointed at the new
database's connection string — get it via `doctl databases connection
survivor-league-db --format Host,Port,User,Password,Database,SSL`) before
the API's first deploy. The resulting connection string is the
`DATABASE_URL` secret referenced in `infra/app.yaml` as
`${survivor-league-db.DATABASE_URL}`.

### 1.5 Obtain the two flagged-open third-party keys

- **`CFBD_API_KEY`** — register at collegefootballdata.com for an API key.
  (Context: a previous live key was found hardcoded in plaintext in the
  old app's repo — confirmed private on GitHub, not publicly exposed, but
  rotated/abandoned rather than carried forward. Get a fresh key; do not
  reuse anything from the old repo.)
- **`RESEND_API_KEY`** — create a Resend account, verify a sending domain,
  and generate an API key. `RESEND_FROM_EMAIL` must be an address on that
  verified domain or sends will fail even with a valid key.

### 1.6 Set `CORS_ALLOWED_ORIGIN` to the real web origin

Once the web static site has a real DO App Platform URL (or, post-DNS-cutover,
the real custom domain — see Part 2), set `CORS_ALLOWED_ORIGIN` to that
exact `scheme://host` with no trailing slash. Per
`api/internal/httpapi/router.go`, this is a single explicit origin (not a
wildcard) — required because the refresh-token cookie uses
`credentials: 'include'`, which browsers refuse to pair with a wildcard
CORS origin.

### 1.7 Set the secrets in App Platform

Either via the control panel, or `doctl apps update` with a secrets file
(never committed — see `infra/app.yaml`'s own comment on this). Every
`${VAR}`-style reference in `infra/app.yaml` (`JWT_SECRET`, `ADMIN_EMAIL`,
`CFBD_API_KEY`) needs a real value bound at this step, plus
`RESEND_API_KEY`/`RESEND_FROM_EMAIL`/`EXPO_ACCESS_TOKEN` which aren't yet
listed in `infra/app.yaml`'s `envs:` block and should be added there
alongside the existing entries before the first real deploy.

### 1.8 Web/mobile build-time env vars

Confirmed via grepping `web/src` for `import.meta.env.` and `mobile/` for
`process.env.`:

| App | Var | Points at |
|---|---|---|
| `web` | `VITE_API_BASE_URL` | The deployed API's public URL — already wired via `infra/app.yaml`'s `${api.PUBLIC_URL}` binding for the App Platform build, no manual step needed there |
| `mobile` | `EXPO_PUBLIC_API_BASE_URL` | The deployed API's public URL — **must be set manually** before each EAS build (App Platform doesn't build the mobile app), and updated again after DNS cutover (Part 2) with a rebuild + resubmit |

---

## 2. DNS cutover

Do this only after Part 1 is complete and the app has been running
successfully on its DO-issued default URLs (`*.ondigitalocean.app`) for
long enough to trust it.

1. **Buy/have a domain** if one isn't already owned.
2. **Add the domain to DO** (`doctl compute domain create <domain>` or via
   the control panel) so DO manages its DNS records.
3. **Attach a custom domain to the App Platform app** for both components:
   - `web` (static site) → e.g. `survivorleague.example` (or `www.`)
   - `api` (service) → e.g. `api.survivorleague.example`
   DO issues/renews TLS certificates for custom domains automatically —
   no manual cert management.
4. **Update the CNAME/A records** DO's domain-add step tells you to create
   at your registrar (only needed if the domain's authoritative nameservers
   aren't already DO's) — propagation can take up to 24-48h.
5. **Update `CORS_ALLOWED_ORIGIN`** on the `api` service to the new
   `https://survivorleague.example` origin (step 1.6 above) and redeploy.
6. **Update `VITE_API_BASE_URL`** — if the API's custom domain changed
   from its `infra/app.yaml`-bound `${api.PUBLIC_URL}` value, confirm this
   still resolves correctly post-cutover (App Platform should keep this
   binding in sync automatically since it tracks the component, not a
   hardcoded URL — verify rather than assume).
7. **Rebuild and resubmit the mobile app** with `EXPO_PUBLIC_API_BASE_URL`
   pointed at the new custom domain (step 1.8) — this is a real app-store
   release, not a hot-swappable web deploy, so budget real lead time here
   (the plan's Phase 7 already flagged EAS/App Store review lead time as a
   "don't let this be a Phase 10 surprise" risk).
8. **Verify end-to-end** post-cutover: register a throwaway account through
   the real custom domain on both web and mobile, confirm CORS isn't
   rejecting requests (a wrong `CORS_ALLOWED_ORIGIN` fails silently from
   the browser's perspective — check the network tab / server logs, not
   just "does the page look right"), and confirm the refresh-token cookie
   round-trips (it requires HTTPS in production per `APP_ENV=production`
   gating `Secure`, so this specifically exercises something local dev
   over `http://localhost` never tests).

---

## 3. Postgres backup/restore — repeatable runbook

Verified locally (see Part 4 below) using a **throwaway second container**
so the shared dev database was never touched destructively. The exact
commands, reusable as-is against `survivor-league-postgres-1` any time a
backup/restore drill needs re-running, or adaptable to DO Managed
Postgres (see note at the end):

```bash
# 1. Dump the running database (custom format — compressed, supports
#    parallel/selective restore via pg_restore).
docker exec survivor-league-postgres-1 pg_dump -U survivor -d survivor_league \
  -F c -f /tmp/survivor_league_backup.dump
docker cp survivor-league-postgres-1:/tmp/survivor_league_backup.dump ./survivor_league_backup.dump
docker exec survivor-league-postgres-1 rm /tmp/survivor_league_backup.dump

# 2. Stand up a throwaway target (never touches the source container).
docker run -d --name survivor-league-restore-drill \
  -e POSTGRES_USER=survivor -e POSTGRES_PASSWORD=survivor -e POSTGRES_DB=survivor_league \
  -p 5433:5432 postgres:16
# wait for it: docker exec survivor-league-restore-drill pg_isready -U survivor -d survivor_league

# 3. Restore.
docker cp ./survivor_league_backup.dump survivor-league-restore-drill:/tmp/backup.dump
docker exec survivor-league-restore-drill pg_restore -U survivor -d survivor_league \
  --no-owner --no-privileges -v /tmp/backup.dump

# 4. Verify — row counts across every core table must match the source
#    exactly, plus zero orphaned foreign keys and zero duplicate rows
#    against picks' two unique constraints:
docker exec survivor-league-restore-drill psql -U survivor -d survivor_league -c "
SELECT 'users' t, count(*) FROM users
UNION ALL SELECT 'leagues', count(*) FROM leagues
UNION ALL SELECT 'league_memberships', count(*) FROM league_memberships
UNION ALL SELECT 'picks', count(*) FROM picks
UNION ALL SELECT 'teams', count(*) FROM teams
UNION ALL SELECT 'weeks', count(*) FROM weeks
UNION ALL SELECT 'games', count(*) FROM games
ORDER BY 1;"

# 5. The real proof: point the app's own test suite (and/or a live
#    `cmd/server`) at the restored DB and confirm it's functionally
#    correct, not just row-count-matched.
TEST_DATABASE_URL="postgres://survivor:survivor@localhost:5433/survivor_league?sslmode=disable" \
  go test ./...

# 6. Tear down the throwaway container.
docker stop survivor-league-restore-drill && docker rm survivor-league-restore-drill
```

**Mapping to production**: DO Managed Postgres already takes automatic
daily backups with point-in-time recovery built in — you will not run
`pg_dump`/`pg_restore` by hand as the primary backup mechanism in
production. This runbook exists for two other real reasons: (a) proving
the restore *mechanism itself* works and produces a functionally correct
database before you ever need it under pressure, and (b) it's the same
mechanism you'd use for an ad-hoc export before a risky migration, or to
seed a staging environment from a production snapshot. Re-run this drill
against a `doctl databases` connection string periodically (swap the
`docker exec ... pg_dump` source for a direct `pg_dump` against the
managed DB's connection string, and the throwaway target for a local
container as shown above) — don't let "DO handles backups automatically"
become a reason this never gets exercised again after Phase 10.

---

## 4. What was actually verified locally (Phase 10 evidence)

All three of these were run for real against the local docker-compose
Postgres and a real running `go run ./cmd/server` instance — not simulated
or described theoretically. Full results (throughput/latency numbers,
route-by-route security classification, backup/restore verification
output) are in the Phase 10 commit message and PR description; summarized
here as a pointer for whoever picks up Part 1/Part 2 above:

1. **Concurrent-pick load test** — `api/cmd/loadtest/` (a standalone Go
   program, not part of `go test ./...`). Hammers
   `PUT /leagues/:id/weeks/:weekId/picks/me` across many goroutines in
   three scenarios (broad multi-member concurrency, a same-membership race
   on one week's pick, and a team-reuse race across two weeks for one
   membership), then queries Postgres directly afterward to confirm zero
   constraint violations. Re-run with:
   ```bash
   docker-compose up -d
   DATABASE_URL=... JWT_SECRET=... PORT=8090 go run ./cmd/server &
   DATABASE_URL=... BASE_URL=http://localhost:8090 go run ./cmd/loadtest
   ```
   Rerun this (with higher `-members`/`-phase1-rounds`/`-phase1-workers`
   values) against a staging deploy before real launch — the numbers in
   this phase's commit are from a local machine, not DO's actual instance
   size.

2. **Security audit — every admin/commissioner route's middleware** —
   route-by-route classification cross-checked against the plan's API
   Surface section, plus live exploit attempts (cross-league ID
   substitution, JWT forgery/`alg:none`, privilege-tier escalation) run
   against a real server with real tokens for a non-member/non-commissioner/
   non-admin user. Found and fixed one real issue: `POST /auth/refresh`
   didn't check the target user's `status`, so a user disabled via
   `POST /admin/users/:id/disable` could keep minting fresh sessions
   forever from an already-issued refresh token — `Login` already guarded
   against this but `Refresh` didn't. Fixed in
   `api/internal/auth/service.go`, regression test in
   `api/internal/auth/service_test.go`
   (`TestRefresh_DisabledUserCannotMintNewSession`).

3. **Postgres backup/restore drill** — see Part 3 above; this is that
   drill's own writeup.

Re-run item 2's live-exploit pass (or at minimum the structural
`api/internal/httpapi/routes_test.go` route-table tests, which run in CI
on every change) any time a new admin or commissioner route is added.
