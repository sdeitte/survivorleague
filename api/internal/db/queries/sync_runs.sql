-- name: CreateSyncRun :one
-- Started at insert time (status='running'); FinishSyncRun closes it out.
-- Two-phase rather than a single post-hoc insert so a crash mid-sync
-- leaves a visibly stuck 'running' row in the admin history rather than no
-- record at all. `details` carries trigger/triggered_by/season_year up
-- front (and gains counts on FinishSyncRun) — sync_runs has no dedicated
-- columns for those per the Phase 0 schema, and the JSONB `details` column
-- was clearly designed for exactly this kind of run metadata.
INSERT INTO sync_runs (kind, status, details)
VALUES (sqlc.arg(kind), 'running', sqlc.arg(details))
RETURNING *;

-- name: FinishSyncRun :one
UPDATE sync_runs
SET status = sqlc.arg(status),
    finished_at = now(),
    error = sqlc.arg(error),
    details = sqlc.arg(details)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListSyncRuns :many
SELECT * FROM sync_runs ORDER BY started_at DESC LIMIT sqlc.arg(row_limit);
