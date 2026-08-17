-- name: CreateAuditLog :one
-- Every commissioner/admin privileged action writes a row here per the
-- plan's Data Model section. league_id/target_type/target_id are nullable
-- (e.g. a schedule_sync action has no league scope).
INSERT INTO audit_log (actor_user_id, league_id, action, target_type, target_id, metadata)
VALUES (sqlc.arg(actor_user_id), sqlc.arg(league_id), sqlc.arg(action), sqlc.arg(target_type), sqlc.arg(target_id), sqlc.arg(metadata))
RETURNING *;

-- name: ListAuditLog :many
-- Backs GET /admin/audit-log (Phase 8, requireSiteAdmin). Newest first,
-- with optional equality filters on action/actor_user_id — both narg so a
-- NULL means "don't filter on this field" (mirrors ListTeams' conference
-- narg pattern in teams.sql).
SELECT * FROM audit_log
WHERE (sqlc.narg(action)::text IS NULL OR action = sqlc.narg(action))
  AND (sqlc.narg(actor_user_id)::uuid IS NULL OR actor_user_id = sqlc.narg(actor_user_id))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountAuditLog :one
SELECT count(*) FROM audit_log
WHERE (sqlc.narg(action)::text IS NULL OR action = sqlc.narg(action))
  AND (sqlc.narg(actor_user_id)::uuid IS NULL OR actor_user_id = sqlc.narg(actor_user_id));
