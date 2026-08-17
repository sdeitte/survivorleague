-- name: CreateAuditLog :one
-- Every commissioner/admin privileged action writes a row here per the
-- plan's Data Model section. league_id/target_type/target_id are nullable
-- (e.g. a schedule_sync action has no league scope).
INSERT INTO audit_log (actor_user_id, league_id, action, target_type, target_id, metadata)
VALUES (sqlc.arg(actor_user_id), sqlc.arg(league_id), sqlc.arg(action), sqlc.arg(target_type), sqlc.arg(target_id), sqlc.arg(metadata))
RETURNING *;
