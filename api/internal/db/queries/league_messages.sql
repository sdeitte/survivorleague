-- name: InsertLeagueMessage :one
-- Returns the same shape ListRecentLeagueMessages does (display_name
-- joined in via the same WITH-then-join, not a second round trip) so the
-- POST handler's response and the GET list's rows are one response type,
-- not two.
WITH inserted AS (
    INSERT INTO league_messages (league_id, user_id, body)
    VALUES (sqlc.arg(league_id), sqlc.arg(user_id), sqlc.arg(body))
    RETURNING *
)
SELECT
    inserted.id,
    inserted.league_id,
    inserted.user_id,
    u.display_name,
    lm.team_name,
    inserted.body,
    inserted.created_at
FROM inserted
JOIN users u ON u.id = inserted.user_id
JOIN league_memberships lm ON lm.league_id = inserted.league_id AND lm.user_id = inserted.user_id;

-- name: ListRecentLeagueMessages :many
-- Every message in the league newer than `since` (internal/chat.Service
-- computes this as now()-7days — the TTL is entirely a read-time filter,
-- see 00006_league_chat.sql), joined with the sender's display_name/
-- team_name so the chat UI doesn't need N+1 lookups. Oldest first (chat
-- reads top-to-bottom), unlike most of this codebase's other list
-- queries. team_name comes from league_memberships, not league_messages
-- itself (a chat message has no membership_id column, only league_id +
-- user_id — see InsertLeagueMessage's identical join, and 00006's
-- original design choice), joined on the UNIQUE(league_id, user_id) pair.
SELECT
    m.id,
    m.league_id,
    m.user_id,
    u.display_name,
    lm.team_name,
    m.body,
    m.created_at
FROM league_messages m
JOIN users u ON u.id = m.user_id
JOIN league_memberships lm ON lm.league_id = m.league_id AND lm.user_id = m.user_id
WHERE m.league_id = sqlc.arg(league_id) AND m.created_at > sqlc.arg(since)
ORDER BY m.created_at ASC;

-- name: DeleteLeagueMessage :execrows
-- Scoped by league_id (not just id) so a commissioner can never delete a
-- message belonging to a different league by guessing/reusing an id —
-- the handler treats 0 rows affected as "not found" (wrong id or wrong
-- league), same convention as every other scoped-delete in this codebase.
DELETE FROM league_messages WHERE id = sqlc.arg(id) AND league_id = sqlc.arg(league_id);
