# api-client

Placeholder package. This will hold a TypeScript client generated from
`api/openapi/openapi.yaml` (the hand-authored OpenAPI 3.1 source of truth),
consumed identically by `web` and `mobile` — see the "Shared types" note in
the plan's Tech Stack section:
`/Users/sdeitte/.claude/plans/witty-questing-barto.md`.

Empty for now (Phase 0). Generation tooling (e.g. `openapi-typescript` +
a thin fetch wrapper, or `orval`) and the generated output land once the
API surface grows beyond `/health` — see the Phased Build Roadmap in the
plan.
