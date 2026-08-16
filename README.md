# Survivor League

A ground-up rewrite of the College Football Survivor Pool app: a Go REST API
backend serving a React web app and a React Native (Expo) mobile app, with
multi-league support, commissioner-managed invites, and automated weekly
elimination.

This is a from-scratch monorepo — it replaces the old app at
`~/Documents/survivor_league/` (11-repo Java/Spring setup), which is left
untouched as a reference only.

**Source of truth for scope, data model, and rationale:**
`/Users/sdeitte/.claude/plans/witty-questing-barto.md`

Current status: **Phase 0 — scaffolding only.** No feature logic (auth,
leagues, picks, grading) exists yet; see the plan's "Phased Build Roadmap"
section for what's next.

## Repo structure

```
survivor-league/
  api/                    Go module (chi + pgx + goose) — cmd/server, cmd/migrate,
                           internal/{auth,leagues,picks,schedule,grading,notify,
                           admin,httpapi,db}, migrations/, openapi/openapi.yaml
  web/                    Vite + React + TypeScript app
  mobile/                 Expo (managed) React Native + TypeScript app
  packages/api-client/    placeholder for the TS client generated from openapi.yaml
  infra/                  DigitalOcean App Platform spec, CI config
  docker-compose.yml      local Postgres for development
```

## Local development

### 1. Start Postgres

```sh
docker compose up -d
```

This starts `postgres:16` on `localhost:5432` with:

| Setting  | Value             |
|----------|-------------------|
| user     | `survivor`        |
| password | `survivor`        |
| database | `survivor_league` |

Connection string: `postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable`

### 2. Run migrations

```sh
cd api
export DATABASE_URL="postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
go run ./cmd/migrate up
```

### 3. Run the API

```sh
cd api
export DATABASE_URL="postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
go run ./cmd/server
# GET http://localhost:8080/health -> {"status":"ok","db":"ok"}
```

See `api/README.md` for env vars, build/vet, and migration details.

### 4. Run the web app

```sh
cd web
npm install
npm run dev
# open http://localhost:5173 — it fetches /health from the API and shows the result
```

### 5. Run the mobile app

```sh
cd mobile
npm install
npm run start
# press i / a / w, or scan the QR with Expo Go
```

See `mobile/README.md` for pointing the app at a non-default API host (e.g.
testing on a physical device).

## CI

`.github/workflows/ci.yml` runs `go build`/`go vet` for `api/` and
`npm ci && npm run build` for `web/` on every push/PR. No deploy step yet.

## Deployment

`infra/app.yaml` is a DigitalOcean App Platform spec (config only — nothing
has been deployed). See the plan's "Hosting" section for the target
architecture (App Platform Service + Static Site + separate DO Managed
PostgreSQL).
